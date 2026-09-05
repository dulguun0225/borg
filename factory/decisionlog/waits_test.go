package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestAWaitIsTwoRows is the pair _What a gate may change_ describes: the
// opening written when the condition is met, and the closing written when
// it is found gone, naming the opening.
func TestAWaitIsTwoRows(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"waiting_on":"an unreachable credential"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("AppendWaitOpen: %v", err)
	}
	if opening.Part != decisionlog.PartOpen || opening.Closes != "" {
		t.Errorf("the wait's opening is %+v, want part %q and no closes", opening, decisionlog.PartOpen)
	}

	closing, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"condition":"gone"}`, FormatVersion: "wait/1", Closes: opening.ID,
	})
	if err != nil {
		t.Fatalf("AppendWaitClose: %v", err)
	}
	if closing.Part != decisionlog.PartClose || closing.Closes != opening.ID {
		t.Errorf("the wait's closing is %+v, want part %q closing %q", closing, decisionlog.PartClose, opening.ID)
	}

	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

// TestAWaitCloseRefusesWhatItDoesNotName checks [decisionlog.ErrNotAnOpening]
// and [decisionlog.ErrAlreadyEnded] for a wait's closing.
func TestAWaitCloseRefusesWhatItDoesNotName(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	decisionOpening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "wait/1", Closes: "dl_00112233445566778899aabbccddeeff",
	}); !errors.Is(err, decisionlog.ErrNotAnOpening) {
		t.Errorf("closing a row that does not exist: %v, want ErrNotAnOpening", err)
	}
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "wait/1", Closes: decisionOpening.ID,
	}); !errors.Is(err, decisionlog.ErrNotAnOpening) {
		t.Errorf("closing a decision's opening as a wait: %v, want ErrNotAnOpening", err)
	}

	waitOpening, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("AppendWaitOpen: %v", err)
	}
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "wait/1", Closes: waitOpening.ID,
	}); err != nil {
		t.Fatalf("AppendWaitClose: %v", err)
	}
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "wait/1", Closes: waitOpening.ID,
	}); !errors.Is(err, decisionlog.ErrAlreadyEnded) {
		t.Errorf("a second closing on one wait: %v, want ErrAlreadyEnded", err)
	}

	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
