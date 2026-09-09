package deploy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/targetseam"
)

// Reading is what the restart asks of each target of a record it stopped in the
// middle of: which build that target is running for the service the record
// names. The caller implements it — this package holds no service name and no
// credential for a record it reads back, and reaching a target is the seam's —
// and a restart given none decides from the record alone.
type Reading interface {
	// RunningBuild is the build the target is running for the service that
	// record names, and empty where nothing runs there.
	RunningBuild(ctx context.Context, d Deploy, address string) (string, error)
}

// Rebuilding is what [Resume] asks the caller for to carry a record it stopped
// in the middle of further, forward or back: the live seams to reach its
// targets with, the credential and the principal a deploy needs, none of
// which a stopped record holds on its own — it names a service and an
// environment by id, and reaching either takes what composed them together.
// The caller implements it: this package reaches no target and holds no
// credential of its own, the way [Reading] and [Artifacts] already do not.
//
// Resume fills What, IntoProduction and StrategyPicked from the record itself
// before it uses what is returned, so the caller supplies the rest: Reaches
// with a live [targetseam.Target] per address, ServiceID, ServiceName,
// EnvironmentID, Credential, Principal, Actor, and, where the deploy is
// production's, Notifier, Bake and BakeVolume — the same fields a fresh
// [Perform] would be given for the same record.
type Rebuilding interface {
	// Rebuild is [Rebuilt] for record d, and false where the release it
	// deploys can no longer be put on a target at all — its build's artifact
	// gone, most often. Resume then reaches d's targets through the same
	// Performance to return the ones it already reached to the release that
	// was current before it, rather than finishing the deploy forward.
	Rebuild(ctx context.Context, d Deploy) (Rebuilt, bool, error)
}

// Rebuilt is what [Rebuilding.Rebuild] returns: the Performance to reach a
// stopped record's targets again, and what its slow return path alone needs —
// the fast one needs nothing beyond the Performance and what the record's own
// rows already say about the kept fleet.
type Rebuilt struct {
	// Performance is used both to finish the record forward, where the
	// release it deploys still can be, and to return the targets it reached
	// to the release before it, where it cannot: [Resume] overwrites its
	// What with the release the path it takes deploys.
	Performance Performance
	// Artifacts and RecordedDigest are what the slow return path verifies the
	// release before this one against, where the fast path finds no kept
	// instances standing on the targets this record reached. Left empty where
	// the caller expects only the fast path, the slow path's own refusal is
	// what a record needing it anyway is answered with.
	Artifacts      Artifacts
	RecordedDigest string
}

// Resume is the deployer's restart: every component's restart is a read of its
// own records, and the deployer's is the deploy records no target has finished.
// It asks each of those targets which build it is running, and has two
// dispositions and not three, both inside the package: the deploy is
// completed, in the target order, or it is returned to the release that was
// current before it, through this package's own return path — the fast way
// where the kept instances stand, the slow way otherwise, writing the
// rollback's record with [SourceOfRestart]. A record neither disposition can
// reach — no caller to ask, no live target for the service at all, or no
// release the environment stood on before this one — is marked failed at
// [StepCannotBeCarried] rather than left standing undecided.
//
// The reading is what a stop mid-call leaves behind: a target running this
// deploy's build with its row not complete was reached before the stop, so the
// row is marked complete from what the target says and no target is deployed to
// twice. A record every target the service runs on is then complete or rolled
// back on is completed here — the deployer stopped between the last target and
// the record's own advance, or an earlier restart's own return path finished
// undoing it.
//
// A record with something still owed is carried, never left: one with
// something already complete is finished forward where [Rebuilding] can still
// put the release somewhere, and returned otherwise; one with nothing
// complete at all has nothing running to redeploy and nothing kept fleet to
// disturb, so it is returned across every target the service runs on rather
// than finished forward — there is nothing there for finishing forward to
// mean. Both return paths ask [Rebuilding] for the live targets to return
// through, and a caller that supplies none, or a record the return path
// cannot resolve a release before it for, is [StepCannotBeCarried].
//
// The one case the restart cannot decide any other way is a schema change the
// record does not mark complete. That record is marked failed at
// [StepSchemaChangeNotComplete] with the previous release left current, on the
// store rule rather than on the deployer knowing how far the change got. A
// record with a target already complete is not that case: the store step runs
// before any target is reached, and the design admits no failed record with a
// target complete.
//
// A backfill's record whose copy has not finished is neither completed nor
// returned: it stands started while the copy runs, which is what the deploy
// carrying one stands as on Ops for as long as that takes.
//
// A failed record is not read at all. What Resume reads is the started ones.
func Resume(ctx context.Context, w *Writer, reading Reading, rebuilding Rebuilding) error {
	unfinished, err := Unfinished(ctx, w.Pool())
	if err != nil {
		return err
	}

	for _, d := range unfinished {
		if err := completeWhatTheTargetsRun(ctx, w, d, reading); err != nil {
			return err
		}
		targets, err := Targets(ctx, w.Pool(), d.ID)
		if err != nil {
			return err
		}
		complete, owed := 0, 0
		for _, target := range targets {
			switch {
			case target.NotRunHere:
			case target.Completion == CompletionComplete:
				complete++
			case target.Completion == CompletionRolledBack:
				// Returned already, on an earlier restart's own call here:
				// neither owed nor complete, which is what lets the record be
				// completed below rather than carried again.
			default:
				owed++
			}
		}

		switch {
		case complete == 0 && len(d.SchemaChanges) > 0 && !d.SchemaChangesCompleted:
			if err := w.MarkFailed(ctx, d.ID, StepSchemaChangeNotComplete); err != nil {
				return err
			}
			continue
		case owed == 0:
			if d.Backfill.Any() && !d.Backfill.Copied {
				continue
			}
			if err := w.Complete(ctx, d.ID); err != nil {
				return err
			}
			continue
		case rebuilding == nil:
			if err := w.MarkFailed(ctx, d.ID, StepCannotBeCarried); err != nil {
				return err
			}
			continue
		}

		var carried bool
		if complete == 0 {
			carried, err = returnNothingReached(ctx, w, rebuilding, d, targets)
		} else {
			carried, err = carryOn(ctx, w, rebuilding, d, targets)
		}
		if err != nil {
			return err
		}
		if !carried {
			if err := w.MarkFailed(ctx, d.ID, StepCannotBeCarried); err != nil {
				return err
			}
		}
	}
	return nil
}

