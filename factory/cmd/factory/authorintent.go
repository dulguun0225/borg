package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/intent"
)

// defaultTier is what this interface proposes at the confirming round. Gate
// policy does not yet author a tier value and package intent defines no
// default of its own, so this is the command-line interface's own placeholder until
// that parameter exists — a later dispatch's decision, not this one's.
var defaultTier = intent.Tier{Value: 1, PolicyVersion: "unauthored"}

// take is the intent a decomposition is authored from. Package intent's
// rewrite dropped the statement-keyed lookup [take] used to read: a detector's
// removal intent and a health monitor's revert intent are now found only by
// evidence or by id, and this command-line interface is handed a statement rather
// than either — so an owner re-typing the exact words of an intent already
// waiting takes a second one in rather than resuming the first, which this
// milestone's decision leaves open.
func (p *path) take(ctx context.Context, statement string) (intent.Intent, error) {
	return p.intake.TakeIn(ctx, p.human, intent.Arrival{
		Source:    intent.SourceOwner,
		Statement: statement,
		ProjectID: p.projectID,
	})
}

// authorIntent takes one intent in, refines it, decomposes every item it yields,
// ratifies the set at Decomposition where it yielded more than one, and authors
// each item's spec, implementation, and build.
//
// The order is the design's: intake, the interview, decomposition, decomposition's own gate,
// and then the stages per item. One simplification is kept from M1 and it shows
// here: the interview's own call is what authors the first item's spec, so on a set
// that spec exists before Decomposition ratified the set. What that costs is one
// spec thrown away on a rejected decomposition, which is what a rejected round throws away
// anyway — its items are superseded and their replacements start at nothing.
func (p *path) authorIntent(ctx context.Context, one asked, of string) (*decompositionSet, []*candidate, error) {
	d := p.d
	if len(one.services) == 0 {
		return nil, nil, fmt.Errorf("factory: the intent %q names no service to decompose an item on", one.statement)
	}

	// 1. Intake: the intent arrives from the owner, unrefined — or, where
	// one.resumeIntentID names an intent already waiting, that one, read
	// rather than taken in again.
	var in intent.Intent
	var err error
	if one.resumeIntentID != "" {
		in, err = intent.Get(ctx, d.pool, one.resumeIntentID)
	} else {
		in, err = p.take(ctx, one.statement)
	}
	if err != nil {
		return nil, nil, err
	}
	set := &decompositionSet{intentID: in.ID}
	fmt.Fprintf(d.out, "Intent %s taken in (%s): %s\n", in.ID, of, in.Statement)
	fmt.Fprintf(d.out, "  it changes %d service(s): %v\n", len(one.services), one.services)

	// gaveUp says on the terminal what stopped the item. It writes nothing: a
	// stage that spent its limit was escalated by dispatch before the error
	// came back — the item written escalated, its pending rows abandoned, and
	// the wait delivered — and a dispatch a condition stopped wrote its hold
	// row there too.
	gaveUp := func(itemID string, err error) error {
		switch {
		case heldHere(err):
			fmt.Fprintln(d.out, describeHold(itemID, "", err))
		case escalatedHere(err):
			fmt.Fprintf(d.out, "The factory gave up on %s: %v\n", itemID, err)
			fmt.Fprintln(d.out, "  the item is escalated, its open rows are abandoned, and duty 12 has been told")
		}
		return err
	}

	// 2. The interview, one round or none. It is the intent's own and produces
	// no spec: the role put on the intent asks what it cannot proceed without
	// and states the reading, and every item's spec is authored at its own
	// stage below.
	requirements, rounds, err := p.interview(ctx, in)
	if err != nil {
		return nil, nil, gaveUp("", err)
	}
	fmt.Fprintf(d.out, "Intent %s refined after %d round(s)\n", in.ID, rounds)

	// 3. Decomposition: one item per service the intent changes, each waiting on the one
	// before it, the service record written where the service does not exist yet, and
	// what each item answers assigned — whole where the set is one item, and a share
	// derived per item where the split spreads a requirement over several.
	candidates, err := p.decomposeItems(ctx, in, one.services, requirements)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range candidates {
		set.itemIDs = append(set.itemIDs, c.itemID)
		c.statement = in.Statement
		itsPromised, err := p.inForceFor(ctx, c.svc, nil)
		if err != nil {
			return nil, nil, err
		}
		c.promised = itsPromised
		c.constraints = constraintsInForce(in)
		if c.hazard, err = p.hazardInForce(ctx, itsPromised); err != nil {
			return nil, nil, err
		}
	}

	// 4. Decomposition, where decomposition yielded more than one item. One verdict
	// covers the whole decomposition however many services it changes.
	if len(candidates) > 1 {
		approved, err := p.decompositionGate(ctx, in, set, candidates)
		if err != nil {
			return nil, nil, err
		}
		if !approved {
			return set, candidates, nil
		}
	}

	// 5. The four authoring stages per item, each with its own gate row: the
	// spec and the criteria it introduces, the implementation plan, the tasks
	// the plan divides into, and the implementation with the build and the
	// consumer contract derived from it. Every item authors its own spec,
	// against the criteria its own service already promises.
	for _, c := range candidates {
		if err := p.specStage(ctx, c); err != nil {
			return nil, nil, gaveUp(c.itemID, err)
		}
		if err := p.planStage(ctx, c); err != nil {
			return nil, nil, gaveUp(c.itemID, err)
		}
		if err := p.tasksStage(ctx, c); err != nil {
			return nil, nil, gaveUp(c.itemID, err)
		}
		if err := p.implementationStage(ctx, c); err != nil {
			return nil, nil, gaveUp(c.itemID, err)
		}
	}
	return set, candidates, nil
}

