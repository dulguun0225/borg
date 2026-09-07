package dispatch

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// The five dispatches, one per role a run is made in. Each is the same
// sequence — the match, the transition, the manifest, the run, the record, the
// limit — around one role's own call, and each is written out rather than
// reached through a dispatch on a name, so a reader of one knows what the
// others do. The sixth role, the decomposer, has no method here: doc.go says
// what would call it.

// Interviewer puts an agent on an intent and returns the reading or the
// question it replied with. It names no stage and no item: the interview runs
// while the intent is unrefined and is what refines it.
func (d *Dispatch) Interviewer(ctx context.Context, on On, material []inputmanifest.Material,
	of agent.Interviewing) (agent.Reading, Run, error) {
	var read agent.Reading
	run, err := d.put(ctx, RoleInterviewer, on, material,
		func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error) {
			reading, err := agent.Interviewer{Model: entry.Model, Prompt: prompt, Effort: entry.Effort}.
				Interview(ctx, as, of)
			read = reading
			return reading.Units, err
		})
	return read, run, err
}

// SpecAuthor puts an agent on the spec stage and returns what it authored.
func (d *Dispatch) SpecAuthor(ctx context.Context, on On, material []inputmanifest.Material,
	of agent.Refining) (agent.Refined, Run, error) {
	var authored agent.Refined
	run, err := d.put(ctx, RoleSpecAuthor, on, material,
		func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error) {
			refined, err := agent.SpecAuthor{Model: entry.Model, Prompt: prompt, Effort: entry.Effort}.Refine(ctx, as, of)
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
			plan, err := agent.Planner{Model: entry.Model, Prompt: prompt, Effort: entry.Effort}.Plan(ctx, as, of)
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
			tasks, err := agent.TaskAuthor{Model: entry.Model, Prompt: prompt, Effort: entry.Effort}.Divide(ctx, as, of)
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
			change, err := agent.Implementer{Model: entry.Model, Prompt: prompt, Effort: entry.Effort}.Implement(ctx, as, of)
			authored = change
			return change.Units, err
		})
	return authored, run, err
}

