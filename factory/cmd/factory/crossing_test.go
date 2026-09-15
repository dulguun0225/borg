// Tests of a crossing found after the window has closed: an incident
// and an unrefined intent, and never a rollback.
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
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

	_, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	d.decide = scriptedAtWork(approvals).decide
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

	if _, err := d.targets.at(d.dir).Stop(ctx, deployerPrincipal, theService, d.credential); err != nil {
		t.Fatalf("stopping the running release: %v", err)
	}
	signal := localtarget.SignalFile(d.dir, c.reverifiedBuildID)
	writeFailureEmissions(t, signal, c.reverifiedBuildID, c.deployID, theService, d.dir)
	time.Sleep(1100 * time.Millisecond)
	fresh := time.Now().UTC().Add(-100 * time.Millisecond)
	appendEmissionRecords(t, signal, []healthmonitor.EmissionRecord{
		{Version: "emission/3", Kind: healthmonitor.RecordArrival,
			Time: fresh, Service: theService, Build: c.reverifiedBuildID, Deploy: c.deployID,
			Target: d.dir, Operation: "checkout", Deadline: time.Second},
		{Version: "emission/3", Kind: healthmonitor.RecordCompletion,
			Time: fresh.Add(time.Millisecond), Service: theService, Build: c.reverifiedBuildID,
			Deploy: c.deployID, Target: d.dir, Operation: "checkout", Outcome: "failure",
			Duration: time.Millisecond},
	})
	path := p(ctx, t, d)

	if _, err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
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
	if _, err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
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
