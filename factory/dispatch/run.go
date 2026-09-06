package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// The four dispatches, one per role. Each is the same sequence — the match,
// the transition, the manifest, the run, the record, the limit — around one
// role's own call, and each is written out rather than reached through a
// dispatch on a name, so a reader of one knows what the other three do.

// SpecAuthor puts an agent on the spec stage and returns what it authored.
func (d *Dispatch) SpecAuthor(ctx context.Context, on On, material []inputmanifest.Material,
	of agent.Refining) (agent.Refined, Run, error) {
	var authored agent.Refined
	run, err := d.put(ctx, RoleSpecAuthor, on, material,
		func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error) {
			refined, err := agent.SpecAuthor{Model: entry.Model, Prompt: prompt}.Refine(ctx, as, of)
			authored = refined
			return refined.Units, err
		})
	return authored, run, err
}

// Planner puts an agent on the implementation plan stage.
func (d *Dispatch) Planner(ctx context.Context, on On, material []inputmanifest.Material,
	of agent.Planning) (agent.Plan, Run, error) {
	var authored agent.Plan
	run, err := d.put(ctx, RoleImplementationPlanner, on, material,
		func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error) {
			plan, err := agent.Planner{Model: entry.Model, Prompt: prompt}.Plan(ctx, as, of)
			authored = plan
			return plan.Units, err
		})
	return authored, run, err
}

// TaskAuthor puts an agent on the tasks stage.
func (d *Dispatch) TaskAuthor(ctx context.Context, on On, material []inputmanifest.Material,
	of agent.Dividing) (agent.Tasks, Run, error) {
	var authored agent.Tasks
	run, err := d.put(ctx, RoleTaskAuthor, on, material,
		func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error) {
			tasks, err := agent.TaskAuthor{Model: entry.Model, Prompt: prompt}.Divide(ctx, as, of)
			authored = tasks
			return tasks.Units, err
		})
	return authored, run, err
}

// Implementer puts an agent on the implementation stage.
func (d *Dispatch) Implementer(ctx context.Context, on On, material []inputmanifest.Material,
	of agent.Implementing) (agent.Change, Run, error) {
	var authored agent.Change
	run, err := d.put(ctx, RoleImplementer, on, material,
		func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error) {
			change, err := agent.Implementer{Model: entry.Model, Prompt: prompt}.Implement(ctx, as, of)
			authored = change
			return change.Units, err
		})
	return authored, run, err
}

// put is the whole of one dispatch, and the four methods above differ only in
// the call they pass:
//
//  1. the intent's state, read before an agent is put on a stage;
//  2. the match — the item's stage against the role, its service and area
//     against the scope, and the role prompt version in force;
//  3. the transition onto the item, which counts the entry;
//  4. the input manifest, written before the agent starts;
//  5. the run, under the principal this dispatch is;
//  6. the agent run record, written after each call;
//  7. the item's own count for the stage against the limit in force, and the
//     escalation over it.
//
// Anything that stops it before (5) is a hold: a row in the log, no page, no
// attempt counted, and [ErrHeld] returned with the run naming the condition.
func (d *Dispatch) put(ctx context.Context, role Role, on On, material []inputmanifest.Material,
	call func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error)) (Run, error) {
	run := Run{ID: record.NewID("dsp"), Role: role}
	stage, err := role.Stage()
	if err != nil {
		return run, err
	}
	if on.ItemID != "" && on.Stage != stage {
		return run, fmt.Errorf("dispatch: %s names the stage %s and this dispatch is for %s",
			role, stage, on.Stage)
	}
	if on.ItemID == "" && on.IntentID == "" {
		return run, errors.New("dispatch: a dispatch is for an item or for an intent, and this one names neither")
	}

	// 1. The intent's state. It is read for a run on an item: a role put on an
	// intent is the interview, which runs while the intent is unrefined and is
	// what refines it.
	if on.ItemID != "" {
		stopped, err := d.intentStops(ctx, on)
		if err != nil {
			return run, err
		}
		if stopped != "" {
			return d.hold(ctx, run, on, HoldTheIntentStops, stopped)
		}
	}

	// 2. The match. Neither of these two is a judgment: an entry covers the
	// role and the scope or it does not, and a version is in force or it is
	// not.
	entry, found, err := d.c.Fleet.EntryFor(ctx, role, on)
	if err != nil {
		return run, err
	}
	if !found {
		return d.hold(ctx, run, on, HoldNoEntryCoversTheStage, "")
	}
	operations, err := role.Narrow(entry.Operations)
	if err != nil {
		return run, err
	}
	entry.Operations = operations
	run.Entry = entry

	prompt, inForce, err := d.c.Prompts.InForce(ctx, role)
	if err != nil {
		return run, err
	}
	if !inForce {
		return d.hold(ctx, run, on, HoldNoRolePromptInForce, "")
	}
	run.RolePromptVersionID = prompt.ID
	told := prompt.Content

	// A dispatch that got this far is a match nothing is holding, so any hold
	// this component left open is re-tested and the ones the match lifts are
	// closed.
	if _, err := d.Rematch(ctx); err != nil {
		return run, err
	}

	// 3. The transition, which counts the entry into the stage.
	if on.ItemID != "" {
		if err := d.enter(ctx, on); err != nil {
			return run, err
		}
	}

	// 4. The manifest, before the agent starts. Context assembly is the
	// component that would select and write it; it is not built, so this
	// component writes what the stage handed over and excludes nothing.
	manifest, err := d.c.Manifests.Write(ctx, Actor, inputmanifest.New{
		ItemID: on.ItemID, Stage: string(on.Stage), IntentID: on.IntentID, Materials: material,
	})
	if err != nil {
		return run, err
	}
	run.InputManifestID = manifest.ID

	limit, err := d.limitFor(ctx, stage)
	if err != nil {
		return run, err
	}
	return d.attempts(ctx, run, on, told, limit, call)
}

