package deploy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Reach is one target of the environment as the deployer reaches it: the
// address, the target itself, how many instances of the release a rollback
// would return to are kept there, and whether the platform behind it serves a
// share. The slice is in the environment's order, which is the order a rollout
// reaches them in.
type Reach struct {
	Address       string
	Target        targetseam.Target
	KeptInstances int
	// ServesAShare is what the environment record declares per target. A target
	// declared as serving one that then refuses the shift is what makes the
	// strategy performed differ from the one picked.
	ServesAShare bool
	// Share is what a control's schedule asks this target to give the release at
	// the start of the rollout, under a strategy with a control.
	Share float64
}

// Bake is the hold between one target and the next: the traffic the targets
// already reached have served, and whether the window's cap has run. The
// deployer holds until the first reaches the volume or the second is true —
// once the cap has run the window closes timed out and the remaining targets are
// reached with no hold between them, since a quiet service that never serves the
// bake volume would otherwise never complete a deploy.
//
// The health monitor is what can answer both, reading the emission and the
// window it opened at this deploy. Nothing implements this yet; a rollout given
// none holds nowhere, and doc.go says so.
type Bake interface {
	// Served is how much the targets reached so far have served since this
	// deploy began, and whether the window's cap has run.
	Served(ctx context.Context, deployID string) (volume int64, capRun bool, err error)
}

// Notifier is what the deployer pages through at the two exits that page: a
// snapshot it could not take and verify, and an artifact digest that differs at
// a rollback. Both meet the page condition word for word — production is running
// a release the factory has just failed, or is about to lose data it cannot put
// back, and nothing the factory has will improve it.
//
// The caller implements it: the notifier is a component of its own and this
// package imports nothing that routes anything.
type Notifier interface {
	Page(ctx context.Context, serviceID, reason string) error
}

var (
	// ErrSnapshotRefused is returned where the snapshot before a change that
	// destroys stored data could not be taken and verified. The record is marked
	// failed at that step and a page fires: a snapshot the deployer cannot take
	// and verify is a deploy not performed.
	ErrSnapshotRefused = errors.New("deploy: the snapshot before a destructive change could not be taken and verified")
	// ErrSchemaChangeRefused is returned where the build's schema change failed
	// to apply. No target is marked complete and the previous release stays
	// current.
	ErrSchemaChangeRefused = errors.New("deploy: the build's schema change did not apply")
	// ErrTargetRefused is returned where a target refused what it was asked. The
	// record stays started with the targets it reached marked complete, which is
	// a recorded partial deploy, unless nothing completed at all — then it is
	// marked failed at the first target.
	ErrTargetRefused = errors.New("deploy: a target refused the deploy")
)

// Performance is one deploy performed: what to write on the record, what to put
// on the targets, and what the strategy asks for between them.
type Performance struct {
	// Actor is who the record names, which is the deployer and never an agent:
	// deploying is not a stage an agent is dispatched to.
	Actor record.Actor
	// Principal is who the calls at seam 4 are made as, which is the deployer
	// calling as itself.
	Principal principal.Principal

	ServiceID string
	// ServiceName is what the target acts on, where ServiceID is what the record
	// stores.
	ServiceName   string
	EnvironmentID string
	What          What
	// IntoProduction is whether this is the production environment, which is the
	// only place a strategy attaches.
	IntoProduction bool
	StrategyPicked Strategy
	// DeliveredReleaseIDs is a revert's deploy listing the releases it delivers.
	DeliveredReleaseIDs []string

	// Credential is the environment record's, resolved on the far side of the
	// seam and never here.
	Credential secretref.Ref
	// Configuration is the resolved value set the build runs under. The digest
	// over it goes on the record, and a rollback restores the version so named.
	Configuration targetseam.ValueSet
	// SchemaChanges are the changes the build declares, in the order they apply.
	// The deployer applies the ones the store's history lacks, before the build
	// takes traffic, and takes a snapshot before any that destroys stored data.
	SchemaChanges []targetseam.SchemaChange
	// SnapshotName is what a snapshot taken before a destructive change is
	// called, and is required where one of the changes destroys stored data.
	SnapshotName string

	// Reaches are the environment's targets in the environment's order.
	Reaches []Reach
	// Bake is the hold between one target and the next, and may be nil, which is
	// no hold.
	Bake Bake
	// BakeVolume is the traffic the targets already reached serve before the
	// next is reached. It is a field on the service record in the design; that
	// record does not carry it yet, so the caller supplies it and doc.go says
	// which caller is not built.
	BakeVolume int64
	// BakePoll is how often the hold asks. A zero value is [DefaultBakePoll].
	BakePoll time.Duration

	// Notifier is what the deployer pages through, and may be nil, which pages
	// nowhere.
	Notifier Notifier
}

