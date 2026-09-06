package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// Actor is who this component writes as: dispatch is the caller and the actor
// on the transitions it writes, on the holds it opens and closes, on the input
// manifests it writes until context assembly exists, and on the agent run
// records it writes after a run.
var Actor = record.Actor{Kind: record.KindComponent, Key: "dispatch"}

// Limits is the one value this component reads out of gate policy: the
// attempt limit in force at a stage, which an owner authored or the score
// supplied. It is an interface for the reason the gate component's own reads
// are: the reader is composed with the score version in force, and a test of
// the match is not a test of that composition. [policy.Reader] is what a run
// supplies.
type Limits interface {
	AttemptLimit(ctx context.Context, s policy.Subjects) (policy.Effective, error)
}

// Escalation is what performs an escalation where the stage has spent its
// limit: the escalated value written onto the item, every pending row of the
// item abandoned, and the wait to the notifier, in that order.
//
// It is an interface the composition supplies rather than a call made here,
// because the abandonment of a pending row is the gate component's and
// ../../end-goal/components.md's row for this component names no gate. What
// the composition wires it to is [gate.Gate.EnforceAttemptLimit], which does
// the three in the order that file fixes.
type Escalation interface {
	Escalate(ctx context.Context, actor record.Actor, itemID string, stage item.Stage) error
}

// Composition is what a dispatch is built from. Every field is required.
type Composition struct {
	Pool  *pgxpool.Pool
	Token lease.Token
	// Fleet is the entries an owner composed and Prompts the role prompt
	// version in force per role: the two records a match is made against.
	Fleet   Fleet
	Prompts Prompts
	// Items is the writer of the item's stage and the count beside it. This
	// component is the item's writer after decomposition, so the transition
	// every stage reports goes through here.
	Items *item.Dispatch
	// Policy is where the attempt limit in force is read, Log is where a hold
	// is written, Manifests is what context assembly would write and this
	// component writes until it exists, and Runs is the agent run record.
	Policy Limits
	Log    *decisionlog.Writer
	// Reader is how the open holds are read back: a hold is a row of the log
	// and a re-match is a read of it.
	Reader     *decisionlog.Reader
	Manifests  *inputmanifest.Writer
	Runs       *agentrun.Writer
	Escalation Escalation
}

// Dispatch is the component: the match of an item's stage against a role and
// of its service and area against a scope, and what runs an agent.
//
// It holds nothing between calls. ../../end-goal/one-process.md gives every
// component's restart, and this one's is nothing: the item's stage and its
// count per stage are records, the holds are rows of the log, and a start
// reads both rather than resuming anything.
type Dispatch struct {
	c Composition
}

// New returns the component. It refuses a composition missing anything,
// because a dispatch that could not write a transition or a run record would
// put an agent on an item and leave no record that it did.
func New(c Composition) (*Dispatch, error) {
	missing := []struct {
		what   string
		absent bool
	}{
		{"a pool", c.Pool == nil},
		{"a fleet", c.Fleet == nil},
		{"the role prompts in force", c.Prompts == nil},
		{"the item's writer", c.Items == nil},
		{"a policy reader", c.Policy == nil},
		{"the log", c.Log == nil},
		{"a reader of the log", c.Reader == nil},
		{"the input manifests", c.Manifests == nil},
		{"the agent run records", c.Runs == nil},
		{"an escalation", c.Escalation == nil},
	}
	for _, one := range missing {
		if one.absent {
			return nil, fmt.Errorf("dispatch: the composition names no %s", one.what)
		}
	}
	return &Dispatch{c: c}, nil
}

// On is what a dispatch is for: the item and the stage, or the intent where
// the role is put on one before any item exists, and the subjects a scope is
// matched against.
type On struct {
	// ItemID and Stage are the item this dispatch puts an agent on. Both are
	// empty on a run put on an intent.
	ItemID string
	Stage  item.Stage
	// IntentID is the intent the item was decomposed from, or the intent the
	// run itself is on where there is no item yet.
	IntentID string

	ProjectID string
	ServiceID string
	AreaID    string

	// Reentering says the stage is being entered again after a reject or a
	// rework request sent the item back to it. [item.Dispatch.ReturnTo] counts
	// nothing, the entry being what counts, and this component cannot tell a
	// stage just returned to from one just advanced into — so the caller that
	// sent the item back says so.
	Reentering bool
	// CountedSoFar is what the attempt limit is compared against for a run on
	// an intent, where there is no per-stage row to read: the interview counts
	// its rounds against the same limit and the intent keeps that count. It is
	// ignored on a run that names an item, whose count is the item's own.
	CountedSoFar int
}

// Run is what one dispatch did: the match it made, the records it wrote, and
// the hold it wrote instead where a condition stopped it.
type Run struct {
	// ID identifies this dispatch on the principal every call the agent made
	// carries. No dispatch record exists — the design has the claim and its
	// expiry, which is not built — so this is minted per run and stored on the
	// principal and nowhere else.
	ID    string
	Role  Role
	Entry Entry
	// RolePromptVersionID is the version in force that was handed to the role,
	// and InputManifestID the manifest written before the agent started.
	RolePromptVersionID string
	InputManifestID     string
	// AgentRunIDs is one id per call made, refused replies included.
	AgentRunIDs []string
	// Attempts is what the item's count for the stage stood at when the run
	// ended, and Escalated whether the limit was exceeded.
	Attempts  int
	Escalated bool
	// Held is the condition that stopped the dispatch and HoldRow the wait row
	// it stands as, both empty where nothing held.
	Held    string
	HoldRow string
}

