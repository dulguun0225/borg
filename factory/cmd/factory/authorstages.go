package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/inputmanifest"
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
// and the Spec gate over the version. It is given the first version the
// interview's own call authored for the first item of a run, and authors its
// own on every re-entry and on every item after the first.
func (p *path) specStage(ctx context.Context, c *candidate, authored agent.Refined,
	firstIsAuthored bool, manifestID string) error {
	d := p.d
	returned := agent.Returned{}
	for {
		if !firstIsAuthored {
			refined, run, err := p.dispatch.SpecAuthor(ctx, p.on(c, item.StageSpec, !returned.Empty()),
				p.specMaterial(c), p.refining(c, returned))
			p.reportAttempts(dispatch.RoleSpecAuthor, run)
			if err != nil {
				return err
			}
			manifestID = run.InputManifestID
			if refined.Question != "" {
				return fmt.Errorf(
					"factory: the spec author asked a question about item %s, and the interview is the intent's and is over", c.itemID)
			}
			authored = refined
		}
		firstIsAuthored = false

		version, err := p.submitSpec(ctx, c, authored, manifestID)
		if err != nil {
			return err
		}
		verdict, reason, err := p.itemGate(ctx, c, gate.Spec, version, &c.specGate)
		if err != nil {
			return err
		}
		if verdict == gate.VerdictApprove {
			c.spec = authored.Spec
			return nil
		}
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageSpec); err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Rejected at %s: %s\nItem %s authors its spec again against what was found wrong\n",
			gate.Spec, reason, c.itemID)
		returned = agent.Returned{Reason: reason, Version: authored.Spec}
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
			Sentence:        one.Sentence,
			NoPatternReason: reason,
			RequirementID:   p.requirementFor(c, one.RequirementID),
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
	c.screenStates = nil
	for _, m := range written {
		c.screenStates = append(c.screenStates, m.States...)
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
			From: t.From, Event: t.Event, To: t.To,
		})
	}
	return draft
}

// planStage is the item's implementation plan version and the Implementation
// plan gate over it.
func (p *path) planStage(ctx context.Context, c *candidate) error {
	returned := agent.Returned{}
	for {
		inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
		if err != nil {
			return err
		}
		planned, run, err := p.dispatch.Planner(ctx, p.on(c, item.StageImplementationPlan, !returned.Empty()),
			[]inputmanifest.Material{
				{Class: "spec", Reference: c.specArtifactID, Bytes: int64(len(c.spec))},
				{Class: "criteria_in_force", Reference: c.svc.ID, Bytes: int64(len(inForce))},
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

		verdict, reason, err := p.itemGate(ctx, c, gate.ImplementationPlan, version.ID, &c.planGate)
		if err != nil {
			return err
		}
		if verdict == gate.VerdictApprove {
			c.plan = planned.Text
			return nil
		}
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageImplementationPlan); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected at %s: %s\nItem %s plans again against what was found wrong\n",
			gate.ImplementationPlan, reason, c.itemID)
		returned = agent.Returned{Reason: reason, Version: planned.Text}
	}
}

// tasksStage is the approved plan divided into the work an agent picks up, and
// the Tasks gate over it. A task is an internal step of the item: it has no
// build, no number and no environment, so nothing here writes a record of one
// beyond the version's own text.
func (p *path) tasksStage(ctx context.Context, c *candidate) error {
	returned := agent.Returned{}
	for {
		divided, run, err := p.dispatch.TaskAuthor(ctx, p.on(c, item.StageTasks, !returned.Empty()),
			[]inputmanifest.Material{
				{Class: "implementation_plan", Reference: c.planArtifactID, Bytes: int64(len(c.plan))},
				{Class: "spec", Reference: c.specArtifactID, Bytes: int64(len(c.spec))},
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

		verdict, reason, err := p.itemGate(ctx, c, gate.Tasks, version.ID, &c.tasksGate)
		if err != nil {
			return err
		}
		if verdict == gate.VerdictApprove {
			c.tasks = divided.Text
			return nil
		}
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageTasks); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected at %s: %s\nItem %s divides the plan again against what was found wrong\n",
			gate.Tasks, reason, c.itemID)
		returned = agent.Returned{Reason: reason, Version: divided.Text}
	}
}

// itemGate fires one of the four rows an item's own artifact is decided at and
// settles it, recording the firing on the candidate. The three rows above the
// build name no build; the Implementation row names the build the stage made
// and hands the gate that build's diff, which is where the score reads the
// change factors.
func (p *path) itemGate(ctx context.Context, c *candidate, row gate.Row, artifactID string, into *fired) (gate.Verdict, string, error) {
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
			return "", "", err
		}
		inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
		if err != nil {
			return "", "", err
		}
		firing.BuildID = c.buildID
		firing.Measurement = c.measurement
		firing.Exposure = reached
		firing.CriteriaInForce = len(inForce)
	}
	opened, err := p.gate.Fire(ctx, firing)
	if err != nil {
		return "", "", err
	}
	report(p.d.out, opened, nil)
	verdict, reason, closing, err := p.settle(ctx, opened, firing)
	if err != nil {
		return "", "", err
	}
	*into = recordFiring(opened, closing)
	return verdict, reason, nil
}

// on is the dispatch one of this item's stages is for: the item, the stage,
// the intent it was decomposed from, and the subjects a scope is matched
// against. reentering says the stage is being entered again after a reject,
// which is what counts the attempt.
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
		{Class: "intent", Reference: c.intentID},
		{Class: "service", Reference: c.svc.ID},
	}
}

// refining is what the spec author is given: the intent's statement, the
// service, the requirements this item answers, the criteria the service
// already promises, and what sent the item back here.
func (p *path) refining(c *candidate, returned agent.Returned) agent.Refining {
	return agent.Refining{
		Statement:    c.statement,
		Service:      c.svc.Name,
		Requirements: c.requirements,
		InForce:      rolePromptCriteria(c.promised),
		Returned:     returned,
	}
}

// requirementFor is the requirement id a criterion names, checked against the
// ids decomposition assigned this item: a role names an id it was given, and a
// role that named one this item does not answer has its criterion recorded
// against the item's first requirement instead — the criterion is still the
// item's, and this interface has one requirement per intent to give it.
func (p *path) requirementFor(c *candidate, named string) string {
	for _, id := range c.requirementIDs {
		if id == named {
			return named
		}
	}
	if len(c.requirementIDs) > 0 {
		return c.requirementIDs[0]
	}
	return ""
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

// heldHere reports whether a condition stopped the dispatch, which is a hold
// and not a failure: no page fires and no attempt counts.
func heldHere(err error) bool { return errors.Is(err, dispatch.ErrHeld) }

// describeHold is what the terminal says about a dispatch that held.
func describeHold(itemID string, stage item.Stage, err error) string {
	return fmt.Sprintf("Item %s waits at %s: %s", itemID, stage,
		strings.TrimPrefix(err.Error(), "dispatch: "))
}
