// The spend ceiling: the sum a credential is compared against, the two stops it
// fails closed on, the clear that authorises an overage for one period, and the
// notice at a fraction of the amount authored. Split from credential_test.go by
// subject at the 500-line bound, sharing db_test.go's fixtures and package.
package dispatch_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/intent"
)

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
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}

	next := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"})
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
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
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
	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"})
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
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}
	held := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(held), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
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
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(held), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the dispatch after the clear: %v", err)
	}

	// The next period. The clear named the period it authorised, so the same
	// units stop the credential again in a period nothing has cleared — here
	// the period the runs already written fall into once the owner re-anchors
	// and lengthens it, which is the re-bucketing the design derives at the
	// read rather than storing on a record.
	c.authorCeiling(t, 2.0, yesterday(), 2)
	next := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("SpecAuthor in the next period = %v holding %q, want the ceiling holding again",
			err, run.Held)
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
	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
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

// TestAClearDoesNotLiftTheUnpricedHold is the same section's division of the two
// stops onto two clears: the ceiling reached is cleared by authorising an
// overage for the period, and a run whose converted amount is absent is cleared
// by authoring the rate. An overage authorised for the period must not lift the
// second, or a kind nobody priced would go on being spent for the rest of it.
func TestAClearDoesNotLiftTheUnpricedHold(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{
		Text: aSpec, Units: map[string]int64{agent.UnitsInput: 3, agent.UnitsOutput: 1},
	}}, nil, 3)
	c.lend(t)
	// Only the input is priced, so every run under this ceiling is unpriced.
	c.authorRate(t, agent.UnitsInput, 0.001)
	c.authorCeiling(t, 1000.0, today(), 1)

	spent := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run whose output kind has no rate: %v", err)
	}
	held := c.oneItem(t, intent.StateRefined)
	if _, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(held), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) ||
		run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("the dispatch under an unpriced run = %v holding %q, want the ceiling hold, failing closed", err, run.Held)
	}

	if err := c.dispatch.ClearCeiling(c.ctx, owner, theCredential); err != nil {
		t.Fatalf("ClearCeiling: %v", err)
	}
	next := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("the dispatch after an overage was authorised = %v holding %q, want the unpriced run holding still",
			err, run.Held)
	}
	stopped, found := c.holdOn(t, next.ID, dispatch.HoldCredentialAtCeiling)
	if !found || len(stopped.WantsARate) != 1 {
		t.Fatalf("the hold is %+v, %v; want one entry naming what wants a rate", stopped, found)
	}
	if _, standing := c.holdOn(t, held.ID, dispatch.HoldCredentialAtCeiling); !standing {
		t.Error("the clear lifted the hold on the item declined for an unpriced run")
	}
}

// TestAClearNamesThePeriodInForceAndNotAPastOne is the row a period that has
// ended leaves: the ceiling's own row is about the credential and the period
// both, so a clear reaches the row of the period in force and refuses where the
// only row open is a past period's — which would otherwise authorise an overage
// in a period nothing has yet reached the ceiling in.
func TestAClearNamesThePeriodInForceAndNotAPastOne(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 5}}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 2.0, today(), 1)

	spent := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}
	held := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(held), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the item the ceiling declined = %v, want ErrHeld", err)
	}

	// The period rolls over: re-anchoring and lengthening puts the period in
	// force at another start, and the row standing open names the old one. The
	// runs re-bucket into the new period and the sum is past the ceiling there
	// too, but nothing has written the row for it yet.
	c.authorCeiling(t, 2.0, yesterday(), 2)
	if err := c.dispatch.ClearCeiling(c.ctx, owner, theCredential); !errors.Is(err, dispatch.ErrNoCeilingHold) {
		t.Fatalf("clearing against a past period's row = %v, want ErrNoCeilingHold", err)
	}

	// The dispatch that meets the ceiling in the new period writes that
	// period's own row, and the clear reaches it.
	next := c.oneItem(t, intent.StateRefined)
	if _, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) ||
		run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("the dispatch in the new period = %v holding %q, want the ceiling holding again", err, run.Held)
	}
	standing := c.credentialRowsOf(t, dispatch.KindCredentialAtCeiling)
	if len(standing) != 2 {
		t.Fatalf("%d ceiling rows stand, want one per period reached", len(standing))
	}
	if err := c.dispatch.ClearCeiling(c.ctx, owner, theCredential); err != nil {
		t.Fatalf("ClearCeiling in the period in force: %v", err)
	}
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the dispatch after the clear: %v", err)
	}
}

