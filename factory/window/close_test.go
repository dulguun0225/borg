// The close of an analysis window tested against the database:
// [window.Writer.Close] writing exactly one exit, the read the exit was
// decided on, the closes that are refused, the CHECK constraint listing every
// exit, and [window.Window.PastCap], which is read against the cap the open
// wrote. The fixtures are db_test.go's, this file being one subject of the
// same external test package.
package window_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// TestAWindowClosesOnceAtExactlyOneOfTheFourExits closes a window of its own at
// each exit in turn, and checks Exit.PassedOrTimedOut against the rule it
// encodes: those two leave a release the factory can return to, failed and
// skipped do not.
func TestAWindowClosesOnceAtExactlyOneOfTheFourExits(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	for _, exit := range window.Exits {
		opened, err := w.Open(ctx, healthMonitor, opening())
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		closed, err := w.Close(ctx, opened.ID, exit, closingFor(exit))
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
		want := exit == window.ExitPassed || exit == window.ExitTimedOut
		if got := exit.PassedOrTimedOut(); got != want {
			t.Errorf("%s.PassedOrTimedOut() = %v, want %v", exit, got, want)
		}
	}
}

// TestTheReadAWindowClosedOnIsPerQuantityAndPerSeries is what makes an exit
// recomputable: the four counts per quantity, and the same per target and
// operation.
func TestTheReadAWindowClosedOnIsPerQuantityAndPerSeries(t *testing.T) {
	ctx, pool, w, _ := newTable(t)
	opened, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	closing := closedOn()
	closed, err := w.Close(ctx, opened.ID, window.ExitPassed, closing)
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	read, err := window.Get(ctx, pool, closed.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read.ClosedOn, closing.On) {
		t.Errorf("the stored read = %+v, want %+v", read.ClosedOn, closing.On)
	}
	if got := read.ClosedOn.Of(gatepolicy.QuantityErrorRate).Units; got != 2000 {
		t.Errorf("the error rate's units read back as %d, want 2000", got)
	}
	if !reflect.DeepEqual(read.FinestSizeReached, closing.FinestSizeReached) {
		t.Errorf("the finest size reached = %+v, want %+v", read.FinestSizeReached, closing.FinestSizeReached)
	}
}

// TestASkippedCloseCarriesNoRead is the exit that is not a reading: a rollback
// aimed below the release ended the window, so a read stored there would be one
// nothing performed.
func TestASkippedCloseCarriesNoRead(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	opened, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitSkipped, closedOn()); !errors.Is(err, window.ErrReadRefused) {
		t.Errorf("Close skipped with a read = %v, want ErrReadRefused", err)
	}
}

