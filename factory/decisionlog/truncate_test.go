package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
)

// TestTruncateRemovesTheOldestRowsAndKeepsTheHead appends a truncation row,
// removes every row before its boundary, and leaves Verify able to walk the
// remainder whole afterwards.
func TestTruncateRemovesTheOldestRowsAndKeepsTheHead(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	appended := appendThreeOpenings(ctx, t, log, reader)
	boundary := appended[1] // the second row becomes the new checkpoint

	truncation, err := log.Truncate(ctx, decisionlog.Cut{
		Actor: owner, Retention: "30d", Boundary: boundary.ID,
		PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("Truncate: %v", err)
	}
	if truncation.Shape != decisionlog.ShapeTruncation {
		t.Errorf("Truncate appended shape %q, want %q", truncation.Shape, decisionlog.ShapeTruncation)
	}

	rows, err := reader.Read(ctx, owner)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// The first appended opening is gone; the second (the boundary) and the
	// third remain, followed by the truncation row and the read events this
	// test's own Verify and Read calls just appended.
	if len(rows) < 2 || rows[0].ID != boundary.ID {
		t.Fatalf("the log after truncation starts with %+v, want the boundary %s first", rows, boundary.ID)
	}
	for _, row := range rows {
		if row.ID == appended[0].ID {
			t.Fatalf("the row before the boundary is still in the log: %+v", row)
		}
	}

	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("Verify after a truncation: %v", err)
	}
}

// TestTruncateRefusesAnEmptyOrUnknownBoundary checks the two refusals
// [decisionlog.Writer.Truncate] states.
func TestTruncateRefusesAnEmptyOrUnknownBoundary(t *testing.T) {
	ctx, _, log, _ := newLog(t)

	if _, err := log.Truncate(ctx, decisionlog.Cut{
		Actor: owner, Retention: "30d", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	}); !errors.Is(err, decisionlog.ErrBoundaryEmpty) {
		t.Errorf("a cut naming no boundary: %v, want ErrBoundaryEmpty", err)
	}
	if _, err := log.Truncate(ctx, decisionlog.Cut{
		Actor: owner, Retention: "30d", Boundary: "dl_00112233445566778899aabbccddeeff",
		PolicyVersion: "policy-1", ScoreVersion: "score-1",
	}); !errors.Is(err, decisionlog.ErrBoundaryUnknown) {
		t.Errorf("a cut naming a boundary that does not exist: %v, want ErrBoundaryUnknown", err)
	}
}

// TestATruncatedTailIsNotCaughtByVerifyAlone records what the chain does not
// prove on its own, and fails if that ever stops being the truth. Verify
// walks forward from the checkpoint and stops at whatever row is last, so a
// tail removed by hand — deleting rows directly, the way a truncation
// bypassed through this package would — leaves an unbroken chain of the rows
// that remain, and Verify returns nil. What actually catches it is the drift
// detector's recorded head, read from its own store outside this package;
// this test is what tells whoever removes that limit from verify.go that it
// was known.
func TestATruncatedTailIsNotCaughtByVerifyAlone(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	// Three openings appended with no read in between, so the third is the
	// tail this test removes.
	var appended []decisionlog.Row
	for _, payload := range []string{"first", "second", "third"} {
		row, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
			Actor: gate, Payload: payload, FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
		})
		if err != nil {
			t.Fatalf("AppendDecisionOpen(%q): %v", payload, err)
		}
		appended = append(appended, row)
	}
	tail := appended[len(appended)-1]

	if _, err := pool.Exec(ctx, `delete from decision_log where seq = $1`, tail.Seq); err != nil {
		t.Fatalf("removing the last row: %v", err)
	}

	// The freed prev_hash is why a truncation is not merely undetected: the
	// log goes on accepting appends as though the removed row never was.
	replacement, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "written over the removed tail", FormatVersion: "decision/1",
		PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("appending over the removed tail: %v", err)
	}
	if replacement.PrevHash != appended[len(appended)-2].Hash {
		t.Fatalf("the replacement names predecessor %s, want the row before the removed one", replacement.PrevHash)
	}
	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("Verify = %v; the head is anchored now, so this test has outlived the limit it records", err)
	}
}
