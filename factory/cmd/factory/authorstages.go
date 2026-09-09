package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// The three stages between an item's first and its build: the spec, the
// implementation plan, and the tasks. Each is the same shape — dispatch puts
// the role on the stage, the artifact store writes what it authored, the
// stage's own gate row fires over that version, and a reject sends the item
// back to be authored again against the reason and the version that was
// rejected — and each is written out, so a reader of one knows what the other
// two do.

// specStage is the item's spec version, the criteria it introduces, the ones
// it withdraws, the screen state machine where the item has a user interface,
// and the Spec gate over the version. Every item authors its own, on the first
// entry and on every re-entry: the interview is the intent's, run by the role
// put on the intent, and it authors no spec.
// returned is what a row after this stage's own found wrong, where the pass that
// re-enters the stage read a rejection off the log rather than taking one from
// its own loop. It is the zero value on a stage entered for the first time.
func (p *path) specStage(ctx context.Context, c *candidate, returned agent.Returned) error {
	d := p.d
	for {
		authored, run, err := p.dispatch.SpecAuthor(ctx, p.on(c, item.StageSpec, !returned.Empty()),
			p.specMaterial(c), p.refining(c, returned))
		p.reportAttempts(dispatch.RoleSpecAuthor, run)
		if err != nil {
			return err
		}
		if authored.Question != "" {
			return fmt.Errorf(
				"factory: the spec author asked a question about item %s, and the interview is the intent's and is over", c.itemID)
		}

		version, err := p.submitSpec(ctx, c, authored, run.InputManifestID)
		if err != nil {
			return err
		}
		// The Spec row rejects in both directions over the requirement a
		// criterion names, mechanically and whatever the score returns, so it
		// is computed over the version just submitted and handed to the firing.
		check, found, err := p.specRejection(ctx, c)
		if err != nil {
			return err
		}
		done, err := p.itemGate(ctx, c, gate.Spec, version, &c.specGate, check, found)
		if err != nil {
			return err
		}
		if done.waiting {
			return nil
		}
		if done.verdict == gate.VerdictApprove {
			c.spec = authored.Spec
			return nil
		}
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageSpec); err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Rejected at %s: %s\nItem %s authors its spec again against what was found wrong\n",
			gate.Spec, done.reason, c.itemID)
		returned = agent.Returned{Reason: done.reason, Version: authored.Spec}
	}
}

// submitSpec writes the version and everything it introduces in one call: the
// criteria, each naming the requirement it answers, the criteria it withdraws,
// and the screen's state machine. A version the gate then rejects takes its
// withdrawals down with it, which is why the withdrawal is recorded on the
// withdrawing version rather than on the criterion.
func (p *path) submitSpec(ctx context.Context, c *candidate, authored agent.Refined, manifestID string) (string, error) {
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: p.d.modelName}
	drafts := make([]criterion.Draft, 0, len(authored.Criteria))
	for _, one := range authored.Criteria {
		reason := ""
		if _, matched := criterion.Classify(one.Sentence); !matched {
			reason = "not classified by the command-line interface"
		}
		drafts = append(drafts, criterion.Draft{
			Sentence:          one.Sentence,
			NoPatternReason:   reason,
			RequirementID:     p.requirementFor(c, one.RequirementID, one.Sentence),
			ConstraintDerived: constraintsFor(c, one.ConstraintDerived),
			HazardDerived:     hazardFor(c, one.HazardDerived),
		})
	}
	var machines []screenstatemachine.Draft
	if authored.Screen != nil {
		machines = append(machines, screenMachine(*authored.Screen))
	}
	version, introduced, written, err := p.store.SubmitSpec(ctx, p.specAuthorActor(), by,
		c.itemID, c.svc.ID, authored.Spec, drafts, authored.Withdrawn, machines, manifestID)
	if err != nil {
		return "", err
	}
	c.specArtifactID = version.ID
	c.criterionIDs = nil
	for _, cr := range introduced {
		c.criterionIDs = append(c.criterionIDs, cr.ID)
		fmt.Fprintf(p.d.out, "Spec %s submitted; criterion %s (%s): %s\n", version.ID, cr.ID, cr.Pattern, cr.Sentence)
	}
	for _, id := range authored.Withdrawn {
		fmt.Fprintf(p.d.out, "  spec %s withdraws criterion %s\n", version.ID, id)
	}
	c.screens = nil
	for _, m := range written {
		screen := agent.ScreenInForce{ID: m.Screen, States: m.States}
		for _, t := range m.Transitions {
			screen.Transitions = append(screen.Transitions, agent.ScreenTransition{
				From: t.From, Event: t.Event, To: t.To, Screen: t.Screen,
			})
		}
		c.screens = append(c.screens, screen)
		fmt.Fprintf(p.d.out, "  spec %s declares screen %s: %d state(s), initial %s\n",
			version.ID, m.Screen, len(m.States), m.Initial)
	}
	if len(introduced) == 0 {
		fmt.Fprintf(p.d.out, "Spec %s submitted, introducing no criterion\n", version.ID)
	}
	return version.ID, nil
}

