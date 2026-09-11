// This file holds the tests of the live reading: an item is live when a
// production deploy record names its release and marks it complete on every
// production target, and an intent is partly delivered when one of its items
// stopped beside a live sibling.
//
// It is the one test file of this package that applies another package's DDL
// beside this package's — package release's and package deploy's — because the
// reading walks from the item to the release minted for it and from there to
// the deploy records of the production environment. What the walk needs is
// records those two packages write, and writing them through their own writers
// is what keeps the test reading the same rows the factory does.
package item_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// theProduction is the production environment these tests read deploys of, and
// the two addresses are its targets. An item is live only once its release is
// complete on both.
const theProduction = "en_" + "pppppppppppppppppppppppppppppppp"

var everyTarget = []string{"target-one", "target-two"}

// deployerActor is who writes a deploy record: the deployer, a component. No
// agent performs a deploy, which package deploy refuses on the actor.
var deployerActor = record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed}

// newLiveStore is [newStore] with the two schemas the live reading walks into
// applied beside this package's, and the writers of each.
func newLiveStore(t *testing.T) (context.Context, *pgxpool.Pool, *item.Decomposition, *item.Dispatch,
	*release.Writer, *deploy.Writer) {
	t.Helper()
	ctx, pool, token := newStore(t)
	for _, statements := range [][]string{release.DDL, deploy.DDL} {
		for n, statement := range statements {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("applying statement %d of another package's schema: %v", n+1, err)
			}
		}
	}
	return ctx, pool, item.NewDecomposition(pool, token, item.NoHolds{}), item.NewDispatch(pool, token),
		release.NewWriter(pool, token), deploy.NewWriter(pool, token)
}

// itemOn is one item of one intent on one service, answering a requirement of
// its own so that the write is not the one decomposition refuses.
func itemOn(ctx context.Context, t *testing.T, decomposition *item.Decomposition,
	intentID, serviceID, branch string) item.Item {
	t.Helper()
	it, err := decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: intentID, ServiceID: serviceID, Branch: branch,
		RequirementsAnswered: []string{record.NewID("rq")},
	}, "", "")
	if err != nil {
		t.Fatalf("decomposing %s: %v", branch, err)
	}
	return it
}

// shipped mints the release the item merged as and deploys it into production,
// marking it complete on the addresses given and leaving the rest not reached.
// delivering is what a revert's deploy lists beside the release it is of.
func shipped(ctx context.Context, t *testing.T, releases *release.Writer, deploys *deploy.Writer,
	it item.Item, completeOn []string, delivering []string) release.Release {
	t.Helper()
	rel, err := releases.Mint(ctx, decompositionActor, release.Minting{
		ServiceID: it.ServiceID, BuildID: record.NewID("bd"), Commit: record.NewID("commit"), ItemID: it.ID,
	})
	if err != nil {
		t.Fatalf("minting the release of %s: %v", it.ID, err)
	}
	deployRelease(ctx, t, deploys, it.ServiceID, rel.ID, completeOn, delivering)
	return rel
}

// deployRelease writes one production deploy record and completes it on the
// addresses given.
func deployRelease(ctx context.Context, t *testing.T, deploys *deploy.Writer,
	serviceID, releaseID string, completeOn, delivering []string) {
	t.Helper()
	reaching := make([]deploy.Reaching, 0, len(everyTarget))
	for _, address := range everyTarget {
		reaching = append(reaching, deploy.Reaching{Address: address, ReleaseInstances: 1})
	}
	d, err := deploys.Start(ctx, deployerActor, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: theProduction,
		What: deploy.OfRelease(releaseID, record.NewID("bd")), Targets: reaching,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		DeliveredReleaseIDs: delivering,
	})
	if err != nil {
		t.Fatalf("starting the deploy of %s: %v", releaseID, err)
	}
	for _, address := range completeOn {
		if err := deploys.CompleteTarget(ctx, d.ID, address, targetseam.ReplacementDrained); err != nil {
			t.Fatalf("completing %s of %s: %v", address, d.ID, err)
		}
	}
}

