package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// Work's own writes: what an owner requests, the interview answered, the
// rounds confirmed, an item's priority and its ending, and every verdict at a
// gate. Every one reaches the writer of the record it changes; the verdict
// carries one field more, which is when the actor opened the row here.

// SupplyIntent is intake's own writer, called from Work: duty 1. The services
// an intent changing more than one names are written into the statement as the
// prefix `svcA,svcB: ` run's own -intent flag already takes, because the
// intent record holds no service — which service an item changes is the
// item's, written when decomposition creates it, and the pass that decomposes
// reads the services back off the statement with the same parser.
func (c *calls) SupplyIntent(ctx context.Context, who principal.Principal, args screens.SupplyIntentArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Statement) == "" {
		return "", errors.New("factory: an intent's statement is not empty")
	}
	projectID, err := c.projectNamed(ctx, args.ProjectName)
	if err != nil {
		return "", err
	}
	// The prefix is always written, the default being the first service this
	// install knows: it is what says which services this intent's
	// decomposition yields items on, and the pass that decomposes has no other
	// place to read that from.
	services := args.Services
	if len(services) == 0 {
		if len(c.p.d.services) == 0 {
			return "", errors.New("factory: an intent names the services it changes, and this install knows none")
		}
		services = []string{c.p.d.services[0].name}
	}
	for _, name := range services {
		if _, err := c.p.d.repoOf(name); err != nil {
			return "", err
		}
	}
	statement := strings.Join(services, ",") + ": " + strings.TrimSpace(args.Statement)
	in, err := c.p.intake.TakeIn(ctx, actor, intent.Arrival{
		Source: intent.SourceOwner, Statement: statement, ProjectID: projectID,
	})
	if err != nil {
		return "", err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	return in.ID, nil
}

// projectNamed is the project a call names, the one this process installs where
// it names none.
func (c *calls) projectNamed(ctx context.Context, name string) (string, error) {
	if name == "" || name == c.p.d.project {
		return c.p.projectID, nil
	}
	prj, err := namedProject(ctx, c.p.d.pool, name)
	if err != nil {
		return "", err
	}
	return prj.ID, nil
}

// AnswerQuestion writes the answer to one round of the interview: duty 3. The
// limit the answer is given is the interview's own, which is what intake tells
// the two escalations apart by — a human's answer clears an escalation the
// rounds caused and never one decomposition caused.
func (c *calls) AnswerQuestion(ctx context.Context, who principal.Principal, args screens.AnswerQuestionArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if strings.TrimSpace(args.Answer) == "" {
		return errors.New("factory: an answer is what the interview's round is spent on, and this one is empty")
	}
	limit, err := intentAttemptLimit(ctx, c.p.d.pool, factorysettings.SubjectInterview)
	if err != nil {
		return err
	}
	answered, err := c.p.intake.Answer(ctx, actor, args.QuestionID, args.Answer, limit)
	if err != nil {
		return err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	_ = answered
	return nil
}

// ConfirmReading closes the round the intent is waiting on, and which of the
// two it is follows from the intent's state rather than from a second call: an
// intent still unrefined is waiting on the confirming round, where the
// requester confirms what the factory understood is wanted, and one already
// refined is waiting on the acceptance round, where the requester says whether
// the intended effect was had.
//
// A correction reopens the interview in both cases, which is what the design
// gives the requester who says the factory got it wrong.
func (c *calls) ConfirmReading(ctx context.Context, who principal.Principal, args screens.ConfirmReadingArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	in, err := intent.Get(ctx, c.p.d.pool, args.IntentID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.IntentID)
	}
	waiting, err := outstandingRound(ctx, c.p.d.pool, in.ID)
	if err != nil {
		return err
	}
	defer func() {
		c.changed("work", listAddressID)
		c.changed("home", listAddressID)
	}()

	if in.State == intent.StateUnrefined {
		if !args.Confirmed {
			_, err := c.p.intake.Correct(ctx, actor, intent.Correction{
				IntentID: in.ID, QuestionID: waiting.ID, Correction: args.Correction,
			})
			return err
		}
		return c.p.confirmTheReading(ctx, actor, in, waiting)
	}
	if !args.Confirmed {
		return c.p.intake.CorrectAcceptance(ctx, actor, in.ID, waiting.ID, args.Correction)
	}
	return c.p.intake.Delivered(ctx, actor, intent.Delivery{
		IntentID: in.ID, QuestionID: waiting.ID,
		Answer: "the effect was had", Outcome: "the effect was had",
	})
}