// DefaultBakePoll is how often a rollout asks whether the targets already
// reached have served the bake volume. A volume is not a period, so the hold
// cannot be a sleep of a known length: it is a read repeated until the answer
// changes.
const DefaultBakePoll = time.Second

// Perform is one deploy from its first step to its last: the record written,
// the store's changes applied before any traffic moves, and then the targets
// reached in the environment's order, one at a time, each marked complete before
// the next is reached and the bake volume served between them.
//
// The order is what bounds how much of production a bad release reaches: the
// first target and no more until that target has been read. On the row without a
// control nothing inside a target limits anything, so there the order and the
// bake volume are the only limit the factory has.
//
// Every deploy the deployer begins has a record from its first step, the steps
// before traffic included. Where the deployer stops before any target is
// complete, the record is marked failed naming the step; where it stops after
// one is, the record stays started with the targets it reached marked complete,
// which is a recorded partial deploy and what the restart reads.
func Perform(ctx context.Context, w *Writer, p Performance) (Deploy, error) {
	token, digest, err := mintWayInToken()
	if err != nil {
		return Deploy{}, err
	}

	d, err := w.Start(ctx, p.Actor, p.beginning(digest))
	if err != nil {
		return Deploy{}, err
	}
	return perform(ctx, w, p, d, token)
}

// perform is the whole of a deploy after its record exists, which is what
// [Perform] and [Restore] share: they differ in what the record names and in
// what is verified before it, and not in how a deploy is carried out.
func perform(ctx context.Context, w *Writer, p Performance, d Deploy, wayInToken string) (Deploy, error) {
	if err := applyToTheStore(ctx, w, p, d); err != nil {
		return d, err
	}

	deployment := targetseam.Deployment{
		Service:       p.ServiceName,
		Build:         p.What.BuildID,
		Credential:    p.Credential,
		Configuration: p.Configuration,
		WayInToken:    wayInToken,
		DeployID:      d.ID,
	}

	for n, reach := range p.Reaches {
		if n > 0 {
			if err := hold(ctx, p, d.ID); err != nil {
				return d, err
			}
		}

		// The row for the target is written before the call and marked complete
		// after, both carrying the fencing token: a stalled deployer's claim is
		// refused, so it makes no call, and one that lapsed mid-call completes
		// nothing.
		if err := w.ReachTarget(ctx, d.ID, reach.Address); err != nil {
			return d, err
		}

		if p.What.Removal() {
			if err := reach.Target.Stop(ctx, p.Principal, p.ServiceName, p.Credential); err != nil {
				return d, refused(ctx, w, p, d, n, reach, err)
			}
			if err := w.CompleteTarget(ctx, d.ID, reach.Address, targetseam.ReplacementDrained); err != nil {
				return d, err
			}
			continue
		}

		placed, err := reach.Target.Deploy(ctx, p.Principal, deployment)
		if err != nil {
			return d, refused(ctx, w, p, d, n, reach, err)
		}
		if err := w.CompleteTarget(ctx, d.ID, reach.Address, placed.Replacement); err != nil {
			return d, err
		}

		if p.StrategyPicked == StrategyWithControl && d.StrategyPerformed == StrategyWithControl {
			shifted, err := shift(ctx, w, p, d, reach)
			if err != nil {
				return d, err
			}
			d.StrategyPerformed = shifted
		}
	}

	if err := w.Complete(ctx, d.ID); err != nil {
		return d, err
	}
	d.Status = StatusComplete
	return d, nil
}

