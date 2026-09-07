// Episode five of the demonstration, over HTTP: the money. A ceiling authored
// on a lent credential at People, duties 8 and 9 at Factory beside it, a pass
// reporting past the ceiling mid-stage on every item it reaches, a hold per
// item naming the credential and routed to the owner, and the owner clearing
// them at Work for that period alone.
package main

import (
	"context"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/screens"
)

// theCeilingCurrency and theUnitRate are what a unit of this fake model's
// output converts at: one unit to one of the currency, so the ceiling a test
// authors is the count of units it allows and the arithmetic is readable in
// the assertions.
const (
	theCeilingCurrency = "USD"
	theUnitRate        = 1.0
)

// TestEpisodeFiveIsTheMoney is
// ../../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md
// through the screens: the ceiling is authored at People on a credential
// somebody lent, dispatch compares at each report the agent makes rather than
// at a stage's end, the hold is written mid-stage by whoever could not proceed
// and routed to the owner, Work shows every item stopped and Factory and the
// badge count them, and clearing at Work authorises an overage for the period
// in force alone.
func TestEpisodeFiveIsTheMoney(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	s := newScreens(t, ctx, d, out)

	// Duty 8 and duty 9 at Factory, which the design puts beside the money in
	// this episode: a parameter authored and a safeguard placed.
	s.mustCall(t, "authorParameter", screens.AuthorParameterArgs{
		Parameter: "attempt_limit", Value: "5", Stage: "implementation",
	})
	if s.mustCall(t, "placeSafeguard", screens.PlaceSafeguardArgs{
		Parameter: "window_limit", SubjectKind: "service", SubjectName: theService, Bound: "2",
	}) == "" {
		t.Fatal("placeSafeguard answered with no id")
	}

	// People: the credential the fleet already runs on, lent by a named human,
	// with a rate per kind and a ceiling far above what anything has spent.
	lender := owner(t, ctx, d.pool, d.token, "lender")
	s.mustCall(t, "lendCredential", screens.LendCredentialArgs{
		HumanKey: lender.Key, Credential: theFakeCredential, Kind: "person",
	})
	s.mustCall(t, "authorRate", screens.AuthorRateArgs{
		Credential: theFakeCredential, Kind: agent.UnitsOutput, ModelVersion: theModel,
		Amount: theUnitRate, Currency: theCeilingCurrency,
	})
	authorCeilingAt(t, s, 1000, today(), 1)

	var declaration screens.People
	s.get(t, "/api/people", &declaration)
	lent := lentCredentialOf(t, declaration, lender.Key)
	if lent.Ceiling == nil || lent.Ceiling.Amount != 1000 {
		t.Fatalf("People reads the ceiling as %+v, want the 1000 that was authored", lent.Ceiling)
	}
	if len(lent.Rates) != 1 || lent.Rates[0].Kind != agent.UnitsOutput {
		t.Errorf("People reads the rates as %+v, want the one authored per kind", lent.Rates)
	}

	// Three items, taken in at Work and carried to their Spec rows by a pass.
	// Every call the interview and the spec authors made is spend against the
	// period in force. Three rather than one because what the design claims is
	// about the items after the first: dispatch declines every further item
	// onto that credential with a hold of its own, so several items waiting on
	// one ceiling are several rows in Work and a count at Factory.
	var taken shipped
	if err := s.p.takeIn(ctx, &taken, of(theStatement, theSecondStatement, theThirdStatement)); err != nil {
		t.Fatalf("taking the intents in: %v\n%s", err, out)
	}
	if len(taken.candidates) != 3 {
		t.Fatalf("the three intents yielded %d item(s), want one each", len(taken.candidates))
	}
	itemIDs := make([]string, 0, len(taken.candidates))
	for _, c := range taken.candidates {
		itemIDs = append(itemIDs, c.itemID)
	}
	if _, err := s.p.advance(ctx); err != nil {
		t.Fatalf("the first pass: %v\n%s", err, out)
	}

	// Factory's two readings while the ceiling stands unreached: the burn rate
	// — units spent so far against the period in force — and when spending at
	// that rate projects to exhaust it.
	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	reading := spendCeilingOf(t, factory, theFakeCredential)
	if reading.Unbounded || reading.Ceiling != 1000 {
		t.Fatalf("Factory reads the ceiling as %+v, want the 1000 authored", reading)
	}
	if reading.BurnRate <= 0 {
		t.Fatalf("Factory reads a burn rate of %v after a pass that made model calls", reading.BurnRate)
	}
	if reading.ProjectedExhaustion == "" {
		t.Errorf("Factory projects no exhaustion at a burn rate of %v against a ceiling of %v",
			reading.BurnRate, reading.Ceiling)
	}
	spent := reading.BurnRate

	// The ceiling lowered under what has already been spent, which is what
	// authoring one again does: nothing reserved anything beforehand, so the
	// sum is what stops the next dispatch.
	authorCeilingAt(t, s, 1.0, today(), 1)

	// The verdicts that let the items leave their Spec rows, typed at Work.
	if approved := approveEveryPendingRow(t, ctx, s, gate.KindSpec); approved != len(itemIDs) {
		t.Fatalf("%d Spec row(s) were pending, want one per item\n%s", approved, out)
	}

	// The next pass reports past the ceiling mid-stage on each item in turn:
	// the hold is written by whoever could not proceed, and a hold is not a
	// failure — the pass goes on to the next item rather than ending at the
	// first, which is what makes the count below reach more than one.
	if _, err := s.p.advance(ctx); err != nil {
		t.Fatalf("the pass under a ceiling already spent = %v; a hold ends no pass\n%s", err, out)
	}

	// One hold per item, each naming the credential and never the entry, and
	// each routed to the owner: raising or clearing one is the owner's, so no
	// row of the three credential holds reaches a human who cannot act on it.
	for _, itemID := range itemIDs {
		held := ceilingHoldOn(t, ctx, s, itemID)
		if held.CredentialName != theFakeCredential {
			t.Errorf("the hold on %s names credential %q, want %q", itemID, held.CredentialName, theFakeCredential)
		}
		if held.RoutedTo != dispatch.RoutedToTheOwner {
			t.Errorf("the hold on %s routes to %q, want the owner", itemID, held.RoutedTo)
		}
	}

	// Work shows every one of them stopped, and the home view's own filter —
	// everything on the board that also waits on a human — keeps them.
	var work screens.Work
	s.get(t, "/api/work?waiting_on_a_human=true", &work)
	stopped := map[string]bool{}
	for _, row := range work.Rows {
		if row.Stop != nil && row.Stop.Cause == dispatch.HoldCredentialAtCeiling {
			stopped[row.ItemID] = true
		}
	}
	for _, itemID := range itemIDs {
		if !stopped[itemID] {
			t.Errorf("Work shows no row for item %s stopped at its credential's ceiling: %+v", itemID, work.Rows)
		}
	}
	if len(stopped) != len(itemIDs) {
		t.Errorf("Work shows %d row(s) stopped at the ceiling, want one per item", len(stopped))
	}

	// Factory counts what is stopped at dispatch, by cause, and the badge
	// counts the same holds — a hold whose named cause is a record only a
	// human writes is a wait on a human.
	s.get(t, "/api/factory", &factory)
	counted := int64(0)
	for _, cause := range factory.StoppedAtDispatch {
		if cause.Cause == dispatch.HoldCredentialAtCeiling {
			counted = cause.Count
		}
	}
	if counted != int64(len(itemIDs)) {
		t.Errorf("Factory counts %d item(s) stopped at the ceiling, want one per item held: %+v",
			counted, factory.StoppedAtDispatch)
	}
	var home screens.Home
	s.get(t, "/api/home", &home)
	if home.Badge.FactoryHoldsForAHuman != int64(len(itemIDs)) {
		t.Errorf("the badge counts %d of the factory's own holds, want one per item at the ceiling, %d",
			home.Badge.FactoryHoldsForAHuman, len(itemIDs))
	}
	if home.Badge.Total < home.Badge.FactoryHoldsForAHuman {
		t.Errorf("the badge totals %d with %d of the factory's own holds in it",
			home.Badge.Total, home.Badge.FactoryHoldsForAHuman)
	}

	// Clearing at Work authorises an overage for the period in force and does
	// not reset the sum. One clear ends every hold the credential's ceiling
	// left, the hold naming the credential and the period rather than an item.
	s.mustCall(t, "clearCeiling", screens.ClearCeilingArgs{Credential: theFakeCredential})
	for _, itemID := range itemIDs {
		if _, found := ceilingHold(t, ctx, s, itemID); found {
			t.Errorf("the ceiling hold still stands on item %s after the owner cleared it", itemID)
		}
	}
	s.get(t, "/api/factory", &factory)
	if reading := spendCeilingOf(t, factory, theFakeCredential); reading.BurnRate < spent {
		t.Errorf("the burn rate reads %v after the clear, want the sum undisturbed at or above %v",
			reading.BurnRate, spent)
	}

	// The clear named the period it authorised, so the same units stop the
	// credential again in a period nothing has cleared. The period is derived
	// at the read from the start date in force, so re-anchoring it is what
	// makes the next period next — which is how a test reaches the second
	// period without waiting a day out.
	authorCeilingAt(t, s, 1.0, yesterdayUTC(), 2)
	if _, err := s.p.advance(ctx); err != nil {
		t.Fatalf("the pass in a period nothing cleared = %v; a hold ends no pass\n%s", err, out)
	}
	for _, itemID := range itemIDs {
		if _, found := ceilingHold(t, ctx, s, itemID); !found {
			t.Errorf("no ceiling hold stands on item %s in the second period, and the clear authorised the first alone",
				itemID)
		}
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the episode: %v", err)
	}
}

