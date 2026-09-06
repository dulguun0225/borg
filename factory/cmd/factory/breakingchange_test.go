// Tests of a breaking change rejected at the merge row naming the
// consumer it would break.
package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// TestABreakingChangeIsRejectedAtTheMergeRowNamingTheConsumer is the milestone's own
// episode: a candidate that passes every criterion in force and removes an element a
// consumer declares, rejected before a verdict is asked for, with the consumer on the
// row.
//
// The break is in no criterion's path, so no rebuild the wrapped model makes
// fixes it: [path.mergeUntilQueued] sends the item back and it comes back with
// the same break every time, so the row keeps rejecting it until the stage's
// own attempt limit is spent and the implementer's dispatch escalates — see
// [TestAStoresForwardPromiseRefusesAnAlwaysPopulatedColumn] for why this uses
// [retriedWithNoFix].
func TestABreakingChangeIsRejectedAtTheMergeRowNamingTheConsumer(t *testing.T) {
	ctx, d, out := newContractPath(t)
	pair(t, ctx, d, out)

	d.in = strings.NewReader(manyApprovals)
	d.model = &retriedWithNoFix{inner: d.model}
	res, err := run(ctx, d, []asked{across(breakStatement, theService)})
	if err == nil {
		t.Fatalf("a candidate that removes an element the consumer declares merged:\n%s", out)
	}
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Errorf("the error is %v, want a stage out of attempts — every rebuild reproduces the same break", err)
	}
	c := only(t, res)
	if c.merged {
		t.Fatalf("a candidate that removes an element the consumer declares merged:\n%s", out)
	}
	if c.checked == nil || c.checked.Check() != gate.AutoRejectedByContractDiff {
		t.Fatalf("the candidate's last completed run was rejected by %q, want the producer's own contract diff",
			checkOf(c))
	}
	if !strings.Contains(c.checked.Why(), "Detail") {
		t.Errorf("the rejection does not name the element: %s", c.checked.Why())
	}
	consumer, found, err := service.ByName(ctx, d.pool, theSecondService)
	if err != nil || !found {
		t.Fatalf("reading the consumer: found %v, %v", found, err)
	}
	if !strings.Contains(c.checked.Why(), consumer.ID) {
		t.Errorf("the rejection does not name the consumer it would break: %s", c.checked.Why())
	}

	// The item is escalated: the stage's own attempt limit is spent rebuilding
	// against a break no rebuild here fixes.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageEscalated {
		t.Errorf("the item that spent its attempts is at %s, want escalated", it.Stage)
	}
	if !strings.Contains(out.String(), "before a verdict was asked for") {
		t.Errorf("the run does not say the check rejected before a verdict:\n%s", out)
	}
}

// checkOf is what a candidate's last completed enforcement read, for a message
// naming what it was rather than nil.
func checkOf(c *candidate) string {
	if c.checked == nil {
		return "(no enforcement read at all)"
	}
	return c.checked.Check()
}
