// The factory's own hold on a declared dependency that is not its
// service's current release.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
)

// TestADeclaredDependencyThatIsNotLiveHolds is the factory's own hold at both
// deploy rows, and the one that writes nothing: a declared dependency that is not
// its service's current release. No run of this interface declares one — decomposition
// yields one item per intent — so the condition is driven directly against an item
// that waits on one that has not shipped.
func TestADeclaredDependencyThatIsNotLiveHolds(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	shippedItem := only(t, res).itemID

	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}

	// An item waiting on the one that shipped: its dependency is live, so nothing
	// holds.
	live, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: "in_dependent", ServiceID: res.serviceID, Branch: "item/dependent-live",
		WaitsOn: []string{shippedItem},
	})
	if err != nil {
		t.Fatalf("decomposing the dependent item: %v", err)
	}
	held, err := p.dependencyHold(ctx, live)
	if err != nil {
		t.Fatalf("dependencyHold: %v", err)
	}
	if held != "" {
		t.Errorf("the dependency is the service's current release and the hold says %q", held)
	}

	// An item waiting on one that has not shipped: the hold fires, and it names
	// the condition rather than a verdict.
	unshipped, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: "in_dependent2", ServiceID: res.serviceID, Branch: "item/dependent-waiting",
	})
	if err != nil {
		t.Fatalf("decomposing the item nothing shipped: %v", err)
	}
	waiting, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: "in_dependent3", ServiceID: res.serviceID, Branch: "item/dependent-held",
		WaitsOn: []string{unshipped.ID},
	})
	if err != nil {
		t.Fatalf("decomposing the held item: %v", err)
	}
	held, err = p.dependencyHold(ctx, waiting)
	if err != nil {
		t.Fatalf("dependencyHold: %v", err)
	}
	if !strings.Contains(held, gate.HoldDependencyNotLive) {
		t.Errorf("the hold says %q, want %q", held, gate.HoldDependencyNotLive)
	}

	// Nothing was written for it: a hold over a record that already exists is
	// recomputed at every firing, and a record for it would be a decision where
	// nothing is decided.
	rows := readLog(t, ctx, d)
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeWait {
			t.Errorf("the log holds a wait row %s, and the dependency hold writes nothing", row.ID)
		}
	}
}
