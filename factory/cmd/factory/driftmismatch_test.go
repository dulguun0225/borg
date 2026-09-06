// Tests of a mismatch the drift detector raised: it holds the
// production deploy row and pages whoever installed the detector — the
// detector's own page, and never a second one on the row it holds, a hold
// paging nobody being the rule for every hold here.
package main

import (
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
)

// TestADriftMismatchHoldsTheProductionDeployAndPages is the one hold the factory
// cannot lift by gathering evidence. What the factory recorded about the
// service is not what is running, so nothing here can be decided on the
// record. The row it holds pages nobody — the page is the detector's own, on
// the mismatch and not on the row — so acknowledging the gate row here writes
// only the decision's acknowledgement and calls the notifier for nothing.
func TestADriftMismatchHoldsTheProductionDeployAndPages(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.driftdetector = newDriftDetectorStore(t, ctx)

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "An drift detector is installed") {
		t.Errorf("the run does not report an drift detector installed:\n%s", out)
	}

	// Installing the drift detector is substrate outside the twelve duties,
	// so the page a mismatch fires reaches whoever the declaration says installed
	// it.
	installer := owner(t, ctx, d.pool, d.token, "sre")
	if _, err := people.NewWriter(d.pool, d.token, policy.NewFactory(d.pool, d.token)).Declare(ctx,
		owner(t, ctx, d.pool, d.token, d.human), installer.Key,
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	// A target changed underneath: the drift detector's own store now holds a
	// mismatch, written by the drift detector and by nothing in the factory.
	raised, err := driftdetector.NewWriter(d.driftdetector).Record(ctx, driftdetector.Pass{
		ServiceID: serviceOf(ctx, t, d), Target: d.dir, Reached: true,
		RunningBuild: "bl_somebodyelses", RecordedBuildID: "bl_thefactorys",
		RecordedReleaseID: "rel_thefactorys",
		// The interval is what this pass promises the next one within, which
		// the notifier reads to tell a detector that stopped from one that
		// found nothing.
		Interval: time.Minute,
	})
	if err != nil {
		t.Fatalf("recording the pass: %v", err)
	}
	if raised.Raised == "" {
		t.Fatal("the pass raised no mismatch, and the target ran something the factory did not record")
	}

	// The next change: the production deploy row fires with the mismatch on its
	// open event and a human at it, and the human holds.
	// Three approvals and then the hold: the three rows above a build put a
	// human there on every item, the change's reach not being computable before
	// anything is built, and every row over a build auto-passes for the second
	// item on this service — so the mismatch is the only thing putting a human
	// at a row that decides a deploy, which is what this test is about.
	d.in = strings.NewReader("approve\napprove\napprove\nacknowledge I have this row\nhold the record is wrong and I am checking the target\n")
	d.model = interviewed(0)
	res, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a hold is not an error: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if !c.held || c.deployID != "" {
		t.Fatalf("the change was not held at the production deploy row: held=%v deploy=%q\n%s", c.held, c.deployID, out)
	}
	if !c.deployGate.humanDecided {
		t.Error("no human decided at the row, and a mismatch puts one there whatever the number reads")
	}
	if c.deployGate.mismatch == "" {
		t.Error("the row carries no mismatch, and what the drift detector found is what put the human there")
	}
	rows := readLog(t, ctx, d)
	var opening decisionlog.Row
	for _, row := range rows {
		if row.ID == c.deployGate.opening {
			opening = row
		}
	}
	payload := openingPayload(t, opening)
	if payload.Mismatch == "" || !strings.Contains(payload.Mismatch, driftdetector.HoldWords) {
		t.Errorf("the open event's mismatch reads %q, and a human approving through has to read what disagreed",
			payload.Mismatch)
	}

	// The gate row itself: it is held, and it pages nobody, so acknowledging it
	// at Work writes the decision's acknowledgement and calls the notifier for
	// nothing — no page event ever named this row.
	onTheRow, err := p(ctx, t, d).notifier.EventsFor(ctx, c.deployGate.opening)
	if err != nil {
		t.Fatalf("reading the page events on the gate row: %v", err)
	}
	if len(onTheRow) != 0 {
		t.Fatalf("the gate row holds %+v, want nothing: a mismatch holds this row and pages nobody", onTheRow)
	}

	// The mismatch's own page: reached, to whoever installed the drift detector,
	// because a mismatch belongs to no duty of the twelve.
	events, err := p(ctx, t, d).notifier.EventsFor(ctx, raised.Raised)
	if err != nil {
		t.Fatalf("reading the page events: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached {
		t.Fatalf("the page's events are %+v, want one reached", events)
	}
	if events[0].Reached != installer.Key {
		t.Errorf("the page reached %q, and %q is who the declaration says installed the drift detector",
			events[0].Reached, installer.Key)
	}
	if !strings.Contains(out.String(), "PAGE reached to "+installer.Key) {
		t.Errorf("the page was not delivered:\n%s", out)
	}

	// Unanswered, it widens exactly once, to the owner. There is no second widening.
	path := p(ctx, t, d)
	for range 3 {
		if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
			t.Fatalf("a pass stopped: %v", err)
		}
	}
	events, err = path.notifier.EventsFor(ctx, raised.Raised)
	if err != nil {
		t.Fatalf("reading the page events: %v", err)
	}
	var widened int
	for _, e := range events {
		if notifier.Event(e.Event) == notifier.EventWidened {
			widened++
			if e.Reached != d.human {
				t.Errorf("the page widened to %q, want the owner %q", e.Reached, d.human)
			}
		}
	}
	if widened != 1 {
		t.Errorf("the page widened %d times, and unanswered it widens exactly once", widened)
	}

	// Cleared at the drift detector and nowhere else, and the answered event
	// is written by the pass that finds it cleared — because that store calls
	// nothing.
	if _, err := driftdetector.NewWriter(d.driftdetector).Clear(ctx, raised.Raised, installer.Key); err != nil {
		t.Fatalf("clearing the mismatch: %v", err)
	}
	if err := path.watchPass(ctx, theServiceRecord(t, ctx, path)); err != nil {
		t.Fatalf("the pass after the clearing stopped: %v", err)
	}
	events, err = path.notifier.EventsFor(ctx, raised.Raised)
	if err != nil {
		t.Fatalf("reading the page events: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != installer.Key {
		t.Errorf("the page's last event is %+v, want answered by %q", last, installer)
	}

	// And with the mismatch cleared, the row is the score's again.
	stillHeld, why, err := driftdetector.NewStore(d.driftdetector).Mismatch(ctx, serviceOf(ctx, t, d))
	if err != nil || stillHeld {
		t.Errorf("Mismatch = %v %q, %v; a cleared one holds nothing", stillHeld, why, err)
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify over a log holding page events beside the decisions: %v", err)
	}
}