// carryOn is the two dispositions [Resume] moved into the package for a
// record with something already complete and something still owed: finish it
// forward in the target order where the caller says the release can still be
// put somewhere, or return every target it reached to the release that was
// current there before it. It reports whether it carried the record one way
// or the other; false is what a caller unable to rebuild either leaves for
// [Resume] to mark [StepCannotBeCarried].
func carryOn(ctx context.Context, w *Writer, rebuilding Rebuilding, d Deploy, targets []Target) (bool, error) {
	rebuilt, found, err := rebuilding.Rebuild(ctx, d)
	if err != nil {
		return false, err
	}
	if found {
		p := rebuilt.Performance
		p.What = What{ReleaseID: d.ReleaseID, BuildID: d.BuildID}
		p.IntoProduction = d.StrategyPicked != ""
		p.StrategyPicked = d.StrategyPicked
		if _, err := perform(ctx, w, p, d); err != nil {
			return false, err
		}
		return true, nil
	}
	var reachedAddresses []string
	for _, target := range targets {
		if target.Completion == CompletionComplete {
			reachedAddresses = append(reachedAddresses, target.Address)
		}
	}
	return returnTargets(ctx, w, rebuilt, d, targets, reachedAddresses)
}

// returnNothingReached is the return path for a record that reached no
// target at all: nothing is running from it and no kept fleet was disturbed,
// so there is nothing for finishing forward to mean and nothing to keep —
// every target the service runs on, read off the live seams [Rebuilding]
// supplies, is returned to the release that was current before this record,
// which is what writes the rollback record naming the target-less return.
func returnNothingReached(ctx context.Context, w *Writer, rebuilding Rebuilding, d Deploy, targets []Target) (bool, error) {
	rebuilt, _, err := rebuilding.Rebuild(ctx, d)
	if err != nil {
		return false, err
	}
	addresses := make([]string, 0, len(rebuilt.Performance.Reaches))
	for _, reach := range rebuilt.Performance.Reaches {
		addresses = append(addresses, reach.Address)
	}
	return returnTargets(ctx, w, rebuilt, d, targets, addresses)
}

