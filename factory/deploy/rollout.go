package deploy

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
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
	Address string
	Target  targetseam.Target
	// ReleaseInstances is how many instances of this deploy's own build run
	// here, and ControlInstances how many the control runs, which is nothing on
	// every target but the one the control runs on.
	ReleaseInstances int
	ControlInstances int
	// KeptInstances is the capacity the release being replaced had, times the
	// fraction its owner authored.
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
	// ControlReleaseID is the release the control runs, under a strategy with
	// one: the newest release below this one whose window closed without failing
	// it, which is the release a rollback of this deploy would return to. A
	// control is defined by which release it runs, so a deploy with a control and
	// no release here is refused at the start.
	ControlReleaseID string
	// DeliveredReleaseIDs is a revert's deploy listing the releases it delivers.
	DeliveredReleaseIDs []string
	// Backfill is what a backfill item's release copies between, and is empty on
	// every other deploy. The record marks the backfill complete by being marked
	// complete.
	Backfill Backfill
	// UndoneDeployIDs are the deploys this deploy undoes — a rollback's, being
	// the failed release's own and those of every release it skipped — each
	// advanced to rolled back target by target as this deploy completes on each.
	// It is empty on every deploy that undoes nothing.
	UndoneDeployIDs []string

	// Credential is the environment record's, resolved on the far side of the
	// seam and never here.
	Credential secretref.Ref
	// Configuration is the resolved value set the build runs under. The digest
	// over it goes on the record, and a rollback restores the version so named.
	Configuration targetseam.ValueSet
	// SchemaChanges are the changes the build declares, in the order they apply.
	// The deployer applies the ones the store's history lacks, before the build
	// takes traffic, and takes a snapshot before any that destroys stored data.
	// The record names every one of them, a revert's deploy being the one deploy
	// that carries more than one.
	SchemaChanges []targetseam.SchemaChange
	// Adoption is whether this is the deploy of the adoption item's release. An
	// adopted service's store arrives at the schema its head declares, so this
	// deploy writes one row per declared change into the store's schema history,
	// naming this release and marked as found applied, and applies none of them.
	// The next release's deploy then applies exactly what its build declares that
	// the history does not hold, as any deploy does.
	Adoption bool
	// SnapshotName is what a snapshot taken before a destructive change is
	// called, and is required where one of the changes destroys stored data.
	SnapshotName string

	// Reaches are the targets of the environment the service runs on, in that
	// set's order. It is the service's set and not the environment's whole list:
	// completion per target and the rollout's order are both over the targets
	// the service record says it runs on, and the caller reads that field.
	Reaches []Reach
	// Bake is the hold between one target and the next, and may be nil, which is
	// no hold.
	Bake Bake
	// BakeVolume is the traffic the targets already reached serve before the
	// next is reached. It is a field on the service record beside the window
	// limit, read from there by the caller and passed here, and where an owner
	// authored none it is what the score supplies.
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
			// What goes on the record is what the seam reported: a platform that
			// ends instances outright reports a cut, and a record naming a drain
			// there would assert a drain nothing performed.
			ended, err := reach.Target.Stop(ctx, p.Principal, p.ServiceName, p.Credential)
			if err != nil {
				return d, refused(ctx, w, p, d, n, reach, err)
			}
			if err := w.CompleteTarget(ctx, d.ID, reach.Address, ended.Replacement); err != nil {
				return d, err
			}
			if err := undoTarget(ctx, w, p, d, reach.Address); err != nil {
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

		// The strategy performed is written once something has been performed
		// and never at the start: on the row with a control it is what the shift
		// returned, and on the row without one it is these instances replaced
		// with none of the build they replace left running.
		if p.IntoProduction {
			performed, err := performed(ctx, w, p, d, reach)
			if err != nil {
				return d, err
			}
			d.StrategyPerformed = performed
		}

		// Each deploy this one undoes is advanced on this target as this deploy
		// completes on it, so a rollback that stopped undoes nothing on the
		// record beyond the targets it reached.
		if err := undoTarget(ctx, w, p, d, reach.Address); err != nil {
			return d, err
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
	control, controlRelease := "", ""
	for _, reach := range p.Reaches {
		targets = append(targets, Reaching{
			Address:          reach.Address,
			ReleaseInstances: reach.ReleaseInstances,
			ControlInstances: reach.ControlInstances,
			KeptInstances:    reach.KeptInstances,
		})
	}
	if p.IntoProduction && p.StrategyPicked == StrategyWithControl && len(p.Reaches) > 0 {
		// A control runs the release a rollback would return to, beside the
		// release itself, and it starts on the first target the rollout reaches.
		control, controlRelease = p.Reaches[0].Address, p.ControlReleaseID
	}
	return Beginning{
		ServiceID:           p.ServiceID,
		EnvironmentID:       p.EnvironmentID,
		What:                p.What,
		Targets:             targets,
		IntoProduction:      p.IntoProduction,
		StrategyPicked:      p.StrategyPicked,
		DeliveredReleaseIDs: p.DeliveredReleaseIDs,
		SchemaChanges:       p.schemaChanges(),
		Backfill:            p.Backfill,
		ConfigurationDigest: DigestConfiguration(p.Configuration),
		WayInTokenDigest:    wayInDigest,
		ControlTarget:       control,
		ControlReleaseID:    controlRelease,
	}
}

// schemaChanges is what the record names as the changes this deploy carries:
// every change the build declares, in the order they apply. A revert's deploy is
// the one deploy that carries more than one, delivering releases that never
// deployed on their own, and a record naming one of several would report a
// deploy that did less than it did.
func (p Performance) schemaChanges() []string {
	named := make([]string, 0, len(p.SchemaChanges))
	for _, change := range p.SchemaChanges {
		named = append(named, change.Change)
	}
	return named
}

// performed is what the deployer performed on one target, written when it has
// performed it. On the row without a control the instances have just been
// replaced with none of the build they replace left running, which is that row
// performed. On the row with one it is the control's own share, asked of a
// target the environment declared as serving one: a target that refuses it — or
// one that was never declared as serving a share — is the deployer performing
// the row without a control on this deploy and writing so, and a rollout that
// ran no comparison is on the record as one.
func performed(ctx context.Context, w *Writer, p Performance, d Deploy, reach Reach) (Strategy, error) {
	if p.StrategyPicked != StrategyWithControl {
		return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
	}
	if !reach.ServesAShare {
		return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
	}
	err := reach.Target.ShiftTraffic(ctx, p.Principal, targetseam.Shift{
		Service: p.ServiceName, Build: p.What.BuildID, Share: reach.Share, Credential: p.Credential,
	})
	if err != nil {
		return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
	}
	if d.StrategyPerformed == StrategyWithoutControl {
		// An earlier target refused the shift, so the deploy as a whole ran
		// without a control whatever this one did.
		return StrategyWithoutControl, nil
	}
	return StrategyWithControl, w.PerformedWithControl(ctx, d.ID)
}

// undoTarget advances every deploy this one undoes on the target this one has
// just completed on, which is what the design means by written target by target
// as the record of the rollback that undid it completes on each. A deploy with
// no row for that address never reached it, and there is nothing there to undo.
func undoTarget(ctx context.Context, w *Writer, p Performance, d Deploy, address string) error {
	for _, undone := range p.UndoneDeployIDs {
		if undone == d.ID {
			continue
		}
		err := w.UndoTarget(ctx, undone, address)
		if err != nil && !errors.Is(err, ErrTargetNotFound) {
			return err
		}
	}
	return nil
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
