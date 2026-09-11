// A report the reply leaves in no group: split from grouping_test.go by
// subject.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/localtarget"
)

// declinedStays is one problem to the fake model on its own, and declinedOmitted
// is the report [fakeModel.decline] pulls out of every group in the reply,
// standing in for a model that declines despite its own prompt forbidding it.
const (
	declinedStays   = "Saving the form hangs and never finishes"
	declinedOmitted = "Exporting a list writes an empty file"
)

// TestADeclinedReportRaisesAnIntentOfItsOwn: C0385. One report from one end
// user is an intent, so a report the reply leaves in no group still ends the
// pass linked to one: it raises an intent of its own, a group of one, rather
// than staying linked to nothing. What a pass reads it always finishes
// grouped, whatever the reply does or does not name.
func TestADeclinedReportRaisesAnIntentOfItsOwn(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	fake := &fakeModel{decline: "empty file"}
	d.model = fake
	newReports(t, ctx, &d)
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

	// The first report groups on its own, arrival raising its intent. The
	// second, arriving after it, is what the reply declines to name: the pass
	// dispatches over both, and the reply's one group holds only the first.
	reportThrough(t, client, declinedStays, "bug", false)
	kept := reportIntents(t, ctx, d, p)
	if len(kept) != 1 {
		t.Fatalf("%d intent(s) from the first report, want the one it raises: %v\n%s",
			len(kept), kept, out)
	}
	reportThrough(t, client, declinedOmitted, "bug", false)

	ids := reportIDs(t, ctx, d, p)
	if len(ids) != 2 {
		t.Fatalf("the store holds %d report(s), want the two submitted", len(ids))
	}
	landed := intentOfReport(t, ctx, d, ids[1])
	if landed == "" {
		t.Fatalf("the declined report is still linked to no intent, want a group of one:\n%s", out)
	}
	if landed == kept[0] {
		t.Errorf("the declined report landed in %q, the first report's own intent, want one of its own", landed)
	}
	raised := reportIntents(t, ctx, d, p)
	if len(raised) != 2 {
		t.Errorf("%d intent(s) stand once the declined report is read, want the first's and its own group of one: %v\n%s",
			len(raised), raised, out)
	}
}
