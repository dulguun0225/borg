package deploy

import (
	"context"
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
	// here, and ControlInstances how many the control on this target runs —
	// there is one control per production target the release has reached,
	// started on that target when the rollout reaches it, and the deploy
	// record names each.
	ReleaseInstances int
	ControlInstances int
	// KeptInstances is the instances the build being replaced had, or the
	// fraction of them an owner authored, kept here until the last window that
	// could return to it closes: a rollback returns production to them, and a
	// share is not a capacity.
	KeptInstances int
	// ServesAShare is what the environment record declares per target. Where a
	// service runs on a target whose platform serves no share, the row with a
	// control is unavailable there, permanently rather than once — every deploy
	// on that target is performed without one and the record says so, and
	// nothing here is refused; a target declared as serving a share that then
	// refuses the shift is what makes the strategy performed differ from the
	// one picked in the same way.
	ServesAShare bool
	// Share is what a control's schedule asks this target to give the release at
	// the start of the rollout, under a strategy with a control.
	Share float64
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
	// ErrTargetNotOfTheEnvironment is returned where a target the deploy would
	// reach is no target of the environment. The record holds a row beside each
	// of the environment's targets, so a reach outside that list is a call with
	// no row to mark.
	ErrTargetNotOfTheEnvironment = errors.New("deploy: the deploy reaches a target the environment does not name")
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
	// control is defined by which release it runs, and a deploy naming none here
	// runs no control — a service's first release, which goes without one
	// whatever the score picked.
	ControlReleaseID string
	// ControlBuildID is the build that release's control runs, which the record
	// names beside the release and the instances running it.
	ControlBuildID string
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

	// WayInAddress is the entrance the way in inside the deployed service
	// presents its token at, handed across the seam beside the token. It is
	// the caller's, this package minting the token and knowing no address,
	// and it is empty where the factory serves no entrance.
	WayInAddress string

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
	// set's order, which is the order the deployer reaches them in. It is the
	// service's set and not the environment's whole list: the rollout's order
	// and the release becoming current are both over the targets the service
	// record says it runs on, and the caller reads that field.
	Reaches []Reach
	// EnvironmentTargets is every target the environment names, in the
	// environment's order. The record holds a row beside each of them saying
	// whether that target has this release yet, and the rows for the targets
	// the service does not run on stay not reached. Where the caller supplies
	// none it is [Performance.Reaches], the two being the same list on an
	// environment every target of which the service runs on.
	EnvironmentTargets []string
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
	// The configuration digest is taken before the token is minted and
	// appended: it is over the resolved set alone, and the token's own digest
	// has its own field.
	configDigest := DigestConfiguration(p.Configuration)
	p, wayInDigest, err := p.mintingTheWayInToken()
	if err != nil {
		return Deploy{}, err
	}
	if err := p.check(); err != nil {
		return Deploy{}, err
	}

	d, err := w.Start(ctx, p.Actor, p.beginning(configDigest, wayInDigest))
	if err != nil {
		return Deploy{}, err
	}
	return perform(ctx, w, p, d)
}

// check is what a deploy is refused for before any record is written: a target
// no row would be written for. A row with a control the deploy cannot run is
// not one of them — a service's first release and a service on a platform that
// serves no share both go without a control, performed and written so rather
// than refused, which [performed] is.
func (p Performance) check() error {
	if len(p.EnvironmentTargets) == 0 {
		return nil
	}
	named := make(map[string]bool, len(p.EnvironmentTargets))
	for _, address := range p.EnvironmentTargets {
		named[address] = true
	}
	for _, reach := range p.Reaches {
		if !named[reach.Address] {
			return fmt.Errorf("%w: %s", ErrTargetNotOfTheEnvironment, reach.Address)
		}
	}
	return nil
}

