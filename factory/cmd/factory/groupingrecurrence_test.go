// The recurrence a member the decomposition boundary left behind names: split
// from grouping_test.go by subject.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/service"
)

// The words this test drives the role with. recurStays and recurJoins are one
// problem to the fake model reading the first word, which is what raises and
// then grows the intent this test decomposes. recurApart is what pulls
// recurStays into a group of its own on the later pass, so the role's reply
// puts recurJoins — the member the boundary leaves behind — together with
// recurThird, a report with no intent yet, in a group naming none of its own.
const (
	recurStays = "Saving the form hangs and never finishes"
	recurJoins = "Saving anything at all takes far longer than it used to"
	recurApart = "hangs and never finishes"
	recurThird = "Saving is broken in a whole new way"
)

// TestARecurrenceNamesTheIntentAMemberWasLeftIn: C0398. Decomposition is the
// boundary a split stops at: a member already in a decomposed intent does not
// move even where the group it is read into names no intent of its own.
// Where that happens, the new intent the rest of the group raises recurs on
// the decomposed intent the member was left in — what it was judged against —
// rather than naming nothing, the same link a group naming a finished intent
// gets.
func TestARecurrenceNamesTheIntentAMemberWasLeftIn(t *testing.T) {
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

	// One intent, both reports in it, raised and joined by arrival alone.
	reportThrough(t, client, recurStays, "bug", false)
	reportThrough(t, client, recurJoins, "bug", false)
	raised := reportIntents(t, ctx, d, p)
	if len(raised) != 1 {
		t.Fatalf("%d intent(s) from one group, want one: %v\n%s", len(raised), raised, out)
	}
	decomposed := raised[0]

	// The intent is decomposed, which is the boundary a split stops at.
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service: %v, found %v", err, found)
	}
	if _, err := item.NewDecomposition(d.pool, d.token, item.NoHolds{}).Create(ctx, decompositionActor, item.New{
		IntentID: decomposed, ServiceID: svc.ID, AreaChain: []string{p.areaID}, Branch: "candidate/recurs",
		RequirementsAnswered: oneRequirement,
	}, p.projectID, p.projectID); err != nil {
		t.Fatalf("decomposing the intent: %v", err)
	}

	// The role changes its mind about recurJoins: this pass, it groups
	// recurJoins with a new report, recurThird, in a group naming no intent of
	// its own — recurStays, the report that raised the decomposed intent, is
	// pulled apart into a singleton and left exactly where it is.
	fake.apart = recurApart
	reportThrough(t, client, recurThird, "bug", false)

	ids := reportIDs(t, ctx, d, p)
	if len(ids) != 3 {
		t.Fatalf("the store holds %d report(s), want the three submitted", len(ids))
	}
	if got := intentOfReport(t, ctx, d, ids[0]); got != decomposed {
		t.Errorf("recurStays is now in %q, want it left in the decomposed %s", got, decomposed)
	}
	if got := intentOfReport(t, ctx, d, ids[1]); got != decomposed {
		t.Errorf("the member the boundary left behind moved to %q, want it left in %s: decomposition is the boundary a split stops at",
			got, decomposed)
	}
	all := reportIntents(t, ctx, d, p)
	if len(all) != 2 {
		t.Fatalf("%d intent(s) after the boundary, want the decomposed one and the recurrence: %v\n%s",
			len(all), all, out)
	}
	var recurred string
	for _, id := range all {
		if id != decomposed {
			recurred = id
		}
	}
	if got := intentOfReport(t, ctx, d, ids[2]); got != recurred {
		t.Errorf("recurThird is in %q, want the new intent %q", got, recurred)
	}
	in := readIntent(t, ctx, d, recurred)
	if in.RecurrenceOf != decomposed {
		t.Errorf("the new intent recurs on %q, want the decomposed intent %s it was judged against",
			in.RecurrenceOf, decomposed)
	}
}
