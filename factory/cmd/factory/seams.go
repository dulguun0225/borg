package main

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// The small values the composition supplies a component that decides events:
// where a safeguard's rows route, what an item's intent is in, whether the
// health monitor raised it, and how a gate reaches a human. Each is here rather
// than in the package that takes it because each follows a record from one to
// the next, which a component that decides events does not do.

// safeguardRouting is where the safeguards that applied at one firing say their
// rows route, which is the read [gate.SafeguardRouting] names. It is the
// composition's because a safeguard is package policy's record at Factory and
// the gate writes no record of its own.
//
// The first safeguard naming either a duty or a human is taken: a row waits on
// one holder or one duty, and an owner who placed two safeguards over one gate
// gets the routing of the first the read returns rather than a row waiting on
// two people at once.
func (p *path) safeguardRouting(ctx context.Context, safeguardIDs []string) (gate.RoutedTo, error) {
	placed, err := safeguard.All(ctx, p.d.pool)
	if err != nil {
		return gate.RoutedTo{}, err
	}
	for _, one := range placed {
		if !slices.Contains(safeguardIDs, one.ID) {
			continue
		}
		if one.Routing.Duty != 0 || one.Routing.HumanKey != "" {
			return gate.RoutedTo{Duty: people.Duty(one.Routing.Duty), Human: one.Routing.HumanKey}, nil
		}
	}
	return gate.RoutedTo{}, nil
}

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

// pagedFiring is the page a gate firing sends. One condition meets it, which
// [gate.Opened.Pages] answers: a human deciding on a revert while the rollback
// that removed the defect still holds.
//
// A mismatch the drift detector found is not a second condition, whatever
// [gate.Opened.Mismatch] says: it holds this row, and the row it holds pages
// nobody, a hold paging nobody being the rule for every hold here. The
// detector's own page is the notifier's sweep of that store, reaching whoever
// installed it and not keyed to this row at all — so a firing that finds a
// mismatch and a revert decision standing at once still sends only the revert
// decision's page, [gate.Opened.Pages] having already read the mismatch as no
// page of this row's.
//
// It is keyed on the open event, which is the row a human acknowledges at Work,
// so the acknowledgement [gateNotifier.Acknowledged] writes lands on the row
// its page was reached on.
//
// It is not deferred by a rollback the health monitor is still calling for: the
// revert's row is reached only after the rollback ran, so
// [notifier.Wait.RollbackOutstanding] is false and the page waits for the hours
// the service authored.
func (p *path) pagedFiring(ctx context.Context, opened gate.Opened) error {
	if p.notifier == nil || !opened.Pages() {
		return nil
	}
	holding, person := whoTheRowWaitsOn(opened.WaitsOn)
	wait := notifier.Wait{
		Row:       opened.Row.ID,
		Worse:     true,
		ServiceID: opened.Subject.ServiceID,
		Kind:      notifier.KindGateRevertDecision,
		Waiting: fmt.Sprintf(
			"%s waits on a human to decide the revert, and until they do master still holds the defect the rollback removed",
			opened.Gate),
		Holding: holding,
		Person:  person,
	}
	_, err := p.notifier.Notify(ctx, wait)
	return err
}