// perform is the whole of a deploy after its record exists, which is what
// [Perform] and [Restore] share, and what [Resume] calls again on a record it
// stopped in the middle of: they differ in what the record names and in what
// is verified before it, and not in how a deploy is carried out. A target
// already marked complete on the record is skipped rather than reached again,
// which is what makes calling this a second time over a record [Resume] is
// finishing safe — the walk picks up where the record's own rows say it
// stopped, and nothing already placed is placed twice.
func perform(ctx context.Context, w *Writer, p Performance, d Deploy) (Deploy, error) {
	if err := applyToTheStore(ctx, w, p, d); err != nil {
		return d, err
	}

	already, err := completeAddresses(ctx, w, d.ID)
	if err != nil {
		return d, err
	}

	deployment := targetseam.Deployment{
		Service:       p.ServiceName,
		Build:         p.What.BuildID,
		Credential:    p.Credential,
		Configuration: addingTheDeployID(p.Configuration, d.ID),
		WayInAddress:  p.WayInAddress,
	}

	for n, reach := range p.Reaches {
		if already[reach.Address] {
			continue
		}
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
			// What goes on the record is what the seam reported: the one outcome
			// the operation may report is a drain, no request dropped, and a
			// platform unable to keep that promise refuses instead — which
			// [refused] below turns into a target refused rather than a record
			// naming a replacement that did not happen.
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
// this deploy is. configDigest is [DigestConfiguration] of the resolved set
// before the way-in token was appended to it, and wayInDigest is the digest of
// the token itself — the two callers take separately, so a token minted fresh
// at every deploy never moves the first.
func (p Performance) beginning(configDigest, wayInDigest string) Beginning {
	runsOn := make(map[string]Reach, len(p.Reaches))
	addresses := make([]string, 0, len(p.Reaches))
	for _, reach := range p.Reaches {
		runsOn[reach.Address] = reach
		addresses = append(addresses, reach.Address)
	}
	if len(p.EnvironmentTargets) > 0 {
		addresses = p.EnvironmentTargets
	}

	targets := make([]Reaching, 0, len(addresses))
	for _, address := range addresses {
		reach, runs := runsOn[address]
		// The control is no part of what is written at the start: there is one
		// per production target the release has reached, started on that target
		// when the rollout reaches it, and [Writer.ControlStarted] is what names
		// it there.
		targets = append(targets, Reaching{
			Address:          address,
			NotRunHere:       !runs,
			ReleaseInstances: reach.ReleaseInstances,
			KeptInstances:    reach.KeptInstances,
		})
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
		ConfigurationDigest: configDigest,
		WayInTokenDigest:    wayInDigest,
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
// performed.
//
// Three things put a deploy that picked the row with a control on the record as
// having run without one, and none of them refuses the deploy. A service's
// first release has no control whatever the score prefers: there is no build
// being replaced, so nothing can keep serving beside it. A target whose platform
// serves no share is one the row is unavailable on, permanently rather than
// once, so every deploy there goes without a control. And a target declared as
// serving a share that then refuses the shift is the deployer performing the row
// without a control on that deploy and writing so. A rollout that ran no
// comparison is on the record as one in all three.
//
// Where the shift returns, the control is running on that target and the record
// names it there: one control per production target the release has reached,
// started on that target when the rollout reaches it.
func performed(ctx context.Context, w *Writer, p Performance, d Deploy, reach Reach) (Strategy, error) {
	switch {
	case p.StrategyPicked != StrategyWithControl,
		p.ControlReleaseID == "",
		!reach.ServesAShare:
		return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
	}
	err := reach.Target.ShiftTraffic(ctx, p.Principal, targetseam.Shift{
		Service: p.ServiceName, Build: p.What.BuildID, Share: reach.Share, Credential: p.Credential,
	})
	if err != nil {
		return StrategyWithoutControl, w.PerformedWithoutControl(ctx, d.ID)
	}
	if err := w.ControlStarted(ctx, d.ID, reach.Address, Control{
		ReleaseID: p.ControlReleaseID, BuildID: p.ControlBuildID, Instances: reach.ControlInstances,
	}); err != nil {
		return d.StrategyPerformed, err
	}
	if d.StrategyPerformed == StrategyWithoutControl {
		// An earlier target refused the shift, so the deploy as a whole ran
		// without a control whatever this one did.
		return StrategyWithoutControl, nil
	}
	return StrategyWithControl, w.PerformedWithControl(ctx, d.ID)
}

// completeAddresses is the set of addresses the record already holds complete,
// which is what lets [perform] skip a target on a second call over a record
// [Resume] is finishing rather than reaching it, and placing something on it,
// a second time.
func completeAddresses(ctx context.Context, w *Writer, deployID string) (map[string]bool, error) {
	targets, err := Targets(ctx, w.Pool(), deployID)
	if err != nil {
		return nil, err
	}
	complete := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.Completion == CompletionComplete {
			complete[target.Address] = true
		}
	}
	return complete, nil
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
