// The ordered walk over an environment's targets and the strategy the
// deployer performed, as against what the record holds: the bake between
// two targets, the control named on the target the rollout reached, a
// target's refusal making the performed strategy differ from the one
// picked, and a repeat writing nothing. What the record carries — the
// digests, the way-in token, the snapshot named on a destroying change —
// is recordfields_test.go; the rollback and the restart are restore_test.go
// and resume_test.go; the step before traffic is schemastep_test.go; and
// the mitigation mitigation_test.go. The fakes and the helpers the files of
// this package share are here. The target is [targetseam.NewFake()];
// localtarget is where a real process runs.
package deploy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// pages records what the deployer paged about, which is the notifier's part of
// the two exits that page.
type pages struct{ reasons []string }

func (p *pages) Page(_ context.Context, serviceID, reason string) error {
	p.reasons = append(p.reasons, serviceID+": "+reason)
	return nil
}

// bakes answers the hold between one target and the next: how much the targets
// already reached have served, and whether the window's cap has run.
type bakes struct {
	served  int64
	capRun  bool
	asked   int
	perAsk  int64
	release func()
}

func (b *bakes) Served(_ context.Context, _ string) (int64, bool, error) {
	b.asked++
	b.served += b.perAsk
	if b.release != nil {
		b.release()
	}
	return b.served, b.capRun, nil
}

// artifacts is the digest of a build's artifact as the host holds it now, which
// is what a rollback verifies against what the build recorded.
type artifacts map[string]string

func (a artifacts) Digest(_ context.Context, buildID string) (string, error) {
	return a[buildID], nil
}

// twoFakes is an environment of two targets, each its own fake, in the order a
// rollout reaches them.
func twoFakes(shares bool) ([]deploy.Reach, []*targetseam.Fake) {
	one, two := targetseam.NewFake(), targetseam.NewFake()
	one.Instances, two.Instances = 2, 2
	return []deploy.Reach{
		{Address: "/srv/one", Target: one, ReleaseInstances: 2, ControlInstances: 1,
			KeptInstances: 1, ServesAShare: shares, Share: 0.1},
		{Address: "/srv/two", Target: two, ReleaseInstances: 2, ControlInstances: 1,
			KeptInstances: 1, ServesAShare: shares, Share: 0.1},
	}, []*targetseam.Fake{one, two}
}

// threeFakes is an environment of three targets, each its own fake, in the
// order a rollout reaches them — what the restart's forward path is tested
// walking past a target already complete with.
func threeFakes() ([]deploy.Reach, []*targetseam.Fake) {
	one, two, three := targetseam.NewFake(), targetseam.NewFake(), targetseam.NewFake()
	one.Instances, two.Instances, three.Instances = 2, 2, 2
	return []deploy.Reach{
		{Address: "/srv/one", Target: one, ReleaseInstances: 2, KeptInstances: 1},
		{Address: "/srv/two", Target: two, ReleaseInstances: 2, KeptInstances: 1},
		{Address: "/srv/three", Target: three, ReleaseInstances: 2, KeptInstances: 1},
	}, []*targetseam.Fake{one, two, three}
}

// rebuilding is [deploy.Rebuilding] as a test supplies it: what [deploy.Resume]
// asks the caller for to carry a stopped record forward or back.
type rebuilding struct {
	performance func(deploy.Deploy) deploy.Performance
	found       bool
	artifacts   deploy.Artifacts
	digest      string
}

func (r rebuilding) Rebuild(_ context.Context, d deploy.Deploy) (deploy.Rebuilt, bool, error) {
	return deploy.Rebuilt{Performance: r.performance(d), Artifacts: r.artifacts, RecordedDigest: r.digest}, r.found, nil
}

func performance(serviceID string, r release.Release, reaches []deploy.Reach) deploy.Performance {
	return deploy.Performance{
		Actor:          deployer,
		Principal:      deployerCalls,
		ServiceID:      serviceID,
		ServiceName:    "checkout",
		EnvironmentID:  productionID,
		What:           deploy.OfRelease(r.ID, r.BuildID),
		IntoProduction: true,
		StrategyPicked: deploy.StrategyWithoutControl,
		Credential:     credential,
		Reaches:        reaches,
	}
}

