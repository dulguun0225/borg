// What the deploy record carries beside a target's completion: the
// configuration digest and the way-in token digest, stable over the
// resolved value set alone; the delivered releases a revert's deploy
// lists; the copy named on the change that destroys, verified against
// what a target holds; and a deploy naming a build and no release
// standing on the build. The ordered walk and the strategy performed are
// rollout_test.go, which holds the fakes this file shares with it.
package deploy_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/wayin"
)

// given is a target that keeps what crossed the seam: the deployment it was
// handed and the changes it was asked to apply. [targetseam.Fake] records the
// call and not the values it carried, for the reason its own doc states, so a
// test reading what the deployer handed over wraps one.
type given struct {
	*targetseam.Fake
	deployment targetseam.Deployment
	changes    []targetseam.SchemaChange
}

func (g *given) Deploy(ctx context.Context, p principal.Principal, d targetseam.Deployment) (targetseam.Placement, error) {
	g.deployment = d
	return g.Fake.Deploy(ctx, p, d)
}

func (g *given) ApplySchemaChange(ctx context.Context, p principal.Principal, c targetseam.SchemaChange) error {
	g.changes = append(g.changes, c)
	return g.Fake.ApplySchemaChange(ctx, p, c)
}

// oneGiven is an environment of one target whose calls a test reads back.
func oneGiven() ([]deploy.Reach, *given) {
	target := &given{Fake: targetseam.NewFake()}
	target.Instances = 2
	return []deploy.Reach{{
		Address: "/srv/one", Target: target, ReleaseInstances: 2, KeptInstances: 1,
		ServesAShare: true, Share: 1,
	}}, target
}

// TestTheRecordCarriesTheDigestsAndTheDeliveredReleases: the configuration
// digest is over the resolved value set alone, and never moves for the token
// minted fresh at every deploy; the way-in token digest is a digest and never
// the token; and a revert's deploy lists the releases it delivers.
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
		t.Errorf("the configuration digest reads %q, want the digest over the resolved value set alone, the way-in token not among it",
			read.ConfigurationDigest)
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

