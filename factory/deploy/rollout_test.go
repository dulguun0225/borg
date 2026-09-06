// The ordered walk over an environment's targets and the strategy the deployer
// performed, as against what the record holds. The step before traffic is
// schemastep_test.go, the rollback and the restart restore_test.go, and the
// mitigation mitigation_test.go; the fakes and the helpers the four share are
// here. The target is [targetseam.NewFake]; localtarget is where a real process
// runs.
package deploy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/record"
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
		{Address: "/srv/two", Target: two, ReleaseInstances: 2,
			KeptInstances: 1, ServesAShare: shares, Share: 0.1},
	}, []*targetseam.Fake{one, two}
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
	fakes[0].Drains = true
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
	if targets[0].ControlReleaseID != "rel_the_rollback_would_return_to" {
		t.Errorf("%s runs control release %q, want the release a rollback returns to",
			targets[0].Address, targets[0].ControlReleaseID)
	}
	if targets[1].ControlReleaseID != "" {
		t.Errorf("%s runs control release %q, want none — it carries no control instances",
			targets[1].Address, targets[1].ControlReleaseID)
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
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: withControlReleaseID(twoTargets, "/srv/one", "rel_below"),
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

// TestTheRecordCarriesTheDigestsAndTheDeliveredReleases: the configuration
// digest is over the resolved value set, the way-in token digest is a digest and
// never the token, and a revert's deploy lists the releases it delivers.
func TestTheRecordCarriesTheDigestsAndTheDeliveredReleases(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	delivered := []string{record.NewID("rel"), record.NewID("rel")}
	reaches, fakes := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.Configuration = targetseam.ValueSet{
		Names:  []string{"DATABASE_URL", "PARTNER_TOKEN"},
		Values: []string{"postgres://one", "sk-a-value"},
	}
	p.DeliveredReleaseIDs = delivered

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ConfigurationDigest != deploy.DigestConfiguration(p.Configuration) || len(read.ConfigurationDigest) != 64 {
		t.Errorf("the configuration digest reads %q, want the digest over the resolved value set", read.ConfigurationDigest)
	}
	if len(read.WayInTokenDigest) != 64 {
		t.Errorf("the way-in token digest reads %q, want a digest", read.WayInTokenDigest)
	}
	if len(read.DeliveredReleaseIDs) != 2 || read.DeliveredReleaseIDs[0] != delivered[0] {
		t.Errorf("the record delivers %v, want %v", read.DeliveredReleaseIDs, delivered)
	}
	// The token itself is on no record and on no recorded call.
	for _, fake := range fakes {
		for _, call := range fake.Calls() {
			if call.Change == read.WayInTokenDigest {
				t.Error("a recorded call holds the way-in token")
			}
		}
	}
}

// assertFailedAt reads the service's one deploy record and asserts it is failed
// at the step named, with no target complete.
func assertFailedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, serviceID, step string) {
	t.Helper()
	unfinished, err := deploy.Unfinished(ctx, pool)
	if err != nil {
		t.Fatalf("Unfinished: %v", err)
	}
	if len(unfinished) != 0 {
		t.Errorf("%d deploys are still started, want the stopped one marked failed", len(unfinished))
	}
	current, found, err := deploy.Current(ctx, pool, serviceID, productionID, addressesOf(twoTargets))
	if err != nil || found {
		t.Errorf("Current = %+v, found %v, %v, want no reader moved by a failed record", current, found, err)
	}
}
