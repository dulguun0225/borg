// TestAnIntentThatStopsWorkIsAHoldAndNotARun and the rest of this file are the
// conditions that stop a dispatch and the rows they leave, split from db_test.go
// by subject when that file passed 500 lines. The re-match that lifts one is
// rematch_test.go's and the role and scope vocabulary they are matched on is
// role_test.go's; all share db_test.go's fixtures and its package.
package dispatch_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// TestAnIntentThatStopsWorkIsAHoldAndNotARun: dispatch reads the intent's
// state before it puts an agent on a stage, and four states stop it. The hold
// is a row of the log, no attempt is counted, and no agent worked.
func TestAnIntentThatStopsWorkIsAHoldAndNotARun(t *testing.T) {
	for _, state := range []intent.State{
		intent.StateUnrefined, intent.StateReDecomposing, intent.StateEscalated, intent.StateDropped,
	} {
		t.Run(string(state), func(t *testing.T) {
			c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
			it := c.oneItem(t, state)

			_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
			if !errors.Is(err, dispatch.ErrHeld) {
				t.Fatalf("SpecAuthor = %v, want ErrHeld", err)
			}
			if run.Held != dispatch.HoldTheIntentStops || run.HoldRow == "" {
				t.Errorf("the run held on %q as row %q, want the intent's state as a wait row", run.Held, run.HoldRow)
			}
			if c.model.calls != 0 {
				t.Error("an agent was put on the stage while the intent stopped work on it")
			}
			held, _, err := c.dispatch.Open(c.ctx)
			if err != nil {
				t.Fatalf("Open: %v", err)
			}
			if len(held) != 1 || held[0].ItemID != it.ID || held[0].Role != string(dispatch.RoleSpecAuthor) {
				t.Errorf("the open holds are %+v, want one naming this item and the role", held)
			}
		})
	}
}

// TestAStageNoEntryCoversAndARoleWithNoPromptAreHolds: two of the six
// conditions, each a row naming which condition held and the values the match
// was made on.
func TestAStageNoEntryCoversAndARoleWithNoPromptAreHolds(t *testing.T) {
	t.Run("no entry covers the stage", func(t *testing.T) {
		c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
		c.withdrawEveryEntry(t)
		it := c.oneItem(t, intent.StateRefined)

		_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
		if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldNoEntryCoversTheStage {
			t.Fatalf("SpecAuthor = %v holding %q, want the no-entry hold", err, run.Held)
		}
		held, _, err := c.dispatch.Open(c.ctx)
		if err != nil {
			t.Fatalf("Open: %v", err)
		}
		if len(held) != 1 || held[0].ServiceID != oneService {
			t.Errorf("the hold is %+v, want the values the match was made on", held)
		}
		// The row names the chain the scope was matched against and not one
		// area: a scope drawn anywhere on it would have covered the item.
		if len(held) == 1 && (len(held[0].AreaChain) != 2 ||
			held[0].AreaChain[0] != c.oneArea || held[0].AreaChain[1] != c.theAreaAbove) {
			t.Errorf("the hold names the area chain %v, want the item's own area and the one above it",
				held[0].AreaChain)
		}

		// A second dispatch of the same item and stage against the same
		// condition writes no second row: a hold is one row per item and stage.
		if _, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"}); !errors.Is(err, dispatch.ErrHeld) {
			t.Fatalf("the retry = %v, want ErrHeld", err)
		} else if run.HoldRow == "" {
			t.Error("the retry named no wait row, and the hold it met is one that stands")
		}
		again, rows, err := c.dispatch.Open(c.ctx)
		if err != nil {
			t.Fatalf("Open after the retry: %v", err)
		}
		if len(again) != 1 {
			t.Errorf("%d holds are open after the retry, want the one row per item and stage", len(rows))
		}
	})

	t.Run("no role prompt version in force", func(t *testing.T) {
		c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
		c.prompts.inForce = false
		it := c.oneItem(t, intent.StateRefined)

		_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
		if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldNoRolePromptInForce {
			t.Fatalf("SpecAuthor = %v holding %q, want the no-prompt hold", err, run.Held)
		}
		if c.model.calls != 0 {
			t.Error("an agent was run with no role prompt version in force")
		}
	})
}