// TestTheWayInTokenIsHandedInTheConfiguration: the deployer mints a token for
// the way in at every deploy and hands it to the service in its configuration
// beside the service's own credentials, writing a digest of it on the record and
// never the token. So the value set that crossed the seam carries it under the
// name the way in reads, while the record's configuration digest is over the
// resolved set alone — the token's own digest already has its own field — and
// the record holds the digest of the token and nothing else of it.
func TestTheWayInTokenIsHandedInTheConfiguration(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, target := oneGiven()

	p := performance(serviceID, r, reaches)
	p.Configuration = targetseam.ValueSet{
		Names:  []string{"DATABASE_URL"},
		Values: []string{"postgres://one"},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	handed := target.deployment.Configuration
	if len(handed.Names) != 3 || handed.Names[1] != deploy.WayInTokenName || handed.Names[2] != targetseam.DeployIDName {
		t.Fatalf("the service was handed %v, want its own value, the way-in token, and the deploy id beside it", handed.Names)
	}
	if handed.Names[0] != "DATABASE_URL" || handed.Values[0] != "postgres://one" {
		t.Errorf("the service's own configuration reads %v = %v, want what the caller resolved",
			handed.Names, handed.Values)
	}
	if handed.Values[2] != d.ID {
		t.Errorf("the deploy id handed to the service is %q, want %q", handed.Values[2], d.ID)
	}
	minted := handed.Values[1]
	if len(minted) != 64 {
		t.Fatalf("the token handed over is %q, want 32 bytes of it", minted)
	}
	digest := sha256.Sum256([]byte(minted))
	if read.WayInTokenDigest != hex.EncodeToString(digest[:]) {
		t.Errorf("the record holds %q, want the digest of the token the service was handed", read.WayInTokenDigest)
	}
	if read.ConfigurationDigest != deploy.DigestConfiguration(p.Configuration) {
		t.Errorf("the configuration digest reads %q, want the digest over the set the caller resolved, the token not among it",
			read.ConfigurationDigest)
	}
	if read.ConfigurationDigest == deploy.DigestConfiguration(handed) {
		t.Error("the configuration digest moved with the token minted at this deploy, want it stable over the resolved set alone")
	}
	found, ok, err := deploy.ByWayInTokenDigest(ctx, pool, read.WayInTokenDigest)
	if err != nil || !ok || found.ID != d.ID {
		t.Errorf("ByWayInTokenDigest = %+v, %v, %v, want the deploy that placed the way in", found, ok, err)
	}
}

// TestTheWayInTokenNameIsTheOneTheWayInReads: the token is handed over under a
// name this package spells and the shipped way in reads, and two spellings of
// one name is what this fails on.
func TestTheWayInTokenNameIsTheOneTheWayInReads(t *testing.T) {
	if deploy.WayInTokenName != wayin.TokenEnv {
		t.Fatalf("the deployer hands the token under %q and the way in reads %q",
			deploy.WayInTokenName, wayin.TokenEnv)
	}
}

// TestTheSnapshotIsNamedOnTheChangeThatDestroys: the copy taken and verified
// before a change that destroys stored data is named on the change the target is
// asked to apply, so the target verifies it against what it finds before it
// applies anything, and on the record beside the change.
func TestTheSnapshotIsNamedOnTheChangeThatDestroys(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, target := oneGiven()

	p := performance(serviceID, r, reaches)
	p.SnapshotName = "before-the-drop"
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0004-add-the-column", Text: "add", Credential: credential},
		{Service: "checkout", Change: "0005-drop-the-old-column", Text: "drop", Destroys: true, Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Snapshot.Name != "before-the-drop" || read.Snapshot.Digest == "" {
		t.Fatalf("the record names %+v, want the copy taken and verified", read.Snapshot)
	}
	if len(target.changes) != 2 {
		t.Fatalf("the target was asked for %d change(s), want the two the build declares", len(target.changes))
	}
	if named := target.changes[1].Snapshot; named.Name != read.Snapshot.Name || named.Digest != read.Snapshot.Digest {
		t.Errorf("the destroying change names %+v, want the copy the record names", named)
	}
	if named := target.changes[0].Snapshot; named.Name != "" {
		t.Errorf("the change that destroys nothing names %+v, want no copy", named)
	}
	for _, change := range target.changes {
		if change.Release != r.ID || change.Build != r.BuildID {
			t.Errorf("%s was applied naming release %q under build %q, want the release that shipped it and the build it ran under",
				change.Change, change.Release, change.Build)
		}
	}
}

// TestAChangeADeployNamingNoReleaseAppliesStandsOnTheBuild: a candidate's deploy
// and the search's name a build and no release, and the history row each writes
// names that build — so a deploy naming no release applies the changes its build
// declares like any other.
func TestAChangeADeployNamingNoReleaseAppliesStandsOnTheBuild(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)
	reaches, target := oneGiven()

	p := performance(serviceID, r, reaches)
	p.IntoProduction = false
	p.StrategyPicked = ""
	p.EnvironmentID = "env_candidate"
	p.What = deploy.OfBuild(r.BuildID)
	p.SchemaChanges = []targetseam.SchemaChange{
		{Service: "checkout", Change: "0006-add-the-column", Text: "add", Credential: credential},
	}

	d, err := deploy.Perform(ctx, w, p)
	if err != nil {
		t.Fatalf("Perform: %v", err)
	}
	if d.Status != deploy.StatusComplete {
		t.Fatalf("the deploy is %s, want complete", d.Status)
	}
	if len(target.changes) != 1 {
		t.Fatalf("the target was asked for %d change(s), want the one the build declares", len(target.changes))
	}
	if change := target.changes[0]; change.Build != r.BuildID || change.Release != "" {
		t.Errorf("the change was applied as %+v, want the build it ran under and no release", change)
	}
	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.SchemaChangesCompleted {
		t.Error("the record does not mark the change complete, and the store carries it")
	}
}
