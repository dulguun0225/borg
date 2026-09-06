package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
)

// The three small values the composition supplies a component that decides
// events: what an item's intent is in, whether the health monitor raised it,
// and how a gate reaches a human. Each is here rather than in the package that
// takes it because each follows a record from one to the next, which a
// component that decides events does not do.

// intentState is [gate.IntentState]: the state of the intent an item was
// decomposed from, read before every firing on that item. It is a function the
// composition supplies because what it takes is the item's intent, and a gate
// decides events rather than following records from one to the next.
func (p *path) intentState(ctx context.Context, itemID string) (intent.State, error) {
	it, err := item.Get(ctx, p.d.pool, itemID)
	if err != nil {
		return "", err
	}
	if it.IntentID == "" {
		return intent.StateRefined, nil
	}
	in, err := intent.Get(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return "", err
	}
	return in.State, nil
}

// raisedByTheHealthMonitor is [gate.RaisedByTheHealthMonitor]: whether the
// intent the item was decomposed from is one the health monitor raised, which
// is what a halt's two exceptions come to. It is composed here for the reason
// [path.intentState] is — what it takes is the item's intent, and a gate
// decides events rather than following records from one to the next — and the
// reading is the intent's source and the component that called intake, the same
// one the merge queue's own stop makes.
func (p *path) raisedByTheHealthMonitor(ctx context.Context, itemID string) (bool, error) {
	it, err := item.Get(ctx, p.d.pool, itemID)
	if err != nil {
		return false, err
	}
	if it.IntentID == "" {
		return false, nil
	}
	in, err := intent.Get(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return false, err
	}
	return in.Source == intent.SourceDetector &&
		in.Actor.Kind == record.KindComponent && in.Actor.Key == healthmonitor.Actor.Key, nil
}

// gateNotifier is [gate.Notifier]: the two calls a gate makes on the component
// that reaches humans. It is a type of its own rather than the notifier itself
// because the gate's two calls take a row and an item, and the notifier's own
// entrance takes a [notifier.Wait] — so the wait is composed here, where what it
// waits on is known.
type gateNotifier struct {
	notifier *notifier.Notifier
	// path is what reads the intent behind an item, which is what decides
	// whether something live is worse for the factory having given up. The
	// gate hands this call an item and a stage and cannot read one itself.
	path *path
}

// Acknowledged is the page's acknowledged event, written where the row that was
// acknowledged also pages: one act at Work writes both.
func (g gateNotifier) Acknowledged(ctx context.Context, openID string, human record.Actor) error {
	if g.notifier == nil {
		return nil
	}
	_, err := g.notifier.Acknowledge(ctx, notifier.Wait{
		Row: openID, Kind: notifier.KindDriftMismatch,
		Waiting: "a human at Work acknowledged the row this page was about",
		Holding: people.OfDuty(takeOverIssues),
	}, human.Key)
	return err
}

// Escalated is the wait an item stopped at the attempt limit leaves, which is
// what puts it in Work as an escalation. It routes to the duty that takes over
// issues the factory cannot fix on its own, and it is worse where something
// live is worse — read from the intent the item was decomposed from, which is
// what says whether the work the factory gave up on was a feature nobody is
// running or a defect in software that is.
func (g gateNotifier) Escalated(ctx context.Context, itemID string, stage item.Stage, reason string) error {
	if g.notifier == nil {
		return nil
	}
	source := intent.SourceOwner
	if g.path != nil {
		it, err := item.Get(ctx, g.path.d.pool, itemID)
		if err != nil {
			return err
		}
		if it.IntentID != "" {
			in, err := intent.Get(ctx, g.path.d.pool, it.IntentID)
			if err != nil {
				return err
			}
			source = in.Source
		}
	}
	_, err := g.notifier.Notify(ctx, notifier.Wait{
		Row:  itemID,
		Kind: notifier.KindItemEscalated,
		Waiting: fmt.Sprintf("the factory gave up on %s at %s: %s (its intent came from %s)",
			itemID, stage, reason, source),
		Holding: people.OfDuty(takeOverIssues),
		Worse:   liveIsWorse(source),
	})
	return err
}