// TestTheFourRolesMatchTheFourAuthoringStages: dispatch is a match of the
// item's stage against the role, so every stage an artifact is authored at has
// exactly one role and no other stage has any.
func TestTheFourRolesMatchTheFourAuthoringStages(t *testing.T) {
	for _, stage := range item.AuthoringStages {
		role, found := dispatch.RoleAt(stage)
		if !found {
			t.Fatalf("no role is put on %s", stage)
		}
		at, err := role.Stage()
		if err != nil || at != stage {
			t.Errorf("%s names stage %s (%v), want %s", role, at, err, stage)
		}
	}
	for _, stage := range []item.Stage{item.StageQueued, item.StageMerged, item.StageDropped} {
		if role, found := dispatch.RoleAt(stage); found {
			t.Errorf("%s is put on %s, and nothing authors there", role, stage)
		}
	}
}

// TestAStageEnteredAgainAfterARejectCountsAndEscalatesAtTheLimit: a reject
// sends the item back to the stage to be entered again, and the entry is what
// counts — so an item sent back for the last time its limit allows escalates
// before an agent is put on it again.
func TestAStageEnteredAgainAfterARejectCountsAndEscalatesAtTheLimit(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 2)
	it := c.oneItem(t, intent.StateRefined)
	returned := c.on(it)
	returned.Reentering = true

	// The first re-entry is the second attempt, which the limit allows.
	if _, run, err := c.dispatch.SpecAuthor(c.ctx, returned, nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("SpecAuthor after a reject: %v", err)
	} else if run.Attempts != 2 {
		t.Errorf("the run stood at %d attempts, want the second entry", run.Attempts)
	}

	// The second re-entry is the third, which it does not.
	_, run, err := c.dispatch.SpecAuthor(c.ctx, returned, nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Fatalf("SpecAuthor = %v, want ErrOutOfAttempts", err)
	}
	if !run.Escalated || len(c.escalation.items) != 1 {
		t.Errorf("the run escalated %v and the escalation saw %v, want the item escalated once", run.Escalated, c.escalation.items)
	}
	// The wait an escalation leaves is dispatch's own call, made after the item
	// was written escalated and its pending rows abandoned.
	if len(c.told.items) != 1 || c.told.items[0] != it.ID ||
		c.told.reasons[0] != dispatch.EscalatedByTheAttemptLimit {
		t.Errorf("the notifier was told %v for %v, want this item at the attempt limit", c.told.reasons, c.told.items)
	}
	if c.model.calls != 1 {
		t.Errorf("%d calls, want no agent put on the stage after the limit was spent", c.model.calls)
	}
}

// TestARolePutOnAnIntentRunsWhileTheIntentIsUnrefined: the interview is one of
// the two roles put on an intent rather than an item. It names no stage, it is
// matched on the intent's project alone, the state that stops every dispatch on
// an item does not stop it — the interview is what refines an unrefined intent —
// and the run record it writes names the intent and no stage.
func TestARolePutOnAnIntentRunsWhileTheIntentIsUnrefined(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{
		Text:  "READING:\nREQUIREMENT: When the charge fails, the system shall retry it once.",
		Units: map[string]int64{agent.UnitsOutput: 4},
	}}, nil, 3)
	in, err := c.intake.TakeIn(c.ctx, owner,
		intent.Arrival{Source: intent.SourceOwner, Statement: "checkout should retry", ProjectID: oneProject})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}

	on := dispatch.On{IntentID: in.ID, ProjectID: oneProject, CountedSoFar: 1}
	read, run, err := c.dispatch.Interviewer(c.ctx, on,
		[]inputmanifest.Material{{Class: fleetentry.ClassIntentStatement, Reference: in.ID, Bytes: 21}},
		agent.Interviewing{Statement: in.Statement})
	if err != nil {
		t.Fatalf("Interviewer on an unrefined intent: %v", err)
	}
	if len(read.Requirements) != 1 {
		t.Fatalf("the interviewer stated %v, want the one requirement the reply carries", read.Requirements)
	}
	if run.Role != dispatch.RoleInterviewer || run.Held != "" {
		t.Errorf("the run is %+v, want the interviewer with nothing holding it", run)
	}

	runs, err := agentrun.ForIntent(c.ctx, c.pool, in.ID)
	if err != nil {
		t.Fatalf("ForIntent: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("%d agent run records, want one per call", len(runs))
	}
	if runs[0].Role != string(dispatch.RoleInterviewer) || runs[0].ItemID != "" || runs[0].Stage != "" {
		t.Errorf("the run record is %+v, want the interviewer on the intent and no item or stage", runs[0])
	}
	if len(runs[0].Sources) != 1 || runs[0].Sources[0] != in.ID {
		t.Errorf("the run names sources %v, want the reference of the material handed over", runs[0].Sources)
	}

	// The two roles put on an intent name no stage, and neither is put on an
	// item.
	for _, role := range []dispatch.Role{dispatch.RoleInterviewer, dispatch.RoleDecomposer} {
		if _, err := role.Stage(); !errors.Is(err, dispatch.ErrRoleNamesNoStage) {
			t.Errorf("%s.Stage() = %v, want ErrRoleNamesNoStage", role, err)
		}
		if !role.OnAnIntent() {
			t.Errorf("%s is not read as a role put on an intent", role)
		}
	}
	it := c.oneItem(t, intent.StateRefined)
	onAnItem := dispatch.On{ItemID: it.ID, IntentID: it.IntentID, ProjectID: oneProject}
	if _, _, err := c.dispatch.Interviewer(c.ctx, onAnItem, nil, agent.Interviewing{Statement: "s"}); !errors.Is(err, dispatch.ErrRoleNamesNoStage) {
		t.Errorf("the interviewer put on an item = %v, want ErrRoleNamesNoStage", err)
	}
}

