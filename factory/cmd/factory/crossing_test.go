// Tests of a crossing found after the window has closed: an incident
// and an unrefined intent, and never a rollback.
package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/window"
)

// TestACrossingAfterTheWindowClosedRaisesAnIntent is the other side of the
// window's authority. The health monitor keeps running after the window closes;
// what it finds then is not a rollback candidate, because the change has been
// live for a week and the window's authority ended long before. It is an
// incident and an unrefined intent at the start of the pipeline.
func TestACrossingAfterTheWindowClosedRaisesAnIntent(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	d.in = strings.NewReader(approvals)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	w, err := window.Get(ctx, d.pool, c.windowID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if w.Open() {
		t.Fatalf("the window is still open, and this test is about what happens after it closes:\n%s", out)
	}

	// The software starts failing after its window has closed. A test writes what the
	// running program would have emitted, which is the one thing here that is not the
	// factory's own doing — the quantity is the build's, and this stands in for a build
	// that got worse.
	//
	// Each line carries the time the unit of work finished, which is the second
	// emission version's shape and what the factory assigns a unit to an interval
	// by. They are spread over the last two seconds, so the reading against the
	// service's own recent past has intervals to read a spread between rather than
	// one interval holding four hundred failures — a window closes on the count of
	// intervals and never on the volume inside one.
	signal := localtarget.SignalFile(d.dir, c.reverifiedBuildID)
	var failing strings.Builder
	for n := range 400 {
		at := time.Now().Add(-2 * time.Second).Add(time.Duration(n) * 5 * time.Millisecond)
		failing.WriteString(at.UTC().Format(time.RFC3339Nano) + "\terror\n")
	}
	if err := os.WriteFile(signal, []byte(failing.String()), 0o644); err != nil {
		t.Fatalf("writing what the running build emits: %v", err)
	}

	path := p(ctx, t, d)
	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the pass stopped: %v\noutput so far:\n%s", err, out)
	}

	incidents, err := incident.ForService(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the incidents: %v", err)
	}
	if len(incidents) != 1 {
		t.Fatalf("%d incidents were raised, want one: %+v", len(incidents), incidents)
	}
	raised := incidents[0]
	if raised.ReleaseID != c.releaseID {
		t.Errorf("the incident names release %s, the crossing was against %s", raised.ReleaseID, c.releaseID)
	}
	if raised.Observations != 0 {
		t.Errorf("the first crossing recorded %d observations", raised.Observations)
	}

	// An intent and no rollback: the window's authority ended when it closed.
	found, err := intent.Get(ctx, d.pool, raised.IntentID)
	if err != nil {
		t.Fatalf("reading the intent the crossing raised: %v", err)
	}
	if found.Source != intent.SourceDetector || found.State != intent.StateUnrefined {
		t.Errorf("the intent is %s from %s, want an unrefined one from the detector", found.State, found.Source)
	}
	if _, rolled, err := deploy.NewestRollback(ctx, d.pool, res.serviceID, res.environmentID); err != nil || rolled {
		t.Errorf("NewestRollback = %v, %v; nothing rolls back after the window has closed", rolled, err)
	}
	if !strings.Contains(out.String(), "A crossing after the window over release") ||
		!strings.Contains(out.String(), "nothing was rolled back") {
		t.Errorf("the pass does not say the crossing was after the window closed and rolled nothing back:\n%s", out)
	}

	// A second crossing on the same service and release is an observation on the
	// incident already open, and never a second intent.
	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the second pass stopped: %v", err)
	}
	again, err := incident.ForService(ctx, d.pool, res.serviceID)
	if err != nil {
		t.Fatalf("reading the incidents again: %v", err)
	}
	if len(again) != 1 {
		t.Fatalf("%d incidents after a second crossing, want the one deduplicated onto: %+v", len(again), again)
	}
	if again[0].Observations != 1 {
		t.Errorf("the incident records %d observations after a second crossing, want one", again[0].Observations)
	}
	var intents int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+intent.Table+` where source = $1`,
		string(intent.SourceDetector)).Scan(&intents); err != nil {
		t.Fatalf("counting the detector's intents: %v", err)
	}
	if intents != 1 {
		t.Errorf("%d intents came from the detector, and a further crossing is an observation and never a second intent", intents)
	}
}