// AcceptDelivery is a human at Work accepting a commit master holds that the
// queue did not make: the queue mints nothing for the service while that stop
// stands, and the release this mints is one no gate decided.
func (c *calls) AcceptDelivery(ctx context.Context, who principal.Principal, args screens.AcceptDeliveryArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	svc, err := service.Get(ctx, c.p.d.pool, args.ServiceID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ServiceID)
	}
	accepted, err := c.p.queue.AcceptCommit(ctx, actor, svc.ID, args.Commit)
	if err != nil {
		return err
	}
	if accepted.Why != "" {
		return fmt.Errorf("factory: commit %s was not accepted: %s (rejection row %s)",
			args.Commit, accepted.Why, accepted.RejectionRow)
	}
	c.changed("service", svc.ID)
	return nil
}

// EndIntent ends an intent for good: no item of it is dispatched and nothing
// below it moves.
func (c *calls) EndIntent(ctx context.Context, who principal.Principal, args screens.EndIntentArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if err := c.p.intake.Drop(ctx, actor, args.IntentID); err != nil {
		return err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	return nil
}

// SetPriority writes the one field that orders every queue an item waits in as
// an item. It goes through dispatch and not beside it, the item having one
// writer after decomposition.
func (c *calls) SetPriority(ctx context.Context, who principal.Principal, args screens.SetPriorityArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := c.p.items.SetPriority(ctx, actor, args.ItemID, int(args.Priority)); err != nil {
		return err
	}
	c.changed("item", args.ItemID)
	return nil
}

// EndItem ends an item for good and tears its candidate environment down with
// it: the design has the environment stay the item's until it merges, is
// dropped, or is superseded.
func (c *calls) EndItem(ctx context.Context, who principal.Principal, args screens.EndItemArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if err := c.p.dropItem(ctx, actor, args.ItemID); err != nil {
		return err
	}
	c.changed("item", args.ItemID)
	return nil
}

// TakeOver is duty 12: an item the factory escalated, taken over and returned
// to a named stage. This is the only caller of [item.Dispatch.ClearEscalation],
// the design putting that act at Work and nowhere else.
func (c *calls) TakeOver(ctx context.Context, who principal.Principal, args screens.TakeOverArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := c.p.items.ClearEscalation(ctx, actor, args.ItemID, item.Stage(args.Stage)); err != nil {
		return err
	}
	c.changed("item", args.ItemID)
	c.changed("home", listAddressID)
	return nil
}

// Decide writes a verdict against the open event the row was rendered from:
// the same call on the gate component the factory's own auto-pass makes, and
// one field more — when the actor opened the row in Work, which no other
// caller can answer for.
func (c *calls) Decide(ctx context.Context, who principal.Principal, args screens.DecideArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	opened, err := c.openRow(ctx, who, args.OpenEventID)
	if err != nil {
		return err
	}
	given := gate.Given{
		Actor: actor, Verdict: gate.Verdict(args.Verdict),
		Reason: args.Reason, OpenedInWorkAt: args.OpenedInWorkAt,
	}
	if given.Verdict == gate.VerdictApprove {
		given.Holds = opened.Holds
	}
	if _, err := c.p.gate.Decide(ctx, opened, given); err != nil {
		return err
	}
	c.afterADecision(opened)
	return nil
}

// ApproveThroughHold is the emergency action the design keeps at the production
// deploy row — approve now, not skip. It is the one verdict a screen gives that
// names no open event: the holds the factory computes at that row stop the row
// being fired, so [calls.Decide] has nothing to name and this call fires the
// row with the holds on it and approves it in the same act, with the human at
// the screen as the actor.
//
// What approving through the hold a rollback leaves redelivers is the defect
// that was just removed. The reason is required for that: it is what the close
// event carries about a verdict nobody can read off the number.
func (c *calls) ApproveThroughHold(ctx context.Context, who principal.Principal, args screens.ApproveThroughHoldArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if strings.TrimSpace(args.Reason) == "" {
		return errors.New("factory: approving through a hold says why, and this one says nothing")
	}
	if _, err := item.Get(ctx, c.p.d.pool, args.ItemID); err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ItemID)
	}
	if err := c.p.approveThrough(ctx, actor, args.ItemID, gate.VerdictApprove,
		strings.TrimSpace(args.Reason), args.OpenedInWorkAt); err != nil {
		return err
	}
	c.changed("item", args.ItemID)
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	c.changed("ops", listAddressID)
	return nil
}

