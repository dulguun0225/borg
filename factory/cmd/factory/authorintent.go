package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/service"
)

// defaultTier is what this interface proposes at the confirming round. Gate
// policy does not yet author a tier value and package intent defines no
// default of its own, so this is the crude interface's own placeholder until
// that parameter exists — a later dispatch's decision, not this one's.
var defaultTier = intent.Tier{Value: 1, PolicyVersion: "unauthored"}

// take is the intent a decomposition is authored from. Package intent's
// rewrite dropped the statement-keyed lookup [take] used to read: a detector's
// removal intent and a health monitor's revert intent are now found only by
// evidence or by id, and this crude interface is handed a statement rather
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

	// gaveUp is the escalation page. A stage that spent its limit is the factory
	// saying it cannot do this one, and whether that also reaches a human out of the
	// product turns on the intent: an owner's request has nothing live that is worse,
	// and an intent a detector wrote is a defect that is live.
	gaveUp := func(itemID string, err error) error {
		if !errors.Is(err, ErrOutOfAttempts) {
			return err
		}
		if pageErr := p.escalated(ctx, in.ID, itemID, err.Error()); pageErr != nil {
			return errors.Join(err, pageErr)
		}
		return err
	}

	// 2. The interview, one round or none, and the first item's spec with it. The
	// service records that exist already are read before it, because the spec author
	// is told which criteria that service already promises and authors one that is
	// not among them.
	first := one.services[0]
	firstSvc, _, err := service.ByName(ctx, d.pool, first)
	if err != nil {
		return nil, nil, err
	}
	promised, err := p.inForceFor(ctx, firstSvc, "")
	if err != nil {
		return nil, nil, err
	}
	specStage, err := limitFor(ctx, p.policy, item.StageSpec,
		policy.Subjects{ServiceID: firstSvc.ID, AreaID: p.areaID})
	if err != nil {
		return nil, nil, err
	}
	refined, requirementIDs, rounds, err := p.interview(ctx, in, first, promised, specStage)
	if err != nil {
		return nil, nil, gaveUp("", err)
	}
	fmt.Fprintf(d.out, "Intent %s refined after %d round(s)\n", in.ID, rounds)

	// 3. Decomposition: one item per service the intent changes, each waiting on the one
	// before it, and the service record written where the service does not exist yet.
	candidates, err := p.decomposeItems(ctx, in, one.services, requirementIDs)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range candidates {
		set.itemIDs = append(set.itemIDs, c.itemID)
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

	// 5. The stages, per item: the spec, the implementation, the consumer contract
	// derived from the same build, and the build record.
	for n, c := range candidates {
		authored := refined
		stage := specStage
		if n > 0 {
			// Every item after the first authors its own spec, against the criteria
			// its own service already promises. The interview is not re-run: it is
			// the intent's and it is over.
			stage, err = limitFor(ctx, p.policy, item.StageSpec, p.subjectsFor(c))
			if err != nil {
				return nil, nil, err
			}
			itsPromised, err := p.inForceFor(ctx, c.svc, "")
			if err != nil {
				return nil, nil, err
			}
			author := agent.SpecAuthor{Model: d.model}
			authored, err = attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
				r, err := author.Refine(ctx, agent.Refining{
					Statement: in.Statement, Service: c.svc.Name, InForce: rolePromptCriteria(itsPromised),
				})
				return r, r.Tokens, err
			})
			callErr := err
			if recErr := p.recordAgentRun(ctx, "spec_author", c.itemID, item.StageSpec, "", stage.spends, outcomeOf(callErr)); recErr != nil {
				return nil, nil, recErr
			}
			if callErr != nil {
				return nil, nil, gaveUp(c.itemID, callErr)
			}
			if authored.Question != "" {
				return nil, nil, fmt.Errorf(
					"factory: the spec author asked a question about item %s, and the interview is the intent's and is over", c.itemID)
			}
		}
		if err := p.specStage(ctx, c, authored); err != nil {
			return nil, nil, err
		}
		if err := p.implementationStage(ctx, c, authored.Spec, gaveUp); err != nil {
			return nil, nil, err
		}
	}
	return set, candidates, nil
}

// outcomeOf is the word an agentrun record's outcome carries for one call: what
// the caller reads off whether the call errored, since this package names no
// vocabulary of its own for it.
func outcomeOf(err error) string {
	if err != nil {
		return "gave up"
	}
	return "authored"
}

