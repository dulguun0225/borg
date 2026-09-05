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

// take is the intent this decomposition is authored from: the unrefined one already
// waiting with exactly this statement, or one intake takes in for it.
//
// The first is how a revert and a removal reach the pipeline. The health monitor took a
// revert's intent in at the rollback and the detector takes a removal's in at the
// pass that finds the list empty, and this interface's run is given a statement
// rather than an intent id — so a run given one of those statements works that
// intent rather than taking in a second one saying the same thing.
//
// Matching on the statement is what an interface with no screen can do. What it
// costs is a false match where an owner types a statement character for character
// equal to one already waiting; the screen that replaces this shows an owner the
// intents that are waiting and has them pick.
func (p *path) take(ctx context.Context, statement string) (intent.Intent, error) {
	waiting, found, err := intent.Unrefined(ctx, p.d.pool, statement)
	if err != nil {
		return intent.Intent{}, err
	}
	if found {
		fmt.Fprintf(p.d.out, "Intent %s is already waiting with this statement, taken in from %s by %s %s; this run works it\n",
			waiting.ID, waiting.Source, waiting.Actor.Kind, waiting.Actor.Name)
		return waiting, nil
	}
	return p.intake.TakeIn(ctx, p.human, intent.SourceOwner, statement)
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

	// 1. Intake: the intent arrives from the owner, unrefined — unless one is
	// already there for this statement, which is how a revert and a removal reach the
	// pipeline.
	in, err := p.take(ctx, one.statement)
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
	refined, interviewSpend, rounds, err := p.interview(ctx, in, first, promised, specStage)
	if err != nil {
		return nil, nil, gaveUp("", err)
	}
	if err := p.intake.MarkRefined(ctx, intakeActor, in.ID); err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(d.out, "Intent %s refined after %d round(s)\n", in.ID, rounds)

	// 3. Decomposition: one item per service the intent changes, each waiting on the one
	// before it, and the service record written where the service does not exist yet.
	candidates, err := p.decomposeItems(ctx, in, one.services)
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
			if err != nil {
				return nil, nil, gaveUp(c.itemID, err)
			}
			if authored.Question != "" {
				return nil, nil, fmt.Errorf(
					"factory: the spec author asked a question about item %s, and the interview is the intent's and is over", c.itemID)
			}
		}
		spent := interviewSpend
		if n > 0 {
			spent = 0
		}
		if err := p.specStage(ctx, c, authored, stage, spent); err != nil {
			return nil, nil, err
		}
		if err := p.implementationStage(ctx, c, authored.Spec, gaveUp); err != nil {
			return nil, nil, err
		}
	}
	return set, candidates, nil
}

// interview is the intent's one round or none, and the spec the same call
// produces. The spec author asks what it cannot author without and proceeds on the
// answer; a second question is an error, which is the stopping rule enforced rather
// than assumed.
//
// It returns what the interview spent apart from the spec, because the design
// counts a round against the same limit and keeps it on the intent, upstream of the
// item's first stage — and an intent has no spend field, so the round's tokens are
// charged to the item's first attempt where that is reported.
func (p *path) interview(ctx context.Context, in intent.Intent, serviceName string,
	promised []criterion.Criterion, stage *stageAttempts) (agent.Refined, int64, int, error) {
	d := p.d
	author := agent.SpecAuthor{Model: d.model}
	refined, err := attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, agent.Refining{
			Statement: in.Statement, Service: serviceName, InForce: rolePromptCriteria(promised),
		})
		return r, r.Tokens, err
	})
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	if refined.Question == "" {
		return refined, 0, 0, nil
	}

	var spend int64
	for _, s := range stage.spends {
		spend += s
	}
	stage.spends = nil
	q, err := p.intake.Ask(ctx, specAuthorActor, in.ID, refined.Question)
	if err != nil {
		return agent.Refined{}, 0, 0, err
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
			return agent.Refined{}, 0, 0, err
		}
		if answer == "" {
			fmt.Fprint(d.out, "An answer is what the interview's one round is spent on; type one: ")
		}
	}
	q, err = p.intake.Answer(ctx, p.human, q.ID, answer)
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	refined, err = attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, agent.Refining{
			Statement: in.Statement, Service: serviceName,
			Answered: []agent.QA{{Question: q.Question, Answer: q.Answer}},
			InForce:  rolePromptCriteria(promised),
		})
		return r, r.Tokens, err
	})
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	if refined.Question != "" {
		return agent.Refined{}, 0, 0,
			errors.New("factory: the spec author asked a second question, and the interview is one round or none")
	}
	return refined, spend, 1, nil
}