// put is the whole of one dispatch, and the five methods above differ only in
// the call they pass:
//
//  1. the intent's state, read before an agent is put on a stage — and not
//     before a role put on the intent itself, the interview being what refines
//     an unrefined intent;
//  2. the match — the item's stage against the role, its service and area
//     against the scope, and the role prompt version in force;
//  3. the three conditions the entry makes readable, in the design's order: a
//     credential already known unreachable, one at its spend ceiling, and a
//     document-kind constraint in force requiring seam 5 enforced;
//  4. the transition onto the item, which counts the entry;
//  5. the input manifest, written before the agent starts, with the classes of
//     material the entry does not name withheld and recorded as excluded;
//  6. the run, under the principal this dispatch is;
//  7. the agent run record, written after each call;
//  8. the item's own count for the stage against the limit in force, and the
//     escalation over it.
//
// Anything that stops it before (6) is a hold: a row in the log, no page, no
// attempt counted, and [ErrHeld] returned with the run naming the condition.
func (d *Dispatch) put(ctx context.Context, role Role, on On, material []inputmanifest.Material,
	call func(entry Entry, prompt string, as principal.Principal) (map[string]int64, error)) (Run, error) {
	run := Run{ID: record.NewID("dsp"), Role: role}
	var stage item.Stage
	if !role.OnAnIntent() {
		named, err := role.Stage()
		if err != nil {
			return run, err
		}
		stage = named
	} else if on.ItemID != "" {
		return run, fmt.Errorf("%w: %s was put on item %s", ErrRoleNamesNoStage, role, on.ItemID)
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
			return d.hold(ctx, run, on, Hold{Condition: HoldTheIntentStops, State: stopped})
		}
	}

	// 2. The match. Neither of these two is a judgment: an entry covers the
	// role and the scope or it does not, and a version is in force or it is
	// not.
	entry, found, err := d.entryFor(ctx, role, on)
	if err != nil {
		return run, err
	}
	if !found {
		return d.hold(ctx, run, on, Hold{Condition: HoldNoEntryCoversTheStage})
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
		return d.hold(ctx, run, on, Hold{Condition: HoldNoRolePromptInForce})
	}
	run.RolePromptVersionID = prompt.ID
	told := prompt.Content

	// 3. The three the entry makes readable, in the design's order. The
	// credential rows are read once here and handed on: a run that succeeds
	// closes the row its own earlier failure left, and reading them again after
	// the call would append a read event per call for an answer this read
	// already has.
	credentials, err := d.credentialWaits(ctx)
	if err != nil {
		return run, err
	}
	held, err := d.credentialStops(ctx, credentials, on, entry)
	if err != nil {
		return run, err
	}
	if held.Condition != "" {
		return d.hold(ctx, run, on, held)
	}
	requiring, err := d.constraintRequiringSeam5(ctx, on)
	if err != nil {
		return run, err
	}
	if requiring != "" {
		return d.hold(ctx, run, on, Hold{
			Condition: HoldConstraintRequiresSeam5, ConstraintID: requiring,
		})
	}

	// A dispatch that got this far is a match nothing is holding, so any hold
	// this component left open is re-tested and the ones the match lifts are
	// closed.
	if _, err := d.Rematch(ctx); err != nil {
		return run, err
	}

	// 4. The transition, which counts the entry into the stage.
	if on.ItemID != "" {
		if err := d.enter(ctx, on); err != nil {
			return run, err
		}
	}

	// 5. The manifest, before the agent starts, and the classes of material the
	// entry does not name withheld before it. Context assembly is the component
	// that would select what fits the read-at-once bound and write the manifest;
	// it is not built, so this component writes what the stage handed over,
	// withholds the classes the entry does not name, and excludes nothing else.
	handed, withheld, err := withhold(entry, material)
	if err != nil {
		return run, err
	}
	readsAtOnce := entry.ReadsAtOnce
	manifest, err := d.c.Manifests.Write(ctx, Actor, inputmanifest.New{
		ItemID: on.ItemID, Stage: string(on.Stage), IntentID: on.IntentID,
		Materials: handed, ReadAtOnceBound: &readsAtOnce, Excluded: withheld,
	})
	if err != nil {
		return run, err
	}
	run.InputManifestID = manifest.ID

	paid, err := d.readPaidFor(ctx, entry.CredentialName)
	if err != nil {
		return run, err
	}
	limit, err := d.limitFor(ctx, role, stage)
	if err != nil {
		return run, err
	}
	return d.attempts(ctx, run, on, told, sourcesOf(handed), paid, credentials, limit, call)
}

// credentialStops is the two conditions the credential an entry names stops a
// dispatch on, in the design's order: a credential already known unreachable,
// and one at its spend ceiling. The cause returned is empty where neither
// holds.
//
// The ceiling's row routes to the owner and not to whoever lent the credential,
// because raising, clearing or lengthening the period is the owner's and a row
// reaching the lender would reach somebody who cannot act on it. The credential
// row it stands beside is opened here so that an owner has one row to clear per
// credential and period, however many items are waiting on it.
func (d *Dispatch) credentialStops(ctx context.Context, read credentialRows, on On, entry Entry) (Hold, error) {
	if read.declines(on, entry.CredentialName) {
		return Hold{Condition: HoldCredentialUnreachable, CredentialName: entry.CredentialName}, nil
	}
	reading, err := d.atCeiling(ctx, entry.CredentialName, read)
	if err != nil {
		return Hold{}, err
	}
	if !reading.reached {
		return Hold{}, nil
	}
	if err := d.atCeilingRow(ctx, read, entry.CredentialName, reading.periodStart); err != nil {
		return Hold{}, err
	}
	return Hold{
		Condition: HoldCredentialAtCeiling, CredentialName: entry.CredentialName,
		WantsARate: reading.wantsARate, RoutedTo: RoutedToTheOwner,
	}, nil
}