// screenMachine is what the spec author declared as the draft the store
// writes. A machine that supersedes another is not authored here: nothing in
// this interface hands the role the machine in force to revise.
func screenMachine(declared agent.ScreenMachine) screenstatemachine.Draft {
	draft := screenstatemachine.Draft{
		Initial:  declared.Initial,
		States:   declared.States,
		Events:   declared.Events,
		Terminal: declared.Terminal,
	}
	for _, t := range declared.Transitions {
		draft.Transitions = append(draft.Transitions, screenstatemachine.Transition{
			From: t.From, Event: t.Event, To: t.To, Screen: t.Screen,
		})
	}
	return draft
}

// constraintsFor is the constraint ids a criterion derives from, narrowed to the
// constraints the stage was handed: a criterion is constraint-derived only where
// the drafting stage named a constraint it held as evidence, so an id nothing
// handed it is not evidence and is dropped.
func constraintsFor(c *candidate, named []string) []string {
	var held []string
	for _, id := range named {
		for _, one := range c.constraints {
			if one.ID == id {
				held = append(held, id)
				break
			}
		}
	}
	return held
}

// hazardFor is the area a criterion bounds the hazardous operation of, and is
// empty unless the criterion names the one area the stage was handed: the
// derivation and the field are the irreversible grade's alone.
func hazardFor(c *candidate, named string) string {
	if named != "" && named == c.hazard.AreaID {
		return named
	}
	return ""
}

// planStage is the item's implementation plan version and the Implementation
// plan gate over it.
func (p *path) planStage(ctx context.Context, c *candidate, returned agent.Returned) error {
	for {
		inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
		if err != nil {
			return err
		}
		planned, run, err := p.dispatch.Planner(ctx, p.on(c, item.StageImplementationPlan, !returned.Empty()),
			[]inputmanifest.Material{
				{Class: fleetentry.ClassRunOutput, Reference: c.specArtifactID, Bytes: int64(len(c.spec))},
				{Class: fleetentry.ClassRunOutput, Reference: c.svc.ID, Bytes: int64(len(inForce))},
			},
			agent.Planning{Spec: c.spec, Criteria: rolePromptCriteria(inForce), Returned: returned})
		p.reportAttempts(dispatch.RoleImplementationPlanner, run)
		if err != nil {
			return err
		}
		by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: p.d.modelName}
		version, err := p.store.SubmitPlan(ctx, p.plannerActor(), by, c.itemID, planned.Text, run.InputManifestID)
		if err != nil {
			return err
		}
		c.planArtifactID = version.ID
		fmt.Fprintf(p.d.out, "Implementation plan %s submitted for item %s\n", version.ID, c.itemID)

		done, err := p.itemGate(ctx, c, gate.ImplementationPlan, version.ID, &c.planGate, "", "")
		if err != nil {
			return err
		}
		if done.waiting {
			return nil
		}
		if done.verdict == gate.VerdictApprove {
			c.plan = planned.Text
			return nil
		}
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageImplementationPlan); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected at %s: %s\nItem %s plans again against what was found wrong\n",
			gate.ImplementationPlan, done.reason, c.itemID)
		returned = agent.Returned{Reason: done.reason, Version: planned.Text}
	}
}

// tasksStage is the approved plan divided into the work an agent picks up, and
// the Tasks gate over it. A task is an internal step of the item: it has no
// build, no number and no environment, so nothing here writes a record of one
// beyond the version's own text.
func (p *path) tasksStage(ctx context.Context, c *candidate, returned agent.Returned) error {
	for {
		divided, run, err := p.dispatch.TaskAuthor(ctx, p.on(c, item.StageTasks, !returned.Empty()),
			[]inputmanifest.Material{
				{Class: fleetentry.ClassRunOutput, Reference: c.planArtifactID, Bytes: int64(len(c.plan))},
				{Class: fleetentry.ClassRunOutput, Reference: c.specArtifactID, Bytes: int64(len(c.spec))},
			},
			agent.Dividing{Plan: c.plan, Spec: c.spec, Returned: returned})
		p.reportAttempts(dispatch.RoleTaskAuthor, run)
		if err != nil {
			return err
		}
		by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: p.d.modelName}
		version, err := p.store.SubmitTasks(ctx, p.taskAuthorActor(), by, c.itemID, divided.Text, run.InputManifestID)
		if err != nil {
			return err
		}
		c.tasksArtifactID = version.ID
		fmt.Fprintf(p.d.out, "Tasks %s submitted for item %s: %d task(s)\n", version.ID, c.itemID, len(divided.Lines))

		done, err := p.itemGate(ctx, c, gate.Tasks, version.ID, &c.tasksGate, "", "")
		if err != nil {
			return err
		}
		if done.waiting {
			return nil
		}
		if done.verdict == gate.VerdictApprove {
			c.tasks = divided.Text
			return nil
		}
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageTasks); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected at %s: %s\nItem %s divides the plan again against what was found wrong\n",
			gate.Tasks, done.reason, c.itemID)
		returned = agent.Returned{Reason: done.reason, Version: divided.Text}
	}
}

