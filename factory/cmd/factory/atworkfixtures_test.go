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
	"time"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// atWork is the scripted human a test drives Work with, composed as
// [deps.decide]: between two passes of the path it reads the rows the gate
// holds pending and closes each with the next token of its script, through the
// call a screen makes for it — [calls.Decide], [calls.Refer],
// [calls.Acknowledge] and [calls.EditInPlace], the same [screens.Calls] the
// server serves — so an end-to-end test exercises one call per act a human
// makes and not the writers beneath it.
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
// [gate.Given.OpenedInWorkAt] is filled on every verdict this driver gives,
// and with a constant: the field is the screen's report of when the actor
// opened the row, and a scripted human never had one open, so every close it
// writes says [theScriptedOpenInWork] before the close.
type atWork struct{ lines *bufio.Scanner }

// theScriptedOpenInWork is how long the scripted human is taken to have had a
// row open in Work before deciding it. It is a constant and not a clock: what
// reads the field is the interval Factory reports as how long a row was open in
// front of a human, and a scripted verdict has no real one to report.
const theScriptedOpenInWork = 90 * time.Second

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
	// The composition's own [screens.Calls], made fresh per row: it holds a
	// path and the views over it and nothing else, and the server it notifies
	// subscribers through is nil here, so a verdict given this way tells no
	// subscriber and needs none.
	made := &calls{p: p, v: &views{p: p}}
	who := principal.OfHuman(p.human.Key, record.BasisClaimed)
	openEventID := opened.Row.ID

	if _, is := strings.CutPrefix(line, "acknowledge"); is {
		// An acknowledgement decides nothing: the row stays pending and the next
		// token is read against it.
		return made.Acknowledge(ctx, who, screens.AcknowledgeArgs{OpenEventID: openEventID})
	}
	if rest, is := strings.CutPrefix(line, "edit"); is {
		return made.EditInPlace(ctx, who, screens.EditInPlaceArgs{
			OpenEventID: openEventID, VersionText: strings.TrimSpace(rest),
		})
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
			return made.Refer(ctx, who, screens.ReferArgs{OpenEventID: openEventID, Reason: reason})
		}
		return made.Decide(ctx, who, screens.DecideArgs{
			OpenEventID: openEventID, Verdict: string(action), Reason: reason,
			OpenedInWorkAt: record.FormatTime(time.Now().Add(-theScriptedOpenInWork)),
		})
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