// state reads the intent's state, which dispatch reads before putting an agent
// on a stage. Four states stop it and each is a hold: an intent nobody has
// refined has no reading for a stage to author against, one being decomposed
// again is about to have its items replaced, and an escalated or dropped
// intent has stopped.
//
// An intent that names no state — an item decomposed from nothing, which the
// tests build — reads as refined, so a dispatch on it proceeds.
func stops(state string) string {
	switch state {
	case "unrefined":
		return "the intent has not been refined, so there is no reading for this stage to author against"
	case "re_decomposing":
		return "the intent is being decomposed again, and this item is about to be replaced"
	case "escalated":
		return "the intent escalated, and the factory has stopped work on it"
	case "dropped":
		return "the intent was dropped, and no work on it continues"
	default:
		return ""
	}
}

// limitFor is the attempt limit in force at the stage. It is read once per
// dispatch rather than once per attempt: an owner re-authoring the limit while
// a stage is retrying would otherwise change the number the stage is being held
// to half way through it.
func (d *Dispatch) limitFor(ctx context.Context, stage item.Stage) (int, error) {
	effective, err := d.c.Policy.AttemptLimit(ctx, policy.Subjects{Stage: stage})
	if err != nil {
		return 0, fmt.Errorf("dispatch: reading the attempt limit at %s: %w", stage, err)
	}
	limit := int(effective.Number)
	if limit < 1 {
		return 0, fmt.Errorf("dispatch: the attempt limit in force at %s is %v, and a stage gets at least one attempt",
			stage, effective.Number)
	}
	return limit, nil
}

// countAt is the item's own count for the stage since the mark a cleared
// escalation left, which is what the limit is compared against. A stage with
// no row stands at nothing.
func (d *Dispatch) countAt(ctx context.Context, itemID string, stage item.Stage) (int, error) {
	totals, err := item.Stages(ctx, d.c.Pool, itemID)
	if err != nil {
		return 0, fmt.Errorf("dispatch: reading what %s has spent at %s: %w", itemID, stage, err)
	}
	for _, t := range totals {
		if t.Stage == stage {
			return t.AttemptsSinceCleared(), nil
		}
	}
	return 0, nil
}

// enter writes the transition onto the item: the advance into the stage where
// the item is at the one before, and nothing where it already stands there —
// the entry that put it there having been counted by whoever wrote it.
//
// Writing a transition somebody else reported is not a judgment, and the
// component that reads the field on every move is the one that keeps it.
func (d *Dispatch) enter(ctx context.Context, on On) error {
	it, err := item.Get(ctx, d.c.Pool, on.ItemID)
	if err != nil {
		return err
	}
	if it.Stage != on.Stage {
		_, err := d.c.Items.Advance(ctx, Actor, on.ItemID, on.Stage)
		return err
	}
	if on.Reentering {
		return d.again(ctx, on)
	}
	return nil
}

// again counts one more attempt at the stage the item stands at, which is what
// a refused reply leaves: the item is entered again and the stored count is
// what the limit is compared against next time round.
func (d *Dispatch) again(ctx context.Context, on On) error {
	if on.ItemID == "" {
		return nil
	}
	_, err := d.c.Items.Enter(ctx, Actor, on.ItemID, on.Stage)
	return err
}

// escalate is the item stopping being retried: the escalated value on the
// item, the pending rows abandoned, and the wait to a human, performed by
// whatever the composition supplied.
func (d *Dispatch) escalate(ctx context.Context, on On) error {
	if on.ItemID == "" {
		return nil
	}
	if err := d.c.Escalation.Escalate(ctx, Actor, on.ItemID, on.Stage); err != nil {
		return fmt.Errorf("dispatch: escalating %s at %s: %w", on.ItemID, on.Stage, err)
	}
	return nil
}

// recordRun writes one agent run record: what ran, what it ran on, what it
// served, and what it spent. One per call the run made, refused replies
// included — a refused attempt cost units too.
func (d *Dispatch) recordRun(ctx context.Context, run Run, on On, units map[string]int64,
	startedAt, finishedAt, outcome string) (string, error) {
	recorded, err := d.c.Runs.Record(ctx, Actor, agentrun.New{
		Role:                string(run.Role),
		RolePromptVersionID: run.RolePromptVersionID,
		ModelVersion:        run.Entry.ModelVersion,
		Effort:              run.Entry.Effort,
		CredentialName:      run.Entry.CredentialName,
		ItemID:              on.ItemID,
		Stage:               string(on.Stage),
		IntentID:            intentOf(on),
		InputManifestID:     run.InputManifestID,
		UnitsByKind:         units,
		StartedAt:           startedAt,
		FinishedAt:          finishedAt,
		Outcome:             outcome,
	})
	if err != nil {
		return "", err
	}
	return recorded.ID, nil
}

// intentOf is the intent an agent run record names. A run on an item names the
// item and its stage, and the record refuses naming both — so the intent goes
// on only where there is no item.
func intentOf(on On) string {
	if on.ItemID != "" {
		return ""
	}
	return on.IntentID
}

// outcomeOf is the word an agent run record's outcome carries. The design names
// no vocabulary for it, so these are this component's words and the store
// requires only that there are some.
func outcomeOf(err error) string {
	switch {
	case err == nil:
		return "authored"
	case errors.Is(err, ErrOutOfAttempts):
		return "gave up"
	default:
		return "refused"
	}
}
