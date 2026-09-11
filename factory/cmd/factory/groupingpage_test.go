// One page per intent, however many passes it takes to see every report that
// marks harm: split from grouping_test.go by subject.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/record"
)

// pageFirst and pageSecond are one problem to the fake model, both starting
// with the same word, and both mark harm: the second is what a later pass
// reads into an intent the first already made the notifier page for.
const (
	pageFirst  = "Saving crashes the whole app for everyone nearby"
	pageSecond = "Saving now crashes it again for a different customer"
)

// TestOnePagePerIntentAcrossPasses: C2126. It is one page per intent and
// never per report: a later report attaching to an intent already paging
// raises nothing further. Arrival makes each of these two reports its own
// pass, so this demonstrates the case the single-pass count in
// TestReportsBecomeIntents does not — the notifier's own delivery record for
// the intent is what the second pass reads before deciding to page again, and
// not a count carried over from the first.
func TestOnePagePerIntentAcrossPasses(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	reports := newReports(t, ctx, &d)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path that groups: %v\n%s", err, out)
	}
	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	client := overTheSocket(socket)

	since := record.Now()
	reportThrough(t, client, pageFirst, "complaint", true)
	raised := reportIntents(t, ctx, d, p)
	if len(raised) != 1 {
		t.Fatalf("%d intent(s) after the first harm-marked report, want the one it raises: %v\n%s",
			len(raised), raised, out)
	}
	intentID := raised[0]
	serviceID := reports.stored(t, ctx)[0].serviceID

	paged, err := notifier.PagedRowsSince(ctx, d.pool, serviceID, notifier.KindHarmMarkedReport, since)
	if err != nil {
		t.Fatalf("counting the pages the first harm mark fired: %v", err)
	}
	if paged != 1 {
		t.Fatalf("%d page(s) fired for the first harm-marked report, want one\n%s", paged, out)
	}

	// A later report, its own arrival and its own pass, attaches to the same
	// intent and marks harm again.
	reportThrough(t, client, pageSecond, "complaint", true)
	ids := reports.ids(t, ctx)
	if len(ids) != 2 {
		t.Fatalf("the store holds %d report(s), want the two submitted", len(ids))
	}
	if got := intentOfReport(t, ctx, d, ids[1]); got != intentID {
		t.Fatalf("the second report landed in %q, want it attached to %s", got, intentID)
	}

	paged, err = notifier.PagedRowsSince(ctx, d.pool, serviceID, notifier.KindHarmMarkedReport, since)
	if err != nil {
		t.Fatalf("counting the pages after the second harm mark attached: %v", err)
	}
	if paged != 1 {
		t.Errorf("%d page(s) stand for the intent once a second harm-marked report attached, want one per intent and never per report",
			paged)
	}
}