// TestAConstraintRequiringSeam5HoldsUntilTheSettingIsOn is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md's
// document-kind constraint that "may also require seam 5 enforced, read at
// dispatch": the row names the constraint, and the hold lifts when the field
// turns on.
func TestAConstraintRequiringSeam5HoldsUntilTheSettingIsOn(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	supplied := c.aConstraintRequiringSeam5(t)
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrHeld) || run.Held != dispatch.HoldConstraintRequiresSeam5 {
		t.Fatalf("SpecAuthor under a constraint requiring seam 5 = %v holding %q, want the seam 5 hold",
			err, run.Held)
	}
	held, found := c.holdOn(t, it.ID, dispatch.HoldConstraintRequiresSeam5)
	if !found || held.ConstraintID != supplied {
		t.Fatalf("the hold is %+v, %v; want the constraint named", held, found)
	}
	if c.model.calls != 0 {
		t.Error("an agent ran on an item a constraint requiring seam 5 holds")
	}

	c.enforceSeam5(t)
	if _, err := c.dispatch.Rematch(c.ctx); err != nil {
		t.Fatalf("Rematch: %v", err)
	}
	if _, found := c.holdOn(t, it.ID, dispatch.HoldConstraintRequiresSeam5); found {
		t.Error("the hold still stands after seam 5 was turned on")
	}
	if _, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"}); err != nil {
		t.Fatalf("SpecAuthor once seam 5 is enforced: %v", err)
	}
}

// aConstraintRequiringSeam5 supplies one document-kind constraint over the
// whole factory requiring seam 5 enforced, and answers with its id.
func (c composed) aConstraintRequiringSeam5(t *testing.T) string {
	t.Helper()
	supplied, err := constraint.NewWriter(c.pool, c.token).Arrive(c.ctx, owner, constraint.New{
		Kind: constraint.KindDocument, Reach: constraint.ReachFactory,
		Statement: "nothing leaves the region", RequiresSeam5Enforced: true,
	})
	if err != nil {
		t.Fatalf("supplying the constraint: %v", err)
	}
	return supplied.ID
}