// attempts is (5) to (7): the calls, one agent run record each, and the limit
// compared against the item's own stored count after each refused reply.
//
// What is retried is a reply the protocol refused and an answer the client
// could not read — both are the model failing to say the thing, which another
// sample may say correctly. Nothing else is: a rate-limited or unauthorised
// account is not an attempt at the work, and what the design does with an
// account that has run out is a hold, so those return on the first failure
// rather than spending the limit on a refusal that will not change.
func (d *Dispatch) attempts(ctx context.Context, run Run, on On, told string, limit int,
	call func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error)) (Run, error) {
	as := principal.OfAgent(run.Entry.ModelVersion, run.ID, run.Entry.Scope.String())
	if err := as.Validate(); err != nil {
		return run, err
	}
	var last error
	for made := 0; ; made++ {
		counted, err := d.counted(ctx, on, made)
		if err != nil {
			return run, err
		}
		run.Attempts = counted
		// The comparison is here and not only after a refused reply, because a
		// stage is entered again after a reject as well as after one: an item
		// sent back by a gate for the last time its limit allows escalates
		// before an agent is put on it again.
		if counted > limit {
			run.Escalated = true
			if err := d.escalate(ctx, on); err != nil {
				return run, err
			}
			if last != nil {
				return run, fmt.Errorf("%w: %s used all %d the limit allows at %s: %w",
					ErrOutOfAttempts, run.Role, limit, on.Stage, last)
			}
			return run, fmt.Errorf("%w: %s has entered %s %d times and the limit is %d",
				ErrOutOfAttempts, on.ItemID, on.Stage, counted, limit)
		}

		startedAt := record.Now()
		units, callErr := call(run.Entry, told, as)
		recorded, err := d.recordRun(ctx, run, on, units, startedAt, record.Now(), outcomeOf(callErr))
		if err != nil {
			return run, err
		}
		run.AgentRunIDs = append(run.AgentRunIDs, recorded)
		if callErr == nil {
			return run, nil
		}
		if !errors.Is(callErr, agent.ErrReply) && !errors.Is(callErr, agent.ErrAnswer) {
			return run, callErr
		}
		// The item is entered again whatever the count stands at, and the
		// comparison at the top of the loop is what stops it: an item that
		// exceeded the limit stops being retried, and exceeding it is a count
		// above the limit rather than one at it. That is the same arithmetic
		// the enforcement the composition supplies makes, so the two cannot
		// disagree about which entry was the last one.
		if err := d.again(ctx, on); err != nil {
			return run, err
		}
		run.Attempts = counted + 1
		last = callErr
	}
}

// counted is what the limit is compared against: the item's own count for the
// stage, which rises on the record as the item is entered again, or — for a
// run on an intent, which has no per-stage row — the count the caller carries
// plus the calls this run has already made.
func (d *Dispatch) counted(ctx context.Context, on On, made int) (int, error) {
	if on.ItemID == "" {
		return on.CountedSoFar + made, nil
	}
	return d.countAt(ctx, on.ItemID, on.Stage)
}
