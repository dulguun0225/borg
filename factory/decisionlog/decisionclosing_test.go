package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestAClosingClosesAnOpeningAndNothingElse is the closing's naming rule,
// checked at the methods: a closing names an open event that exists, and no
// other kind of row names anything.
func TestAClosingClosesAnOpeningAndNothingElse(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	page, err := log.AppendPageEvent(ctx, decisionlog.Entry{Actor: gate, Payload: "a page", FormatVersion: "page_event/1"})
	if err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}

	t.Run("a closing names something", func(t *testing.T) {
		entry := decisionlog.Entry{Actor: owner, Payload: "a verdict over nothing", FormatVersion: "decision/1", Verdict: "approve"}
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrClosesMissing) {
			t.Errorf("a closing naming no row: %v, want ErrClosesMissing", err)
		}
	})

	t.Run("nothing else names anything", func(t *testing.T) {
		naming := decisionlog.Entry{
			Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
			Closes: opening.ID,
		}
		if _, err := log.AppendDecisionOpen(ctx, naming); !errors.Is(err, decisionlog.ErrClosesRefused) {
			t.Errorf("an opening naming a row: %v, want ErrClosesRefused", err)
		}
		pageNaming := decisionlog.Entry{Actor: gate, Payload: "x", FormatVersion: "page_event/1", Closes: opening.ID}
		if _, err := log.AppendPageEvent(ctx, pageNaming); !errors.Is(err, decisionlog.ErrClosesRefused) {
			t.Errorf("a page event naming a row: %v, want ErrClosesRefused", err)
		}
		waitNaming := decisionlog.Entry{Actor: gate, Payload: "x", FormatVersion: "wait/1", Closes: opening.ID}
		if _, err := log.AppendWaitOpen(ctx, waitNaming); !errors.Is(err, decisionlog.ErrClosesRefused) {
			t.Errorf("a wait's opening naming a row: %v, want ErrClosesRefused", err)
		}
	})

	t.Run("the named row is an opening", func(t *testing.T) {
		entry := decisionlog.Entry{
			Actor: owner, Payload: "a verdict", FormatVersion: "decision/1", Verdict: "approve",
			Closes: "dl_00112233445566778899aabbccddeeff",
		}
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming no row that exists: %v, want ErrNotAnOpening", err)
		}
		entry.Closes = page.ID
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming a page event: %v, want ErrNotAnOpening", err)
		}

		closing, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
			Actor: owner, Payload: "a verdict", FormatVersion: "decision/1", Verdict: "approve", Closes: opening.ID,
		})
		if err != nil {
			t.Fatalf("AppendDecisionClose: %v", err)
		}
		entry.Closes = closing.ID
		if _, err := log.AppendDecisionClose(ctx, entry); !errors.Is(err, decisionlog.ErrNotAnOpening) {
			t.Errorf("a closing naming a closing: %v, want ErrNotAnOpening", err)
		}
	})

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestOneOpeningTakesOneClosing is what the partial unique indexes are for.
// The method checks for a second ending proactively; the index refuses it
// again where a row reaches the store around the method.
func TestOneOpeningTakesOneClosing(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "the verdict", FormatVersion: "decision/1", Verdict: "approve", Closes: opening.ID,
	}); err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
	}

	_, err = log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "a second verdict", FormatVersion: "decision/1", Verdict: "approve", Closes: opening.ID,
	})
	if !errors.Is(err, decisionlog.ErrAlreadyEnded) {
		t.Errorf("a second closing through the method: %v, want ErrAlreadyEnded", err)
	}

	second := aRow()
	second.FormatVersion = "decision/1"
	second.Shape = decisionlog.ShapeDecision
	second.Part = decisionlog.PartClose
	second.Closes = opening.ID
	second.Verdict = "approve"
	if got, want := refusedBy(t, insertAround(ctx, pool, second)), "decision_log_one_closing"; got != want {
		t.Errorf("a second closing around the method was refused by %q, want %q", got, want)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestAnAbandonmentEndsAnOpeningAndRefusesASecondEnding checks that a closing
// after an abandonment, and an abandonment after a closing, are both
// refused.
func TestAnAbandonmentEndsAnOpeningAndRefusesASecondEnding(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	abandonment, err := log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor: gate, Payload: "stopped at the attempt limit", FormatVersion: "decision/1",
		Closes: opening.ID, Reason: "the item stopped at the attempt limit",
	})
	if err != nil {
		t.Fatalf("AppendDecisionAbandonment: %v", err)
	}
	if abandonment.Part != decisionlog.PartAbandonment || abandonment.Reason == "" {
		t.Errorf("the abandonment is %+v, want part %q and a reason", abandonment, decisionlog.PartAbandonment)
	}

	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "too late", FormatVersion: "decision/1", Verdict: "approve", Closes: opening.ID,
	}); !errors.Is(err, decisionlog.ErrAlreadyEnded) {
		t.Errorf("a closing after an abandonment: %v, want ErrAlreadyEnded", err)
	}

	second, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "a second firing", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", Closes: second.ID, Reason: "dropped",
	}); err != nil {
		t.Fatalf("AppendDecisionAbandonment: %v", err)
	}
	if _, err := log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", Closes: second.ID, Reason: "dropped again",
	}); !errors.Is(err, decisionlog.ErrAlreadyEnded) {
		t.Errorf("a second abandonment: %v, want ErrAlreadyEnded", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestAnAcknowledgementTwiceFromOneHumanIsRefused is the per-human unique
// index, reached through the method and around it.
func TestAnAcknowledgementTwiceFromOneHumanIsRefused(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "the firing", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionAcknowledgement(ctx, decisionlog.Entry{
		Actor: owner, FormatVersion: "decision/1", Closes: opening.ID,
	}); err != nil {
		t.Fatalf("AppendDecisionAcknowledgement: %v", err)
	}
	if _, err := log.AppendDecisionAcknowledgement(ctx, decisionlog.Entry{
		Actor: otherHuman, FormatVersion: "decision/1", Closes: opening.ID,
	}); err != nil {
		t.Fatalf("a second human's own acknowledgement: %v", err)
	}
	if _, err := log.AppendDecisionAcknowledgement(ctx, decisionlog.Entry{
		Actor: owner, FormatVersion: "decision/1", Closes: opening.ID,
	}); !errors.Is(err, decisionlog.ErrAlreadyAcknowledged) {
		t.Errorf("owner's second acknowledgement: %v, want ErrAlreadyAcknowledged", err)
	}
	if _, err := log.AppendDecisionAcknowledgement(ctx, decisionlog.Entry{
		Actor: gate, FormatVersion: "decision/1", Closes: opening.ID,
	}); !errors.Is(err, decisionlog.ErrAcknowledgementNotHuman) {
		t.Errorf("a component's acknowledgement: %v, want ErrAcknowledgementNotHuman", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestARejectOrAHoldWithNoReasonIsRefused checks [decisionlog.ErrReasonMissing]
// at the method and reason_required at the store.
func TestARejectOrAHoldWithNoReasonIsRefused(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	for _, verdict := range []string{"reject", "hold"} {
		opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
			Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
		})
		if err != nil {
			t.Fatalf("AppendDecisionOpen: %v", err)
		}
		if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
			Actor: owner, FormatVersion: "decision/1", Verdict: verdict, Closes: opening.ID,
		}); !errors.Is(err, decisionlog.ErrReasonMissing) {
			t.Errorf("a %s with no reason through the method: %v, want ErrReasonMissing", verdict, err)
		}

		closing := aRow()
		closing.FormatVersion = "decision/1"
		closing.Shape = decisionlog.ShapeDecision
		closing.Part = decisionlog.PartClose
		closing.Closes = opening.ID
		closing.Verdict = verdict
		if got, want := refusedBy(t, insertAround(ctx, pool, closing)), "reason_required"; got != want {
			t.Errorf("a %s with no reason around the method was refused by %q, want %q", verdict, got, want)
		}
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
