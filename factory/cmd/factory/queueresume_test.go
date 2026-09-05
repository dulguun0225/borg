// A run that stopped after one gate approved leaves its item at the queued
// stage for the next queue to finish.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// TestARunThatStoppedLeavesAnItemTheNextQueueFinishes is the queue's membership
// being the service's and not the run's. A run that stopped after one Merge to master gate
// approved leaves that item at the queued stage, and nothing in the crude interface
// clears one — so the next run has to finish it rather than failing on it, which is
// what a run of a service whose queue holds somebody else's item would otherwise do
// after it had already spent the model calls.
func TestARunThatStoppedLeavesAnItemTheNextQueueFinishes(t *testing.T) {
	// The input runs out after the first candidate's Merge to master gate: the interview, two
	// candidate deploy rows, and one merge approval, and then nothing for the second
	// merge row to read.
	ctx, d, out := newPath(t, theAnswer+"\napprove\napprove\napprove\n")

	stopped, err := run(ctx, d, of(theStatement, theSecondStatement))
	if err == nil {
		t.Fatalf("the run finished, and its input ended before the second merge row:\n%s", out)
	}
	if len(stopped.candidates) != 2 {
		t.Fatalf("the run authored %d candidates before it stopped, want two", len(stopped.candidates))
	}
	left := stopped.candidates[0]
	it, err := item.Get(ctx, d.pool, left.itemID)
	if err != nil {
		t.Fatalf("reading the item the run left: %v", err)
	}
	if it.Stage != item.StageQueued {
		t.Fatalf("the first item is at %s, and the run stopped after its Merge to master gate approved", it.Stage)
	}

	// A later run on the same service: its queue holds the item left behind and its
	// own, and it finishes both. It is asked for verdicts because the stopped run
	// minted no release, so the service still has nothing to return to and its number
	// still reads over the threshold.
	d.in = strings.NewReader(approvals)
	next, err := run(ctx, d, of(theFourthStatement))
	if err != nil {
		t.Fatalf("the next run stopped on an item the earlier one left queued: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "was left in the queue by an earlier run") {
		t.Errorf("the run does not report adopting the item left behind:\n%s", out)
	}

	byItem := map[string]*candidate{}
	for _, c := range next.candidates {
		byItem[c.itemID] = c
	}
	adopted := byItem[left.itemID]
	if adopted == nil {
		t.Fatalf("the run reports %d candidates and none of them is the adopted item %s", len(next.candidates), left.itemID)
	}
	if !adopted.merged || adopted.releaseID == "" {
		t.Errorf("the adopted item merged=%v release=%q", adopted.merged, adopted.releaseID)
	}
	if !adopted.tornDown {
		t.Error("the adopted item merged and its environment was not torn down")
	}
	if adopted.deployID == "" {
		t.Error("the adopted item was minted a release and deployed nothing")
	}
	for _, c := range next.candidates {
		if c.itemID != left.itemID && !c.merged {
			t.Errorf("the run's own item %s did not merge alongside the adopted one", c.itemID)
		}
	}

	// One release per item, and no item has two: the queue mints once per merge.
	var releases, items int
	if err := d.pool.QueryRow(ctx, `select count(*), count(distinct item_id) from `+release.Table).Scan(&releases, &items); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != items {
		t.Errorf("%d releases across %d items, and one item is one release", releases, items)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}
