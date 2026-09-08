// The safeguard on the report store that holds a report-derived intent, as this
// component reads it: what waits while it stands, and what moves when a human
// admits the intent or the owner withdraws the safeguard. Split from
// hold_test.go by subject at the length a file is held to, sharing db_test.go's
// fixtures and package.
package dispatch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// heldIntents is [dispatch.Admissions] a test sets: whether the safeguard on
// the report store that holds a report-derived intent stands.
type heldIntents struct{ holds bool }

// HoldsReportDerivedIntents answers what the test set.
func (h *heldIntents) HoldsReportDerivedIntents(context.Context) (bool, error) {
	return h.holds, nil
}

// TestAReportDerivedIntentWaitsForAHumansAdmission: with the safeguard on the
// report store in force, an intent grouped from reports waits — no agent is put
// on the intent itself, so no interview round runs, and none on anything
// decomposed from it either. The human's admission is what lets both proceed.
func TestAReportDerivedIntentWaitsForAHumansAdmission(t *testing.T) {
	reading := agent.Reply{
		Text:  "READING:\nREQUIREMENT: When the form is saved, the system shall answer inside a second.",
		Units: map[string]int64{agent.UnitsOutput: 4},
	}
	c := newDispatch(t, []agent.Reply{reading, reading}, nil, 3)
	c.admissions.holds = true

	in, err := c.intake.TakeIn(c.ctx, owner, intent.Arrival{
		Source: intent.SourceReports, ProjectID: oneProject,
		Statement: "2 end-user report(s) grouped as one problem",
	})
	if err != nil {
		t.Fatalf("TakeIn a report-derived intent: %v", err)
	}
	onTheIntent := dispatch.On{IntentID: in.ID, ProjectID: oneProject, CountedSoFar: 1}

	_, run, err := c.dispatch.Interviewer(c.ctx, onTheIntent, nil,
		agent.Interviewing{Statement: in.Statement})
	if !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("Interviewer on an intent nobody has admitted = %v, want ErrHeld", err)
	}
	if run.Held != dispatch.HoldIntentAwaitsAdmission || run.HoldRow == "" {
		t.Errorf("the run held on %q as row %q, want the admission as a wait row", run.Held, run.HoldRow)
	}
	if c.model.calls != 0 {
		t.Error("an interview round ran on an intent no human had admitted")
	}

	// An item decomposed from it is held the same way, so nothing below the
	// intent is spent either.
	it, err := c.decomposition.Create(c.ctx, decompositionActor, item.New{
		IntentID: in.ID, ServiceID: oneService, AreaID: oneArea, Branch: "item/save",
	}, oneProject, oneProject, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	_, run, err = c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldIntentAwaitsAdmission {
		t.Errorf("SpecAuthor on an item of an unadmitted intent = %v, held %q", err, run.Held)
	}

	// The human's admission at Work, and the same dispatch runs.
	if err := c.intake.Admit(c.ctx, owner, in.ID); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	read, run, err := c.dispatch.Interviewer(c.ctx, onTheIntent, nil,
		agent.Interviewing{Statement: in.Statement})
	if err != nil {
		t.Fatalf("Interviewer after the admission: %v", err)
	}
	if len(read.Requirements) != 1 || run.Held != "" {
		t.Errorf("the run after the admission is %+v, want the reading with nothing holding it", run)
	}
	if c.model.calls != 1 {
		t.Errorf("the model was called %d times, want the one round the admission let run", c.model.calls)
	}
}

// TestWithdrawingTheAdmissionSafeguardReleasesWhatWaited: the safeguard is read
// in force at every dispatch and not marked at the intent's arrival, so an
// owner who withdraws it releases what was waiting on it without any human
// admitting anything.
func TestWithdrawingTheAdmissionSafeguardReleasesWhatWaited(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{
		Text:  "READING:\nREQUIREMENT: When the form is saved, the system shall answer inside a second.",
		Units: map[string]int64{agent.UnitsOutput: 4},
	}}, nil, 3)
	c.admissions.holds = true

	in, err := c.intake.TakeIn(c.ctx, owner, intent.Arrival{
		Source: intent.SourceReports, ProjectID: oneProject,
		Statement: "1 end-user report(s) grouped as one problem",
	})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	onTheIntent := dispatch.On{IntentID: in.ID, ProjectID: oneProject, CountedSoFar: 1}
	if _, _, err := c.dispatch.Interviewer(c.ctx, onTheIntent, nil,
		agent.Interviewing{Statement: in.Statement}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("Interviewer while the safeguard stands = %v, want ErrHeld", err)
	}

	c.admissions.holds = false
	_, run, err := c.dispatch.Interviewer(c.ctx, onTheIntent, nil,
		agent.Interviewing{Statement: in.Statement})
	if err != nil || run.Held != "" {
		t.Fatalf("Interviewer after the withdrawal = %v, held %q", err, run.Held)
	}

	// The row the wait stood as is closed by the re-match, so nothing goes on
	// showing a human as owing an admission they no longer owe.
	held, _, err := c.dispatch.Open(c.ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, one := range held {
		if one.Condition == dispatch.HoldIntentAwaitsAdmission {
			t.Errorf("the admission's row still stands after the safeguard was withdrawn: %+v", one)
		}
	}
}