// beginning is what [Writer.Start] is given, assembled from the performance so
// that the record's fields and the calls that follow cannot disagree about what
// this deploy is.
func (p Performance) beginning(wayInDigest string) Beginning {
	targets := make([]Reaching, 0, len(p.Reaches))
	control := ""
	for _, reach := range p.Reaches {
		targets = append(targets, Reaching{Address: reach.Address, KeptInstances: reach.KeptInstances})
	}
	if p.IntoProduction && p.StrategyPicked == StrategyWithControl && len(p.Reaches) > 0 {
		// A control runs the release a rollback would return to, beside the
		// release itself, and it starts on the first target the rollout reaches.
		control = p.Reaches[0].Address
	}
	return Beginning{
		ServiceID:           p.ServiceID,
		EnvironmentID:       p.EnvironmentID,
		What:                p.What,
		Targets:             targets,
		IntoProduction:      p.IntoProduction,
		StrategyPicked:      p.StrategyPicked,
		DeliveredReleaseIDs: p.DeliveredReleaseIDs,
		SchemaChange:        p.schemaChange(),
		ConfigurationDigest: DigestConfiguration(p.Configuration),
		WayInTokenDigest:    wayInDigest,
		ControlTarget:       control,
	}
}

// schemaChange is what the record names as the change this deploy carries: the
// one the build declares, or the last of them on a revert's deploy, which is the
// one deploy that can carry more than one.
func (p Performance) schemaChange() string {
	if len(p.SchemaChanges) == 0 {
		return ""
	}
	return p.SchemaChanges[len(p.SchemaChanges)-1].Change
}

// applyToTheStore is every step before traffic: the snapshot before a change
// that destroys stored data, and the changes the store's history lacks, applied
// in order through the environment's credential. The store is one per service
// per environment, so the changes are applied once — through the first target,
// every target of the environment holding the same credential and reaching the
// same store.
func applyToTheStore(ctx context.Context, w *Writer, p Performance, d Deploy) error {
	if len(p.SchemaChanges) == 0 || len(p.Reaches) == 0 {
		return nil
	}
	through := p.Reaches[0]

	running, err := through.Target.ReadRunning(ctx, p.Principal, p.ServiceName, p.Credential)
	if err != nil {
		return fail(ctx, w, p, d, StepSchemaChange, fmt.Errorf("%w: reading the schema history: %w",
			ErrSchemaChangeRefused, err))
	}
	carried := make([]string, 0, len(running.SchemaHistory))
	for _, applied := range running.SchemaHistory {
		carried = append(carried, applied.Change)
	}

	var owed []targetseam.SchemaChange
	destructive := false
	for _, change := range p.SchemaChanges {
		if slices.Contains(carried, change.Change) {
			continue
		}
		owed = append(owed, change)
		destructive = destructive || change.Destroys
	}
	if len(owed) == 0 {
		return nil
	}

	if destructive {
		if p.SnapshotName == "" {
			return fail(ctx, w, p, d, StepSnapshot, fmt.Errorf("%w: it names no copy", ErrSnapshotRefused))
		}
		taken, err := through.Target.Snapshot(ctx, p.Principal, targetseam.SnapshotRequest{
			Service: p.ServiceName, Name: p.SnapshotName, Credential: p.Credential,
		})
		if err != nil {
			return fail(ctx, w, p, d, StepSnapshot, fmt.Errorf("%w: %w", ErrSnapshotRefused, err))
		}
		if err := w.NameSnapshot(ctx, d.ID, taken.Name, taken.Digest); err != nil {
			return err
		}
	}

	for _, change := range owed {
		if err := through.Target.ApplySchemaChange(ctx, p.Principal, change); err != nil {
			return fail(ctx, w, p, d, StepSchemaChange, fmt.Errorf("%w: %s: %w",
				ErrSchemaChangeRefused, change.Change, err))
		}
	}
	return w.MarkSchemaChangeComplete(ctx, d.ID)
}