// withhold is the material the entry may be handed and the material it may not,
// which context assembly reads the classes for at every dispatch: a class the
// entry does not name is withheld before any selection rule selects anything,
// and the manifest records each withheld source as excluded with the entry as
// the reason. An entry naming no class is handed nothing but the role prompt.
//
// A class outside [fleetentry.MaterialClasses] is [ErrMaterialClassUnknown] and
// not silently withheld: the classes an entry names and the classes a stage
// hands over are one vocabulary, so a class no entry could ever name is a
// caller's mistake and not an owner's narrowing.
//
// What it withholds is what the manifest and the run record name. It is not
// what the role sends the provider: the payload each of the five methods passes
// is the caller's own, assembled from the same sources, and this strips nothing
// out of it — doc.go says so, that being what context assembly would own.
func withhold(entry Entry, material []inputmanifest.Material) ([]inputmanifest.Material,
	[]inputmanifest.Exclusion, error) {
	var handed []inputmanifest.Material
	var withheld []inputmanifest.Exclusion
	for _, one := range material {
		if !slices.Contains(fleetentry.MaterialClasses, one.Class) {
			return nil, nil, fmt.Errorf("%w: %q on %s", ErrMaterialClassUnknown, one.Class, one.Reference)
		}
		if slices.Contains(entry.MaterialClasses, one.Class) {
			handed = append(handed, one)
			continue
		}
		withheld = append(withheld, inputmanifest.Exclusion{
			What:   one.Reference,
			Reason: "withheld: the fleet entry " + entry.ID + " does not name class " + one.Class,
		})
	}
	return handed, withheld, nil
}

// sourcesOf is the sources handed over, as the agent run record names them:
// the reference of each material the manifest was written from, in the order
// the stage handed them over. It is called with what the entry's classes
// admitted and never with what the stage offered, so a class the entry does not
// name is on the manifest as excluded and on no run record as a source: the
// manifest names what was withheld and the run record names what was sent, and
// both name a source by reference.
func sourcesOf(material []inputmanifest.Material) []string {
	sources := make([]string, 0, len(material))
	for _, one := range material {
		sources = append(sources, one.Reference)
	}
	return sources
}

// attempts is (6) to (8): the calls, one agent run record each, and the limit
// compared against the item's own stored count after each refused reply.
//
// What is retried is a reply the protocol refused and an answer the client
// could not read — both are the model failing to say the thing, which another
// sample may say correctly. Nothing else is: a rate-limited or unauthorised
// account is not an attempt at the work, and what the design does with an
// account that has run out is a hold, so those return on the first failure
// rather than spending the limit on a refusal that will not change. This is
// where that hold is written: a failure [unreachable] recognises opens the
// credential's own row, with the agent that could not reach as the caller and
// the actor, and a call that succeeds closes it and re-matches, which lifts
// every item this component declined onto that credential.
func (d *Dispatch) attempts(ctx context.Context, run Run, on On, told string, sources []string,
	paid paidFor, credentials credentialRows, limit int,
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
		recorded, err := d.recordRun(ctx, run, on, sources, units, paid, startedAt, record.Now(), outcomeOf(callErr))
		if err != nil {
			return run, err
		}
		run.AgentRunIDs = append(run.AgentRunIDs, recorded)
		if callErr == nil {
			// The credential was reached, so a row saying it could not be is
			// gone: the hold ends where the work resumes rather than waiting
			// for the agent that stopped to close it.
			closed, err := d.reached(ctx, credentials, run.Entry.CredentialName)
			if err != nil {
				return run, err
			}
			if closed {
				if _, err := d.Rematch(ctx); err != nil {
					return run, err
				}
			}
			return run, nil
		}
		if unreachable(callErr) {
			row, err := d.couldNotReach(ctx, as, on, run.Entry.CredentialName)
			if err != nil {
				return run, err
			}
			run.Held, run.HoldRow = HoldCredentialUnreachable, row
			return run, fmt.Errorf("%w: %s: %w", ErrHeld, HoldCredentialUnreachable, callErr)
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
		// The ceiling is compared at each report the agent makes and not only
		// at a stage's start, so overshoot is bounded to one report's worth of
		// units: a stage whose last call put the sum past it is held here,
		// mid-stage, by whoever could not proceed, rather than retrying on an
		// account the owner bounded.
		read, err := d.credentialWaits(ctx)
		if err != nil {
			return run, err
		}
		held, err := d.credentialStops(ctx, read, on, run.Entry)
		if err != nil {
			return run, err
		}
		if held.Condition != "" {
			return d.hold(ctx, run, on, held)
		}
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
