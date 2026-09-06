// Tests of a change freeze taking the same two exceptions a halt does: a
// revert, and an item the health monitor raised on that service, so a freeze
// never holds the fix for what the freeze made worse.
package main

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// freezeStartsAt and freezeEndsAt bound a period wide enough to cover every
// moment any run of this test executes in, which is what makes the freeze
// itself uninteresting here and the two exceptions the whole of what is
// tested.
const (
	freezeStartsAt = "2000-01-01T00:00:00.000000000Z"
	freezeEndsAt   = "9999-01-01T00:00:00.000000000Z"
)

// frozenService authors a change freeze covering every moment this test runs
// in, on the install's one service, and reads the record back — a freeze
// being authored outright with nothing supplied.
func frozenService(ctx context.Context, t *testing.T, path *path) service.Service {
	t.Helper()
	svc := theServiceRecord(t, ctx, path)
	author := owner(t, ctx, path.d.pool, path.d.token, path.d.human)
	if _, err := path.factory.AuthorChangeFreezePeriod(ctx, author, svc.ID, freezeStartsAt, freezeEndsAt); err != nil {
		t.Fatalf("authoring the change freeze: %v", err)
	}
	return theServiceRecord(t, ctx, path)
}

// TestAChangeFreezeHoldsAnOrdinaryChange is the freeze's own hold, over an item
// that is neither a revert nor one a detector raised — the baseline the two
// exceptions are read against.
func TestAChangeFreezeHoldsAnOrdinaryChange(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	path := p(ctx, t, d)
	svc := frozenService(ctx, t, path)

	c := authorOne(t, ctx, path, theStatement, out)
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}

	held, err := path.changeFreezeHold(ctx, svc, it)
	if err != nil {
		t.Fatalf("changeFreezeHold: %v", err)
	}
	if held == "" {
		t.Fatal("a service frozen for every moment this test runs in holds nothing for an ordinary item")
	}
	if !strings.Contains(held, gate.HoldChangeFreeze) {
		t.Errorf("the hold reads %q, want the freeze's own words", held)
	}

	standing, err := path.Standing(ctx, gate.Subjects{
		Row: gate.DeployToProduction, ItemID: c.itemID, ServiceID: svc.ID,
	})
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if !slices.Contains(standing, gate.HoldChangeFreeze) {
		t.Errorf("Standing = %v, want the change freeze among the holds the row reads", standing)
	}
}

// TestAChangeFreezePassesARevert is the freeze's first exception: a revert
// passes it for the same reason it passes a halt, so a freeze never holds the
// fix for what the freeze made worse.
func TestAChangeFreezePassesARevert(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	shipped := only(t, res)

	path := p(ctx, t, d)
	svc := frozenService(ctx, t, path)

	if err := path.revertIntent(ctx, svc, shipped.releaseID, "it broke checkout"); err != nil {
		t.Fatalf("revertIntent: %v", err)
	}
	revert, found, err := intent.OnEvidence(ctx, d.pool, intent.Evidence{ServiceID: svc.ID, ReleaseID: shipped.releaseID})
	if err != nil || !found {
		t.Fatalf("OnEvidence over the revert = found %v, %v", found, err)
	}
	revertItem, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: revert.ID, ServiceID: svc.ID, Branch: "item/revert",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the revert item: %v", err)
	}

	held, err := path.changeFreezeHold(ctx, svc, revertItem)
	if err != nil {
		t.Fatalf("changeFreezeHold over the revert: %v", err)
	}
	if held != "" {
		t.Errorf("the freeze holds %q against a revert, and a freeze never holds the fix for what the freeze made worse", held)
	}
}

// TestAChangeFreezePassesAnItemTheHealthMonitorRaised is the freeze's second
// exception: an item whose intent's source is the factory's own with the
// health monitor as the detector. An ordinary request on the same service
// still waits, which is what says only the two exceptions pass and not every
// detector's intent.
func TestAChangeFreezePassesAnItemTheHealthMonitorRaised(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	path := p(ctx, t, d)
	svc := frozenService(ctx, t, path)

	raised, err := path.intake.TakeIn(ctx, healthmonitor.Actor, intent.Arrival{
		Source:    intent.SourceDetector,
		Statement: svc.Name + " is failing a share of its requests",
		Evidence:  intent.Evidence{ServiceID: svc.ID},
	})
	if err != nil {
		t.Fatalf("taking the health monitor's intent in: %v", err)
	}
	fix, err := path.decomposition.Create(ctx, decompositionActor, item.New{
		IntentID: raised.ID, ServiceID: svc.ID, Branch: "fix-the-failing-share",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("writing the item the health monitor's intent decomposes into: %v", err)
	}

	held, err := path.changeFreezeHold(ctx, svc, fix)
	if err != nil {
		t.Fatalf("changeFreezeHold over the health monitor's own item: %v", err)
	}
	if held != "" {
		t.Errorf("the freeze holds %q against the item raised to fix it, and a freeze never holds the fix for what the freeze made worse", held)
	}

	// The owner's own request on the same service still waits: it is neither a
	// revert nor an item the health monitor raised.
	c := authorOne(t, ctx, path, theStatement, out)
	asked, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the owner's item: %v", err)
	}
	if held, err := path.changeFreezeHold(ctx, svc, asked); err != nil || held == "" {
		t.Errorf("changeFreezeHold over an owner's request = %q, %v; only the two exceptions pass", held, err)
	}
}
