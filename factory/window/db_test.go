// The database tests of this package are in window_test rather than in
// window, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as "test window ->
// postgres".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package window_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// comparison is the one writer of watch windows, the way doc.go names it.
var comparison = record.Actor{Kind: record.KindComponent, Name: "comparison"}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *window.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m3_win_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	return ctx, pool, window.NewWriter(pool)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writer's statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// opening is a complete Opening over ids of its own, so a test that needs one
// or several does not repeat the six required fields and the three shares.
func opening() window.Opening {
	return window.Opening{
		DeployID:       record.NewID("dep"),
		ReleaseID:      record.NewID("rel"),
		ServiceID:      record.NewID("svc"),
		CleanAvailable: true,
		Size:           0.1,
		Confidence:     0.95,
		CapSeconds:     3600,
		Formula:        "wilson",
		PolicyVersion:  "pv_1",
		ScoreVersion:   "sv_1",
	}
}

func TestAWindowOpensWithEveryFieldIntact(t *testing.T) {
	ctx, pool, w := newTable(t)
	o := opening()

	opened, err := w.Open(ctx, comparison, o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.DeployID != o.DeployID || opened.ReleaseID != o.ReleaseID || opened.ServiceID != o.ServiceID {
		t.Errorf("Open = %+v, which does not name what it was opened over", opened)
	}
	if opened.CleanAvailable != o.CleanAvailable || opened.HeldOut != o.HeldOut ||
		opened.Size != o.Size || opened.Confidence != o.Confidence ||
		opened.CapSeconds != o.CapSeconds || opened.Formula != o.Formula ||
		opened.PolicyVersion != o.PolicyVersion || opened.ScoreVersion != o.ScoreVersion {
		t.Errorf("Open = %+v, does not carry the parameters it was given, %+v", opened, o)
	}
	if !opened.Open() {
		t.Error("a freshly opened window reads as closed")
	}
	if opened.Exit != "" || opened.ClosedAt != "" {
		t.Errorf("a freshly opened window has exit %q closed at %q, want both empty", opened.Exit, opened.ClosedAt)
	}
	if _, err := time.Parse(record.TimeLayout, opened.At); err != nil {
		t.Errorf("the window has timestamp %q: %v", opened.At, err)
	}

	read, err := window.Get(ctx, pool, opened.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != opened {
		t.Errorf("Get = %+v, want %+v", read, opened)
	}
}

func TestASecondWindowOverOneDeployIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	o := opening()
	if _, err := w.Open(ctx, comparison, o); err != nil {
		t.Fatalf("Open: %v", err)
	}

	again := opening()
	again.DeployID = o.DeployID
	if _, err := w.Open(ctx, comparison, again); err == nil {
		t.Error("a second window over one deploy was accepted")
	}
}

func TestASecondWindowOverOneReleaseIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	o := opening()
	if _, err := w.Open(ctx, comparison, o); err != nil {
		t.Fatalf("Open: %v", err)
	}

	again := opening()
	again.ReleaseID = o.ReleaseID
	if _, err := w.Open(ctx, comparison, again); err == nil {
		t.Error("a second window over one release was accepted")
	}
}

// TestAWindowClosesOnceAtExactlyOneOfTheFourExits closes a window of its own at
// each exit in turn, and checks Exit.Counts against what doc.go says it means:
// clean and cap leave a release the factory can return to, harm and swept do
// not.
func TestAWindowClosesOnceAtExactlyOneOfTheFourExits(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, exit := range window.Exits {
		opened, err := w.Open(ctx, comparison, opening())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		closed, err := w.Close(ctx, opened.ID, exit, readFor(exit))
		if err != nil {
			t.Fatalf("Close(%s): %v", exit, err)
		}
		if closed.Open() {
			t.Errorf("Close(%s) leaves the window open", exit)
		}
		if closed.Exit != exit {
			t.Errorf("Close(%s) recorded exit %q", exit, closed.Exit)
		}
		if _, err := time.Parse(record.TimeLayout, closed.ClosedAt); err != nil {
			t.Errorf("the closed time is %q: %v", closed.ClosedAt, err)
		}
		want := exit == window.ExitClean || exit == window.ExitCap
		if got := exit.Counts(); got != want {
			t.Errorf("%s.Counts() = %v, want %v", exit, got, want)
		}
	}
}

func TestASecondCloseOnOneWindowIsAlreadyClosed(t *testing.T) {
	ctx, _, w := newTable(t)
	opened, err := w.Open(ctx, comparison, opening())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitClean, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitHarm, closedOn()); !errors.Is(err, window.ErrAlreadyClosed) {
		t.Errorf("Close = %v, want ErrAlreadyClosed", err)
	}
}

