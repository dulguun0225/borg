// This file is the reads' tests: [window.Get], [window.ForRelease],
// [window.ForDeploy], [window.AllOpen], [window.CountOpen],
// [window.ClosedWithoutFailing], and [window.Closed]. db_test.go holds the
// package comment, the shared test fixtures — [newTable], [inSchema],
// [opening], [closedOn], [readFor] — and the writer's own tests.
package window_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// TestCountOpenAllOpenAndClosedWithoutFailingSeeOnlyWhatMatches opens four windows
// of one service and closes three of them at three different exits, so the
// three reads see three different subsets of the same rows.
func TestCountOpenAllOpenAndClosedWithoutFailingSeeOnlyWhatMatches(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	openOne := func() window.Window {
		o := opening()
		o.ServiceID = serviceID
		win, err := w.Open(ctx, healthMonitor, o)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return win
	}

	stillOpen := openOne()
	harmed := openOne()
	if _, err := w.Close(ctx, harmed.ID, window.ExitFailed, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	passed := openOne()
	if _, err := w.Close(ctx, passed.ID, window.ExitPassed, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	atCap := openOne()
	if _, err := w.Close(ctx, atCap.ID, window.ExitTimedOut, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if count, err := window.CountOpen(ctx, pool, serviceID); err != nil || count != 1 {
		t.Errorf("CountOpen = %d, %v, want 1", count, err)
	}
	allOpen, err := window.AllOpen(ctx, pool, serviceID)
	if err != nil {
		t.Fatalf("AllOpen: %v", err)
	}
	if len(allOpen) != 1 || allOpen[0].ID != stillOpen.ID {
		t.Errorf("AllOpen = %+v, want just %s", allOpen, stillOpen.ID)
	}

	withoutHarm, err := window.ClosedWithoutFailing(ctx, pool, serviceID)
	if err != nil {
		t.Fatalf("ClosedWithoutFailing: %v", err)
	}
	if len(withoutHarm) != 2 {
		t.Fatalf("ClosedWithoutFailing = %+v, want the clean window and the cap window", withoutHarm)
	}
	seen := map[string]bool{withoutHarm[0].ID: true, withoutHarm[1].ID: true}
	if !seen[passed.ID] || !seen[atCap.ID] {
		t.Errorf("ClosedWithoutFailing = %+v, want %s and %s", withoutHarm, passed.ID, atCap.ID)
	}
}

func TestForReleaseAndForDeployAreFalseWhereNothingMatches(t *testing.T) {
	ctx, pool, _ := newTable(t)

	if _, found, err := window.ForRelease(ctx, pool, record.NewID("rel")); err != nil || found {
		t.Errorf("ForRelease on a release never watched = found %v, %v", found, err)
	}
	if _, found, err := window.ForDeploy(ctx, pool, record.NewID("dep")); err != nil || found {
		t.Errorf("ForDeploy on a deploy that opened no window = found %v, %v", found, err)
	}
}

// TestAHeldOutWindowIsToldFromOneWithNoBaseline: both run to the cap and neither
// may be passed, and a reader with only passed_available could not tell which was
// which. The score's sample is why the second field exists.
func TestAHeldOutWindowIsToldFromOneWithNoBaseline(t *testing.T) {
	ctx, pool, w := newTable(t)

	firstRelease := opening()
	firstRelease.PassedAvailable = false
	first, err := w.Open(ctx, healthMonitor, firstRelease)
	if err != nil {
		t.Fatalf("Open over a release with no baseline: %v", err)
	}

	sampled := opening()
	sampled.PassedAvailable, sampled.HeldOut = false, true
	held, err := w.Open(ctx, healthMonitor, sampled)
	if err != nil {
		t.Fatalf("Open over a held-out release: %v", err)
	}

	if first.HeldOut {
		t.Error("a release with no baseline reads as held out")
	}
	if !held.HeldOut {
		t.Error("a held-out release does not read as held out")
	}
	read, err := window.Get(ctx, pool, held.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.HeldOut || read.PassedAvailable {
		t.Errorf("the window reads back as %+v, want held out with clean unavailable", read)
	}
	for _, w := range []window.Window{first, held} {
		if w.PassedAvailable {
			t.Error("a window that runs to the cap says the passed exit is available to it")
		}
	}
}

// TestClosedReadsEveryClosedWindowAndNoOpenOne: the read the score learns from,
// which is the one here that is not per service — the subjects it learns about are
// the services the windows name.
func TestClosedReadsEveryClosedWindowAndNoOpenOne(t *testing.T) {
	ctx, pool, w := newTable(t)

	var closedIDs []string
	for _, exit := range window.Exits {
		o := opening()
		opened, err := w.Open(ctx, healthMonitor, o)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, err := w.Close(ctx, opened.ID, exit, readFor(exit)); err != nil {
			t.Fatalf("Close at %s: %v", exit, err)
		}
		closedIDs = append(closedIDs, opened.ID)
	}
	stillOpen, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open one more: %v", err)
	}

	closed, err := window.Closed(ctx, pool)
	if err != nil {
		t.Fatalf("Closed: %v", err)
	}
	if len(closed) != len(closedIDs) {
		t.Fatalf("Closed read %d windows, want the %d that closed", len(closed), len(closedIDs))
	}
	for _, c := range closed {
		if c.ID == stillOpen.ID {
			t.Error("Closed read a window that is still open, and an open window has no exit to learn from")
		}
		if c.Exit == "" {
			t.Errorf("Closed read %s with no exit", c.ID)
		}
	}
}