// TestTheDeployerReachesTheTargetsInOrder: a target is not reached until the
// target before it is marked complete, and the row for a target is written
// before the call to it and marked complete after — which is what bounds what a
// deployer whose lease lapsed mid-call can leave behind.
func TestTheDeployerReachesTheTargetsInOrder(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)

	// The first target asserts, while it is being called, that its own row is
	// written and the second target has not been reached at all.
	var order []string
	p := performance(serviceID, r, reaches)
	p.Bake = &bakes{perAsk: 1, release: func() { order = append(order, "the bake volume between them") }}
	p.BakeVolume = 1

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if d.Status != deploy.StatusComplete {
		t.Fatalf("the deploy is %s, want complete", d.Status)
	}

	targets, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for n, target := range targets {
		if target.Completion != deploy.CompletionComplete {
			t.Errorf("target %s is %s, want complete", target.Address, target.Completion)
		}
		if target.ReachedAt == "" || target.ReachedAt > target.CompleteAt {
			t.Errorf("target %s was reached at %q and completed at %q, want the row written before the call",
				target.Address, target.ReachedAt, target.CompleteAt)
		}
		order = append(order, targets[n].Address)
	}
	if len(order) != 3 || order[0] != "the bake volume between them" {
		t.Errorf("the walk asked for %v, want the bake volume held between two targets", order)
	}
	for n, fake := range fakes {
		if len(fake.Calls()) == 0 || fake.Calls()[0].Op != targetseam.OpDeploy {
			t.Errorf("target %d was not deployed to: %+v", n+1, fake.Calls())
		}
	}
}

// TestOnceTheCapHasRunTheRemainingTargetsAreReachedWithNoHold: a quiet service
// that never serves the bake volume would otherwise never complete a deploy.
func TestOnceTheCapHasRunTheRemainingTargetsAreReachedWithNoHold(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.Bake = &bakes{capRun: true}
	p.BakeVolume = 1_000_000

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if d.Status != deploy.StatusComplete {
		t.Errorf("the deploy is %s, want complete once the window's cap has run", d.Status)
	}
}

// TestATargetThatRefusesTheShiftMakesThePerformedStrategyDiffer: the two differ
// where a target declared as serving a share refused the operation of seam 4
// that shifts one, and a rollout that ran no comparison is on the record as one.
func TestATargetThatRefusesTheShiftMakesThePerformedStrategyDiffer(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(true)
	fakes[0].RefuseShift = errors.New("this platform moves a process rather than traffic")

	p := performance(serviceID, r, reaches)
	p.StrategyPicked = deploy.StrategyWithControl
	p.ControlReleaseID = "rel_the_rollback_would_return_to"
	p.ControlBuildID = "bl_the_rollback_would_return_to"

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyPicked != deploy.StrategyWithControl {
		t.Errorf("the picked strategy reads %q, want the one the score picked", read.StrategyPicked)
	}
	if read.StrategyPerformed != deploy.StrategyWithoutControl {
		t.Errorf("the performed strategy reads %q, want without a control", read.StrategyPerformed)
	}
	targets, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	// The control is named where one was started, which is the target whose
	// shift returned: the first refused it and runs none.
	if targets[0].ControlReleaseID != "" || targets[0].ControlBuildID != "" {
		t.Errorf("%s runs control %q of %q, want none — it refused the shift",
			targets[0].Address, targets[0].ControlBuildID, targets[0].ControlReleaseID)
	}
	if targets[1].ControlReleaseID != "rel_the_rollback_would_return_to" ||
		targets[1].ControlBuildID != "bl_the_rollback_would_return_to" {
		t.Errorf("%s runs control %q of %q, want the release a rollback returns to and the build it is",
			targets[1].Address, targets[1].ControlBuildID, targets[1].ControlReleaseID)
	}
}

// TestAPlatformThatCannotDrainFailsTheDeployAtThatStep: a platform unable to
// hold a request open across the replacement refuses rather than reporting a
// drain that did not happen, and the deployer marks the deploy failed at the
// first target before any traffic moves, with no target complete.
func TestAPlatformThatCannotDrainFailsTheDeployAtThatStep(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)
	fakes[0].RefuseDrain = targetseam.ErrCannotDrain

	p := performance(serviceID, r, reaches)
	d, err := deploy.Perform(ctx, w, p)
	if !errors.Is(err, targetseam.ErrCannotDrain) {
		t.Fatalf("Perform on a platform that cannot drain = %v, want ErrCannotDrain", err)
	}

	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusFailed || read.FailedStep != deploy.StepFirstTarget {
		t.Errorf("the record is %s at %q, want failed at the first target", read.Status, read.FailedStep)
	}
	targets, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, target := range targets {
		if target.Completion == deploy.CompletionComplete {
			t.Errorf("%s reads complete, want no target complete once the first refused to drain", target.Address)
		}
	}
	if len(fakes[1].Calls()) != 0 {
		t.Errorf("the second target was reached after the first refused: %+v", fakes[1].Calls())
	}
}

