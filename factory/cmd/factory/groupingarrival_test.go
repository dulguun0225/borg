// The arrival trigger: a report raises an intent the moment it is accepted,
// with no periodic tick in between. Split from grouping_test.go by subject.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/localtarget"
)

// arrivalReport is the one report this test submits. It needs no partner to
// group against — a report that goes with no other is a group of its own —
// so one submission is enough to show whether arrival itself raised it.
const arrivalReport = "Saving a draft loses every unsaved change"

// TestAReportIsGroupedOnArrival: C0384. A report raises an intent on arrival
// rather than waiting for a batch: the entrance hands the report just
// accepted to the grouper's own pass at once, through
// [reportChannel.handOffToTheGrouper], and Submit does not return until that
// pass has run. So the intent already stands the moment reportThrough
// returns, with no call to [passes.Tick] in between — the periodic tick
// behind [passGrouper] is left untouched as the catch-up a restart needs, and
// this test never calls it.
func TestAReportIsGroupedOnArrival(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	newReports(t, ctx, &d)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path that groups: %v\n%s", err, out)
	}
	if p.grouper == nil {
		t.Fatal("a composition holding a report store composed no grouper")
	}

	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	client := overTheSocket(socket)

	reportThrough(t, client, arrivalReport, "bug", false)

	raised := reportIntents(t, ctx, d, p)
	if len(raised) != 1 {
		t.Fatalf("%d intent(s) stand right after Submit returned, want the one arrival raises: %v\n%s",
			len(raised), raised, out)
	}
	ids := reportIDs(t, ctx, d, p)
	if len(ids) != 1 {
		t.Fatalf("the store holds %d report(s), want the one submitted", len(ids))
	}
	if landed := intentOfReport(t, ctx, d, ids[0]); landed != raised[0] {
		t.Errorf("the report landed in %q, want the intent arrival raised, %q", landed, raised[0])
	}
}
