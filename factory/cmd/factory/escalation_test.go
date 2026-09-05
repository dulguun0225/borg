// Tests of the page's condition: it fires only where something live is
// worse, read off a record rather than off a list.
package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
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
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
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
	// The statement is one this fake can author a spec for, because what makes this
	// page is where the intent came from and not the words in it.
	detected, err := intent.NewIntake(d.pool).TakeIn(ctx, healthmonitor.Actor, intent.SourceDetector, theSecondStatement)
	if err != nil {
		t.Fatalf("taking in the detector's intent: %v", err)
	}
	if detected.Source != intent.SourceDetector {
		t.Fatalf("the intent's source is %s", detected.Source)
	}
	d.in = strings.NewReader(theAnswer + "\n" + approvals)
	if _, err := run(ctx, d, of(detected.Statement)); err == nil {
		t.Fatalf("the run finished, and every implementer reply was refused:\n%s", out)
	}

	var paged int
	rows, err = decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
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
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}