// TestTheStrategyPerformedIsWrittenOnlyOnceSomethingWasPerformed: a deployer
// that stopped between the record's write and the shift would otherwise leave a
// record naming a control that never ran, which is the reading the performed
// field exists to prevent.
func TestTheStrategyPerformedIsWrittenOnlyOnceSomethingWasPerformed(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(true)

	started, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	read, err := deploy.Get(ctx, pool, started.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyPerformed != "" {
		t.Errorf("the record names %q performed at the start, want nothing performed yet", read.StrategyPerformed)
	}

	// A whole rollout with a control writes it once the shift returns.
	p := performance(serviceID, r, reaches)
	p.StrategyPicked = deploy.StrategyWithControl
	p.ControlReleaseID = "rel_below"
	p.ControlBuildID = "bl_below"
	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if read, err = deploy.Get(ctx, pool, d.ID); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyPerformed != deploy.StrategyWithControl {
		t.Errorf("the performed strategy reads %q, want with a control once the shift returned", read.StrategyPerformed)
	}
}

// TestAStrategyAttachesToAProductionDeployAndNoOther: a candidate deploy
// carries neither field, a strategy deciding whether a control runs and a
// control existing only where organic traffic does.
func TestAStrategyAttachesToAProductionDeployAndNoOther(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.IntoProduction = false
	p.StrategyPicked = ""
	p.EnvironmentID = "env_candidate"
	p.What = deploy.OfBuild(r.BuildID)

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.StrategyPicked != "" || read.StrategyPerformed != "" {
		t.Errorf("a candidate deploy names strategies %q and %q, want neither",
			read.StrategyPicked, read.StrategyPerformed)
	}
	if read.ReleaseID != "" || read.BuildID != r.BuildID {
		t.Errorf("the record names release %q and build %q, want the build and no release",
			read.ReleaseID, read.BuildID)
	}

	// A production deploy naming no strategy is refused for the same reason
	// from the other side.
	p.IntoProduction = true
	if _, err := deploy.Perform(ctx, w, p); !errors.Is(err, deploy.ErrStrategyNotProduction) {
		t.Errorf("a production deploy with no strategy = %v, want ErrStrategyNotProduction", err)
	}
}

// TestADeployThatCanRunNoControlGoesWithoutOne: a service's first release has no
// control whatever the score prefers, and on a platform that serves no share
// every deploy goes without one — the deploy is performed and the record says a
// rollout ran no comparison, and neither is refused.
func TestADeployThatCanRunNoControlGoesWithoutOne(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	// The first release: the score picked a control and there is no build being
	// replaced for one to run.
	shares, sharing := twoFakes(true)
	first := performance(serviceID, r, shares)
	first.StrategyPicked = deploy.StrategyWithControl
	firstDeploy, err := deploy.Perform(ctx, w, first)
	if err != nil {
		t.Fatalf("a first release under a control the score picked: %v", err)
	}
	assertWithoutAControl(t, ctx, pool, firstDeploy.ID)
	for n, fake := range sharing {
		for _, call := range fake.Calls() {
			if call.Op == targetseam.OpShiftTraffic {
				t.Errorf("target %d was asked to shift traffic for a release with no control below it", n+1)
			}
		}
	}

	// A platform that serves no share: the row is unavailable there,
	// permanently rather than once.
	next := mintRelease(t, ctx, pool, token, serviceID)
	noShare, _ := twoFakes(false)
	p := performance(serviceID, next, noShare)
	p.StrategyPicked = deploy.StrategyWithControl
	p.ControlReleaseID = r.ID
	p.ControlBuildID = r.BuildID
	onNoShare, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("a control picked on a platform serving no share: %v", err)
	}
	assertWithoutAControl(t, ctx, pool, onNoShare.ID)
}

