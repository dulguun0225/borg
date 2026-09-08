// Moving a report from one intent to another, which is a group the grouper got
// wrong being split. Split from db_test.go by subject at the length a file is
// held to, sharing its fixtures and package.
package reportstore_test

import (
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/reportstore"
)

// TestALinkMovesFromOneIntentToAnother: a link is written once by
// [reportstore.Store.Link] and moved by [reportstore.Store.Relink], which is
// the one write here that changes a link rather than making one. A second Link
// is still refused, so nothing but a move moves one.
//
// Decomposition is the boundary the move stops at and this store does not read
// it: the item record is in the factory's graph and this store is a second
// database, so the caller checks before it calls. What this test holds is the
// write itself and its three refusals.
func TestALinkMovesFromOneIntentToAnother(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	collected := time.Now()
	var ids []string
	for n := 0; n < 2; n++ {
		written, err := store.Submit(ctx, submission("tok"), collected.Add(time.Duration(n)*time.Minute))
		if err != nil || !written.Accepted {
			t.Fatalf("Submit %d = %+v, %v", n, written, err)
		}
		ids = append(ids, written.Report.ID)
	}

	// A report in no group is Link's and never Relink's: a move is from one
	// intent to another.
	if err := store.Relink(ctx, ids[0], "int_b"); !errors.Is(err, reportstore.ErrNotGrouped) {
		t.Errorf("Relink over an ungrouped report = %v, want ErrNotGrouped", err)
	}
	for _, id := range ids {
		if err := store.Link(ctx, id, "int_a"); err != nil {
			t.Fatalf("Link %s: %v", id, err)
		}
	}
	if err := store.Link(ctx, ids[0], "int_b"); !errors.Is(err, reportstore.ErrAlreadyGrouped) {
		t.Errorf("a second Link = %v, want ErrAlreadyGrouped: a link is written once and moved by Relink", err)
	}

	// The move: one report of the group goes to another intent, and the other
	// stays where it was.
	if err := store.Relink(ctx, ids[1], "int_b"); err != nil {
		t.Fatalf("Relink: %v", err)
	}
	for n, want := range []string{"int_a", "int_b"} {
		read, err := store.Get(ctx, principal.OfComponent("grouper"), ids[n])
		if err != nil {
			t.Fatalf("Get %s: %v", ids[n], err)
		}
		if read.IntentID != want {
			t.Errorf("report %d is in %q, want %q", n, read.IntentID, want)
		}
		if read.Text == "" {
			t.Errorf("report %d lost its words in the move", n)
		}
	}
	if grouped, err := store.ByIntent(ctx, "int_a"); err != nil || len(grouped) != 1 || grouped[0] != ids[0] {
		t.Errorf("the first intent holds %v, %v; want the one report that did not move", grouped, err)
	}

	// Moving one already there changes nothing, so a pass applied twice writes
	// once, and a report the store does not hold is not found.
	if err := store.Relink(ctx, ids[1], "int_b"); err != nil {
		t.Errorf("Relink into the intent it already names = %v, want it to change nothing", err)
	}
	if err := store.Relink(ctx, "rep_nothing", "int_b"); !errors.Is(err, reportstore.ErrNotFound) {
		t.Errorf("Relink over a report the store does not hold = %v, want ErrNotFound", err)
	}
	if err := store.Relink(ctx, ids[1], ""); !errors.Is(err, reportstore.ErrIntentIDEmpty) {
		t.Errorf("Relink naming no intent = %v, want ErrIntentIDEmpty", err)
	}
}