// interview is the intent's one round or none, the confirming round the
// design gives every requester, and the spec the whole exchange produces.
// The spec author asks what it cannot author without and proceeds on the
// answer; a second question is an error, which is the stopping rule enforced
// rather than assumed.
//
// It returns the requirement ids [Intake.Confirm] wrote, which is what
// decomposition answers each item with — one requirement per intent, this
// milestone deriving no more than that from the spec author's own criterion
// sentence.
func (p *path) interview(ctx context.Context, in intent.Intent, serviceName string,
	promised []criterion.Criterion, stage *stageAttempts) (agent.Refined, []string, int, error) {
	d := p.d
	author := agent.SpecAuthor{Model: d.model}

	rounds, err := p.intake.OpenRound(ctx, intakeActor, in.ID)
	if err != nil {
		return agent.Refined{}, nil, 0, err
	}

	before := len(stage.spends)
	refined, err := attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, agent.Refining{
			Statement: in.Statement, Service: serviceName, InForce: rolePromptCriteria(promised),
		})
		return r, r.Tokens, err
	})
	if recErr := p.recordIntentRun(ctx, "spec_author", in.ID, stage.spends[before:], outcomeOf(err)); recErr != nil {
		return agent.Refined{}, nil, 0, recErr
	}
	if err != nil {
		return agent.Refined{}, nil, 0, err
	}

	if refined.Question != "" {
		q, err := p.intake.Ask(ctx, intakeActor, in.ID, refined.Question)
		if err != nil {
			return agent.Refined{}, nil, 0, err
		}
		fmt.Fprintf(d.out, "The spec author asks: %s\n", q.Question)
		// An empty line is asked again rather than sent: the answer is write-once, and
		// the interview's one round is what it is spent on, so a blank one would stamp
		// the question answered and leave the spec author authoring on nothing. Input
		// that ends instead of answering is readLine's error and stops the path.
		answer := ""
		for answer == "" {
			answer, err = readLine(p.lines)
			if err != nil {
				return agent.Refined{}, nil, 0, err
			}
			if answer == "" {
				fmt.Fprint(d.out, "An answer is what the interview's one round is spent on; type one: ")
			}
		}
		q, err = p.intake.Answer(ctx, p.human, q.ID, answer)
		if err != nil {
			return agent.Refined{}, nil, 0, err
		}
		before = len(stage.spends)
		refined, err = attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
			r, err := author.Refine(ctx, agent.Refining{
				Statement: in.Statement, Service: serviceName,
				Answered: []agent.QA{{Question: q.Question, Answer: q.Answer}},
				InForce:  rolePromptCriteria(promised),
			})
			return r, r.Tokens, err
		})
		if recErr := p.recordIntentRun(ctx, "spec_author", in.ID, stage.spends[before:], outcomeOf(err)); recErr != nil {
			return agent.Refined{}, nil, 0, recErr
		}
		if err != nil {
			return agent.Refined{}, nil, 0, err
		}
		if refined.Question != "" {
			return agent.Refined{}, nil, 0,
				errors.New("factory: the spec author asked a second question, and the interview is one round or none")
		}
	}

	// The confirming round: the factory states, in the requester's own terms,
	// what it has understood is wanted, and the requester's answer confirms
	// that reading. An intent the factory raised itself has no requester and
	// takes no such round — it enumerates its requirements from the evidence
	// instead, which [Intake.Confirm] does on its own where it is given
	// neither a question nor an answer. This interface has no screen for the
	// round a requester does owe, so where there is one it states the one
	// criterion the spec author produced and confirms it itself — a
	// simplification this milestone's crude interface makes rather than
	// prompting a second time for a round the design gives the requester, and
	// an open point this dispatch leaves.
	confirmation := intent.Confirmation{IntentID: in.ID}
	if in.Source != intent.SourceDetector {
		confirmQuestion := fmt.Sprintf("The factory understood: %s — confirm?", refined.Criterion)
		cq, err := p.intake.Ask(ctx, intakeActor, in.ID, confirmQuestion)
		if err != nil {
			return agent.Refined{}, nil, 0, err
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

	escapeReason := ""
	if _, matched := criterion.Classify(refined.Criterion); !matched {
		escapeReason = "not classified by the command-line interface"
	}
	confirmation.Requirements = []intent.NewRequirement{
		{Statement: refined.Criterion, EscapeReason: escapeReason},
	}
	reading, err := p.intake.Confirm(ctx, intakeActor, confirmation)
	if err != nil {
		return agent.Refined{}, nil, 0, err
	}
	requirementIDs := make([]string, len(reading))
	for n, r := range reading {
		requirementIDs[n] = r.ID
	}
	return refined, requirementIDs, rounds, nil
}
