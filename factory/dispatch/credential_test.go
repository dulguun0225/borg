// The two conditions the credential an entry runs on stops a dispatch on — a
// credential a run could not reach, and one at its spend ceiling — and what a
// run that proceeded records about whose account it spent. Split from
// db_test.go by subject, sharing its fixtures and its package.
package dispatch_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
)

// theLender is the per-person key of whoever lent theCredential, which is what
// a run record carries and never a name.
const theLender = "pk_00000000000000000000000000000001"

// lend declares that theLender lent theCredential, which is what makes the
// credential one the People declaration knows: a credential nobody lent is
// unbounded and has no lender to record.
func (c composed) lend(t *testing.T) {
	t.Helper()
	if _, err := c.lends.Lend(c.ctx, owner, theLender, theCredential, people.AccountPerson); err != nil {
		t.Fatalf("Lend: %v", err)
	}
}

// authorRate authors the price of one kind of unit on theCredential, for the
// model version and effort these tests dispatch at.
func (c composed) authorRate(t *testing.T, unit string, rate float64) {
	t.Helper()
	if _, err := c.lends.AuthorRate(c.ctx, owner, theCredential, "USD", unit, modelName, "", rate); err != nil {
		t.Fatalf("AuthorRate for %s: %v", unit, err)
	}
}

// authorCeiling authors a spend ceiling on theCredential over periods of days
// beginning on startDate. Authoring it again replaces it, which is how one is
// raised, lowered, lengthened or re-anchored — and re-anchoring is what puts
// the runs already written into another period, the period a run falls in being
// derived at the read and on no record.
func (c composed) authorCeiling(t *testing.T, amount float64, startDate string, days int) {
	t.Helper()
	if _, err := c.lends.AuthorCeiling(c.ctx, owner, theCredential, people.Ceiling{
		Amount: amount, Currency: "USD", Length: days, Unit: people.PeriodDay,
		StartDate: startDate, StartZone: "UTC",
	}); err != nil {
		t.Fatalf("AuthorCeiling: %v", err)
	}
}

// today and yesterday are the calendar dates a ceiling's period is anchored on.
func today() string     { return time.Now().UTC().Format("2006-01-02") }
func yesterday() string { return time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02") }

// credentialRowsOf is the credential rows of one kind standing open in the log,
// read the way Work reads them: a wait row whose payload names the kind. They
// are not this component's per-item holds, so [dispatch.Dispatch.Open] does not
// answer them.
func (c composed) credentialRowsOf(t *testing.T, kind string) []dispatch.CredentialWait {
	t.Helper()
	rows, err := c.reader.ByShape(c.ctx, principal.OfComponent("test"), decisionlog.ShapeWait)
	if err != nil {
		t.Fatalf("ByShape: %v", err)
	}
	closed := map[string]bool{}
	for _, row := range rows {
		if row.Part == decisionlog.PartClose {
			closed[row.Closes] = true
		}
	}
	var standing []dispatch.CredentialWait
	for _, row := range rows {
		if row.Part != decisionlog.PartOpen || closed[row.ID] {
			continue
		}
		var wait dispatch.CredentialWait
		if err := json.Unmarshal([]byte(row.Payload), &wait); err != nil || wait.Kind != kind {
			continue
		}
		standing = append(standing, wait)
	}
	return standing
}

// holdOn is the open hold of one condition on one item, and false where none
// stands.
func (c composed) holdOn(t *testing.T, itemID, condition string) (dispatch.Hold, bool) {
	t.Helper()
	open, _, err := c.dispatch.Open(c.ctx)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	for _, one := range open {
		if one.ItemID == itemID && one.Condition == condition {
			return one, true
		}
	}
	return dispatch.Hold{}, false
}

// TestACredentialAUnreachableRunDeclinesEveryOtherItemAndIsClearedByASuccess is
// ../../end-goal/how-the-factory-works/10-fleet/05-an-account-that-runs-out-is-a-hold.md:
// the row is written by whoever could not reach, naming the credential and
// never the entry; dispatch puts nothing further onto that name, writing a hold
// of its own per item it declines; and dispatching again closes the credential
// row and each of its own, so the hold ends where the work resumes.
func TestACredentialAUnreachableRunDeclinesEveryOtherItemAndIsClearedByASuccess(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{}, {Text: aSpec}},
		[]error{&agent.StatusError{Status: 429, Body: "the quota is spent"}, nil}, 3)
	first := c.oneItem(t, intent.StateRefined)
	second := c.oneItem(t, intent.StateRefined)

	// The run's own failure to reach the model is the hold, not a failed
	// attempt: no reply was refused, so nothing is retried here.
	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(first), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialUnreachable {
		t.Fatalf("SpecAuthor on an unreachable credential = %v holding %q, want the unreachable hold",
			err, run.Held)
	}
	standing := c.credentialRowsOf(t, dispatch.KindCredentialUnreachable)
	if len(standing) != 1 || standing[0].CredentialName != theCredential {
		t.Fatalf("the credential rows standing are %+v, want one naming %s", standing, theCredential)
	}
	if standing[0].OpenedFor != first.ID {
		t.Errorf("the row was opened for %q, want the run whose failure opened it", standing[0].OpenedFor)
	}

	// Every further item onto that credential is declined with a hold of its
	// own, so two items waiting on one credential are two rows in Work.
	_, declined, err := c.dispatch.SpecAuthor(c.ctx, on(second), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || declined.Held != dispatch.HoldCredentialUnreachable {
		t.Fatalf("the second item = %v holding %q, want a hold of its own", err, declined.Held)
	}
	held, found := c.holdOn(t, second.ID, dispatch.HoldCredentialUnreachable)
	if !found || held.CredentialName != theCredential {
		t.Fatalf("the second item's hold is %+v, %v; want one naming the credential", held, found)
	}
	if c.model.calls != 1 {
		t.Errorf("%d calls, want no agent put onto a credential already known unreachable", c.model.calls)
	}

	// The run whose failure opened the row is the one that reaches for the
	// credential again, and a call that succeeds closes the row and lifts the
	// hold on the item that was declined.
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(first), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the retry of the run that opened the row: %v", err)
	}
	if standing := c.credentialRowsOf(t, dispatch.KindCredentialUnreachable); len(standing) != 0 {
		t.Errorf("%d credential rows still stand after a run reached it", len(standing))
	}
	if _, found := c.holdOn(t, second.ID, dispatch.HoldCredentialUnreachable); found {
		t.Error("the declined item's hold still stands after the credential was reached")
	}
}

