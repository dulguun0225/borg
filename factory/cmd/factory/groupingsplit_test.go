// The split: a group the role got wrong corrected on a later pass, and the
// boundary it stops at. Split from grouping_test.go by subject at the length a
// file is held to, sharing its fixtures and package.
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/service"
)

// The words the split tests write. The first two are one problem to a role
// reading the word each starts with, and the second is what a later pass is
// told to put in a group of its own — which is the role changing its mind, and
// what a group it got wrong being split comes back as.
const (
	splitStays  = "Saving the form hangs and never finishes"
	splitMoves  = "Saving anything at all takes far longer than it used to"
	splitApart  = "far longer"
	splitThird  = "Exporting a list writes an empty file"
	splitFourth = "Printing a summary loses the last page"
)

// TestASplitMovesReportsBeforeDecomposition: decomposition is the boundary, and
// before it the role may still split a group it got wrong. The first pass puts
// two reports in one intent; the second reads them again and answers
// differently, and the report the role changed its mind about moves out of that
// intent and into one of its own. The intent it left keeps the report that
// raised it, so nothing is ended.
//
// The third pass is the other direction: the role puts every report in one
// group, so the reports of two intents move into the first and those two are
// left holding nothing. An intent holding no report names nothing — its
// statement summarizes reports that are somewhere else — so it is ended through
// intake.
func TestASplitMovesReportsBeforeDecomposition(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	fake := &fakeModel{}
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

	// One group, one intent, both reports in it. Arrival makes each report its
	// own pass, so both stand grouped the moment the second reportThrough
	// returns.
	reportThrough(t, client, splitStays, "bug", false)
	reportThrough(t, client, splitMoves, "complaint", false)
	first := reportIntents(t, ctx, d, p)
	if len(first) != 1 {
		t.Fatalf("%d intent(s) from one group, want one: %v\n%s", len(first), first, out)
	}
	ids := reportIDs(t, ctx, d, p)
	if len(ids) != 2 {
		t.Fatalf("the store holds %d report(s), want the two submitted", len(ids))
	}

	// The role changes its mind. A third report is what makes arrival's own
	// pass read at all — a pass with nothing ungrouped does nothing — and the
	// split is over the reports that pass reads again.
	fake.apart = splitApart
	reportThrough(t, client, splitThird, "bug", false)

	if got := intentOfReport(t, ctx, d, ids[0]); got != first[0] {
		t.Errorf("the report that raised the intent is now in %q, want it left in %s", got, first[0])
	}
	moved := intentOfReport(t, ctx, d, ids[1])
	if moved == first[0] || moved == "" {
		t.Errorf("the report the role changed its mind about is in %q, want an intent of its own", moved)
	}
	if !strings.Contains(out.String(), "which is a group it got wrong being split") {
		t.Errorf("the pass does not report the split:\n%s", out)
	}
	if raised := reportIntents(t, ctx, d, p); len(raised) != 3 {
		t.Errorf("%d intent(s) after the split, want three: the first, the split, and the third report's",
			len(raised))
	}

	// The other direction: every report in one group, so the two intents the
	// split left move into the first and are ended holding nothing.
	fake.apart, fake.allTogether = "", true
	reportThrough(t, client, splitFourth, "bug", false)

	for _, id := range reportIDs(t, ctx, d, p) {
		if got := intentOfReport(t, ctx, d, id); got != first[0] {
			t.Errorf("report %s is in %q after the merge, want every one of them in %s", id, got, first[0])
		}
	}
	ended := 0
	for _, id := range reportIntents(t, ctx, d, p) {
		in := readIntent(t, ctx, d, id)
		if in.ID == first[0] {
			if in.State == intent.StateDropped {
				t.Error("the intent every report moved into was ended")
			}
			continue
		}
		if in.State != intent.StateDropped {
			t.Errorf("%s holds no report and is %s, want it ended", in.ID, in.State)
		}
		ended++
	}
	if ended != 2 {
		t.Errorf("%d intent(s) were left holding nothing and ended, want the two the merge emptied\n%s",
			ended, out)
	}
}

// TestAReportDoesNotMoveOutOfADecomposedIntent: after the boundary a report
// matching work already decomposed attaches and raises the count rather than
// being taken out of it. The role changes its mind the same way it does before
// decomposition, and the report stays where it was put.
func TestAReportDoesNotMoveOutOfADecomposedIntent(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	fake := &fakeModel{}
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

	reportThrough(t, client, splitStays, "bug", false)
	reportThrough(t, client, splitMoves, "complaint", false)
	grouped := reportIntents(t, ctx, d, p)
	if len(grouped) != 1 {
		t.Fatalf("%d intent(s) from one group, want one: %v\n%s", len(grouped), grouped, out)
	}
	ids := reportIDs(t, ctx, d, p)

	// The intent is decomposed, which is the boundary.
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service: %v, found %v", err, found)
	}
	if _, err := item.NewDecomposition(d.pool, d.token, item.NoHolds{}).Create(ctx, decompositionActor, item.New{
		IntentID: grouped[0], ServiceID: svc.ID, AreaChain: []string{p.areaID}, Branch: "candidate/grouped",
		RequirementsAnswered: oneRequirement,
	}, p.projectID, p.projectID); err != nil {
		t.Fatalf("decomposing the report-derived intent: %v", err)
	}

	fake.apart = splitApart
	reportThrough(t, client, splitThird, "bug", false)

	for n, id := range ids {
		if got := intentOfReport(t, ctx, d, id); got != grouped[0] {
			t.Errorf("report %d is in %q, want it left in the decomposed %s: after the boundary a report attaches",
				n, got, grouped[0])
		}
	}
	if strings.Contains(out.String(), "which is a group it got wrong being split") {
		t.Errorf("the pass moved a report out of a decomposed intent:\n%s", out)
	}
	if raised := reportIntents(t, ctx, d, p); len(raised) != 2 {
		t.Errorf("%d intent(s), want the decomposed one and the third report's own", len(raised))
	}
}

// reportIDs is every report in the store of this project's services, oldest
// first, which is how a test names one: the way in renders no id in the session
// that submitted.
func reportIDs(t *testing.T, ctx context.Context, d deps, p *path) []string {
	t.Helper()
	services, err := servicesInAProject{p: p}.InProject(ctx, p.projectID)
	if err != nil {
		t.Fatalf("reading the services of %s: %v", p.projectID, err)
	}
	read, err := d.reports.Reports(ctx, grouperPrincipal, services, false)
	if err != nil {
		t.Fatalf("reading the reports: %v", err)
	}
	ids := make([]string, 0, len(read))
	for _, one := range read {
		ids = append(ids, one.ID)
	}
	return ids
}
