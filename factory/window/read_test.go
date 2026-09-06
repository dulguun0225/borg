// This file is the reads' tests: [window.Get], [window.ForRelease],
// [window.ForDeploy], [window.AllOpen], [window.CountOpen],
// [window.ClosedPassedOrTimedOut], [window.LastKnownGood] and [window.Closed],
// and the mark a named human at Ops writes. db_test.go holds the package
// comment, the shared test fixtures — [newTable], [inSchema], [opening],
// [closedOn], [closingFor] — and the writer's own tests.
package window_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// TestTheThreeReadsOverOneServiceSeeOnlyWhatMatches opens four windows of one
// service and closes three of them at three different exits, so the reads see
// three different subsets of the same rows.
func TestTheThreeReadsOverOneServiceSeeOnlyWhatMatches(t *testing.T) {
	ctx, pool, w, _ := newTable(t)
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
	failed := openOne()
	if _, err := w.Close(ctx, failed.ID, window.ExitFailed, closedOn()); err != nil {
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

	returnable, err := window.ClosedPassedOrTimedOut(ctx, pool, serviceID)
	if err != nil {
		t.Fatalf("ClosedPassedOrTimedOut: %v", err)
	}
	if len(returnable) != 2 {
		t.Fatalf("ClosedPassedOrTimedOut = %+v, want the passed window and the one at its cap", returnable)
	}
	seen := map[string]bool{returnable[0].ID: true, returnable[1].ID: true}
	if !seen[passed.ID] || !seen[atCap.ID] {
		t.Errorf("ClosedPassedOrTimedOut = %+v, want %s and %s", returnable, passed.ID, atCap.ID)
	}

	last, found, err := window.LastKnownGood(ctx, pool, serviceID)
	if err != nil || !found {
		t.Fatalf("LastKnownGood = found %v, %v", found, err)
	}
	if last.ID != atCap.ID {
		t.Errorf("LastKnownGood = %s, want the newest close %s", last.ID, atCap.ID)
	}
}

// TestASkippedWindowIsNotSomethingToReturnTo is the exit the two queries descend
// past: nothing is left running the release's build.
func TestASkippedWindowIsNotSomethingToReturnTo(t *testing.T) {
	ctx, pool, w, _ := newTable(t)
	serviceID := record.NewID("svc")

	o := opening()
	o.ServiceID = serviceID
	opened, err := w.Open(ctx, healthMonitor, o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitSkipped, window.Closing{}); err != nil {
		t.Fatalf("Close skipped: %v", err)
	}
	returnable, err := window.ClosedPassedOrTimedOut(ctx, pool, serviceID)
	if err != nil {
		t.Fatalf("ClosedPassedOrTimedOut: %v", err)
	}
	if len(returnable) != 0 {
		t.Errorf("ClosedPassedOrTimedOut = %+v, want none: a skipped release has nothing left running", returnable)
	}
	if _, found, err := window.LastKnownGood(ctx, pool, serviceID); err != nil || found {
		t.Errorf("LastKnownGood over a service whose only window was skipped = found %v, %v", found, err)
	}
}

func TestForReleaseAndForDeployAreFalseWhereNothingMatches(t *testing.T) {
	ctx, pool, _, _ := newTable(t)

	if _, found, err := window.ForRelease(ctx, pool, record.NewID("rel")); err != nil || found {
		t.Errorf("ForRelease on a release never watched = found %v, %v", found, err)
	}
	if _, found, err := window.ForDeploy(ctx, pool, record.NewID("dep")); err != nil || found {
		t.Errorf("ForDeploy on a deploy that opened no window = found %v, %v", found, err)
	}
}

// TestAHeldOutWindowIsToldFromOneWithNoControl: both run to the cap and neither
// may be passed, and a reader with only passed_available could not tell which
// was which. The score's sample is why the second field exists.
func TestAHeldOutWindowIsToldFromOneWithNoControl(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	firstRelease := opening()
	firstRelease.PassedAvailable = false
	first, err := w.Open(ctx, healthMonitor, firstRelease)
	if err != nil {
		t.Fatalf("Open over a release with no control: %v", err)
	}

	sampled := opening()
	sampled.PassedAvailable, sampled.HeldOut = false, true
	held, err := w.Open(ctx, healthMonitor, sampled)
	if err != nil {
		t.Fatalf("Open over a held-out release: %v", err)
	}

	if first.HeldOut {
		t.Error("a release with no control reads as held out")
	}
	if !held.HeldOut {
		t.Error("a held-out release does not read as held out")
	}
	read, err := window.Get(ctx, pool, held.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.HeldOut || read.PassedAvailable {
		t.Errorf("the window reads back as %+v, want held out with passed unavailable", read)
	}
	for _, w := range []window.Window{first, held} {
		if w.PassedAvailable {
			t.Error("a window that runs to the cap says the passed exit is available to it")
		}
	}
}

// TestClosedReadsEveryClosedWindowAndNoOpenOne: the read the score learns from,
// which is the one here that is not per service — the subjects it learns about
// are the services the windows name. It reads them at one boundary version:
// passed and failed under two constructions are not one currency, so a window
// closed under another version is not evidence a mechanism that learns may
// fold in.
func TestClosedReadsEveryClosedWindowAtTheVersionInForceAndNoOpenOne(t *testing.T) {
	ctx, pool, w, _ := newTable(t)

	var closedIDs []string
	for _, exit := range window.Exits {
		opened, err := w.Open(ctx, healthMonitor, opening())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, err := w.Close(ctx, opened.ID, exit, closingFor(exit)); err != nil {
			t.Fatalf("Close at %s: %v", exit, err)
		}
		closedIDs = append(closedIDs, opened.ID)
	}
	stillOpen, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open one more: %v", err)
	}

	// One more closed window under a boundary version this factory no longer
	// ships, written directly: nothing in this package opens a window at another
	// version, the construction being the factory's own.
	underAnotherVersion, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open one more: %v", err)
	}
	if _, err := w.Close(ctx, underAnotherVersion.ID, window.ExitPassed, closingFor(window.ExitPassed)); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := pool.Exec(ctx, `update `+window.Table+`
		set boundary_version = 'interval-paired-difference/v0' where id = $1`, underAnotherVersion.ID); err != nil {
		t.Fatalf("writing an older boundary version onto %s: %v", underAnotherVersion.ID, err)
	}

	closed, err := window.ClosedAtTheVersionInForce(ctx, pool)
	if err != nil {
		t.Fatalf("ClosedAtTheVersionInForce: %v", err)
	}
	for _, c := range closed {
		if c.ID == underAnotherVersion.ID {
			t.Error("a window closed under another boundary version was read as evidence")
		}
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

// TestTheMarkIsWrittenOnceByANamedHuman is the mark that a rollback was not
// caused by the release: a named human at Ops writes it against the rollback's
// deploy record, with the reason they state, and nothing writes a second.
func TestTheMarkIsWrittenOnceByANamedHuman(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	ops := record.Actor{Kind: record.KindHuman, Key: "ada", Basis: record.BasisClaimed}
	deployID, serviceID := record.NewID("dep"), record.NewID("svc")

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	mark, err := window.WriteMark(ctx, tx, token, ops, deployID, serviceID, "the zone lost its network under the release's instances")
	if err != nil {
		t.Fatalf("WriteMark: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if mark.DeployID != deployID || mark.Reason == "" || mark.Actor.Key != "ada" {
		t.Errorf("the mark = %+v, want the rollback, the reason, and the human", mark)
	}

	read, found, err := window.Marked(ctx, pool, deployID)
	if err != nil || !found {
		t.Fatalf("Marked = found %v, %v", found, err)
	}
	if read != mark {
		t.Errorf("Marked = %+v, want %+v", read, mark)
	}
	marks, err := window.Marks(ctx, pool)
	if err != nil || len(marks) != 1 {
		t.Errorf("Marks = %+v, %v, want the one mark", marks, err)
	}

	second, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = second.Rollback(ctx) }()
	if _, err := window.WriteMark(ctx, second, token, ops, deployID, serviceID, "again"); !errors.Is(err, window.ErrAlreadyMarked) {
		t.Errorf("a second mark on one rollback = %v, want ErrAlreadyMarked", err)
	}
}

// TestAComponentMayNotMarkARollback is the one judgment the design keeps a
// human's: a crossing is not proof that the release caused it, and only a named
// human at Ops says so.
func TestAComponentMayNotMarkARollback(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = window.WriteMark(ctx, tx, token, healthMonitor, record.NewID("dep"), record.NewID("svc"), "the comparison was confounded")
	if !errors.Is(err, window.ErrNotAHuman) {
		t.Errorf("WriteMark as the health monitor = %v, want ErrNotAHuman", err)
	}
}

func TestAMarkMissingSomethingIsRefused(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	ops := record.Actor{Kind: record.KindHuman, Key: "ada", Basis: record.BasisClaimed}

	for _, c := range []struct{ what, deployID, serviceID, reason string }{
		{"rollback", "", record.NewID("svc"), "a reason"},
		{"service", record.NewID("dep"), "", "a reason"},
		{"reason", record.NewID("dep"), record.NewID("svc"), ""},
	} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		_, err = window.WriteMark(ctx, tx, token, ops, c.deployID, c.serviceID, c.reason)
		if !errors.Is(err, window.ErrMarkIncomplete) {
			t.Errorf("WriteMark with no %s = %v, want ErrMarkIncomplete", c.what, err)
		}
		_ = tx.Rollback(ctx)
	}
	if _, found, err := window.Marked(ctx, pool, ""); err != nil || found {
		t.Errorf("Marked on no rollback at all = found %v, %v", found, err)
	}
}
