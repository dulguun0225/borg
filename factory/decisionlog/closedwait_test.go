package decisionlog_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestPendingWaitsAndClosedWaits is C0979: a first row with no second is what
// Work reads as still holding, and the pair is what makes how long it held a
// subtraction — the same reader read.go and closed.go already give decision
// openings, given here to waits.
func TestPendingWaitsAndClosedWaits(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	stillOpen, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"waiting_on":"a"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("AppendWaitOpen: %v", err)
	}
	ended, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"waiting_on":"b"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("AppendWaitOpen: %v", err)
	}
	closing, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"condition":"gone"}`, FormatVersion: "wait/1", Closes: ended.ID,
	})
	if err != nil {
		t.Fatalf("AppendWaitClose: %v", err)
	}

	pending, err := reader.PendingWaits(ctx, ownerReading)
	if err != nil {
		t.Fatalf("PendingWaits: %v", err)
	}
	if len(pending) != 1 || pending[0].ID != stillOpen.ID {
		t.Errorf("PendingWaits = %v, want just %s", idsOf(pending), stillOpen.ID)
	}

	closedWaits, err := reader.ClosedWaits(ctx, ownerReading)
	if err != nil {
		t.Fatalf("ClosedWaits: %v", err)
	}
	if len(closedWaits) != 1 || closedWaits[0].OpenEvent.ID != ended.ID || closedWaits[0].CloseEvent.ID != closing.ID {
		t.Errorf("ClosedWaits = %+v, want the one pair %s/%s", closedWaits, ended.ID, closing.ID)
	}
}

func idsOf(rows []decisionlog.Row) []string {
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}