// theFakeCredential is the credential the composition's own fleet entries run
// on, which is what a ceiling in these tests is authored against.
const theFakeCredential = "model.fake"

// today and yesterdayUTC are the calendar dates a ceiling's period is anchored
// on, in the zone the ceiling is authored in.
func today() string        { return time.Now().UTC().Format("2006-01-02") }
func yesterdayUTC() string { return time.Now().UTC().AddDate(0, 0, -1).Format("2006-01-02") }

// authorCeilingAt authors the ceiling on the fake credential over periods of
// days beginning on startDate. Authoring it again replaces it, which is how one
// is raised, lowered, lengthened or re-anchored.
func authorCeilingAt(t *testing.T, s *screenServer, amount float64, startDate string, days int64) {
	t.Helper()
	s.mustCall(t, "authorCeiling", screens.AuthorCeilingArgs{
		Credential: theFakeCredential, Amount: amount, Currency: theCeilingCurrency,
		PeriodUnit: "day", Length: days, StartDate: startDate, Zone: "UTC",
	})
}

// lentCredentialOf is the one credential a named human's People row lends.
func lentCredentialOf(t *testing.T, view screens.People, key string) screens.LentCredential {
	t.Helper()
	for _, row := range view.Rows {
		if row.Key != key || len(row.Credentials) == 0 {
			continue
		}
		return row.Credentials[0]
	}
	t.Fatalf("People holds no lent credential on the row of %s: %+v", key, view.Rows)
	return screens.LentCredential{}
}

