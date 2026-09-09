package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// defaultTier is what this interface proposes at the confirming round. Gate
// policy does not yet author a tier value and package intent defines no
// default of its own, so this is the command-line interface's own placeholder until
// that parameter exists — a later dispatch's decision, not this one's.
var defaultTier = intent.Tier{Value: 1, PolicyVersion: "unauthored"}

// take is the intent a decomposition is authored from: the one already waiting
// on these exact words in this project, where a detector or the health monitor
// raised it, and otherwise a new one taken in from the owner. This interface is
// handed a statement and never an id, so the words are how a human names the
// revert a rollback already raised.
func (p *path) take(ctx context.Context, statement string) (intent.Intent, error) {
	waiting, found, err := intent.Waiting(ctx, p.d.pool, p.projectID, statement)
	if err != nil {
		return intent.Intent{}, err
	}
	if found {
		fmt.Fprintf(p.d.out, "Intent %s is already waiting on these words, from %s; it is worked rather than taken in again\n", waiting.ID, waiting.Source)
		return waiting, nil
	}
	return p.intake.TakeIn(ctx, p.human, intent.Arrival{
		Source:    intent.SourceOwner,
		Statement: statement,
		ProjectID: p.projectID,
	})
}

// authorIntent takes one intent in and drives it as far as this composition
// can take it: the interview, and decomposition where the interview refined it.
//
// It is [path.takeIn]'s and a test's way in, and it performs no step the pass
// does not: [path.refineIntent] and [path.decomposeSet] are the intent stage of
// [path.advance], reached here for an intent this caller has just taken in
// rather than read off the records.
//
// A question the interview asked that this composition has no answer for leaves
// the intent waiting in Work and returns no candidate: the answer is given at a
// screen, and the next pass continues from it.
func (p *path) authorIntent(ctx context.Context, one asked, of string) (*decompositionSet, []*candidate, error) {
	d := p.d
	if len(one.services) == 0 {
		return nil, nil, fmt.Errorf("factory: the intent %q names no service to decompose an item on", one.statement)
	}

	// The statement is written with the service prefix the caller gave it,
	// because the intent record holds no service and the prefix is the only
	// thing on the record that says what its decomposition yields items on: a
	// pass in a later process reads it back the same way it reads one an
	// intent taken in at Work carries.
	var in intent.Intent
	var err error
	if one.resumeIntentID != "" {
		in, err = intent.Get(ctx, d.pool, one.resumeIntentID)
	} else {
		in, err = p.take(ctx, strings.Join(one.services, ",")+": "+one.statement)
	}
	if err != nil {
		return nil, nil, err
	}
	set := &decompositionSet{intentID: in.ID}
	p.sets[in.ID] = set
	p.servicesOf[in.ID] = one.services
	fmt.Fprintf(d.out, "Intent %s taken in (%s): %s\n", in.ID, of, in.Statement)
	fmt.Fprintf(d.out, "  it changes %d service(s): %v\n", len(one.services), one.services)

	refined, err := p.refineIntent(ctx, in.ID)
	if err != nil {
		return nil, nil, p.gaveUp("", err)
	}
	if !refined {
		return set, nil, nil
	}
	candidates, err := p.decomposeSet(ctx, in.ID, set)
	return set, candidates, err
}

// intentStage is the pass's own intake: every intent the records hold that has
// not reached its items yet, interviewed as far as this composition can take it
// and then decomposed. It is what makes an intent taken in at Work reach an
// item without a run — [path.advance] performs it before it performs any step
// on an item, because an item is what a step is performed on.
func (p *path) intentStage(ctx context.Context) ([]*candidate, error) {
	ids, err := p.unworkedIntents(ctx)
	if err != nil {
		return nil, err
	}
	var decomposed []*candidate
	for _, id := range ids {
		refined, err := p.refineIntent(ctx, id)
		if err != nil {
			return decomposed, p.gaveUp("", err)
		}
		if !refined {
			continue
		}
		set := p.sets[id]
		if set == nil {
			set = &decompositionSet{intentID: id}
			p.sets[id] = set
		}
		if len(set.itemIDs) > 0 {
			continue
		}
		candidates, err := p.decomposeSet(ctx, id, set)
		if err != nil {
			return decomposed, err
		}
		decomposed = append(decomposed, candidates...)
	}
	return decomposed, nil
}