// enforceSeam5 turns the factory-wide field on, which is the one thing that
// clears the constraint's hold. It is turned on once and never off.
func (c composed) enforceSeam5(t *testing.T) {
	t.Helper()
	settings, err := factorysettings.NewWriter(c.pool, c.token).Ensure(c.ctx, owner)
	if err != nil {
		t.Fatalf("Ensure the settings record: %v", err)
	}
	tx, err := c.pool.Begin(c.ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(c.ctx) }()
	if err := factorysettings.SetSeam5Enforced(c.ctx, tx, settings.ID, true); err != nil {
		t.Fatalf("SetSeam5Enforced: %v", err)
	}
	if err := tx.Commit(c.ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// TestTheGrouperIsMatchedToAnEntryOnTheProjectItReads is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md's
// "it runs under a fleet entry like any other agent ... in a role put on
// neither an intent nor an item: its scope is a project". The match is read
// through a re-match: a hold naming the grouper and a project stands while no
// entry covers it and lifts when one does. Nothing here puts an agent in this
// role — doc.go says what would — so the hold is the way in to the match.
func TestTheGrouperIsMatchedToAnEntryOnTheProjectItReads(t *testing.T) {
	// One version per role in Roles is what the first start enters, so a role
	// missing from it is a role whose words are never entered.
	if !slices.Contains(dispatch.Roles, dispatch.RoleGrouper) {
		t.Fatal("the grouper is not one of dispatch.Roles, and one role prompt version per role there is what a first start enters")
	}
	if _, err := dispatch.RoleGrouper.Stage(); !errors.Is(err, dispatch.ErrRoleNamesNoStage) {
		t.Errorf("RoleGrouper.Stage() = %v, want ErrRoleNamesNoStage", err)
	}
	if !dispatch.RoleGrouper.OnAProject() || dispatch.RoleGrouper.OnAnIntent() {
		t.Error("the grouper is not read as the role put on a project")
	}
	if ops, err := dispatch.RoleGrouper.Operations(); err != nil || len(ops) != 1 ||
		ops[0] != dispatch.OperationReadTheReports {
		t.Errorf("the grouper carries operations %v (%v), want reading the reports and nothing else", ops, err)
	}

	const anotherProject = "pr_11111111111111111111111111111111"
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.withdrawEveryEntry(t)
	c.aGrouperEntryOn(t, anotherProject)
	standing := c.aGrouperHoldOn(t, oneProject)

	lifted, err := c.dispatch.Rematch(c.ctx)
	if err != nil {
		t.Fatalf("Rematch: %v", err)
	}
	if len(lifted) != 0 {
		t.Fatalf("Rematch lifted %v, want the hold to stand: the only entry is drawn on another project", lifted)
	}

	c.aGrouperEntryOn(t, oneProject)
	lifted, err = c.dispatch.Rematch(c.ctx)
	if err != nil {
		t.Fatalf("Rematch after the entry on this project: %v", err)
	}
	if len(lifted) != 1 || lifted[0] != standing {
		t.Fatalf("Rematch lifted %v, want the hold %s the entry on this project covers", lifted, standing)
	}
}

// aGrouperEntryOn is an owner's fleet entry for the grouper drawn on one
// project, which is the line a scope on this role is drawn on: it names no
// service and no area, an entry naming either covering no run put on a project.
func (c composed) aGrouperEntryOn(t *testing.T, projectID string) {
	t.Helper()
	if _, err := c.entries.Write(c.ctx, owner, fleetentry.New{
		ModelVersion:                    modelName,
		Role:                            string(dispatch.RoleGrouper),
		Scope:                           fleetentry.Scope{ProjectID: projectID},
		CredentialName:                  theCredential,
		ProcessingLocation:              "vendor/test-region",
		MaterialClasses:                 fleetentry.MaterialClasses,
		ReadsAtOnce:                     200000,
		DispatchesBetweenEvaluationRuns: 50,
	}); err != nil {
		t.Fatalf("writing the grouper's entry on %s: %v", projectID, err)
	}
}

// aGrouperHoldOn opens one hold row of this component's own shape, naming the
// grouper and a project and no item, and answers with the row it stands as.
func (c composed) aGrouperHoldOn(t *testing.T, projectID string) string {
	t.Helper()
	payload, err := json.Marshal(dispatch.Hold{
		Kind:      dispatch.HoldKind,
		Condition: dispatch.HoldNoEntryCoversTheStage,
		Role:      string(dispatch.RoleGrouper),
		ProjectID: projectID,
	})
	if err != nil {
		t.Fatalf("marshalling the hold on %s: %v", projectID, err)
	}
	row, err := decisionlog.NewWriter(c.pool, c.token).AppendWaitOpen(c.ctx, decisionlog.Entry{
		Actor: dispatch.Actor, Payload: string(payload), FormatVersion: dispatch.HoldFormatVersion,
	})
	if err != nil {
		t.Fatalf("opening the hold on %s: %v", projectID, err)
	}
	return row.ID
}