// spendCeilingOf is Factory's reading of one credential's ceiling.
func spendCeilingOf(t *testing.T, view screens.Factory, credential string) screens.SpendCeiling {
	t.Helper()
	for _, one := range view.SpendCeilings {
		if one.Credential == credential {
			return one
		}
	}
	t.Fatalf("Factory reads no ceiling on %s: %+v", credential, view.SpendCeilings)
	return screens.SpendCeiling{}
}

// ceilingHold is the ceiling hold standing on one item, and whether one does.
func ceilingHold(t *testing.T, ctx context.Context, s *screenServer, itemID string) (dispatch.Hold, bool) {
	t.Helper()
	open, _, err := s.p.dispatch.Open(ctx)
	if err != nil {
		t.Fatalf("reading the holds dispatch wrote: %v", err)
	}
	for _, one := range open {
		if one.ItemID == itemID && one.Condition == dispatch.HoldCredentialAtCeiling {
			return one, true
		}
	}
	return dispatch.Hold{}, false
}

// ceilingHoldOn is [ceilingHold] where the test has already established that
// one stands.
func ceilingHoldOn(t *testing.T, ctx context.Context, s *screenServer, itemID string) dispatch.Hold {
	t.Helper()
	held, found := ceilingHold(t, ctx, s, itemID)
	if !found {
		t.Fatalf("no ceiling hold stands on item %s", itemID)
	}
	return held
}

// approveEveryPendingRow types an approve at Work against every row of the
// kind given that a pass left pending, which is what lets those items reach
// the stage after it, and answers with how many it closed.
func approveEveryPendingRow(t *testing.T, ctx context.Context, s *screenServer, kind gate.Kind) int {
	t.Helper()
	pending, err := s.p.gate.Pending(ctx)
	if err != nil {
		t.Fatalf("reading the pending rows: %v", err)
	}
	approved := 0
	for _, opened := range pending {
		if opened.Gate.Kind != kind {
			continue
		}
		s.mustCall(t, "decide", screens.DecideArgs{
			OpenEventID: opened.Row.ID, Verdict: string(gate.VerdictApprove),
		})
		approved++
	}
	if approved == 0 {
		t.Fatalf("no %s row is pending: %+v", kind, pending)
	}
	return approved
}
