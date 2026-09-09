// The per-author prior over a batch a revert's deploy delivered under one
// window each: only the release the search settled on takes the failed
// outcome, and the rest of the batch rule nothing out — the reading the
// design gives a batch no search named at all.
//
// It is in score_test for the reason report_test.go is: the fixture applies
// the whole factory schema through package postgres, which reaches this
// package back, so an internal test file importing postgres would make
// package score import itself. It does not skip when the database is
// unreachable.
package score_test

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/window"
)

// batchShares and batchPowers are the size and power every window this file
// opens carries, none of it read by the rule under test.
var (
	batchShares = map[gatepolicy.Quantity]float64{
		gatepolicy.QuantityRequestRate: 0.1, gatepolicy.QuantityErrorRate: 0.05, gatepolicy.QuantityLatency: 0.1,
	}
	batchPowers = map[gatepolicy.Quantity]float64{
		gatepolicy.QuantityRequestRate: 0.8, gatepolicy.QuantityErrorRate: 0.8, gatepolicy.QuantityLatency: 0.8,
	}
	batchDeployer = record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed}
)

// openBatchWindow opens and closes a window naming releaseID, over deployID,
// the way a release a revert's deploy delivers is watched.
func openBatchWindow(t *testing.T, win *window.Writer, deployID, releaseID, serviceID string, exit window.Exit) {
	t.Helper()
	opened, err := win.Open(t.Context(), batchDeployer, window.OpenEvent{
		DeployID: deployID, ReleaseID: releaseID, BuildID: "bld_" + releaseID, ServiceID: serviceID,
		PassedAvailable: true, Size: batchShares, Power: batchPowers, Confidence: 0.95, CapSeconds: 3600,
		BoundaryVersion: "boundary/1", Targets: []string{"one.example"},
		EmissionVersionRelease: "emission/1", PolicyVersion: "pv_1", ScoreVersion: "sv_1",
	})
	if err != nil {
		t.Fatalf("opening the window of %s: %v", releaseID, err)
	}
	if _, err := win.Close(t.Context(), opened.ID, exit, window.Closing{}); err != nil {
		t.Fatalf("closing the window of %s: %v", releaseID, err)
	}
}

// openSearchStep opens and closes a search's own window: it names the build
// and no release, and its deploy's one delivered release is onto — the
// release the step's build applied the revert onto, which is what the next
// step of the search and, here, the prior's own reading resumes from.
func openSearchStep(t *testing.T, deploys *deploy.Writer, win *window.Writer, serviceID, environmentID, onto string, exit window.Exit) {
	t.Helper()
	step, err := deploys.Start(t.Context(), batchDeployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: environmentID, What: deploy.OfBuild("bld_step_" + onto),
		Targets:             []deploy.Reaching{{Address: "one.example"}},
		DeliveredReleaseIDs: []string{onto},
	})
	if err != nil {
		t.Fatalf("starting the search's deploy onto %s: %v", onto, err)
	}
	openBatchWindow(t, win, step.ID, "", serviceID, exit)
}

// TestOnlyTheReleaseTheSearchNamedTakesTheFailedOutcome: a batch of three
// releases delivered under one window each still rules all three out where no
// search step narrows the range, but once the search's own steps narrow the
// batch to one release, that release alone takes the failed outcome and the
// rest keep ruling nothing out.
func TestOnlyTheReleaseTheSearchNamedTakesTheFailedOutcome(t *testing.T) {
	ctx, pool, _, win, rel, token := newReportStore(t)
	const service, environment = "svc_batch", "env_batch"
	deploys := deploy.NewWriter(pool, token)
	store := artifact.NewStore(pool, token)

	// Three items of one author, each released and each watched by a window
	// naming it, over the one deploy the revert made.
	versions := map[string]string{}
	for _, item := range []string{"it_a", "it_b", "it_c"} {
		v, err := store.SubmitImplementation(ctx, marking,
			artifact.By{Authorship: artifact.AuthorshipAgent, Author: "author-1"},
			item, "content of "+item, "im_"+item)
		if err != nil {
			t.Fatalf("submitting %s: %v", item, err)
		}
		versions[item] = v.ID
	}
	relA, err := rel.Mint(ctx, marking, release.Minting{ServiceID: service, BuildID: "bld_a", Commit: "commit_a", ItemID: "it_a"})
	if err != nil {
		t.Fatalf("minting relA: %v", err)
	}
	relB, err := rel.Mint(ctx, marking, release.Minting{ServiceID: service, BuildID: "bld_b", Commit: "commit_b", ItemID: "it_b"})
	if err != nil {
		t.Fatalf("minting relB: %v", err)
	}
	relC, err := rel.Mint(ctx, marking, release.Minting{ServiceID: service, BuildID: "bld_c", Commit: "commit_c", ItemID: "it_c"})
	if err != nil {
		t.Fatalf("minting relC: %v", err)
	}
	batch := []string{relA.ID, relB.ID, relC.ID}

	// A deploy's own window is unique per deploy and per release, so the
	// revert's catch-up is one deploy and one window per release it delivers
	// — every one of the three naming the same full batch, which is what
	// [underOneWindow] reads off any one of them.
	for _, id := range batch {
		d, err := deploys.Start(ctx, batchDeployer, deploy.Beginning{
			ServiceID: service, EnvironmentID: environment, What: deploy.OfRelease(id, "bld_"+id),
			Targets:             []deploy.Reaching{{Address: "one.example"}},
			DeliveredReleaseIDs: batch,
		})
		if err != nil {
			t.Fatalf("starting the revert's deploy of %s: %v", id, err)
		}
		openBatchWindow(t, win, d.ID, id, service, window.ExitFailed)
	}

	scored := score.New(score.Composition{Pool: pool, Draw: score.NeverDraw{}, Token: token})
	change := score.Change{
		ItemID: "it_a", ServiceID: service, ArtifactID: versions["it_a"],
		FactorSet: score.SetAboveABuild,
	}
	priorReading := func() string {
		t.Helper()
		assessed, err := scored.Assess(ctx, change)
		if err != nil {
			t.Fatalf("Assess: %v", err)
		}
		f, found := factorNamed(assessed, "author.prior")
		if !found {
			t.Fatalf("the vector carries no author prior: %+v", assessed.Vector)
		}
		return f.Reading
	}

	// No search step ran, so the batch's own failure teaches nothing about
	// any one of the three — the reading an absent input already gets.
	if reading := priorReading(); !strings.Contains(reading, "0 failed") || !strings.Contains(reading, "3 window(s) that ruled nothing out") {
		t.Errorf("with no search step the prior reads %q, want all three ruling nothing out", reading)
	}

	// The search narrows the batch: a step onto relA passed, ruling relA out
	// and putting the fault above it; a step onto relB failed, which is what
	// settles the search on relB with relC never tested.
	openSearchStep(t, deploys, win, service, environment, relA.ID, window.ExitPassed)
	openSearchStep(t, deploys, win, service, environment, relB.ID, window.ExitFailed)

	if reading := priorReading(); !strings.Contains(reading, "1 failed") || !strings.Contains(reading, "2 window(s) that ruled nothing out") {
		t.Errorf("once the search named relB the prior reads %q, want relB alone failed and the other two ruling nothing out", reading)
	}
}
