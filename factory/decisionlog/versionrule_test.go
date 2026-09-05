package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestOnlyADecisionNamesTheVersions checks both places the rule is enforced:
// the four append methods, and the CHECK constraint that catches a row
// written around them.
func TestOnlyADecisionNamesTheVersions(t *testing.T) {
	ctx, pool, log := newLog(t)

	t.Run("the methods refuse", func(t *testing.T) {
		withPolicy := decisionlog.Entry{Actor: gate, Payload: "x", PolicyVersion: "policy-1"}
		withScore := decisionlog.Entry{Actor: gate, Payload: "x", ScoreVersion: "score-1"}
		closingWithPolicy := withPolicy
		closingWithPolicy.Closes = "dl_00112233445566778899aabbccddeeff"
		closingWithScore := withScore
		closingWithScore.Closes = closingWithPolicy.Closes
		refused := map[string]func() error{
			"page event with a policy version": func() error { _, err := log.AppendPageEvent(ctx, withPolicy); return err },
			"page event with a score version":  func() error { _, err := log.AppendPageEvent(ctx, withScore); return err },
			"wait with a policy version":       func() error { _, err := log.AppendWait(ctx, withPolicy); return err },
			"wait with a score version":        func() error { _, err := log.AppendWait(ctx, withScore); return err },
			"closing with a policy version": func() error {
				_, err := log.AppendDecisionClose(ctx, closingWithPolicy)
				return err
			},
			"closing with a score version": func() error {
				_, err := log.AppendDecisionClose(ctx, closingWithScore)
				return err
			},
		}
		for name, append := range refused {
			if err := append(); !errors.Is(err, decisionlog.ErrVersionsRefused) {
				t.Errorf("%s: %v, want ErrVersionsRefused", name, err)
			}
		}

		missing := map[string]decisionlog.Entry{
			"neither version": {Actor: gate, Payload: "x"},
			"no score":        {Actor: gate, Payload: "x", PolicyVersion: "policy-1"},
			"no policy":       {Actor: gate, Payload: "x", ScoreVersion: "score-1"},
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
		page.Shape = decisionlog.ShapePageEvent
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
		opening.Shape = decisionlog.ShapeDecision
		opening.Part = decisionlog.PartOpen
		if got := refusedBy(t, insertAround(ctx, pool, opening)); got != want {
			t.Errorf("an opening naming no version was refused by %q, want %q", got, want)
		}

		closing := aRow()
		closing.Shape = decisionlog.ShapeDecision
		closing.Part = decisionlog.PartClose
		closing.Closes = "dl_00112233445566778899aabbccddeeff"
		closing.PolicyVersion = "policy-1"
		closing.ScoreVersion = "score-1"
		if got := refusedBy(t, insertAround(ctx, pool, closing)); got != want {
			t.Errorf("a closing naming both versions was refused by %q, want %q", got, want)
		}

		unknown := aRow()
		unknown.Shape = "veto"
		if got, want := refusedBy(t, insertAround(ctx, pool, unknown)), "shape_known"; got != want {
			t.Errorf("a fourth shape was refused by %q, want %q", got, want)
		}
	})

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