// criteriaTheBuildDecided is every result the build's own process wrote for one
// build, which is what the Implementation row rejects over. A result from the
// candidate environment is left out: that run has not happened when this row
// fires, and what it decides is read at Merge to master.
func (p *path) criteriaTheBuildDecided(ctx context.Context, buildID string) ([]gate.CriterionResult, error) {
	if buildID == "" {
		return nil, nil
	}
	latest, err := criterion.Latest(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	var decided []gate.CriterionResult
	for _, r := range latest {
		if r.Place != criterion.PlaceBuild {
			continue
		}
		decided = append(decided, gate.CriterionResult{
			CriterionID: r.CriterionID, Outcome: r.Outcome, Place: r.Place,
		})
	}
	return decided, nil
}

// itemGate fires one of the four rows an item's own artifact is decided at and
// settles it, recording the firing on the candidate. The three rows above the
// build name no build; the Implementation row names the build the stage made
// and hands the gate that build's diff, which is where the score reads the
// change factors.
//
// check and found are a mechanical rejection the caller computed, empty where
// the row has none or nothing rejected. The row fires first either way, so the
// vector exists and the rejection is readable against it, and then the
// factory's own reject closes it before a verdict is asked for — which is the
// shape the merge row's own rejection takes.
func (p *path) itemGate(ctx context.Context, c *candidate, row gate.Row, artifactID string,
	into *fired, check, found string) (settled, error) {
	firing := gate.Firing{
		Row:           row,
		ItemID:        c.itemID,
		ArtifactID:    artifactID,
		ServiceID:     c.svc.ID,
		AreaID:        p.areaID,
		EnvironmentID: p.production.ID,
	}
	if row.Kind == gate.KindImplementation {
		reached, err := p.exposureOf(ctx, c.buildID)
		if err != nil {
			return settled{}, err
		}
		inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
		if err != nil {
			return settled{}, err
		}
		firing.BuildID = c.buildID
		firing.Measurement = c.measurement
		firing.Exposure = reached
		firing.CriteriaInForce = len(inForce)

		// The transition check and the drivers, derived from the checkout the
		// build was made from — still on the item's branch at the commit
		// commitAndBuild left it on. A screen the extractor could not derive
		// resolves the factor [screenstatemachine.Derivation.Unavailable] reads
		// rather than rejecting, so it is carried on the firing whatever
		// [gate.ScreenRejection] finds, and the mechanical rejection is
		// computed here rather than by the caller, the way the exposure and the
		// criteria in force already are — except where the caller already found
		// a build that does not compile and handed check non-empty: a build
		// the compiler refused has no transition to admit or forbid, so that
		// finding is what is reported and this one is left uncomputed.
		screensInForce, err := screenstatemachine.InForce(ctx, p.d.pool, c.svc.ID, []string{c.itemID})
		if err != nil {
			return settled{}, err
		}
		derivedScreens := screenstatemachine.DeriveTransitions(c.svc.Repository, screensInForce,
			screenstatemachine.GoExtractor(factoryVersion))
		derivedDrivers, err := screenstatemachine.DeriveDrivers(c.svc.Repository)
		if err != nil {
			return settled{}, err
		}
		firing.Screens = derivedScreens
		if check == "" {
			check, found, _ = gate.ScreenRejection(derivedScreens, derivedDrivers, screensInForce)
		}

		// What the build's own process decided. An encoding declares which of
		// two places decides it, and the ones declaring the build ran while
		// the build runner built, so their results are readable here — before
		// any environment exists, which is what lets this row reject on them.
		decided, err := p.criteriaTheBuildDecided(ctx, c.buildID)
		if err != nil {
			return settled{}, err
		}
		firing.Criteria = decided
		if check == "" {
			check, found, _ = gate.CriterionRejection(decided)
		}
	}
	opened, err := p.gate.Fire(ctx, firing)
	if err != nil {
		return settled{}, err
	}
	p.moved = true
	report(p.d.out, opened, nil)
	if check != "" {
		closing, err := p.gate.AutoReject(ctx, opened, check, found)
		if err != nil {
			return settled{}, err
		}
		*into = recordFiring(opened, closing)
		fmt.Fprintf(p.d.out, "Rejected by %s before a verdict was asked for: %s\n", check, found)
		fmt.Fprintf(p.d.out, "  close event %s written as %s\n", closing.ID, closing.Actor.Key)
		return settled{verdict: gate.VerdictReject, reason: found, closing: closing}, nil
	}
	done, err := p.settle(ctx, opened)
	if err != nil {
		return settled{}, err
	}
	// The firing is recorded whether or not it was decided here: a row left
	// waiting in Work is a row that fired, and what the firing read is on it.
	*into = recordFiring(opened, done.closing)
	if done.waiting {
		c.waiting = row
	}
	return done, nil
}

// on is the dispatch one of this item's stages is for: the item, the stage,
// the intent it was decomposed from, and the subjects a scope is matched
// against. reentering says the stage is being entered again after a reject,
// which is what counts the attempt.
//
// The area is the one this interface declares per run and nothing above it:
// dispatch follows the chain up to the project itself, an entry drawn on any
// area above the item's reaching it.
func (p *path) on(c *candidate, stage item.Stage, reentering bool) dispatch.On {
	return dispatch.On{
		ItemID:     c.itemID,
		Stage:      stage,
		IntentID:   c.intentID,
		ProjectID:  p.projectID,
		ServiceID:  c.svc.ID,
		AreaID:     p.areaID,
		Reentering: reentering,
	}
}

// specMaterial is what the spec stage hands the agent, named by reference for
// the manifest dispatch writes before the run.
func (p *path) specMaterial(c *candidate) []inputmanifest.Material {
	return []inputmanifest.Material{
		{Class: fleetentry.ClassIntentStatement, Reference: c.intentID},
		{Class: fleetentry.ClassRepository, Reference: c.svc.ID},
	}
}

// refining is what the spec author is given: the intent's statement, the
// service, the requirements this item answers, the criteria the service
// already promises, the constraints in force, the item's area where it is
// graded irreversible, and what sent the item back here. The last two are what
// a criterion's constraint-derived and hazard-derived provenance is written
// from, which the stage cannot name unless the role was told them.
func (p *path) refining(c *candidate, returned agent.Returned) agent.Refining {
	return agent.Refining{
		Statement:    c.statement,
		Service:      c.svc.Name,
		Requirements: c.requirements,
		InForce:      rolePromptCriteria(c.promised),
		Constraints:  c.constraints,
		Hazard:       c.hazard,
		Returned:     returned,
	}
}

// constraintsInForce is the constraints the drafting stage holds. There is no
// constraint record in this factory, so the only constraint it can name is
// whatever arrived with the request, which the intent carries by id and with no
// text; agent's doc.go names that as what the composition does not supply.
func constraintsInForce(in intent.Intent) []agent.Constraint {
	if in.ConstraintID == "" {
		return nil
	}
	return []agent.Constraint{{ID: in.ConstraintID}}
}

// hazardInForce is the item's area as the spec author is told it, where the
// grade in force for that area is irreversible and the area itself names the
// hazardous operation. Anything else is the zero value: the derivation and the
// hazard-derived field are the irreversible grade's alone.
//
// promised is the service's criteria in force, read for whether one already
// bounds the operation — the stage derives one only where none does.
func (p *path) hazardInForce(ctx context.Context, promised []criterion.Criterion) (agent.Hazard, error) {
	if p.areaID == "" {
		return agent.Hazard{}, nil
	}
	grade, err := area.SeverityInForce(ctx, p.d.pool, p.areaID)
	if err != nil {
		return agent.Hazard{}, err
	}
	if grade != area.GradeIrreversible {
		return agent.Hazard{}, nil
	}
	ar, err := area.Get(ctx, p.d.pool, p.areaID)
	if err != nil {
		return agent.Hazard{}, err
	}
	h := agent.Hazard{AreaID: p.areaID, Operation: ar.Hazard.Operation}
	for _, one := range promised {
		if one.HazardDerived == p.areaID {
			h.Controlled = true
		}
	}
	return h, nil
}

// requirementFor is the requirement id a criterion names: the one the role's
// reply named, whether or not this item answers it. A criterion naming a
// requirement assigned elsewhere is written and rejected at the Spec row, which
// is where the design puts that rejection — repointing it here would be this
// interface deciding what the gate decides mechanically.
//
// A reply that named none is the interview's own first call, where the role
// message listed no requirement and the criteria are what the requirements were
// then written from, one per criterion and in order. So the requirement is the
// item's own whose statement is this criterion's sentence, and a sentence
// matching none leaves the criterion naming nothing — which package criterion
// refuses for a sentence fitting a pattern.
func (p *path) requirementFor(c *candidate, named, sentence string) string {
	if named != "" {
		return named
	}
	for _, r := range c.requirements {
		if r.Statement == sentence {
			return r.ID

		}
	}
	return ""
}