// assertWithoutAControl reads the record back as a rollout that ran no
// comparison: the strategy it picked stands beside the one it performed, so an
// owner reads on one record whether the platform was the reason, and no target
// names a control.
func assertWithoutAControl(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) {
	t.Helper()
	read, err := deploy.Get(ctx, pool, id)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Status != deploy.StatusComplete {
		t.Errorf("the deploy is %s, want it performed and complete", read.Status)
	}
	if read.StrategyPicked != deploy.StrategyWithControl {
		t.Errorf("the picked strategy reads %q, want the one the score picked", read.StrategyPicked)
	}
	if read.StrategyPerformed != deploy.StrategyWithoutControl {
		t.Errorf("the performed strategy reads %q, want without a control", read.StrategyPerformed)
	}
	targets, err := deploy.Targets(ctx, pool, id)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, target := range targets {
		if target.ControlReleaseID != "" || target.ControlBuildID != "" {
			t.Errorf("%s names a control: %+v", target.Address, target)
		}
	}
}

// TestAControlIsNamedOnTheTargetTheRolloutReached: there is one control per
// production target the release has reached, started on that target when the
// rollout reaches it, and the record names the build it runs beside the release
// and the instances running it — so a target the rollout has not reached names
// none.
func TestAControlIsNamedOnTheTargetTheRolloutReached(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	// The record as the deploy starts, before the rollout has reached either
	// target: the control is no part of it.
	started, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	targets, err := deploy.Targets(ctx, pool, started.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	for _, target := range targets {
		if target.ControlReleaseID != "" || target.ControlBuildID != "" || target.Fleets.Control.Instances != 0 {
			t.Fatalf("%s names a control at the start: %+v", target.Address, target)
		}
	}

	if err := w.ControlStarted(ctx, started.ID, "/srv/one", theControl); err != nil {
		t.Fatalf("ControlStarted: %v", err)
	}
	targets, err = deploy.Targets(ctx, pool, started.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if got := targets[0]; got.ControlReleaseID != theControl.ReleaseID ||
		got.ControlBuildID != theControl.BuildID || got.Fleets.Control.Instances != theControl.Instances {
		t.Errorf("the target the rollout reached names %+v, want the control started there", got)
	}
	if got := targets[1]; got.ControlReleaseID != "" || got.ControlBuildID != "" {
		t.Errorf("the target the rollout has not reached names %+v, want no control", got)
	}

	// A control naming no build is refused: the record names the build it runs.
	err = w.ControlStarted(ctx, started.ID, "/srv/two", deploy.Control{ReleaseID: "rel_below"})
	if !errors.Is(err, deploy.ErrControlIncomplete) {
		t.Errorf("a control naming no build = %v, want ErrControlIncomplete", err)
	}
}

// TestARepeatOfAReachOrACompletionWritesNothing: the step the restart must not
// repeat is keyed on the deploy record and the target, so a second reach keeps
// the date the first wrote and a second completion is neither an error nor a
// second write — and a target a rollback has advanced to rolled back is not
// completed again by one.
func TestARepeatOfAReachOrACompletionWritesNothing(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	d, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, d.ID, "/srv/one")
	first, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}

	completeOn(t, ctx, w, d.ID, "/srv/one")
	again, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if again[0].ReachedAt != first[0].ReachedAt || again[0].CompleteAt != first[0].CompleteAt {
		t.Errorf("the repeat wrote %+v over %+v, want the dates the first write left", again[0], first[0])
	}

	// A rolled-back target is not completed again by a repeat of the deploy
	// that put the build there.
	if err := w.UndoTarget(ctx, d.ID, "/srv/one"); err != nil {
		t.Fatalf("UndoTarget: %v", err)
	}
	if err := w.CompleteTarget(ctx, d.ID, "/srv/one", targetseam.ReplacementDrained); err != nil {
		t.Fatalf("a repeat over a rolled-back target: %v", err)
	}
	undone, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	if undone[0].Completion != deploy.CompletionRolledBack {
		t.Errorf("the rolled-back target reads %s, want it left rolled back", undone[0].Completion)
	}

	// A target the record does not name is still not found.
	if err := w.ReachTarget(ctx, d.ID, "/srv/three"); !errors.Is(err, deploy.ErrTargetNotFound) {
		t.Errorf("reaching a target the record does not name = %v, want ErrTargetNotFound", err)
	}
}
