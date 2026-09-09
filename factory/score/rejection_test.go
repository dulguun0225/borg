package score

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

var reviewer = record.Actor{Kind: record.KindHuman, Key: "person:reviewer", Basis: record.BasisClaimed}

// rejected is one firing a human rejected, naming what they named.
func rejected(itemID, artifactID, named, at string) Firing {
	return Firing{
		OpenEvent:   OpenEvent{ItemID: itemID, ArtifactID: artifactID, Gate: "implementation", FactorSet: SetWithABuild},
		CloseEvent:  CloseEvent{Verdict: VerdictRejected, RejectionNamed: named},
		HumanClosed: true, ClosedBy: reviewer, At: at,
	}
}

// approvedBy is one firing a human approved, at a time after the rejection.
func approvedBy(itemID, artifactID, at string) Firing {
	return Firing{
		OpenEvent:   OpenEvent{ItemID: itemID, ArtifactID: artifactID, Gate: "implementation", FactorSet: SetWithABuild},
		CloseEvent:  CloseEvent{Verdict: VerdictApproved},
		HumanClosed: true, ClosedBy: reviewer, At: at,
	}
}

// TestARejectionMovesNothingUntilItHasResolved: the rejected version never
// ships, so a rework that then passed is consistent with the rejection having
// been right and with it having been a false alarm. What the record holds is how
// the rejection resolved, and the threshold reads it only once it has.
func TestARejectionMovesNothingUntilItHasResolved(t *testing.T) {
	unresolved := newEvidence()
	unresolved.firings = []Firing{rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z")}
	unresolved.contents = map[string]string{"av_1": "the missing check is absent"}
	unresolved.index()
	if got := unresolved.resolvedRejections()[0].Resolution; got != "" {
		t.Errorf("a rejection nothing has answered resolved as %q", got)
	}
	if unresolved.Outcome("it_a") == OutcomeBadly {
		t.Error("an unresolved rejection made the item's outcome badly")
	}

	for _, c := range []struct {
		what     string
		firings  []Firing
		contents map[string]string
		want     string
		moves    bool
		newLimit bool
	}{
		{
			what: "the re-authored version approved, differing by content digest",
			firings: []Firing{
				rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z"),
				approvedBy("it_a", "av_2", "2026-08-20T01:00:00Z"),
			},
			contents: map[string]string{
				"av_1": "the missing check is absent\nand the rest stands",
				"av_2": "the missing check is written\nand the rest stands",
			},
			want: ResolvedReAuthoredApproved, moves: true,
		},
		{
			// The re-authored version differs — the whole version's digest
			// moved — and the part the rejection named reads the same, which is
			// the false alarm the whole digest could not have found.
			what: "approval without differing there",
			firings: []Firing{
				rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z"),
				approvedBy("it_a", "av_2", "2026-08-20T01:00:00Z"),
			},
			contents: map[string]string{
				"av_1": "the missing check is absent\nand the rest stands",
				"av_2": "the missing check is absent\nand the rest was rewritten",
			},
			want: ResolvedApprovedUnchanged, moves: false,
		},
		{
			// A trail the store no longer holds the words of leaves the
			// rejection unresolved: it moves nothing, and reading it as a false
			// alarm would publish one against the human on nothing.
			what: "a re-authored version the store no longer holds",
			firings: []Firing{
				rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z"),
				approvedBy("it_a", "av_2", "2026-08-20T01:00:00Z"),
			},
			contents: map[string]string{"av_1": "the missing check is absent"},
			want:     "", moves: false,
		},
		{
			what: "a second rejection",
			firings: []Firing{
				rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z"),
				rejected("it_a", "av_2", "the missing check", "2026-08-20T01:00:00Z"),
			},
			contents: map[string]string{"av_1": "one", "av_2": "two"},
			want:     ResolvedRejectedAgain, moves: true,
		},
	} {
		e := newEvidence()
		e.firings = c.firings
		e.contents = c.contents
		e.index()
		got := e.resolvedRejections()[0]
		if got.Resolution != c.want {
			t.Errorf("%s resolved as %q, want %q", c.what, got.Resolution, c.want)
		}
		if got.MovesTheThreshold() != c.moves {
			t.Errorf("%s moves the threshold: %v, want %v", c.what, got.MovesTheThreshold(), c.moves)
		}
		if (e.Outcome("it_a") == OutcomeBadly) != c.moves {
			t.Errorf("%s made the item's outcome %s", c.what, e.Outcome("it_a"))
		}
	}

	// The fourth way: the item reaching the attempt limit.
	limit, _ := Starting(gatepolicy.AttemptLimit)
	stalled := newEvidence()
	stalled.firings = []Firing{rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z")}
	stalled.contents = map[string]string{"av_1": "the missing check is absent"}
	stalled.items = []item.Item{{ID: "it_a", AreaID: "ar_a", Stage: item.StageImplementation}}
	stalled.stages = []item.StageTotals{{ItemID: "it_a", Stage: item.StageImplementation, Attempts: int(limit.Value)}}
	stalled.index()
	if got := stalled.resolvedRejections()[0].Resolution; got != ResolvedAttemptLimit {
		t.Errorf("an item at the attempt limit resolved its rejection as %q, want %q", got, ResolvedAttemptLimit)
	}
}

// TestAFalseAlarmIsPublishedPerHumanAndMovesNothing: without it, rejecting would
// be the response that costs the person nothing, and at the same time a lever on
// the one parameter that decides how much human work the factory removes.
func TestAFalseAlarmIsPublishedPerHumanAndMovesNothing(t *testing.T) {
	e := newEvidence()
	e.firings = []Firing{
		rejected("it_a", "av_1", "the missing check", "2026-08-20T00:00:00Z"),
		approvedBy("it_a", "av_2", "2026-08-20T01:00:00Z"),
		rejected("it_b", "av_3", "the missing check", "2026-08-20T02:00:00Z"),
		approvedBy("it_b", "av_4", "2026-08-20T03:00:00Z"),
	}
	e.contents = map[string]string{
		"av_1": "the missing check is absent",
		"av_2": "the missing check is absent\nand the rest was rewritten",
		"av_3": "the missing check is absent",
		"av_4": "the missing check is written",
	}
	e.index()

	published := e.falseAlarms()
	if len(published) != 1 {
		t.Fatalf("the pass published %d false-alarm rows, want one per human", len(published))
	}
	if published[0].Human != reviewer.Key {
		t.Errorf("the row names %q, want the human who rejected", published[0].Human)
	}
	if published[0].Count != 1 || published[0].Rejections != 2 {
		t.Errorf("the row reads %+v, want one false alarm of two resolved rejections", published[0])
	}
	if e.Outcome("it_a") == OutcomeBadly {
		t.Error("a false alarm made the item's outcome badly, and it moves nothing")
	}
}