// Refer closes a row with the fourth verdict, which re-fires it to a holder who
// has not referred it.
func (c *calls) Refer(ctx context.Context, who principal.Principal, args screens.ReferArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	opened, err := c.openRow(ctx, who, args.OpenEventID)
	if err != nil {
		return err
	}
	again, err := c.p.firingFor(ctx, opened)
	if err != nil {
		return err
	}
	if _, err := c.p.gate.Refer(ctx, opened, actor, args.Reason, again); err != nil {
		return err
	}
	c.afterADecision(opened)
	return nil
}

// EditInPlace is the action a human takes at a document gate instead of
// rejecting: they author the version themselves, the artifact store writes it
// with the gate component as the authorship, and the row fires again over the
// new version with the one it supersedes abandoned.
func (c *calls) EditInPlace(ctx context.Context, who principal.Principal, args screens.EditInPlaceArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	opened, err := c.openRow(ctx, who, args.OpenEventID)
	if err != nil {
		return err
	}
	again, err := c.p.firingFor(ctx, opened)
	if err != nil {
		return err
	}
	if _, err := c.p.editInPlace(ctx, opened, again, actor, args.VersionText); err != nil {
		return err
	}
	c.afterADecision(opened)
	return nil
}

// Acknowledge is a holder saying they have a row. It decides nothing and the
// row stays in front of every other holder of the duty.
func (c *calls) Acknowledge(ctx context.Context, who principal.Principal, args screens.AcknowledgeArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	opened, err := c.openRow(ctx, who, args.OpenEventID)
	if err != nil {
		return err
	}
	if _, err := c.p.gate.Acknowledge(ctx, opened, actor); err != nil {
		return err
	}
	c.afterADecision(opened)
	return nil
}

// SupplyIntentConstraint is duty 2 at Work: a constraint whose reach is one
// intent, supplied on that intent and withdrawn there too.
func (c *calls) SupplyIntentConstraint(ctx context.Context, who principal.Principal, args screens.SupplyIntentConstraintArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	if _, err := intent.Get(ctx, c.p.d.pool, args.IntentID); err != nil {
		return "", fmt.Errorf("%w: %s", screens.ErrNotFound, args.IntentID)
	}
	arriving, err := arrivingConstraint(args.Statement, constraint.ReachIntent, args.IntentID,
		args.BindsFrom, args.ReviewDate, args.Zone)
	if err != nil {
		return "", err
	}
	written, err := constraint.NewWriter(c.p.d.pool, c.p.d.token).Arrive(ctx, actor, arriving)
	if err != nil {
		return "", err
	}
	c.changed("constraint", written.ID)
	c.changed("work", listAddressID)
	return written.ID, nil
}

// WithdrawConstraint withdraws a constraint of any reach, the permanent ones
// supplied at Factory among them.
func (c *calls) WithdrawConstraint(ctx context.Context, who principal.Principal, args screens.WithdrawConstraintArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := constraint.NewWriter(c.p.d.pool, c.p.d.token).Withdraw(ctx, actor, args.ConstraintID); err != nil {
		return err
	}
	c.changed("constraint", args.ConstraintID)
	c.changed("factory", listAddressID)
	return nil
}

// ClearCeiling authorises an overage for the period standing on the named
// credential alone: it does not reset the sum a further report is compared
// against, which is what the design asks of it.
func (c *calls) ClearCeiling(ctx context.Context, who principal.Principal, args screens.ClearCeilingArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if err := c.p.dispatch.ClearCeiling(ctx, actor, args.Credential); err != nil {
		return err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	c.changed("factory", listAddressID)
	return nil
}

// arrivingConstraint is one constraint as the writer takes it: the document
// kind, the reach and the record it names, and the two calendar values with the
// IANA zone the client sent beside them.
func arrivingConstraint(statement string, reach constraint.Reach, subjectID,
	bindsFrom, reviewDate, zone string) (constraint.New, error) {
	if strings.TrimSpace(statement) == "" {
		return constraint.New{}, errors.New("factory: a constraint states what it binds, and this one states nothing")
	}
	arriving := constraint.New{
		Kind: constraint.KindDocument, Reach: reach, SubjectID: subjectID,
		Statement: strings.TrimSpace(statement),
	}
	if bindsFrom == "" && reviewDate == "" {
		return arriving, nil
	}
	if err := zoneNamed(zone); err != nil {
		return constraint.New{}, err
	}
	if bindsFrom != "" {
		arriving.BindsFrom = &constraint.CalendarDate{Date: bindsFrom, Zone: zone}
	}
	if reviewDate != "" {
		arriving.ReviewBy = &constraint.CalendarDate{Date: reviewDate, Zone: zone}
	}
	return arriving, nil
}