func TestClosingAtAnExitOutsideExitsIsExitUnknown(t *testing.T) {
	ctx, _, w := newTable(t)
	opened, err := w.Open(ctx, comparison, opening())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.Exit("flaky"), closedOn()); !errors.Is(err, window.ErrExitUnknown) {
		t.Errorf("Close = %v, want ErrExitUnknown", err)
	}
}

// TestDDLListsEveryExit keeps the CHECK constraint and window.Exits from
// disagreeing, the way deploy/schema_test.go's TestDDLListsEveryStrategyAndStatus
// does for strategies and statuses: every value in window.Exits inserts
// cleanly around the writer, and a value outside it does not.
func TestDDLListsEveryExit(t *testing.T) {
	ctx, pool, _ := newTable(t)

	for _, exit := range window.Exits {
		o := opening()
		_, err := pool.Exec(ctx, `insert into `+window.Table+`
			(id, actor_kind, actor_name, at, deploy_id, release_id, service_id, clean_available, held_out,
			 size, confidence, cap_seconds, formula, policy_version, score_version, exit, closed_at,
			 closed_on_units, closed_on_failures, closed_on_baseline_units, closed_on_baseline_failures)
			values ($1, 'component', 'comparison', $2, $3, $4, $5, $6, false, $7, $8, $9, $10, $11, $12, $13, $14,
			 0, 0, 0, 0)`,
			record.NewID(window.IDPrefix), record.Now(), o.DeployID, o.ReleaseID, o.ServiceID, o.CleanAvailable,
			o.Size, o.Confidence, o.CapSeconds, o.Formula, o.PolicyVersion, o.ScoreVersion, string(exit), record.Now())
		if err != nil {
			t.Errorf("inserting exit %q, one of window.Exits, was refused: %v", exit, err)
		}
	}

	o := opening()
	_, err := pool.Exec(ctx, `insert into `+window.Table+`
		(id, actor_kind, actor_name, at, deploy_id, release_id, service_id, clean_available, held_out,
		 size, confidence, cap_seconds, formula, policy_version, score_version, exit, closed_at,
		 closed_on_units, closed_on_failures, closed_on_baseline_units, closed_on_baseline_failures)
		values ($1, 'component', 'comparison', $2, $3, $4, $5, $6, false, $7, $8, $9, $10, $11, $12, 'flaky', $13,
		 0, 0, 0, 0)`,
		record.NewID(window.IDPrefix), record.Now(), o.DeployID, o.ReleaseID, o.ServiceID, o.CleanAvailable,
		o.Size, o.Confidence, o.CapSeconds, o.Formula, o.PolicyVersion, o.ScoreVersion, record.Now())
	if err == nil {
		t.Error("the store accepted an exit outside window.Exits")
	}
}