// TestACeilingReachedHoldsEveryFurtherItemAndRoutesToTheOwner is
// ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md: the sum
// is a query over the converted amounts of the run records naming the
// credential in the period, dispatch puts nothing further onto that name until
// it clears, and the row routes to the owner rather than to whoever lent it.
func TestACeilingReachedHoldsEveryFurtherItemAndRoutesToTheOwner(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 5}}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 2.0, today(), 1)

	// The first run spends five units at a rate of one, which is past a ceiling
	// of two: nothing reserved it beforehand, so the run happens and the sum is
	// what stops the next dispatch.
	spent := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}

	next := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(next), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("SpecAuthor onto a credential at its ceiling = %v holding %q, want the ceiling hold",
			err, run.Held)
	}
	held, found := c.holdOn(t, next.ID, dispatch.HoldCredentialAtCeiling)
	if !found {
		t.Fatal("no ceiling hold stands on the item that was declined")
	}
	if held.CredentialName != theCredential || held.RoutedTo != dispatch.RoutedToTheOwner {
		t.Errorf("the hold is %+v, want the credential named and the row routed to the owner", held)
	}
	// Beside the per-item row stands one row about the credential and the
	// period, which is what an owner clears.
	standing := c.credentialRowsOf(t, dispatch.KindCredentialAtCeiling)
	if len(standing) != 1 || standing[0].PeriodStart == "" {
		t.Fatalf("the ceiling rows standing are %+v, want one naming the period's start", standing)
	}
}

// TestAnUnpricedKindFailsClosed is the same section's other half: a run whose
// converted amount is absent because a kind it returned has no rate gives the
// ceiling nothing to sum, so a credential under one fails closed, and the row
// names the kind, the model version and the effort that want a rate.
func TestAnUnpricedKindFailsClosed(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{
		Text: aSpec, Units: map[string]int64{agent.UnitsInput: 3, agent.UnitsOutput: 1},
	}}, nil, 3)
	c.lend(t)
	// Only the input is priced, so the run the ceiling would sum has no amount.
	c.authorRate(t, agent.UnitsInput, 0.001)
	c.authorCeiling(t, 1000.0, today(), 1)

	spent := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run whose output kind has no rate: %v", err)
	}
	runs, err := agentrun.ForItem(c.ctx, c.pool, spent.ID)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(runs) != 1 || runs[0].Priced {
		t.Fatalf("the run record is %+v, want a converted amount absent for the unpriced kind", runs)
	}

	next := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(next), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("SpecAuthor under a ceiling with an unpriced run = %v holding %q, want the ceiling hold, failing closed",
			err, run.Held)
	}
	held, found := c.holdOn(t, next.ID, dispatch.HoldCredentialAtCeiling)
	if !found || len(held.WantsARate) != 1 {
		t.Fatalf("the hold is %+v, %v; want one entry naming what wants a rate", held, found)
	}
	wants := held.WantsARate[0]
	if wants.ModelVersion != modelName || wants.Effort != "" ||
		len(wants.Kinds) != 1 || wants.Kinds[0] != agent.UnitsOutput {
		t.Errorf("the hold wants a rate for %+v, want the output kind at this model version and effort", wants)
	}
}