// TestLiveIsReadOffTheProductionDeployRecord: an item is live when a
// production deploy record names its release, as the one it deployed or in the
// list of releases a revert's deploy delivered, and marks it complete on every
// production target — and not before. Whether an item is live is no id list a
// caller asserts: what the caller supplies is the environment and the targets
// each service runs on, both of them records this package does not hold.
func TestLiveIsReadOffTheProductionDeployRecord(t *testing.T) {
	ctx, pool, decomposition, _, releases, deploys := newLiveStore(t)
	const service = "svc_" + "llllllllllllllllllllllllllllllll"
	const intentID = "in_" + "llllllllllllllllllllllllllllllll"

	complete := itemOn(ctx, t, decomposition, intentID, service, "item/complete")
	shipped(ctx, t, releases, deploys, complete, everyTarget, nil)

	partial := itemOn(ctx, t, decomposition, intentID, service, "item/partial")
	shipped(ctx, t, releases, deploys, partial, everyTarget[:1], nil)

	unreleased := itemOn(ctx, t, decomposition, intentID, service, "item/unreleased")

	// A release the hold was holding, delivered by a revert's own deploy: the
	// item is live on that record and has no complete deploy of its own.
	delivered := itemOn(ctx, t, decomposition, intentID, service, "item/delivered")
	held, err := releases.Mint(ctx, decompositionActor, release.Minting{
		ServiceID: service, BuildID: record.NewID("bd"), Commit: record.NewID("commit"), ItemID: delivered.ID,
	})
	if err != nil {
		t.Fatalf("minting the held release: %v", err)
	}
	revert := itemOn(ctx, t, decomposition, intentID, service, "item/revert")
	shipped(ctx, t, releases, deploys, revert, everyTarget, []string{held.ID})

	items := []item.Item{complete, partial, unreleased, delivered, revert}
	addresses := map[string][]string{service: everyTarget}
	live, err := item.Live(ctx, pool, items, theProduction, addresses)
	if err != nil {
		t.Fatalf("Live: %v", err)
	}
	want := []string{complete.ID, delivered.ID, revert.ID}
	if strings.Join(live, ",") != strings.Join(want, ",") {
		t.Errorf("Live = %v, want %v: the complete deploy, the release a revert delivered, and the revert",
			live, want)
	}

	// A target the caller does not name is a target the reading does not
	// require, and a service it names none for is one nothing is complete on
	// every target of.
	if live, err := item.Live(ctx, pool, items, theProduction, nil); err != nil || len(live) != 0 {
		t.Errorf("Live with no addresses = %v, %v, want nothing live", live, err)
	}
	oneTarget := map[string][]string{service: everyTarget[:1]}
	if live, err := item.Live(ctx, pool, items, theProduction, oneTarget); err != nil ||
		strings.Join(live, ",") != strings.Join([]string{complete.ID, partial.ID, delivered.ID, revert.ID}, ",") {
		t.Errorf("Live over one target = %v, %v, want the partly deployed item among them", live, err)
	}
	// Another environment's records are not the production deploy record.
	if live, err := item.Live(ctx, pool, items, "en_candidate", addresses); err != nil || len(live) != 0 {
		t.Errorf("Live in another environment = %v, %v, want nothing live", live, err)
	}
}

// TestPartlyDeliveredIsARepeatableReading: an intent whose items did not all
// ship is at least one stopped item beside at least one live sibling. Nothing
// writes it down, so it is a reading and not a field, and it is read off the
// deploy records each time.
func TestPartlyDeliveredIsARepeatableReading(t *testing.T) {
	ctx, pool, decomposition, dispatch, releases, deploys := newLiveStore(t)
	const service = "svc_" + "dddddddddddddddddddddddddddddddd"
	const intentID = "in_" + "dddddddddddddddddddddddddddddddd"
	addresses := map[string][]string{service: everyTarget}

	first := itemOn(ctx, t, decomposition, intentID, service, "item/first")
	second := itemOn(ctx, t, decomposition, intentID, service, "item/second")

	// Both still moving: in progress rather than partly delivered.
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, theProduction, addresses); err != nil || partly {
		t.Errorf("PartlyDelivered with both moving = %v, %v", partly, err)
	}
	// One stopped and none live: stopped rather than partly delivered.
	if _, err := dispatch.Drop(ctx, workActor, first.ID); err != nil {
		t.Fatalf("Drop: %v", err)
	}
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, theProduction, addresses); err != nil || partly {
		t.Errorf("PartlyDelivered with nothing live = %v, %v", partly, err)
	}
	// The sibling's release deployed, and complete on one target of two is not
	// live: the intent is stopped until the rollout finishes.
	shipped(ctx, t, releases, deploys, second, everyTarget[:1], nil)
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, theProduction, addresses); err != nil || partly {
		t.Errorf("PartlyDelivered with the sibling on one target of two = %v, %v", partly, err)
	}
	// One stopped, one live.
	deployRelease(ctx, t, deploys, service, releaseOf(ctx, t, pool, second), everyTarget, nil)
	if partly, err := item.PartlyDelivered(ctx, pool, intentID, theProduction, addresses); err != nil || !partly {
		t.Errorf("PartlyDelivered with a live sibling = %v, %v", partly, err)
	}
}

// releaseOf is the release minted for one item, which the second deploy of it
// is written against.
func releaseOf(ctx context.Context, t *testing.T, pool *pgxpool.Pool, it item.Item) string {
	t.Helper()
	rel, minted, err := release.ForItem(ctx, pool, it.ID)
	if err != nil || !minted {
		t.Fatalf("reading the release of %s: %v, %v", it.ID, minted, err)
	}
	return rel.ID
}