// TestCountOpenAllOpenAndClosedWithoutHarmSeeOnlyWhatMatches opens four windows
// of one service and closes three of them at three different exits, so the
// three reads see three different subsets of the same rows.
func TestCountOpenAllOpenAndClosedWithoutHarmSeeOnlyWhatMatches(t *testing.T) {
	ctx, pool, w := newTable(t)
	serviceID := record.NewID("svc")

	openOne := func() window.Window {
		o := opening()
		o.ServiceID = serviceID
		win, err := w.Open(ctx, comparison, o)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		return win
	}

	stillOpen := openOne()
	harmed := openOne()
	if _, err := w.Close(ctx, harmed.ID, window.ExitHarm, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	cleared := openOne()
	if _, err := w.Close(ctx, cleared.ID, window.ExitClean, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	atCap := openOne()
	if _, err := w.Close(ctx, atCap.ID, window.ExitCap, closedOn()); err != nil {
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

	withoutHarm, err := window.ClosedWithoutHarm(ctx, pool, serviceID)
	if err != nil {
		t.Fatalf("ClosedWithoutHarm: %v", err)
	}
	if len(withoutHarm) != 2 {
		t.Fatalf("ClosedWithoutHarm = %+v, want the clean window and the cap window", withoutHarm)
	}
	seen := map[string]bool{withoutHarm[0].ID: true, withoutHarm[1].ID: true}
	if !seen[cleared.ID] || !seen[atCap.ID] {
		t.Errorf("ClosedWithoutHarm = %+v, want %s and %s", withoutHarm, cleared.ID, atCap.ID)
	}
}

// TestPastCapIsTrueOnlyAfterATinyCapElapsesAndNeverOnceClosed covers the one
// exit that is not a reading of the quantity: it fires on elapsed time alone,
// and stops firing the moment the window closes.
func TestPastCapIsTrueOnlyAfterATinyCapElapsesAndNeverOnceClosed(t *testing.T) {
	ctx, _, w := newTable(t)

	long := opening()
	long.CapSeconds = 3600
	longWin, err := w.Open(ctx, comparison, long)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if past, err := longWin.PastCap(time.Now()); err != nil || past {
		t.Errorf("PastCap on a window whose cap has not elapsed = %v, %v, want false", past, err)
	}

	tiny := opening()
	tiny.CapSeconds = 0.01
	tinyWin, err := w.Open(ctx, comparison, tiny)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if past, err := tinyWin.PastCap(time.Now()); err != nil || !past {
		t.Errorf("PastCap after a tiny cap elapsed = %v, %v, want true", past, err)
	}

	closed, err := w.Close(ctx, tinyWin.ID, window.ExitCap, closedOn())
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if past, err := closed.PastCap(time.Now()); err != nil || past {
		t.Errorf("PastCap on a closed window = %v, %v, want false", past, err)
	}
}

// TestAnOpeningMissingAFieldIsIncomplete covers every required field the same
// way, one Opening with exactly one of them cleared per case.
func TestAnOpeningMissingAFieldIsIncomplete(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*window.Opening)
	}{
		{"deploy", func(o *window.Opening) { o.DeployID = "" }},
		{"release", func(o *window.Opening) { o.ReleaseID = "" }},
		{"service", func(o *window.Opening) { o.ServiceID = "" }},
		{"formula", func(o *window.Opening) { o.Formula = "" }},
		{"policy version", func(o *window.Opening) { o.PolicyVersion = "" }},
		{"score version", func(o *window.Opening) { o.ScoreVersion = "" }},
	} {
		o := opening()
		c.mut(&o)
		if _, err := w.Open(ctx, comparison, o); !errors.Is(err, window.ErrOpeningIncomplete) {
			t.Errorf("Open missing %s = %v, want ErrOpeningIncomplete", c.what, err)
		}
	}
}

// TestASizeConfidenceOrCapOutOfRangeIsIncomplete covers the three shares: size
// is above nothing and at most one, confidence is above nothing and below one,
// and the cap is above nothing.
func TestASizeConfidenceOrCapOutOfRangeIsIncomplete(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*window.Opening)
	}{
		{"size at zero", func(o *window.Opening) { o.Size = 0 }},
		{"size above one", func(o *window.Opening) { o.Size = 1.5 }},
		{"confidence at zero", func(o *window.Opening) { o.Confidence = 0 }},
		{"confidence at one", func(o *window.Opening) { o.Confidence = 1 }},
		{"cap at zero", func(o *window.Opening) { o.CapSeconds = 0 }},
	} {
		o := opening()
		c.mut(&o)
		if _, err := w.Open(ctx, comparison, o); !errors.Is(err, window.ErrOpeningIncomplete) {
			t.Errorf("Open with %s = %v, want ErrOpeningIncomplete", c.what, err)
		}
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
// may close clean, and a reader with only clean_available could not tell which was
// which. The score's sample is why the second field exists.
func TestAHeldOutWindowIsToldFromOneWithNoBaseline(t *testing.T) {
	ctx, pool, w := newTable(t)

	firstRelease := opening()
	firstRelease.CleanAvailable = false
	first, err := w.Open(ctx, comparison, firstRelease)
	if err != nil {
		t.Fatalf("Open over a release with no baseline: %v", err)
	}

	sampled := opening()
	sampled.CleanAvailable, sampled.HeldOut = false, true
	held, err := w.Open(ctx, comparison, sampled)
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
	if !read.HeldOut || read.CleanAvailable {
		t.Errorf("the window reads back as %+v, want held out with clean unavailable", read)
	}
	for _, w := range []window.Window{first, held} {
		if w.CleanAvailable {
			t.Error("a window that runs to the cap says clean is available to it")
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
		opened, err := w.Open(ctx, comparison, o)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if _, err := w.Close(ctx, opened.ID, exit, readFor(exit)); err != nil {
			t.Fatalf("Close at %s: %v", exit, err)
		}
		closedIDs = append(closedIDs, opened.ID)
	}
	stillOpen, err := w.Open(ctx, comparison, opening())
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

// closedOn is the read a test closes a window on: a pair of counts with a
// baseline in it, which is what an exit other than swept always has. The numbers
// are not what any of these tests assert over — what they assert is the exit —
// but a close with no read is refused, and rightly: an exit nobody can recompute
// is one nobody can argue with.
func closedOn() boundary.Observed {
	return boundary.Observed{Units: 200, Failures: 2, BaselineUnits: 200, BaselineFailures: 2}
}

// readFor is [closedOn] for every exit but swept, which takes none. A loop closing
// windows at each of the four exits needs the read to follow the exit.
func readFor(exit window.Exit) boundary.Observed {
	if exit == window.ExitSwept {
		return boundary.Observed{}
	}
	return closedOn()
}