// TestAClearAuthorisesAnOverageForThatPeriodAlone: clearing at Work authorises
// an overage for the period in force and does not reset the sum, so the same
// units stop the next period at the count authored for it. The period is
// derived at the read from the start date in force, so re-anchoring it is what
// makes the next period next.
func TestAClearAuthorisesAnOverageForThatPeriodAlone(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 5}}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 2.0, today(), 1)

	spent := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}
	held := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(held), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the item the ceiling declined = %v, want ErrHeld", err)
	}

	// Clearing is the owner's: a component that could clear one would be the
	// factory authorising its own spend.
	if err := c.dispatch.ClearCeiling(c.ctx, dispatch.Actor, theCredential); !errors.Is(err, dispatch.ErrNotTheOwner) {
		t.Errorf("a component clearing the ceiling = %v, want ErrNotTheOwner", err)
	}
	if err := c.dispatch.ClearCeiling(c.ctx, owner, theCredential); err != nil {
		t.Fatalf("ClearCeiling: %v", err)
	}
	if _, found := c.holdOn(t, held.ID, dispatch.HoldCredentialAtCeiling); found {
		t.Error("the ceiling hold still stands after the owner cleared it")
	}
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(held), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the dispatch after the clear: %v", err)
	}

	// The next period. The clear named the period it authorised, so the same
	// units stop the credential again in a period nothing has cleared — here
	// the period the runs already written fall into once the owner re-anchors
	// and lengthens it, which is the re-bucketing the design derives at the
	// read rather than storing on a record.
	c.authorCeiling(t, 2.0, yesterday(), 2)
	next := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(next), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("SpecAuthor in the next period = %v holding %q, want the ceiling holding again",
			err, run.Held)
	}
}

// TestTheRunRecordNamesWhatItRanOnAndWhatItSpent: the six fields no caller
// filled before the fleet entry was a record — the processing location off the
// entry, and the lender's per-person key, the account kind, the rates and the
// converted amount off the People declaration at the run.
func TestTheRunRecordNamesWhatItRanOnAndWhatItSpent(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{
		Text: aSpec, Units: map[string]int64{agent.UnitsInput: 20, agent.UnitsOutput: 5},
	}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsInput, 0.01)
	c.authorRate(t, agent.UnitsOutput, 0.1)

	it := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}
	runs, err := agentrun.ForItem(c.ctx, c.pool, it.ID)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("%d run records, want one per call", len(runs))
	}
	recorded := runs[0]
	if recorded.ProcessingLocation != "vendor/test-region" {
		t.Errorf("the run names processing location %q, want the entry's", recorded.ProcessingLocation)
	}
	if recorded.LenderKey != theLender || recorded.AccountKind != agentrun.AccountPerson {
		t.Errorf("the run names lender %q on a %q account, want the declaration's key and kind",
			recorded.LenderKey, recorded.AccountKind)
	}
	if recorded.RatesByKind[agent.UnitsInput] != 0.01 || recorded.RatesByKind[agent.UnitsOutput] != 0.1 {
		t.Errorf("the run names rates %v, want the ones authored per kind", recorded.RatesByKind)
	}
	// Twenty units of input at a hundredth and five of output at a tenth.
	if !recorded.Priced || fmt.Sprintf("%.4f", recorded.ConvertedAmount) != "0.7000" ||
		recorded.Currency != "USD" {
		t.Errorf("the run converted to %v %s (priced %v), want 0.7 USD",
			recorded.ConvertedAmount, recorded.Currency, recorded.Priced)
	}
}

// TestTheCeilingIsComparedAtEachReport: the ceiling is compared at each report
// the agent makes rather than at a stage's end, so a stage whose refused reply
// put the sum past it is held mid-stage instead of retrying on an account the
// owner bounded — which is what bounds overshoot to one report's worth of
// units.
func TestTheCeilingIsComparedAtEachReport(t *testing.T) {
	c := newDispatch(t, []agent.Reply{
		{Text: "not the protocol", Units: map[string]int64{agent.UnitsOutput: 5}},
		{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 5}},
	}, nil, 5)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 2.0, today(), 1)

	it := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, on(it), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("SpecAuthor whose first report spent the ceiling = %v holding %q, want the ceiling hold",
			err, run.Held)
	}
	if c.model.calls != 1 {
		t.Errorf("%d calls, want the stage held after the report that spent the ceiling", c.model.calls)
	}
	if len(run.AgentRunIDs) != 1 {
		t.Errorf("%d run records, want one for the call that was made", len(run.AgentRunIDs))
	}
}
