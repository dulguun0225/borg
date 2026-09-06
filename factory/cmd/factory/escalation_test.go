// Tests of the page's condition: it fires only where something live is
// worse, read off a record rather than off a list.
package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/service"
)

// TestAnEscalationPagesOnlyWhereSomethingLiveIsWorse is the page's condition read off
// a record rather than off a list. The factory giving up on a defect that is live is
// production staying worse until a human takes it over; giving up on a feature nobody is
// running is not, and that one waits in Work.
func TestAnEscalationPagesOnlyWhereSomethingLiveIsWorse(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	// An owner's request the factory cannot do: no page.
	d.model = &refusingModel{inner: &fakeModel{}, refusals: attemptLimit + 5}
	if _, err := run(ctx, d, of(theStatement)); err == nil {
		t.Fatalf("the run finished, and every implementer reply was refused:\n%s", out)
	}
	rows := readLog(t, ctx, d)
	for _, row := range rows {
		if row.Shape == decisionlog.ShapePageEvent {
			t.Errorf("an escalation on an owner's feature fired page event %s, and nothing live is worse for it", row.ID)
		}
	}
	if !strings.Contains(out.String(), "mail to owner") {
		t.Errorf("the escalation was not delivered on mail:\n%s", out)
	}

	// A detector's intent the factory cannot fix: the same escalation, and a page,
	// because the defect it describes is live.
	//
	// `p.take` no longer resumes an intent already waiting by matching its
	// statement's text — package intent's rewrite drops that lookup,
	// authorintent.go's own comment says why — so a run given this statement
	// would take a fresh owner's intent in rather than working the detector's.
	// What this test is about is the page's condition, which the gate reads off
	// the intent the escalated item was decomposed from, so it is exercised
	// against an item of the detector's intent rather than through a run this
	// milestone cannot route there.
	detected, err := intent.NewIntake(d.pool, d.token, intent.NoNotifier{}).TakeIn(ctx, healthmonitor.Actor, intent.Arrival{
		Source: intent.SourceDetector, Statement: theSecondStatement,
		Evidence: intent.Evidence{ServiceID: "svc_escalation_test"},
	})
	if err != nil {
		t.Fatalf("taking in the detector's intent: %v", err)
	}
	if detected.Source != intent.SourceDetector {
		t.Fatalf("the intent's source is %s", detected.Source)
	}
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	svc, err := service.NewWriter(d.pool, d.token).Create(ctx, decompositionActor,
		"escalation-test", t.TempDir(), p.projectID)
	if err != nil {
		t.Fatalf("writing the service: %v", err)
	}
	it, err := p.decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: detected.ID, ServiceID: svc.ID, Branch: "item/" + detected.ID,
		RequirementsAnswered: []string{"rq_escalation_test"},
	}, "", svc.ProjectID, nil)
	if err != nil {
		t.Fatalf("decomposing the detector's item: %v", err)
	}
	// The composed value and not one this test builds: what decides whether
	// something live is worse is the path the composition handed it, and a test
	// that supplied its own would pass over a composition that supplied none.
	if err := p.escalations.Escalated(ctx, it.ID,
		item.StageImplementation, dispatch.EscalatedByTheAttemptLimit); err != nil {
		t.Fatalf("escalating: %v", err)
	}

	var paged int
	rows = readLog(t, ctx, d)
	for _, row := range rows {
		if row.Shape != decisionlog.ShapePageEvent {
			continue
		}
		paged++
		var payload notifier.Payload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the page event: %v", err)
		}
		if payload.WaitKind != string(notifier.KindItemEscalated) {
			t.Errorf("the page is about a %q, want an item escalated", payload.WaitKind)
		}
		if !strings.Contains(payload.Waiting, string(intent.SourceDetector)) {
			t.Errorf("the page says %q, and what makes it one is where the intent came from", payload.Waiting)
		}
		if payload.Holding != people.OfDuty(takeOverIssues).String() {
			t.Errorf("the page routes by %q, want the duty of taking over issues the factory cannot fix", payload.Holding)
		}
	}
	if paged != 1 {
		t.Errorf("%d page events were written, want the one on the detector's item", paged)
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}