// TestAHeldOutWindowCannotClosePassed is the sample the score holds out running
// to the cap rather than stopping where the boundary would allow.
func TestAHeldOutWindowCannotClosePassed(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	o := opening()
	o.HeldOut = true
	o.PassedAvailable = false
	opened, err := w.Open(ctx, healthMonitor, o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitPassed, closedOn()); !errors.Is(err, window.ErrPassedUnavailable) {
		t.Errorf("Close passed on a held-out window = %v, want ErrPassedUnavailable", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitTimedOut, closedOn()); err != nil {
		t.Errorf("Close timed out on a held-out window: %v", err)
	}
}

// TestAWindowThatMeasuresNothingCarriesNoParameters is the window a service
// missing one of the four fields the deployer populates opens.
func TestAWindowThatMeasuresNothingCarriesNoParameters(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	o := window.OpenEvent{
		DeployID:        record.NewID("dep"),
		ReleaseID:       record.NewID("rel"),
		BuildID:         record.NewID("bld"),
		ServiceID:       record.NewID("svc"),
		MeasuresNothing: true,
		BoundaryVersion: boundary.Version,
		PolicyVersion:   "pv_1",
		ScoreVersion:    "sv_1",
	}
	opened, err := w.Open(ctx, healthMonitor, o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !opened.MeasuresNothing || opened.PassedAvailable || len(opened.Size) != 0 {
		t.Errorf("the window = %+v, want one that records only that it measures nothing", opened)
	}
	past, err := opened.PastCap(time.Now())
	if err != nil || !past {
		t.Errorf("PastCap on a window that measures nothing = %v, %v, want true at once", past, err)
	}

	withParameters := o
	withParameters.DeployID = record.NewID("dep")
	withParameters.ReleaseID = record.NewID("rel")
	withParameters.Size = map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.1}
	if _, err := w.Open(ctx, healthMonitor, withParameters); !errors.Is(err, window.ErrMeasuresNothingCarriesNoParameters) {
		t.Errorf("Open = %v, want ErrMeasuresNothingCarriesNoParameters", err)
	}
}

func TestASecondCloseOnOneWindowIsAlreadyClosed(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	opened, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitPassed, closedOn()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := w.Close(ctx, opened.ID, window.ExitFailed, closedOn()); !errors.Is(err, window.ErrAlreadyClosed) {
		t.Errorf("Close = %v, want ErrAlreadyClosed", err)
	}
}

func TestClosingAtAnExitOutsideExitsIsExitUnknown(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	opened, err := w.Open(ctx, healthMonitor, opening())
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
	ctx, pool, _, _ := newTable(t)

	for _, exit := range append(append([]window.Exit{}, window.Exits...), "flaky") {
		err := insertAround(ctx, pool, string(exit))
		if err != nil && exit != "flaky" {
			t.Errorf("inserting exit %q, one of window.Exits, was refused: %v", exit, err)
		}
		if err == nil && exit == "flaky" {
			t.Error("the store accepted an exit outside window.Exits")
		}
	}
}

// insertAround writes a row without the writer, which is how the store's own
// refusals are tested.
func insertAround(ctx context.Context, pool *pgxpool.Pool, exit string) error {
	_, err := pool.Exec(ctx, `insert into `+window.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		 deploy_id, release_id, build_id, service_id, measures_nothing, passed_available, held_out,
		 sizes, powers, confidence, cap_seconds, boundary_version, targets, operations_read_alone,
		 emission_version_release, emission_version_control, quantities_outside,
		 own_history_sizes, own_history_run_length, threshold_sizes, threshold_run_length,
		 policy_version, score_version, exit, closed_at, closed_on, finest_size_reached)
		values ($1, $2, 'component', 'health_monitor', '', $3, $4, $5, $6, $7, false, true, false,
		 '{"error_rate":0.1}', '{"error_rate":0.8}', 0.95, 3600, $8, 'one.example', '',
		 'emission/1', 'emission/1', '', '{}', 0, '{}', 0, 'pv_1', 'sv_1', $9, $10, '', '')`,
		record.NewID(window.IDPrefix), window.FormatVersion, record.Now(),
		record.NewID("dep"), record.NewID("rel"), record.NewID("bld"), record.NewID("svc"),
		boundary.Version, exit, record.Now())
	return err
}

// TestPastCapIsTrueOnlyAfterATinyCapElapsesAndNeverOnceClosed covers the one
// exit that is not a reading of the quantity: it fires on elapsed time alone,
// and stops firing the moment the window closes.
func TestPastCapIsTrueOnlyAfterATinyCapElapsesAndNeverOnceClosed(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	long := opening()
	long.CapSeconds = 3600
	longWin, err := w.Open(ctx, healthMonitor, long)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if past, err := longWin.PastCap(time.Now()); err != nil || past {
		t.Errorf("PastCap on a window whose cap has not elapsed = %v, %v, want false", past, err)
	}

	tiny := opening()
	tiny.CapSeconds = 0.01
	tinyWin, err := w.Open(ctx, healthMonitor, tiny)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if past, err := tinyWin.PastCap(time.Now()); err != nil || !past {
		t.Errorf("PastCap after a tiny cap elapsed = %v, %v, want true", past, err)
	}

	closed, err := w.Close(ctx, tinyWin.ID, window.ExitTimedOut, closedOn())
	if err != nil {
		t.Fatalf("Close: %v", err)
	}
	if past, err := closed.PastCap(time.Now()); err != nil || past {
		t.Errorf("PastCap on a closed window = %v, %v, want false", past, err)
	}
}

func closedOn() window.Closing {
	return window.Closing{
		On: window.Read{
			Quantities: map[gatepolicy.Quantity]boundary.Counts{
				gatepolicy.QuantityErrorRate: {Units: 2000, Count: 20, BaselineUnits: 2000, BaselineCount: 18},
			},
			Series: []window.SeriesCounts{{
				Target: "one.example", Operation: "GET /items", Quantity: gatepolicy.QuantityErrorRate,
				Counts: boundary.Counts{Units: 1000, Count: 10, BaselineUnits: 1000, BaselineCount: 9},
			}},
		},
		FinestSizeReached: map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.06},
	}
}

// closingFor is [closedOn] for every exit but skipped, which takes none. A loop
// closing windows at each of the four exits needs the read to follow the exit.
func closingFor(exit window.Exit) window.Closing {
	if exit == window.ExitSkipped {
		return window.Closing{}
	}
	return closedOn()
}