// returnTargets is the return path both [carryOn] and [returnNothingReached]
// share: addresses, back to the release that was current on them before d,
// through the fast way where every one of them still keeps that release's
// fleet standing, or the slow way otherwise — the same choice [ShiftBack] and
// [Restore] are named for, made here from the record's own rows rather than
// left to the caller. It reports false, with nothing written, where addresses
// is empty or nothing was current on the first of them before d — a
// service's first release, most often, or a caller with no live target for
// the service at all — which the caller then marks [StepCannotBeCarried].
func returnTargets(ctx context.Context, w *Writer, rebuilt Rebuilt, d Deploy, targets []Target, addresses []string) (bool, error) {
	if len(addresses) == 0 {
		return false, nil
	}
	keptStands := true
	byAddress := make(map[string]Target, len(targets))
	for _, target := range targets {
		byAddress[target.Address] = target
	}
	for _, address := range addresses {
		target := byAddress[address]
		if target.Fleets.Kept.Instances == 0 || target.Fleets.Kept.TornDownAt != "" {
			keptStands = false
		}
	}

	toWhat, found, err := releaseBefore(ctx, w.Pool(), d, byAddress[addresses[0]], addresses[0])
	if err != nil {
		return false, err
	}
	if !found {
		return false, nil
	}

	p := rebuilt.Performance
	p.What = toWhat
	p.IntoProduction = d.StrategyPicked != ""
	p.SchemaChanges = nil
	p.UndoneDeployIDs = []string{d.ID}
	p.Reaches = onlyAddresses(p.Reaches, addresses)
	undoing := Undoing{FailedReleaseID: d.ReleaseID, Source: SourceOfRestart}

	if keptStands {
		_, err = ShiftBack(ctx, w, Returning{Performance: p, Undoing: undoing, KeptBy: d.ID})
	} else {
		_, err = Restore(ctx, w, Restoration{
			Performance: p, Undoing: undoing,
			Artifacts: rebuilt.Artifacts, RecordedDigest: rebuilt.RecordedDigest,
		})
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// releaseBefore is the release and build that was current on address before
// d: the release the control there already names, where a control ran on
// that target — [TargetTable].control_release_id is defined as exactly that
// release — and [PreviousOnTarget]'s read of the store otherwise, for a
// target the rollout reached without one.
func releaseBefore(ctx context.Context, pool *pgxpool.Pool, d Deploy, target Target, address string) (What, bool, error) {
	if target.ControlReleaseID != "" {
		return What{ReleaseID: target.ControlReleaseID, BuildID: target.ControlBuildID}, true, nil
	}
	before, found, err := PreviousOnTarget(ctx, pool, d.ServiceID, d.EnvironmentID, address, d.Number)
	if err != nil || !found {
		return What{}, false, err
	}
	return What{ReleaseID: before.ReleaseID, BuildID: before.BuildID}, true, nil
}

// onlyAddresses is reaches narrowed to the addresses named, in reaches' own
// order — the return path only ever puts traffic back on a target it is
// undoing something on, and never on one the stopped record never reached.
func onlyAddresses(reaches []Reach, addresses []string) []Reach {
	keep := make(map[string]bool, len(addresses))
	for _, address := range addresses {
		keep[address] = true
	}
	var narrowed []Reach
	for _, reach := range reaches {
		if keep[reach.Address] {
			narrowed = append(narrowed, reach)
		}
	}
	return narrowed
}

// completeWhatTheTargetsRun marks complete every target of the record that is
// running the build the record names and whose row does not say so, which is
// what a deployer stopped between the call and the write leaves. It asks and
// writes nothing on a record that put no build anywhere — a removal — and on a
// restart given nothing to ask with.
//
// The replacement it writes is the drain the seam has one of: the target is
// running the build, so the replacement it performed finished the requests the
// instance it replaced held, which is the only outcome the operation has.
func completeWhatTheTargetsRun(ctx context.Context, w *Writer, d Deploy, reading Reading) error {
	if reading == nil || d.BuildID == "" {
		return nil
	}
	targets, err := Targets(ctx, w.Pool(), d.ID)
	if err != nil {
		return err
	}
	for _, target := range targets {
		if target.NotRunHere || target.Completion != CompletionNotReached {
			continue
		}
		running, err := reading.RunningBuild(ctx, d, target.Address)
		if err != nil {
			return fmt.Errorf("deploy: asking %s what it runs for %s: %w", target.Address, d.ID, err)
		}
		if running != d.BuildID {
			continue
		}
		if err := w.CompleteTarget(ctx, d.ID, target.Address, targetseam.ReplacementDrained); err != nil {
			return err
		}
	}
	return nil
}

// Partial is the targets of a deploy that are not complete, in the
// environment's order, which is what a caller resuming a recorded partial deploy
// reaches next. A target the service does not run on is none of them: the
// deployer never reaches one, so there is nothing there to carry on with.
func Partial(ctx context.Context, pool *pgxpool.Pool, deployID string) ([]Target, error) {
	targets, err := Targets(ctx, pool, deployID)
	if err != nil {
		return nil, err
	}
	var owed []Target
	for _, target := range targets {
		if target.NotRunHere || target.Completion == CompletionComplete {
			continue
		}
		owed = append(owed, target)
	}
	return owed, nil
}