// unworkedIntents is every intent of this project this stage may work: one
// that has no item yet, has not stopped for good, and says which services its
// decomposition yields items on. An intent with an item has reached the steps
// [path.advance] performs over items.
//
// Which services it changes is the one thing this interface is told and never
// decides, and the intent record holds no service — so an intent is worked here
// only where something said: a caller of this process, which is what
// [path.servicesOf] holds, or the statement's own service prefix, which is what
// an intent taken in at Work carries.
//
// Two kinds are left alone, and both are the same gap. An intent the factory
// raised — a detector's, the health monitor's — and a revert a human raised at
// Ops both name their evidence and neither says what to decompose: what would
// decide that is a stage that decides a decomposition, which this interface
// does not have. So each waits for a caller to name its services, which is
// `run -intent` with the statement, and this stage passes over it.
func (p *path) unworkedIntents(ctx context.Context) ([]string, error) {
	all, err := intent.InProject(ctx, p.d.pool, p.projectID)
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, in := range all {
		switch in.State {
		case intent.StateDropped, intent.StateDelivered, intent.StateEscalated, intent.StateReDecomposing:
			continue
		}
		if len(p.servicesOf[in.ID]) == 0 {
			if in.Evidence != "" || in.Source != intent.SourceOwner ||
				!namesItsServices(in.Statement, p.d.services) {
				continue
			}
		}
		items, err := item.ForIntent(ctx, p.d.pool, in.ID)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			continue
		}
		ids = append(ids, in.ID)
	}
	return ids, nil
}

// decomposeSet is decomposition and its own gate for one refined intent: one
// item per service the intent changes, what each answers assigned, and the row
// that ratifies the set where it yielded more than one.
//
// Which services the intent changes is what this interface is told and never
// what it decides — a stage that decides a decomposition is a later
// milestone's — so it is read off the list the caller gave, and off the
// statement's own service prefix for an intent no caller of this process
// named.
func (p *path) decomposeSet(ctx context.Context, intentID string, set *decompositionSet) ([]*candidate, error) {
	in, err := intent.Get(ctx, p.d.pool, intentID)
	if err != nil {
		return nil, err
	}
	services := p.servicesOf[in.ID]
	if len(services) == 0 {
		if services, _, err = servicesFor(in.Statement, p.d.services); err != nil {
			return nil, err
		}
		p.servicesOf[in.ID] = services
	}
	inForce, err := intent.Requirements(ctx, p.d.pool, in.ID)
	if err != nil {
		return nil, err
	}
	requirements := make([]agent.Requirement, 0, len(inForce))
	for _, r := range inForce {
		requirements = append(requirements, agent.Requirement{ID: r.ID, Statement: r.Statement})
	}

	candidates, err := p.decomposeItems(ctx, in, services, requirements)
	if err != nil {
		return nil, err
	}
	_, statement, err := servicesFor(in.Statement, p.d.services)
	if err != nil {
		return nil, err
	}
	for _, c := range candidates {
		set.itemIDs = append(set.itemIDs, c.itemID)
		c.statement = statement
		itsPromised, err := p.inForceFor(ctx, c.svc, nil)
		if err != nil {
			return nil, err
		}
		c.promised = itsPromised
		c.constraints = constraintsInForce(in)
		if c.hazard, err = p.hazardInForce(ctx, itsPromised); err != nil {
			return nil, err
		}
		p.holdCandidate(c)
		p.authored[c.itemID] = true
	}
	p.moved = true

	// The Decomposition row, where decomposition yielded more than one item.
	// One verdict covers the whole decomposition however many services it
	// changes.
	if len(candidates) > 1 {
		if _, err := p.decompositionGate(ctx, in, set, candidates); err != nil {
			return nil, err
		}
	}
	return candidates, nil
}

// reportAttempts says what a dispatch spent where it spent more than the one
// entry that put the item on the stage: every attempt past the first is a
// reply the protocol refused, and the item was entered again for each.
func (p *path) reportAttempts(role dispatch.Role, run dispatch.Run) {
	if len(run.AgentRunIDs) < 2 {
		return
	}
	fmt.Fprintf(p.d.out, "The %s's reply was refused %d time(s); the stage was entered again for each, and the item stands at %d attempt(s)\n",
		role, len(run.AgentRunIDs)-1, run.Attempts)
}