// whoTheRowWaitsOn is whose wait a paging gate row is: the duty the row
// belongs to, which is the routing that showed the row in Work, the named
// human a safeguard's routing gave it where it belongs to no duty, and both
// zero where it belongs to neither — which the notifier routes to the owner,
// the same answer a duty nobody holds gets. The production deploy row names
// no duty of its own, so a revert decided there reaches the owner until a
// safeguard's routing or a hold puts a duty or a named human on it.
func whoTheRowWaitsOn(w gate.Waits) (people.Holding, string) {
	if w.Duty != 0 {
		return people.OfDuty(w.Duty), ""
	}
	return people.Holding{}, w.Human
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
// component that reaches humans, which ../../../end-goal/components.md gives to dispatch and not
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

// NearingASpendCeiling is the notice at a fixed fraction of an authored spend
// ceiling: a delivery and never a page, routed to the owner the way the hold at
// the ceiling is, so the hold is not the first anyone hears of it.
//
// It goes out once per credential and period. Dispatch compares the sum at
// every report and holds no record of what has been delivered, so the key is
// composed here — the credential and the period's start are the row the
// notifier's delivery record is written against, and a row that already has one
// is not delivered again. A delivery the transport refused leaves a record too,
// so a refusal is not retried at every dispatch for the rest of the period.
func (g dispatchNotifier) NearingASpendCeiling(ctx context.Context, credentialName, periodStart string,
	spent, ceiling float64, currency string) error {
	if g.notifier == nil || g.path == nil {
		return nil
	}
	row := "ceiling:" + credentialName + ":" + periodStart
	already, err := notifier.DeliveriesOf(ctx, g.path.d.pool, row)
	if err != nil {
		return err
	}
	if len(already) > 0 {
		return nil
	}
	_, err = g.notifier.Notify(ctx, notifier.Wait{
		Row:  row,
		Kind: notifier.KindSpendCeilingFraction,
		Waiting: fmt.Sprintf("%s has spent %.2f %s of the %.2f %s ceiling authored on it, in the period beginning %s",
			credentialName, spent, currency, ceiling, currency, periodStart),
		Person: lenderOf(ctx, g.path.d.pool, credentialName),
	})
	return err
}

// lenderOf is the per-person key of whoever People records as having lent
// credentialName, and empty where that person took it back or where nobody
// has lent one yet — the notice then reaching the owner, the same as a page
// about a credential already widens to. A read this call cannot make is not
// this notice's error to report: the ceiling is still delivered, to the
// owner.
func lenderOf(ctx context.Context, pool *pgxpool.Pool, credentialName string) string {
	credential, found, err := people.CredentialNamed(ctx, pool, credentialName)
	if err != nil || !found || !credential.Lent() {
		return ""
	}
	return credential.Key
}

// intakeNotifier is [intent.Notifier]: the three calls intake makes on the
// component that reaches humans — a round of interview questions, an intent
// escalated, and the acceptance round that follows production. It is a type of
// its own for the reason [gateNotifier] is: intake hands over an intent and a
// question, and the wait is composed here.
type intakeNotifier struct {
	notifier *notifier.Notifier
	// path is what reads the intent an escalation is about, which is what
	// decides whether something live is worse for the factory having given up
	// on refining it.
	path *path
}

// Interviewed is the wait a question of the interview leaves. It reaches the
// requester where the intent has one, and whoever answers the interview
// otherwise, and it pages nobody: an owner's silence there consumes no
// compute, and nothing deployed is worse for it.
func (n intakeNotifier) Interviewed(ctx context.Context, intentID, questionID, question string) error {
	if n.notifier == nil {
		return nil
	}
	_, err := n.notifier.Notify(ctx, notifier.Wait{
		Row:     questionID,
		Kind:    notifier.KindInterview,
		Waiting: fmt.Sprintf("the factory asks about intent %s: %s", intentID, question),
		Holding: people.OfDuty(answerTheInterview),
		Person:  requesterOf(ctx, n.path, intentID),
	})
	return err
}

// AcceptanceRound is the wait the round that follows production leaves. It
// routes where a round of the interview does — the requester where the
// intent has one, and whoever answers the interview otherwise — which is the
// routing the design gives it, and it pages nobody: everything the intent
// asked for is live and nothing about the software is wrong, so what waits
// is a verdict and not a repair.
func (n intakeNotifier) AcceptanceRound(ctx context.Context, intentID, questionID, question string) error {
	if n.notifier == nil {
		return nil
	}
	_, err := n.notifier.Notify(ctx, notifier.Wait{
		Row:     questionID,
		Kind:    notifier.KindInterview,
		Waiting: fmt.Sprintf("the factory asks whether intent %s had the effect it was for: %s", intentID, question),
		Holding: people.OfDuty(answerTheInterview),
		Person:  requesterOf(ctx, n.path, intentID),
	})
	return err
}

// requesterOf is the per-person key of the intent's requester — an owner's
// request, and the actor that made it a human — and empty where the intent
// has none: one the factory raised, one grouped from reports, whose reporter
// is reachable by nothing, or one this read cannot find. A raiser with no
// path to read the intent through, [intakeNotifier.Escalated]'s callers
// before path composed one, answers empty the same way.
func requesterOf(ctx context.Context, p *path, intentID string) string {
	if p == nil {
		return ""
	}
	in, err := intent.Get(ctx, p.d.pool, intentID)
	if err != nil || in.Source != intent.SourceOwner || in.Actor.Kind != record.KindHuman {
		return ""
	}
	return in.Actor.Key
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
