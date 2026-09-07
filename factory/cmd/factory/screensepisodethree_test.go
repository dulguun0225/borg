// Episode three of the demonstration, over HTTP: silence. With nothing
// waiting the home view is the digest and the badge is at zero, and a
// component whose pass has stopped is a named row beside it rather than the
// same emptiness.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// theSilentWatchEvery is the interval the watch pass promises the next one
// within in this episode, and it is longer than the run's own for the reason
// DEMO.md gives: raising the interval is what makes the record late, and a
// record naming the run's fifty milliseconds would be late before the view
// that is to show it clean could be read. Three seconds is what the episode
// waits out, and the second the store's own resolution refuses is why it is
// not shorter.
const theSilentWatchEvery = 3 * time.Second

// TestEpisodeThreeIsSilence is the third episode: one item shipped and every
// round answered, after which the badge is at zero and the digest — the part
// of the home view that appears only at zero — is what a human reads. Then the
// health monitor's pass for the one service stops, and its own last check goes
// past the interval it names: a named row appears beside the digest, outside
// the badge, because a component whose pass merely ran late is not a wait on a
// human. Beside it is the drift detector's own last check, which is absent
// until the detector has run and reads as a factory with no drift detector
// installed.
//
// What the episode cannot drive is the restart DEMO.md performs — a second
// serve process started with a longer -every-watch — because one process holds
// the lease at a time and this test is that process. What the restart does to
// the records is what is driven instead: the pass writes the interval it
// promises the next one within, and the record goes past it when the pass
// stops running.
func TestEpisodeThreeIsSilence(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the run stopped: %v\noutput so far:\n%s", err, out)
	}
	shipped := only(t, res)

	// The watch pass below is composed with a longer interval than the run's,
	// which is the whole of what -every-watch does to this record.
	d.watchEvery = theSilentWatchEvery
	s := newScreens(t, ctx, d, out)

	// The acceptance round the run left waiting, answered at Work on the
	// intent: it is the last thing waiting on a human, and the digest appears
	// only once nothing does.
	s.mustCall(t, "confirmReading", screens.ConfirmReadingArgs{
		IntentID: shipped.intentID, Confirmed: true,
	})

	svc := theServiceRecord(t, ctx, s.p)
	if _, err := s.p.watchPass(ctx, svc); err != nil {
		t.Fatalf("the watch pass: %v\noutput so far:\n%s", err, out)
	}

	var home screens.Home
	s.get(t, "/api/home", &home)
	if home.Badge.Total != 0 {
		t.Fatalf("the badge totals %d with the item live and every round answered: %+v", home.Badge.Total, home.Badge)
	}
	if home.Digest == nil {
		t.Fatal("the digest is not shown at zero, and it is the part that appears only at zero")
	}
	if home.Digest.Releases != 1 || home.Digest.Decisions == 0 {
		t.Errorf("the digest reads %+v, want the one release the run shipped and the decisions it took", home.Digest)
	}
	if row, shown := checkOn(home.LastChecks, lastcheck.ComponentHealthMonitor, svc.ID); shown {
		t.Errorf("the health monitor's row is shown %v after its own pass, and the next one is not owed yet", row)
	}

	// The pass stops there, and the record goes past the interval it names.
	waitOutTheInterval(t, ctx, d, lastcheck.ComponentHealthMonitor, svc.ID)

	s.get(t, "/api/home", &home)
	row, shown := checkOn(home.LastChecks, lastcheck.ComponentHealthMonitor, svc.ID)
	if !shown {
		t.Fatalf("no row names the health monitor past its own interval: %+v", home.LastChecks)
	}
	if !row.FurtherPassOwed {
		t.Error("the row says no further pass is owed, and a record past its interval with none owed is a service that went away")
	}
	if row.IntervalSeconds != int64(theSilentWatchEvery/time.Second) {
		t.Errorf("the row names an interval of %ds, want the %v the pass promised the next one within",
			row.IntervalSeconds, theSilentWatchEvery)
	}
	if home.Badge.Total != 0 {
		t.Errorf("the badge totals %d with a component late and nothing waiting on a human: %+v", home.Badge.Total, home.Badge)
	}
	if home.Digest == nil {
		t.Error("the digest gave way to a component that merely ran late, and the two are shown beside each other")
	}
	if _, shown := checkOn(home.LastChecks, "drift detector", ""); shown {
		t.Error("a drift detector row is shown, and no detector is installed on this factory")
	}

	// Ops reads the same record against the service it names.
	var view screens.Service
	s.get(t, "/api/service/"+svc.ID+"/on/"+s.p.production.ID, &view)
	if _, shown := checkOn(view.LastChecks, lastcheck.ComponentHealthMonitor, svc.ID); !shown {
		t.Errorf("the service on production shows no last check of the health monitor: %+v", view.LastChecks)
	}
	if view.DriftMismatch != "" {
		t.Errorf("the service reads a drift mismatch %q with no detector installed", view.DriftMismatch)
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the episode: %v", err)
	}
}

// checkOn is one component's row of a view's last checks, and whether the view
// holds one. A row is per component and per subject, never aggregated over a
// class, which is what a subject of its own is for.
func checkOn(checks []screens.LastCheck, component, subject string) (screens.LastCheck, bool) {
	for _, one := range checks {
		if one.Component == component && (subject == "" || one.Checks == subject) {
			return one, true
		}
	}
	return screens.LastCheck{}, false
}

// waitOutTheInterval waits until one last check record is past the interval it
// names, which is when the component that wrote it has missed a pass. It is
// wall-clock time and not a clock a test sets: the record carries when the pass
// ran, written by its own writer, and nothing takes a time from a caller.
func waitOutTheInterval(t *testing.T, ctx context.Context, d deps, component, subject string) {
	t.Helper()
	check, found, err := lastcheck.Get(ctx, d.pool, component, subject)
	if err != nil || !found {
		t.Fatalf("reading the last check of %s over %q: found %v, %v", component, subject, found, err)
	}
	ran, err := record.ParseTime(check.CheckedAt)
	if err != nil {
		t.Fatalf("reading when the pass ran: %v", err)
	}
	// A tenth of a second past the interval: the reader compares against the
	// clock it takes at the read, so waiting exactly the interval leaves the
	// two comparisons a rounding apart.
	time.Sleep(time.Until(ran.Add(check.Interval + 100*time.Millisecond)))
}