// escalatedHere reports whether the error is the factory giving up on the item
// at this stage, which dispatch escalated before returning.
func escalatedHere(err error) bool { return errors.Is(err, dispatch.ErrOutOfAttempts) }

// intentLeftItsStop is the re-match an intent leaving the state that stopped it
// runs, called from wherever that state is written. A hold opened on an
// intent's state ends when the state does, and what says so is the writer of
// the record that ended it: nothing polls, and a hold left to the next
// unrelated dispatch would outlive its condition for as long as that took.
func (p *path) intentLeftItsStop(ctx context.Context) error {
	lifted, err := p.dispatch.RematchOnIntentState(ctx)
	if err != nil {
		return err
	}
	if len(lifted) > 0 {
		fmt.Fprintf(p.d.out, "The intent's state no longer stops work: %d hold(s) lifted\n", len(lifted))
	}
	return nil
}

// heldHere reports whether a condition stopped the dispatch, which is a hold
// and not a failure: no page fires and no attempt counts.
func heldHere(err error) bool { return errors.Is(err, dispatch.ErrHeld) }

// describeHold is what the terminal says about a dispatch that held.
func describeHold(itemID string, stage item.Stage, err error) string {
	return fmt.Sprintf("Item %s waits at %s: %s", itemID, stage,
		strings.TrimPrefix(err.Error(), "dispatch: "))
}

// gaveUp says on the terminal what stopped the item. It writes nothing: a stage
// that spent its limit was escalated by dispatch before the error came back —
// the item written escalated, its pending rows abandoned, and the wait
// delivered — and a dispatch a condition stopped wrote its hold row there too.
func (p *path) gaveUp(itemID string, err error) error {
	switch {
	case heldHere(err):
		fmt.Fprintln(p.d.out, describeHold(itemID, "", err))
	case escalatedHere(err):
		fmt.Fprintf(p.d.out, "The factory gave up on %s: %v\n", itemID, err)
		fmt.Fprintln(p.d.out, "  the item is escalated, its open rows are abandoned, and duty 12 has been told")
	}
	return err
}

// refineIntent is the intent stage performed on one intent, driven as far as
// this composition can take it: the interviewer put on the intent by dispatch,
// the question it asks left waiting in Work, and the confirming round the
// design asks the requester for left waiting there too.
//
// It reports whether the intent is refined, which is what says decomposition
// may run. A round waiting on an answer this composition does not supply is not
// an error: the answer is given at a screen through
// [screens.Calls.AnswerQuestion], and the next pass continues from it.
//
// Where [deps.answer] names one, it is what a round of the interview is
// answered with and the confirming round is confirmed by the composition
// itself — which is what keeps the run subcommand a run rather than a process
// waiting for a screen nobody has open.
func (p *path) refineIntent(ctx context.Context, intentID string) (bool, error) {
	for {
		in, err := intent.Get(ctx, p.d.pool, intentID)
		if err != nil {
			return false, err
		}
		if in.State != intent.StateUnrefined {
			return in.State == intent.StateRefined, nil
		}
		waiting, found, err := unansweredRound(ctx, p.d.pool, in.ID)
		if err != nil {
			return false, err
		}
		if found {
			if p.d.answer == "" {
				fmt.Fprintf(p.d.out, "Intent %s waits in Work on a round of the interview: %s\n",
					in.ID, waiting.Question)
				return false, nil
			}
			if err := p.answerARound(ctx, in, waiting); err != nil {
				return false, err
			}
			continue
		}
		if err := p.statesTheReading(ctx, in); err != nil {
			return false, err
		}
	}
}

// answerARound writes the answer this composition supplies to one round: the
// confirming round is confirmed and every other round is answered, which is
// the same pair of calls a screen makes at Work.
func (p *path) answerARound(ctx context.Context, in intent.Intent, waiting intent.Question) error {
	p.moved = true
	if readingIn(waiting.Question) != nil {
		return p.confirmTheReading(ctx, p.human, in, waiting)
	}
	// The limit the answer is given is the interview's own, and it is what
	// intake tells the two escalations apart by: a human's answer clears an
	// escalation the rounds caused and never one decomposition caused.
	limit, err := intentAttemptLimit(ctx, p.d.pool, factorysettings.SubjectInterview)
	if err != nil {
		return err
	}
	_, err = p.intake.Answer(ctx, p.human, waiting.ID, p.d.answer, limit)
	return err
}

