// Tests reading the score's factors against records the real decomposition,
// artifact store, and gate wrote, rather than fixtures a test wrote by hand —
// so what passes here is what the score reads out of the factory's own writes.
package score_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAFirstItemIsDecidedByAHumanAndTheNextIsNot is the milestone's
// demonstration at the level of the score. On a factory that has just been
// installed the first item reads over the supplied threshold — no earlier release
// to return to, an author nobody has approved, an area with no history, and every
// file in the tree touched — and after a human has approved that one, the item
// after it reads under it.
func TestAFirstItemIsDecidedByAHumanAndTheNextIsNot(t *testing.T) {
	ctx, pool, s := newScore(t)
	supplied, _ := score.Starting(gatepolicy.RiskThreshold)
	threshold := supplied.Value
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: threshold}, gate.NoDriftDetector{})

	first, firstImplementation := decomposeItem(t, ctx, pool, "item/one")
	opened, err := g.Fire(ctx, firing(first, firstImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 2}))
	if err != nil {
		t.Fatalf("Fire over the first item: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatalf("the first item on a fresh factory reads %v against a threshold of %v, and a human is meant to decide it",
			opened.Assessment.Number, threshold)
	}
	// The readings that put it there, each stated so a failure says which moved.
	for _, c := range []struct {
		name string
		want float64
	}{
		{"author.prior", 1},
		{"context.business_area", 1},
		{"change.reversibility", 1},
		{"change.reach", 1},
		{"change.area_churn", 0},
		{"context.consumers", 0},
	} {
		if got := levelOf(t, opened.Assessment, c.name); got != c.want {
			t.Errorf("the first item's %s reads %v, want %v", c.name, got, c.want)
		}
	}

	// The human approves, and the release is minted. Those two are what the next
	// item's prior, area history, and reversibility read.
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictApprove, ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if _, err := release.NewWriter(pool).Mint(ctx, mergeActor, serviceID, "bl_0000000000000000000000000000000a", first.ID); err != nil {
		t.Fatalf("Mint: %v", err)
	}

	second, secondImplementation := decomposeItem(t, ctx, pool, "item/two")
	openedAgain, err := g.Fire(ctx, firing(second, secondImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire over the second item: %v", err)
	}
	if openedAgain.HumanDecides {
		t.Fatalf("the second item reads %v against a threshold of %v, and nobody is meant to decide it: %s",
			openedAgain.Assessment.Number, threshold, openedAgain.WhyHuman)
	}
	for _, c := range []struct {
		name string
		want float64
	}{
		{"author.prior", 0.5},
		{"context.business_area", 0.5},
		{"change.reversibility", 0.3},
		{"change.area_churn", 0.2},
	} {
		if got := levelOf(t, openedAgain.Assessment, c.name); got != c.want {
			t.Errorf("the second item's %s reads %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := g.AutoPass(ctx, openedAgain); err != nil {
		t.Fatalf("AutoPass: %v", err)
	}

	// The auto-pass is not evidence about the author: it is the factory agreeing
	// with itself, so a third item's prior reads what the second's did.
	third, thirdImplementation := decomposeItem(t, ctx, pool, "item/three")
	openedThird, err := g.Fire(ctx, firing(third, thirdImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire over the third item: %v", err)
	}
	if got := levelOf(t, openedThird.Assessment, "author.prior"); got != 0.5 {
		t.Errorf("the prior after an auto-pass reads %v, want the 0.5 one human approval left it at", got)
	}
}

// TestAHoldTeachesTheScoreNothing: a hold is not a reject and not an approval —
// it leaves the event queued with the change still good — so no factor moves.
func TestAHoldTeachesTheScoreNothing(t *testing.T) {
	ctx, pool, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.1}, gate.NoDriftDetector{})

	first, firstImplementation := decomposeItem(t, ctx, pool, "item/one")
	deployRow := firing(first, firstImplementation, score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4})
	deployRow.Row = gate.DeployToProduction
	deployRow.ArtifactID = ""
	opened, err := g.Fire(ctx, deployRow)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictHold, "the window is still open"); err != nil {
		t.Fatalf("Decide(hold): %v", err)
	}

	second, secondImplementation := decomposeItem(t, ctx, pool, "item/two")
	openedAgain, err := g.Fire(ctx, firing(second, secondImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if got := levelOf(t, openedAgain.Assessment, "author.prior"); got != 1 {
		t.Errorf("the prior after a hold reads %v, want the top of the scale a hold leaves it at", got)
	}
	if got := levelOf(t, openedAgain.Assessment, "context.business_area"); got != 1 {
		t.Errorf("the area's history after a hold reads %v, want the top of the scale", got)
	}
}

// TestARejectCountsAgainstTheAuthor: the score learns from a reject, which is
// what separates it from a hold.
func TestARejectCountsAgainstTheAuthor(t *testing.T) {
	ctx, pool, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.1}, gate.NoDriftDetector{})

	first, firstImplementation := decomposeItem(t, ctx, pool, "item/one")
	opened, err := g.Fire(ctx, firing(first, firstImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictReject, "the encoding asserts the code"); err != nil {
		t.Fatalf("Decide(reject): %v", err)
	}

	second, secondImplementation := decomposeItem(t, ctx, pool, "item/two")
	openedAgain, err := g.Fire(ctx, firing(second, secondImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	// One rejection and no approval leaves the level at the top of the scale, the
	// same place an unseen author sits: what the level says is how far the score
	// trusts the author, and it cannot trust one less than not at all.
	if got := levelOf(t, openedAgain.Assessment, "author.prior"); got != 1 {
		t.Errorf("the prior after a reject reads %v, want the top of the scale", got)
	}
	// An approval after the rejection narrows it less than an approval alone
	// would have, which is the rejection counting.
	if _, err := g.Decide(ctx, openedAgain, owner, gate.VerdictApprove, ""); err != nil {
		t.Fatalf("Decide(approve): %v", err)
	}
	third, thirdImplementation := decomposeItem(t, ctx, pool, "item/three")
	openedThird, err := g.Fire(ctx, firing(third, thirdImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if got := levelOf(t, openedThird.Assessment, "author.prior"); got <= 0.5 {
		t.Errorf("one approval against one rejection reads %v, want worse than the 0.5 an approval alone leaves", got)
	}
}

// TestAMeasurementThatCouldNotBeTakenGatesTheChange: the diff is the one input
// that is not a record, so the component that measures it says why it could not,
// and the two factors it feeds carry that reason. The formula then reduces the
// whole vector to the top of the scale.
func TestAMeasurementThatCouldNotBeTakenGatesTheChange(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, _ := decomposeItem(t, ctx, pool, "item/one")
	const reason = "the diff against master could not be taken: the commit is not in the repository"
	assessment, err := s.Assess(ctx, score.Change{
		ItemID:          it.ID,
		ServiceID:       serviceID,
		AreaID:          areaID,
		Measurement:     score.Measurement{Unavailable: reason},
		CriteriaInForce: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if assessment.Number != 1 {
		t.Errorf("the number is %v, want the top of the scale", assessment.Number)
	}
	unavailable := assessment.UnavailableFactors()
	if len(unavailable) != 2 {
		t.Fatalf("%v are unavailable, want the two the diff feeds", unavailable)
	}
	for _, f := range assessment.Vector {
		switch f.Name {
		case "change.size", "change.reach":
			if f.Unavailable != reason {
				t.Errorf("%s says it is unavailable because %q, want the measurement's reason", f.Name, f.Unavailable)
			}
			if f.Level != 1 {
				t.Errorf("%s resolves to %v, want the top of the scale", f.Name, f.Level)
			}
		default:
			if f.Unavailable != "" {
				t.Errorf("%s is unavailable: %s", f.Name, f.Unavailable)
			}
		}
	}

	// A tree with no files is the other way reach cannot be computed: the share
	// one change touches is undefined rather than large.
	assessment, err = s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: areaID,
		Measurement: score.Measurement{LinesChanged: 3, FilesChanged: 1, FilesInTree: 0},
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := assessment.UnavailableFactors(); len(got) != 1 || got[0] != "change.reach" {
		t.Errorf("%v are unavailable, want change.reach alone", got)
	}
}

// TestAnItemWithNoAreaCannotBeScoredOnContext: an item may name no area, and the
// two factors that read one then say so rather than reading as low risk.
func TestAnItemWithNoAreaCannotBeScoredOnContext(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, err := item.NewDecomposition(pool).Create(ctx, decompositionActor, item.New{
		IntentID:  "in_a",
		ServiceID: serviceID,
		Branch:    "item/no-area",
	})
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	if _, err = artifact.NewStore(pool).SubmitImplementation(ctx, implementerActor,
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: modelVersion}, it.ID, "a commit"); err != nil {
		t.Fatalf("submitting the implementation: %v", err)
	}

	assessment, err := s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID,
		Measurement:     score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10},
		CriteriaInForce: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := assessment.UnavailableFactors(); len(got) != 2 {
		t.Errorf("%v are unavailable, want the two factors that read an area", got)
	}
	if assessment.Number != 1 {
		t.Errorf("the number is %v, want the top of the scale", assessment.Number)
	}
}

// TestAnItemWithNoImplementationHasNoAuthorToHoldAPriorOn: the prior is computed
// from that author's own work, so an item with no implementation version leaves
// the factor unavailable rather than reading as an author with no history.
func TestAnItemWithNoImplementationHasNoAuthorToHoldAPriorOn(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, err := item.NewDecomposition(pool).Create(ctx, decompositionActor, item.New{
		IntentID: "in_a", ServiceID: serviceID, AreaID: areaID, Branch: "item/unbuilt",
	})
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	assessment, err := s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: areaID,
		Measurement:     score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10},
		CriteriaInForce: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := assessment.UnavailableFactors(); len(got) != 1 || got[0] != "author.prior" {
		t.Errorf("%v are unavailable, want author.prior alone", got)
	}
}

// TestAFailedCriterionIsTheTopOfItsScale: the gate above is what rejects a
// failing build; a number that read low on one would be the score disagreeing
// with a run.
func TestAFailedCriterionIsTheTopOfItsScale(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, _ := decomposeItem(t, ctx, pool, "item/one")
	assessment, err := s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: areaID,
		Measurement:     score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10},
		CriteriaInForce: 4, CriteriaFailed: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := levelOf(t, assessment, "change.test_coverage"); got != 1 {
		t.Errorf("a build with a failed criterion reads coverage %v, want the top of the scale", got)
	}
}

// TestAChangeNamingNoItemIsACallersDefect: every firing has an item and a
// service, so a blank is not a factor to mark unavailable.
func TestAChangeNamingNoItemIsACallersDefect(t *testing.T) {
	ctx, _, s := newScore(t)

	if _, err := s.Assess(ctx, score.Change{ServiceID: serviceID}); err == nil {
		t.Error("Assess over a change naming no item was accepted")
	}
	if _, err := s.Assess(ctx, score.Change{ItemID: "it_a"}); err == nil {
		t.Error("Assess over a change naming no service was accepted")
	}
}
