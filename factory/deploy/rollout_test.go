// The tests of what the deployer does, as against what the record holds: the
// ordered walk over the targets, the steps before traffic, the rollback's
// verification, the restart, and the mitigation. The target is
// [targetseam.NewFake]; localtarget is where a real process runs.
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
		{Address: "/srv/one", Target: one, KeptInstances: 1, ServesAShare: shares, Share: 0.1},
		{Address: "/srv/two", Target: two, KeptInstances: 1, ServesAShare: shares, Share: 0.1},
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
	if read.ControlTarget != "/srv/one" {
		t.Errorf("the control target reads %q, want the first target the rollout reaches", read.ControlTarget)
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

// TestASnapshotThatCannotBeVerifiedStopsTheDeployAndPages: a snapshot the
// deployer cannot take and verify is a deploy not performed — the record is
// marked failed at that step, no target is marked complete, and a page fires.
func TestASnapshotThatCannotBeVerifiedStopsTheDeployAndPages(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(false)

	paged := &pages{}
	p := performance(serviceID, r, reaches)
	p.Notifier = paged
	p.SchemaChanges = []targetseam.SchemaChange{{
		Service: "checkout", Change: "0003-drop-the-old-column", Text: "drop", Destroys: true,
		Credential: credential,
	}}
	// No snapshot name, so there is no copy to take and verify.
	_, err := deploy.Perform(ctx, w, p)
	if !errors.Is(err, deploy.ErrSnapshotRefused) {
		t.Fatalf("Perform = %v, want ErrSnapshotRefused", err)
	}
	if len(paged.reasons) != 1 {
		t.Errorf("the deployer paged %d times, want once at that exit: %v", len(paged.reasons), paged.reasons)
	}
	assertFailedAt(t, ctx, pool, serviceID, deploy.StepSnapshot)
}

// TestASchemaChangeThatFailsToApplyStopsTheDeploy: a change that fails to apply
// stops the deploy before any traffic shifts, no target is marked complete, the
// previous release stays current, and the failure stands on the record for Ops.
func TestASchemaChangeThatFailsToApplyStopsTheDeploy(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, _ := twoFakes(false)

	p := performance(serviceID, r, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{{
		Service: "checkout", Change: "", Text: "add", Credential: credential,
	}}
	_, err := deploy.Perform(ctx, w, p)
	if !errors.Is(err, deploy.ErrSchemaChangeRefused) {
		t.Fatalf("Perform = %v, want ErrSchemaChangeRefused", err)
	}
	assertFailedAt(t, ctx, pool, serviceID, deploy.StepSchemaChange)
}

// TestAChangeTheHistoryAlreadyHoldsIsNotApplied: which changes a store carries
// is read from the schema history the deployer keeps in the store and never
// from a deploy record, so a deploy applies the changes its build declares that
// the history lacks.
func TestAChangeTheHistoryAlreadyHoldsIsNotApplied(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, fakes := twoFakes(false)
	fakes[0].SchemaHistory["checkout"] = []targetseam.SchemaChangeApplied{
		{Change: "0001-add-the-column", Checksum: "a", Widened: true},
	}

	p := performance(serviceID, r, reaches)
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0001-add-the-column", Credential: credential},
		{Service: "checkout", Change: "0002-backfill", Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	applied := 0
	for _, call := range fakes[0].Calls() {
		if call.Op == targetseam.OpApplySchemaChange {
			applied++
			if call.Change != "0002-backfill" {
				t.Errorf("the deploy applied %s, want the change the history lacks", call.Change)
			}
		}
	}
	if applied != 1 {
		t.Errorf("the deploy applied %d changes, want the one the history lacks", applied)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.SchemaChangeCompleted || read.SchemaChange != "0002-backfill" {
		t.Errorf("the record says %q completed %v, want the change it carried", read.SchemaChange, read.SchemaChangeCompleted)
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