// statesTheReading opens a round, dispatches the interviewer over it, and
// writes what came back: a question asked, or the reading stated — as the
// confirming round's own question for an intent somebody asked for, and
// straight into the requirement set for one the factory raised, which has no
// requester to ask.
//
// It writes one round and returns: what the round left is read back off the
// records by [path.refineIntent]'s next turn, so the two answers a round can
// leave — a question waiting, or a reading written — are told apart from the
// records and not from a flag this call returns.
func (p *path) statesTheReading(ctx context.Context, in intent.Intent) error {
	p.moved = true
	rounds, err := p.intake.OpenRound(ctx, intakeActor, in.ID)
	if err != nil {
		return err
	}
	answered, err := intent.Questions(ctx, p.d.pool, in.ID)
	if err != nil {
		return err
	}
	interviewing := agent.Interviewing{Statement: in.Statement}
	for _, q := range answered {
		if q.Answered() {
			interviewing.Answered = append(interviewing.Answered,
				agent.Question{Question: q.Question, Answer: q.Answer})
		}
	}
	material := []inputmanifest.Material{
		{Class: fleetentry.ClassIntentStatement, Reference: in.ID, Bytes: int64(len(in.Statement))},
	}
	read, _, err := p.dispatch.Interviewer(ctx,
		dispatch.On{IntentID: in.ID, ProjectID: p.projectID, CountedSoFar: rounds}, material, interviewing)
	if err != nil {
		return err
	}

	if read.Question != "" {
		q, err := p.intake.Ask(ctx, intakeActor, in.ID, read.Question)
		if err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "The interviewer asks: %s\n", q.Question)
		return nil
	}

	// An intent the factory raised has no requester, so it takes no confirming
	// round: it enumerates its requirements from the evidence instead, which
	// [intent.Intake.Confirm] does on its own where it is given neither a
	// question nor an answer.
	if in.Source == intent.SourceDetector {
		if _, err := p.intake.Confirm(ctx, intakeActor, intent.Confirmation{
			IntentID:     in.ID,
			Requirements: newRequirements(read.Requirements),
		}); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Intent %s is refined: the factory raised it, so it enumerates its requirements from the evidence\n", in.ID)
		return p.intentLeftItsStop(ctx)
	}

	// The confirming round: the factory states, in the requester's own terms,
	// what it has understood is wanted, and the requester's answer confirms
	// that reading at Work. The reading is stated in the question, which is the
	// only record that holds it before the round that confirms it writes the
	// requirement set.
	asked, err := p.intake.Ask(ctx, intakeActor, in.ID, confirmingQuestion(read.Requirements))
	if err != nil {
		return err
	}
	fmt.Fprintf(p.d.out, "The confirming round of intent %s asks: %s\n", in.ID, asked.Question)
	return nil
}

// newRequirements is the reading as the requirement set takes it, with the
// escape reason on every statement this interface cannot classify.
func newRequirements(read []string) []intent.NewRequirement {
	written := make([]intent.NewRequirement, 0, len(read))
	for _, statement := range read {
		escapeReason := ""
		if _, matched := criterion.Classify(statement); !matched {
			escapeReason = "not classified by the command-line interface"
		}
		written = append(written, intent.NewRequirement{Statement: statement, EscapeReason: escapeReason})
	}
	return written
}

// unansweredRound is the round of the interview waiting on an answer, and false
// where none is: the newest question with no answer on it.
func unansweredRound(ctx context.Context, pool *pgxpool.Pool, intentID string) (intent.Question, bool, error) {
	questions, err := intent.Questions(ctx, pool, intentID)
	if err != nil {
		return intent.Question{}, false, err
	}
	for n := len(questions) - 1; n >= 0; n-- {
		if !questions[n].Answered() {
			return questions[n], true, nil
		}
	}
	return intent.Question{}, false, nil
}
