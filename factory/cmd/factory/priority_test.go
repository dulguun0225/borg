// The priority an owner reorders a queue with, written at Work through
// dispatch rather than beside it.
package main

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// TestWorkReordersAQueueByPriority is duty 9's write on an item: an owner
// reorders a queue at Work, through dispatch rather than beside it, and the
// board reads the number back.
func TestWorkReordersAQueueByPriority(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("ByName(%s) = found %v, %v", theService, found, err)
	}
	in, err := s.p.intake.TakeIn(ctx, s.p.human, intent.Arrival{
		Source: intent.SourceOwner, Statement: theService + ": " + theStatement, ProjectID: s.p.projectID,
	})
	if err != nil {
		t.Fatalf("taking the intent in: %v", err)
	}
	it, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor,
		item.New{IntentID: in.ID, ServiceID: svc.ID, Branch: "item/a", RequirementsAnswered: oneRequirement}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}

	s.mustCall(t, "setPriority", screens.SetPriorityArgs{ItemID: it.ID, Priority: 7})
	read, err := item.Get(ctx, d.pool, it.ID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if read.Priority != 7 {
		t.Errorf("the item's priority is %d, the owner set 7", read.Priority)
	}

	var work screens.Work
	s.get(t, "/api/work", &work)
	found = false
	for _, row := range work.Rows {
		if row.ItemID == it.ID {
			found = true
			if row.Priority != 7 {
				t.Errorf("the board reads priority %d on the item the owner set to 7", row.Priority)
			}
		}
	}
	if !found {
		t.Errorf("the board holds no row for item %s: %+v", it.ID, work.Rows)
	}

	// An item id naming nothing is refused. The call is made directly rather
	// than over HTTP so the error itself is read: package item's own
	// ErrNotFound is what says the id names no item, and the server answers
	// every error but screens.ErrNotFound alike.
	who := principal.OfHuman(s.p.human.Key, record.BasisClaimed)
	if err := s.made.SetPriority(ctx, who, screens.SetPriorityArgs{Priority: 7}); err == nil {
		t.Error("a priority naming no item was accepted")
	}
	if err := s.made.SetPriority(ctx, who, screens.SetPriorityArgs{
		ItemID: "it_missing", Priority: 1,
	}); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("a priority on a missing item = %v, want ErrNotFound", err)
	}
}
