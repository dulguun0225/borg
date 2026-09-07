// The human at Work, scripted: what a test closes a pending gate row with,
// through the calls a screen makes, and the loop that drives one item's
// authoring stages the way a pass and those calls drive them between them.
package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
)

// atWork is the scripted human a test drives the gate component with, composed
// as [deps.decide]: between two passes of the path it reads the rows the gate
// holds pending and closes each with the next token of its script, through the
// same calls a screen makes — [gate.Gate.Decide], [gate.Gate.Refer],
// [gate.Gate.Acknowledge] and [path.editInPlace], which is what a screen's
// Edit in place reaches.
//
// The script is the value's own and no longer a stream the path holds: the
// terminal read the interview's answer and every verdict off standard input,
// and neither comes from one now — the answer is [deps.answer] and these are
// here.
//
// A token is a line: "approve", "reject <feedback>", "hold <reason>",
// "refer <what you could not judge>", "acknowledge <note>" — which decides
// nothing, so the row is read again — and "edit <text>", which authors the
// version at the gate.
//
// [gate.Given.OpenedInWorkAt] is empty on every close: the field is the
// screen's report of when the actor opened the row, and this driver reaches the
// gate component directly rather than through a screen.
type atWork struct{ lines *bufio.Scanner }

// scriptedAtWork is one script's own driver.
func scriptedAtWork(script string) *atWork {
	return &atWork{lines: bufio.NewScanner(strings.NewReader(script))}
}

// decide closes every pending row a human decides with the next token of the
// script. The count returned is how many rows it acted on, which is what the
// run's loop reads to know whether to make another pass.
func (a *atWork) decide(ctx context.Context, p *path) (int, error) {
	pending, err := p.gate.Pending(ctx)
	if err != nil {
		return 0, err
	}
	acted := 0
	for _, opened := range pending {
		if !opened.HumanDecides {
			// A row open only because a hold stands is the gate's own to
			// re-evaluate, and closing it would decide the event the hold
			// exists to stop.
			continue
		}
		line, err := a.next()
		if err != nil {
			return acted, err
		}
		if err := decideOne(ctx, p, opened, line); err != nil {
			return acted, err
		}
		acted++
	}
	return acted, nil
}

// next is the next token of the script, without its line ending and
// surrounding blank space.
func (a *atWork) next() (string, error) {
	if !a.lines.Scan() {
		if err := a.lines.Err(); err != nil {
			return "", fmt.Errorf("the script: %w", err)
		}
		return "", errors.New("the script ended before every pending row was decided")
	}
	return strings.TrimSpace(a.lines.Text()), nil
}

// decideOne is one token acted on against one pending row.
func decideOne(ctx context.Context, p *path, opened gate.Opened, line string) error {
	if _, is := strings.CutPrefix(line, "acknowledge"); is {
		// An acknowledgement decides nothing: the row stays pending and the next
		// token is read against it.
		_, err := p.gate.Acknowledge(ctx, opened, p.human)
		return err
	}
	if rest, is := strings.CutPrefix(line, "edit"); is {
		firing, err := p.firingFor(ctx, opened)
		if err != nil {
			return err
		}
		_, err = p.editInPlace(ctx, opened, firing, p.human, strings.TrimSpace(rest))
		return err
	}
	actions, err := gate.Actions(opened.Gate)
	if err != nil {
		return err
	}
	for _, action := range actions {
		rest, matched := strings.CutPrefix(line, string(action))
		if !matched {
			continue
		}
		reason := strings.TrimSpace(rest)
		if action == gate.VerdictRefer {
			firing, err := p.firingFor(ctx, opened)
			if err != nil {
				return err
			}
			_, err = p.gate.Refer(ctx, opened, p.human, reason, firing)
			return err
		}
		given := gate.Given{Actor: p.human, Verdict: action, Reason: reason}
		if action == gate.VerdictApprove {
			given.Holds = opened.Holds
		}
		_, err := p.gate.Decide(ctx, opened, given)
		return err
	}
	return fmt.Errorf("the verdict at %s is one of %v, not %q", opened.Gate, actions, line)
}

// authorStages drives one item's four authoring stages the way a pass and the
// screens drive them between them: the pass performs what it can and stops at
// the row a human decides, the scripted human closes it, and the candidate is
// read back out of the records before the next step. It is what
// [path.authorFrom] plus [atWork.decide] plus [path.rehydrate] amount to, for a
// test that drives the steps below it directly rather than calling run.
func authorStages(t *testing.T, ctx context.Context, p *path, c *candidate, out *bytes.Buffer) {
	t.Helper()
	for {
		if _, err := p.authorFrom(ctx, c); err != nil {
			t.Fatalf("authoring item %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
		if c.waiting == (gate.Row{}) {
			return
		}
		acted, err := p.d.decide(ctx, p)
		if err != nil {
			t.Fatalf("deciding the row item %s waits at: %v\noutput so far:\n%s", c.itemID, err, out)
		}
		if acted == 0 {
			t.Fatalf("item %s waits at %s and nothing closed the row", c.itemID, c.waiting)
		}
		fresh, err := p.rehydrate(ctx, c.itemID)
		if err != nil {
			t.Fatalf("reading item %s back out of the records: %v", c.itemID, err)
		}
		*c = *fresh
	}
}
