package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestOnlyADecisionsOpeningOrATruncationNamesTheVersions checks both places
// the rule is enforced: the append methods, and the CHECK constraint that
// catches a row written around them.
func TestOnlyADecisionsOpeningOrATruncationNamesTheVersions(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	t.Run("the methods refuse", func(t *testing.T) {
		withPolicy := decisionlog.Entry{Actor: gate, Payload: "x", FormatVersion: "page_event/1", PolicyVersion: "policy-1"}
		withScore := decisionlog.Entry{Actor: gate, Payload: "x", FormatVersion: "wait/1", ScoreVersion: "score-1"}
		closingWithPolicy := decisionlog.Entry{
			Actor: gate, Payload: "x", FormatVersion: "decision/1", Verdict: "approve",
			Closes: "dl_00112233445566778899aabbccddeeff", PolicyVersion: "policy-1",
		}
		closingWithScore := closingWithPolicy
		closingWithScore.PolicyVersion, closingWithScore.ScoreVersion = "", "score-1"

		refused := map[string]func() error{
			"page event with a policy version": func() error { _, err := log.AppendPageEvent(ctx, withPolicy); return err },
			"wait's opening with a score version": func() error {
				_, err := log.AppendWaitOpen(ctx, withScore)
				return err
			},
			"closing with a policy version": func() error {
				_, err := log.AppendDecisionClose(ctx, closingWithPolicy)
				return err
			},
			"closing with a score version": func() error {
				_, err := log.AppendDecisionClose(ctx, closingWithScore)
				return err
			},
		}
		for name, appendFn := range refused {
			if err := appendFn(); !errors.Is(err, decisionlog.ErrVersionsRefused) {
				t.Errorf("%s: %v, want ErrVersionsRefused", name, err)
			}
		}

		missing := map[string]decisionlog.Entry{
			"neither version": {Actor: gate, Payload: "x", FormatVersion: "decision/1"},
			"no score":        {Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1"},
			"no policy":       {Actor: gate, Payload: "x", FormatVersion: "decision/1", ScoreVersion: "score-1"},
		}
		for name, entry := range missing {
			if _, err := log.AppendDecisionOpen(ctx, entry); !errors.Is(err, decisionlog.ErrVersionsMissing) {
				t.Errorf("an opening with %s: %v, want ErrVersionsMissing", name, err)
			}
		}
	})

	t.Run("the store refuses", func(t *testing.T) {
		const want = "versions_match_part"

		page := aRow()
		page.FormatVersion = "page_event/1"
		page.Shape = decisionlog.ShapePageEvent
		page.Part = ""
		page.PolicyVersion = "policy-1"
		if got := refusedBy(t, insertAround(ctx, pool, page)); got != want {
			t.Errorf("a page event naming a policy version was refused by %q, want %q", got, want)
		}

		wait := aRow()
		wait.ScoreVersion = "score-1"
		if got := refusedBy(t, insertAround(ctx, pool, wait)); got != want {
			t.Errorf("a wait naming a score version was refused by %q, want %q", got, want)
		}

		opening := aRow()
		opening.FormatVersion = "decision/1"
		opening.Shape = decisionlog.ShapeDecision
		opening.Part = decisionlog.PartOpen
		if got := refusedBy(t, insertAround(ctx, pool, opening)); got != want {
			t.Errorf("an opening naming no version was refused by %q, want %q", got, want)
		}

		closing := aRow()
		closing.FormatVersion = "decision/1"
		closing.Shape = decisionlog.ShapeDecision
		closing.Part = decisionlog.PartClose
		closing.Closes = "dl_00112233445566778899aabbccddeeff"
		closing.Verdict = "approve"
		closing.PolicyVersion = "policy-1"
		closing.ScoreVersion = "score-1"
		if got := refusedBy(t, insertAround(ctx, pool, closing)); got != want {
			t.Errorf("a closing naming both versions was refused by %q, want %q", got, want)
		}

		truncationNoVersions := aRow()
		truncationNoVersions.FormatVersion = "truncation/1"
		truncationNoVersions.Shape = decisionlog.ShapeTruncation
		truncationNoVersions.Part = ""
		if got := refusedBy(t, insertAround(ctx, pool, truncationNoVersions)); got != want {
			t.Errorf("a truncation naming no version was refused by %q, want %q", got, want)
		}

		// An eleventh shape violates format_version_matches_shape too, since
		// no pair in that list names it; the store reports that constraint
		// rather than shape_known for this row, and TestDDLListsEveryShape
		// is what actually keeps shape_known and Shapes in agreement.
		unknown := aRow()
		unknown.FormatVersion = "veto/1"
		unknown.Shape = "veto"
		unknown.Part = ""
		if got, want := refusedBy(t, insertAround(ctx, pool, unknown)), "format_version_matches_shape"; got != want {
			t.Errorf("an eleventh shape was refused by %q, want %q", got, want)
		}
	})

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