// shift is the control's own share, asked of a target the environment declared
// as serving one. A target that refuses it is the deployer performing the row
// without a control on this deploy and writing so, and a rollout that ran no
// comparison is on the record as one.
func shift(ctx context.Context, w *Writer, p Performance, d Deploy, reach Reach) (Strategy, error) {
	if !reach.ServesAShare {
		return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
	}
	err := reach.Target.ShiftTraffic(ctx, p.Principal, targetseam.Shift{
		Service: p.ServiceName, Build: p.What.BuildID, Share: reach.Share, Credential: p.Credential,
	})
	if err == nil {
		return StrategyWithControl, nil
	}
	return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
}

// hold is the bake volume between one target and the next. It returns as soon as
// the targets already reached have served the volume, or as soon as the window's
// cap has run, whichever comes first — and at once where the caller supplied no
// way to ask.
func hold(ctx context.Context, p Performance, deployID string) error {
	if p.Bake == nil || p.BakeVolume <= 0 {
		return nil
	}
	poll := p.BakePoll
	if poll <= 0 {
		poll = DefaultBakePoll
	}
	for {
		served, capRun, err := p.Bake.Served(ctx, deployID)
		if err != nil {
			return fmt.Errorf("deploy: reading what %s has served: %w", deployID, err)
		}
		if capRun || served >= p.BakeVolume {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(poll):
		}
	}
}

// refused is what a target error leaves. With no target complete behind it the
// record is marked failed at the first target; with one complete it stays
// started, a recorded partial deploy the restart reads and the drift detector
// checks the targets of.
func refused(ctx context.Context, w *Writer, p Performance, d Deploy, n int, reach Reach, cause error) error {
	wrapped := fmt.Errorf("%w: %s of %s: %w", ErrTargetRefused, reach.Address, d.ID, cause)
	if n > 0 {
		return wrapped
	}
	return fail(ctx, w, p, d, StepFirstTarget, wrapped)
}

// fail marks the record failed at the step that stopped it and pages where the
// step is one of the two that page. The caller's error is returned whatever the
// write does, the deploy having stopped either way.
func fail(ctx context.Context, w *Writer, p Performance, d Deploy, step string, cause error) error {
	if err := w.MarkFailed(ctx, d.ID, step); err != nil {
		return fmt.Errorf("%w (and marking it failed: %v)", cause, err)
	}
	if p.Notifier != nil && (step == StepSnapshot || step == StepArtifactDigest) {
		if err := p.Notifier.Page(ctx, p.ServiceID, step+": "+cause.Error()); err != nil {
			return fmt.Errorf("%w (and paging: %v)", cause, err)
		}
	}
	return cause
}

// DigestConfiguration is the digest over a resolved value set: each name and
// each value in the order the caller assembled them, separated so that two sets
// differing only in where one value ends do not digest the same. It is what goes
// on the deploy record beside the build's digest, and what a rollback restores
// the configuration version by.
func DigestConfiguration(values targetseam.ValueSet) string {
	if len(values.Names) == 0 {
		return ""
	}
	sum := sha256.New()
	for n, name := range values.Names {
		sum.Write([]byte(name))
		sum.Write([]byte{0})
		if n < len(values.Values) {
			sum.Write([]byte(values.Values[n]))
		}
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil))
}

// mintWayInToken is the token the deployer mints for the way in at every deploy
// and the digest it writes on the record. The token is handed to the service in
// its configuration and stored nowhere; the digest is what the report store
// would find the deploy record by. Neither the way in nor the report store is
// built, so nothing reads either yet.
func mintWayInToken() (token, digest string, err error) {
	var bytes [32]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", "", fmt.Errorf("deploy: minting the way-in token: %w", err)
	}
	token = hex.EncodeToString(bytes[:])
	sum := sha256.Sum256([]byte(token))
	return token, hex.EncodeToString(sum[:]), nil
}