// TestTheFractionIsNotifiedOncePerCredentialAndPeriod is the reading the design
// puts before the hold: the factory notifies at a fixed fraction of an authored
// ceiling and of nothing else, through the notifier as a delivery, so the hold
// is not the first anyone hears of it. Dispatch makes the call wherever it reads
// the sum, naming the credential and the period the delivery is keyed on.
func TestTheFractionIsNotifiedOncePerCredentialAndPeriod(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 1}}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 10.0, today(), 1)

	// One unit at a rate of one is a tenth of the ceiling, which is under the
	// fraction: the first dispatch reads the sum and says nothing.
	first := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(first), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the first run: %v", err)
	}
	if len(c.told.nearing) != 0 {
		t.Fatalf("the notifier was told %+v with a tenth of the ceiling spent", c.told.nearing)
	}

	// Eight more units put the sum at nine tenths, past the fraction.
	c.model.replies = []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 8}}}
	c.model.calls = 0
	second := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(second), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that passes the fraction: %v", err)
	}
	third := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(third), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run after the fraction was passed: %v", err)
	}
	if len(c.told.nearing) == 0 {
		t.Fatal("the notifier was told nothing with nine tenths of the ceiling spent")
	}
	told := c.told.nearing[0]
	if told.credential != theCredential || told.periodStart == "" || told.ceiling != 10.0 ||
		told.currency != "USD" || told.spent < dispatch.NotifiedAtFraction*10.0 {
		t.Errorf("the notice is %+v, want the credential, the period, the amount authored and the sum past the fraction", told)
	}
}

// TestACallThatSucceededIsAReportTheCeilingIsComparedAt: the ceiling is
// compared at each report and not only at a stage's start and on the retry
// path, so the call that put the sum past it leaves the credential's own row
// and the notice before it at once — not at whatever the next dispatch onto
// that credential is. The stage that finished is not held for it: it proceeded,
// and what the row is about is the credential.
func TestACallThatSucceededIsAReportTheCeilingIsComparedAt(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 5}}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 2.0, today(), 1)

	it := c.oneItem(t, intent.StateRefined)
	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}
	if run.Held != "" {
		t.Errorf("the run that authored held on %q, and a stage that proceeded is not held", run.Held)
	}
	standing := c.credentialRowsOf(t, dispatch.KindCredentialAtCeiling)
	if len(standing) != 1 || standing[0].PeriodStart == "" {
		t.Fatalf("the ceiling rows standing after the report are %+v, want one naming the period's start", standing)
	}
	if len(c.told.nearing) == 0 {
		t.Error("the notifier was told nothing at the report that took the sum past the ceiling")
	}
	if _, found := c.holdOn(t, it.ID, dispatch.HoldCredentialAtCeiling); found {
		t.Error("the item whose stage authored carries a ceiling hold, and nothing about it could not proceed")
	}
}

// TestARematchClosesACeilingRowWhoseConditionHasEnded: a hold ends when its
// condition ends, and the credential's own ceiling row is the one no component
// is left to close. A re-match writes its second row where the ceiling no
// longer holds — and the close is not an overage authorised for the period, so
// the same credential holds again the moment the sum passes the amount in
// force.
func TestARematchClosesACeilingRowWhoseConditionHasEnded(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec, Units: map[string]int64{agent.UnitsOutput: 5}}}, nil, 3)
	c.lend(t)
	c.authorRate(t, agent.UnitsOutput, 1.0)
	c.authorCeiling(t, 2.0, today(), 1)

	spent := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(spent), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("the run that spends the ceiling: %v", err)
	}
	held := c.oneItem(t, intent.StateRefined)
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(held), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("the item the ceiling declined = %v, want ErrHeld", err)
	}
	if standing := c.credentialRowsOf(t, dispatch.KindCredentialAtCeiling); len(standing) != 1 {
		t.Fatalf("%d ceiling rows stand before the ceiling is raised, want one", len(standing))
	}

	// The owner raises the ceiling over what has been spent, which ends the
	// condition without authorising anything.
	c.authorCeiling(t, 100.0, today(), 1)
	if _, err := c.dispatch.Rematch(c.ctx); err != nil {
		t.Fatalf("Rematch: %v", err)
	}
	if standing := c.credentialRowsOf(t, dispatch.KindCredentialAtCeiling); len(standing) != 0 {
		t.Fatalf("the ceiling rows standing after the raise are %+v, want none", standing)
	}
	if _, found := c.holdOn(t, held.ID, dispatch.HoldCredentialAtCeiling); found {
		t.Error("the item's own ceiling hold stands after the ceiling was raised over the sum")
	}

	// The close said the condition ended and never that an overage was
	// authorised for the period: the same period stops the credential again at
	// the amount now in force.
	c.authorCeiling(t, 2.0, today(), 1)
	next := c.oneItem(t, intent.StateRefined)
	if _, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(next), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) ||
		run.Held != dispatch.HoldCredentialAtCeiling {
		t.Fatalf("the dispatch under the ceiling in force again = %v holding %q, want the ceiling hold",
			err, run.Held)
	}
}
