package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/gate"
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

// pagedFiring is the page a gate firing sends. Two firings meet the page's
// condition and [gate.Opened.Pages] is what answers for both: a mismatch the
// drift detector found, which waits on a human and on nothing else where the
// holds beside it lift themselves; and a human deciding on a revert while the
// rollback that removed the defect still holds.
//
// It is keyed on the open event, which is the row a human acknowledges at Work,
// so the acknowledgement [gateNotifier.Acknowledged] writes lands on the row
// its page was reached on. A page keyed anywhere else could never be
// acknowledged by the act that decides the row.
//
// The mismatch's is beside the notifier's own and not instead of it: the sweep
// of the drift detector's store pages about the record there, which a human ends
// by clearing it, and this pages about the deploy that record is holding — which
// a human ends at this row. Where both conditions hold at one firing the
// mismatch's is the one sent, a page being the sequence of events on one row and
// two on one row being one page: the mismatch is what holds the deploy, and the
// revert row is reached again at the next firing once it is cleared.
//
// Neither is deferred by a rollback the health monitor is still calling for. A
// mismatch is not about a release, and the revert's row is reached only after the
// rollback ran — so [notifier.Wait.RollbackOutstanding] is false on both, and the
// revert's page waits for the hours the service authored.
func (p *path) pagedFiring(ctx context.Context, opened gate.Opened) error {
	if p.notifier == nil || !opened.Pages() {
		return nil
	}
	wait := notifier.Wait{
		Row:       opened.Row.ID,
		Worse:     true,
		ServiceID: opened.Subject.ServiceID,
	}
	switch {
	case opened.Mismatch != "":
		wait.Kind = notifier.KindDriftMismatch
		wait.Waiting = fmt.Sprintf("%s waits on a human: %s", opened.Gate, opened.Mismatch)
		wait.Holding = people.OfObligation(people.ObligationDriftDetector)
	default:
		wait.Kind = notifier.KindGateRevertDecision
		wait.Waiting = fmt.Sprintf(
			"%s waits on a human to decide the revert, and until they do master still holds the defect the rollback removed",
			opened.Gate)
		wait.Holding = whoTheRowWaitsOn(opened.WaitsOn)
	}
	_, err := p.notifier.Notify(ctx, wait)
	return err
}

// whoTheRowWaitsOn is whose wait a paging gate row is: the duty the row belongs to,
// which is the routing that showed the row in Work, and the zero value where the
// row belongs to none — which the notifier routes to the owner, the same answer
// a duty nobody holds gets. The production deploy row names no duty of its own,
// so a revert decided there reaches the owner until a safeguard's routing or a
// hold puts a duty on it.
func whoTheRowWaitsOn(w gate.Waits) people.Holding {
	if w.Duty != 0 {
		return people.OfDuty(w.Duty)
	}
	return people.Holding{}
}

// gateNotifier is [gate.Notifier]: the one call a gate makes on the component
// that reaches humans. It is a type of its own rather than the notifier itself
// because the gate's call takes a row, and the notifier's own entrance takes a
// [notifier.Wait] — so the wait is composed here, where what it waits on is
// known.
type gateNotifier struct {
	notifier *notifier.Notifier
}

// Acknowledged is the page's acknowledged event, written where the row that was
// acknowledged also pages: one act at Work writes both. A gate row that pages
// nobody still takes the acknowledgement, and there the notifier writes
// nothing — which is why the kind named here is the ordinary gate decision, the
// one kind of wait every firing leaves. Where the row did page, the event is
// written under the kind the page it acknowledges was reached under.
func (g gateNotifier) Acknowledged(ctx context.Context, openID string, human record.Actor) error {
	if g.notifier == nil {
		return nil
	}
	_, err := g.notifier.Acknowledge(ctx, notifier.Wait{
		Row: openID, Kind: notifier.KindGateDecision,
		Waiting: "a human at Work acknowledged the row this page was about",
		Holding: people.OfDuty(takeOverIssues),
	}, human.Key)
	return err
}

// dispatchNotifier is [dispatch.Notifier]: the one call dispatch makes on the
// component that reaches humans, which components.md gives to dispatch and not
// to the gate. It is a type of its own for the reason [gateNotifier] is, and it
// holds the path because deciding whether something live is worse is a read of
// the intent behind the item, which dispatch hands over neither.
type dispatchNotifier struct {
	notifier *notifier.Notifier
	// path is what reads the intent behind an item, which is what decides
	// whether something live is worse for the factory having given up.
	// Dispatch hands this call an item and a stage and cannot read one itself.
	path *path
}

// Escalated is the wait an item stopped at the attempt limit leaves, which is
// what puts it in Work as an escalation. It routes to the duty that takes over
// issues the factory cannot fix on its own, and it is worse where something
// live is worse — read from the intent the item was decomposed from, which is
// what says whether the work the factory gave up on was a feature nobody is
// running or a defect in software that is.
func (g dispatchNotifier) Escalated(ctx context.Context, itemID string, stage item.Stage, reason string) error {
	if g.notifier == nil || g.path == nil {
		return nil
	}
	source := intent.SourceOwner
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
	_, err = g.notifier.Notify(ctx, notifier.Wait{
		Row:  itemID,
		Kind: notifier.KindItemEscalated,
		Waiting: fmt.Sprintf("the factory gave up on %s at %s: %s (its intent came from %s)",
			itemID, stage, reason, source),
		Holding: people.OfDuty(takeOverIssues),
		Worse:   liveIsWorse(source),
	})
	return err
}

// intakeNotifier is [intent.Notifier]: the two calls intake makes on the
// component that reaches humans — a round of interview questions, and an intent
// escalated. It is a type of its own for the reason [gateNotifier] is: intake
// hands over an intent and a question, and the wait is composed here.
type intakeNotifier struct {
	notifier *notifier.Notifier
	// path is what reads the intent an escalation is about, which is what
	// decides whether something live is worse for the factory having given up
	// on refining it.
	path *path
}

// Interviewed is the wait a question of the interview leaves. It routes to the
// duty that answers the interview, and it pages nobody: an owner's silence
// there consumes no compute, and nothing deployed is worse for it.
func (n intakeNotifier) Interviewed(ctx context.Context, intentID, questionID, question string) error {
	if n.notifier == nil {
		return nil
	}
	_, err := n.notifier.Notify(ctx, notifier.Wait{
		Row:     questionID,
		Kind:    notifier.KindInterview,
		Waiting: fmt.Sprintf("the factory asks about intent %s: %s", intentID, question),
		Holding: people.OfDuty(answerTheInterview),
	})
	return err
}

// Escalated is the wait an intent that exceeded the attempt limit leaves, which
// is what puts it in Work as an escalation. It is worse where something live is
// worse, read the same way an item's escalation reads it: off the source of the
// intent the factory gave up on.
func (n intakeNotifier) Escalated(ctx context.Context, intentID string) error {
	if n.notifier == nil || n.path == nil {
		return nil
	}
	in, err := intent.Get(ctx, n.path.d.pool, intentID)
	if err != nil {
		return err
	}
	_, err = n.notifier.Notify(ctx, notifier.Wait{
		Row:  intentID,
		Kind: notifier.KindIntentEscalated,
		Waiting: fmt.Sprintf("the factory gave up on refining intent %s: %d round(s) and %d re-decomposition(s) (it came from %s)",
			intentID, in.Rounds, in.ReDecompositions, in.Source),
		Holding: people.OfDuty(takeOverIssues),
		Worse:   liveIsWorse(in.Source),
	})
	return err
}
