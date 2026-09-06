// The service level objective at the production deploy row: the hold an
// exhausted or uncomputed error budget sets, and the two items that pass it.
package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// theObjective is what an owner authors here: the proportion of a quantity that
// must be good, and the period it is counted over, thirty days being the
// ordinary shape.
const (
	theObjective       = 0.999
	theObjectivePeriod = 30 * 24 * 3600
)

// TestAnAuthoredObjectiveHoldsTheProductionDeploy is the objective's first half.
// This platform keeps no series per service across builds, so no period can be
// cut out of one and the budget is uncomputed — which holds the way an exhausted
// one does, a budget taken as intact over records that are not there being an
// absent input read as evidence. The hold reaches the row it is computed at and
// the list the gate reads, and it is one of the fourteen the design names rather
// than a sentence in the output.
func TestAnAuthoredObjectiveHoldsTheProductionDeploy(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	path := p(ctx, t, d)
	svc := withObjective(ctx, t, path)

	c := authorOne(t, ctx, path, theStatement, out)
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}

	held, err := path.objectiveHold(ctx, svc, it)
	if err != nil {
		t.Fatalf("objectiveHold: %v", err)
	}
	if held == "" {
		t.Fatalf("an authored objective the store cannot compute holds nothing:\n%s", out)
	}
	if !strings.Contains(held, gate.HoldErrorBudgetExhausted) {
		t.Errorf("the hold reads %q, want the error budget's own words", held)
	}

	standing, err := path.Standing(ctx, gate.Subjects{
		Row: gate.DeployToProduction, ItemID: c.itemID, ServiceID: svc.ID,
	})
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if !slices.Contains(standing, gate.HoldErrorBudgetExhausted) {
		t.Errorf("Standing = %v, want the error budget among the holds the row reads", standing)
	}
}

// TestAnItemADetectorRaisedOnThatServicePassesTheBudgetHold is the exception
// that makes the hold liftable. The fix for whatever exhausted the budget is
// itself a production deploy, so an item whose intent a detector raised on that
// service passes; a request an owner raised on the same service does not, and
// its route is the objective's own intent.
func TestAnItemADetectorRaisedOnThatServicePassesTheBudgetHold(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	path := p(ctx, t, d)
	svc := withObjective(ctx, t, path)

	raised, err := path.intake.TakeIn(ctx, intakeActor, intent.Arrival{
		Source:    intent.SourceDetector,
		Statement: theService + " is failing a share of its requests",
		Evidence:  intent.Evidence{ServiceID: svc.ID},
	})
	if err != nil {
		t.Fatalf("taking the detector's intent in: %v", err)
	}
	fix, err := path.decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: raised.ID, ServiceID: svc.ID, Branch: "fix-the-failing-share",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("writing the item the detector's intent decomposes into: %v", err)
	}

	held, err := path.objectiveHold(ctx, svc, fix)
	if err != nil {
		t.Fatalf("objectiveHold over the detector's own item: %v", err)
	}
	if held != "" {
		t.Errorf("the budget holds %q against the item raised to fix it, and the hold would then hold hardest where production is worst", held)
	}

	// The owner's own request on the same service still waits.
	c := authorOne(t, ctx, path, theStatement, out)
	asked, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the owner's item: %v", err)
	}
	if held, err := path.objectiveHold(ctx, svc, asked); err != nil || held == "" {
		t.Errorf("objectiveHold over an owner's request = %q, %v; the route past the hold is the objective's intent", held, err)
	}
}

// withObjective authors the objective on the install's one service and reads the
// record back, an objective being authored outright with nothing supplied.
func withObjective(ctx context.Context, t *testing.T, path *path) service.Service {
	t.Helper()
	svc := theServiceRecord(t, ctx, path)
	author := owner(t, ctx, path.d.pool, path.d.token, path.d.human)
	if _, err := path.factory.AuthorObjective(ctx, author, svc.ID, theObjective, theObjectivePeriod); err != nil {
		t.Fatalf("authoring the objective: %v", err)
	}
	return theServiceRecord(t, ctx, path)
}
