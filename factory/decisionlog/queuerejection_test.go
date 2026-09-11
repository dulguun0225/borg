package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestAQueueRejectionNamesItsReadingAndTheMovedRelease is C0061: one row
// naming which of the queue's readings it was and the moved release where
// there is one.
func TestAQueueRejectionNamesItsReadingAndTheMovedRelease(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	row, err := log.AppendQueueRejection(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "queue_rejection/1",
		Reading: "a dependency's release moved between the two runs", MovedRelease: "rel_00000000000000000000000000000001",
	})
	if err != nil {
		t.Fatalf("AppendQueueRejection with a reading and a moved release: %v", err)
	}
	if row.Reading != "a dependency's release moved between the two runs" ||
		row.MovedRelease != "rel_00000000000000000000000000000001" {
		t.Errorf("the rejection is %+v, want the reading and the moved release", row)
	}

	noMove, err := log.AppendQueueRejection(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "queue_rejection/1",
		Reading: "a candidate that no longer passes against what it will actually ship beside",
	})
	if err != nil {
		t.Fatalf("AppendQueueRejection with no moved release: %v", err)
	}
	if noMove.MovedRelease != "" {
		t.Errorf("a rejection naming no move stored %q", noMove.MovedRelease)
	}

	if _, err := log.AppendQueueRejection(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "queue_rejection/1",
	}); !errors.Is(err, decisionlog.ErrReadingMissing) {
		t.Errorf("a rejection naming no reading: %v, want ErrReadingMissing", err)
	}

	if _, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "page_event/1", Reading: "x",
	}); !errors.Is(err, decisionlog.ErrReadingRefused) {
		t.Errorf("a page event naming a reading: %v, want ErrReadingRefused", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