// interview is the intent's one round or none and the confirming round the
// design gives every requester. The interviewer is put on the intent by
// dispatch, which is where the role, the scope, the manifest, the agent run
// record and the attempt limit are — the interview counts its rounds against
// the same limit a stage does, and the count it is compared against is the
// intent's own.
//
// The role asks what it cannot proceed without and states the reading on the
// answer; a second question is an error, which is the stopping rule enforced
// rather than assumed.
//
// It returns the requirements [intent.Intake.Confirm] wrote, which is what
// decomposition answers each item with and what the spec author names on each
// criterion.
func (p *path) interview(ctx context.Context, in intent.Intent) ([]agent.Requirement, int, error) {
	d := p.d

	rounds, err := p.intake.OpenRound(ctx, intakeActor, in.ID)
	if err != nil {
		return nil, 0, err
	}

	on := dispatch.On{IntentID: in.ID, ProjectID: p.projectID, CountedSoFar: rounds}
	material := []inputmanifest.Material{
		{Class: "intent", Reference: in.ID, Bytes: int64(len(in.Statement))},
	}
	interviewing := agent.Interviewing{Statement: in.Statement}
	read, _, err := p.dispatch.Interviewer(ctx, on, material, interviewing)
	if err != nil {
		return nil, 0, err
	}

	if read.Question != "" {
		q, err := p.intake.Ask(ctx, intakeActor, in.ID, read.Question)
		if err != nil {
			return nil, 0, err
		}
		fmt.Fprintf(d.out, "The interviewer asks: %s\n", q.Question)
		// An empty line is asked again rather than sent: the answer is write-once, and
		// the interview's one round is what it is spent on, so a blank one would stamp
		// the question answered and leave the interview proceeding on nothing. Input
		// that ends instead of answering is readLine's error and stops the path.
		answer := ""
		for answer == "" {
			answer, err = readLine(p.lines)
			if err != nil {
				return nil, 0, err
			}
			if answer == "" {
				fmt.Fprint(d.out, "An answer is what the interview's one round is spent on; type one: ")
			}
		}
		// The limit the answer is given is the interview's own, and it is what
		// intake tells the two escalations apart by: a human's answer clears an
		// escalation the rounds caused and never one decomposition caused.
		limit, err := intentAttemptLimit(ctx, d.pool, factorysettings.SubjectInterview)
		if err != nil {
			return nil, 0, err
		}
		q, err = p.intake.Answer(ctx, p.human, q.ID, answer, limit)
		if err != nil {
			return nil, 0, err
		}
		next, err := p.intake.OpenRound(ctx, intakeActor, in.ID)
		if err != nil {
			return nil, 0, err
		}
		rounds = next
		on.CountedSoFar = next
		interviewing.Answered = []agent.Question{{Question: q.Question, Answer: q.Answer}}
		read, _, err = p.dispatch.Interviewer(ctx, on, material, interviewing)
		if err != nil {
			return nil, 0, err
		}
		if read.Question != "" {
			return nil, 0,
				errors.New("factory: the interviewer asked a second question, and the interview is one round or none")
		}
	}

	// The confirming round: the factory states, in the requester's own terms,
	// what it has understood is wanted, and the requester's answer confirms
	// that reading. An intent the factory raised itself has no requester and
	// takes no such round — it enumerates its requirements from the evidence
	// instead, which [intent.Intake.Confirm] does on its own where it is given
	// neither a question nor an answer. This interface has no screen for the
	// round a requester does owe, so where there is one it states the criteria
	// the spec author produced and confirms them itself — a simplification this
	// milestone's command-line interface makes rather than prompting a second
	// time for a round the design gives the requester, and an open point this
	// dispatch leaves.
	confirmation := intent.Confirmation{IntentID: in.ID}
	if in.Source != intent.SourceDetector {
		confirmQuestion := fmt.Sprintf("The factory understood: %s — confirm?", strings.Join(read.Requirements, "; "))
		cq, err := p.intake.Ask(ctx, intakeActor, in.ID, confirmQuestion)
		if err != nil {
			return nil, 0, err
		}
		// Confirm answers the confirming question itself, inside the same call
		// that writes the reading — answering it here first would leave nothing
		// for Confirm to answer and it would refuse the write-once question a
		// second time.
		const confirmAnswer = "confirmed"
		confirmation.QuestionID = cq.ID
		confirmation.Answer = confirmAnswer
		confirmation.IntendedEffect = in.Statement
		confirmation.Tier = defaultTier
	}

	for _, statement := range read.Requirements {
		escapeReason := ""
		if _, matched := criterion.Classify(statement); !matched {
			escapeReason = "not classified by the command-line interface"
		}
		confirmation.Requirements = append(confirmation.Requirements,
			intent.NewRequirement{Statement: statement, EscapeReason: escapeReason})
	}
	written, err := p.intake.Confirm(ctx, intakeActor, confirmation)
	if err != nil {
		return nil, 0, err
	}
	requirements := make([]agent.Requirement, len(written))
	for n, r := range written {
		requirements[n] = agent.Requirement{ID: r.ID, Statement: r.Statement}
	}
	return requirements, rounds, nil
}
