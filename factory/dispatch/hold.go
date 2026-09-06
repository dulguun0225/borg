package dispatch

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// HoldFormatVersion is what a hold row declares itself as. A hold is a wait
// and not a decision: no gate fired, nothing is decided, and the condition is
// not a record.
const HoldFormatVersion = "wait/1"

// The conditions that stop a dispatch, in the words the row stores. Six stop
// one in the design and three are computable here; which three, and why the
// other three are not, is in doc.go.
const (
	// HoldNoEntryCoversTheStage is a stage no fleet entry covers.
	HoldNoEntryCoversTheStage = "no fleet entry covers this stage on this item"
	// HoldNoRolePromptInForce is a stage whose role has no role prompt version
	// in force, which is what an upgrade that added a role leaves.
	HoldNoRolePromptInForce = "the role has no role prompt version in force"
	// HoldTheIntentStops is the intent's own state stopping every component
	// that could move the item.
	HoldTheIntentStops = "the intent's state stops work on this item"
)

// Hold is what a hold row says: the condition that held, the item and the
// stage or the intent and the role it held, and the values the match was made
// on — so what would clear it is read off the row rather than followed from a
// pointer at an entry.
type Hold struct {
	Kind      string `json:"kind"`
	Condition string `json:"condition"`
	Role      string `json:"role"`
	ItemID    string `json:"item_id,omitempty"`
	Stage     string `json:"stage,omitempty"`
	IntentID  string `json:"intent_id,omitempty"`
	ProjectID string `json:"project_id,omitempty"`
	ServiceID string `json:"service_id,omitempty"`
	// AreaChain is the item's area and every area above it, which is the value
	// the scope was matched against: a scope drawn anywhere on the chain covers
	// the item, so the chain and not one area is what says why no entry
	// covered it. Its first entry is the item's own area.
	AreaChain []string `json:"area_chain,omitempty"`
	// State is the intent's state where that was the cause, and empty
	// otherwise.
	State string `json:"state,omitempty"`
}

// area is the item's own area, which is the first of the chain the row names.
func (h Hold) area() string {
	if len(h.AreaChain) == 0 {
		return ""
	}
	return h.AreaChain[0]
}

// HoldKind is what every hold row of this component carries, so a reader tells
// one from every other kind of wait.
const HoldKind = "dispatch_hold"

// hold opens one hold row and returns the run naming it. No page fires and no
// attempt counts: nothing deployed is worse for it, and no agent worked.
//
// One row per item and stage: a hold already open for that item, that stage and
// that condition is returned as it stands rather than written again, so a stage
// retried against a condition that has not moved is one row and not one per
// retry. For a run on no item the row is per intent and role, and per project
// for a run on one, which is the same read one field along.
func (d *Dispatch) hold(ctx context.Context, run Run, on On, condition, state string) (Run, error) {
	standing, rows, err := d.Open(ctx)
	if err != nil {
		return run, err
	}
	for n, open := range standing {
		if open.Condition == condition && open.sameSubject(run.Role, on) {
			run.Held, run.HoldRow = condition, rows[n]
			return run, fmt.Errorf("%w: %s", ErrHeld, condition)
		}
	}

	payload, err := json.Marshal(Hold{
		Kind: HoldKind, Condition: condition, Role: string(run.Role),
		ItemID: on.ItemID, Stage: string(on.Stage), IntentID: on.IntentID,
		ProjectID: on.ProjectID, ServiceID: on.ServiceID, AreaChain: on.Areas(),
		State: state,
	})
	if err != nil {
		return run, fmt.Errorf("dispatch: marshalling the hold on %s: %w", on.ItemID, err)
	}
	row, err := d.c.Log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: HoldFormatVersion,
	})
	if err != nil {
		return run, err
	}
	run.Held, run.HoldRow = condition, row.ID
	return run, fmt.Errorf("%w: %s", ErrHeld, condition)
}

// sameSubject reports whether an open hold is about what this dispatch is
// about: the item and the stage where there is an item, and the intent and the
// role where the run is on an intent. It is what makes a hold one row per item
// and stage rather than one per retry.
func (h Hold) sameSubject(role Role, on On) bool {
	if on.ItemID != "" {
		return h.ItemID == on.ItemID && h.Stage == string(on.Stage)
	}
	return h.ItemID == "" && h.IntentID == on.IntentID && h.Role == string(role)
}

// Open is every hold this component has open, read off the log. It is what a
// re-match reads: a hold is a row and not a field, so a start finds the open
// ones by reading records and never by keeping a list.
func (d *Dispatch) Open(ctx context.Context) ([]Hold, []string, error) {
	rows, err := d.c.Reader.ByShape(ctx, Actor, decisionlog.ShapeWait)
	if err != nil {
		return nil, nil, err
	}
	closed := map[string]bool{}
	for _, row := range rows {
		if row.Part == decisionlog.PartClose {
			closed[row.Closes] = true
		}
	}
	var open []Hold
	var ids []string
	for _, row := range rows {
		if row.Part != decisionlog.PartOpen || closed[row.ID] {
			continue
		}
		var held Hold
		if err := json.Unmarshal([]byte(row.Payload), &held); err != nil || held.Kind != HoldKind {
			continue
		}
		open = append(open, held)
		ids = append(ids, row.ID)
	}
	return open, ids, nil
}

// Rematch re-tests every open hold and writes the second row of each one the
// match now lifts, so no hold outlives its condition and none is left for a
// component that has stopped to close. It returns the rows it closed.
//
// It is called where a record able to clear one arrives — an owner composing a
// fleet entry, the gate a version fires putting a role prompt in force, an
// intent leaving the state that stopped it — and at every start, a hold being
// a row and a start being a read of it.
func (d *Dispatch) Rematch(ctx context.Context) ([]string, error) {
	open, ids, err := d.Open(ctx)
	if err != nil {
		return nil, err
	}
	var lifted []string
	for n, held := range open {
		still, err := d.stillHolds(ctx, held)
		if err != nil {
			return lifted, err
		}
		if still {
			continue
		}
		if _, err := d.c.Log.AppendWaitClose(ctx, decisionlog.Entry{
			Actor: Actor, Payload: `{"kind":"` + HoldKind + `","lifted":"` + held.Condition + `"}`,
			FormatVersion: HoldFormatVersion, Closes: ids[n],
		}); err != nil {
			return lifted, err
		}
		lifted = append(lifted, ids[n])
	}
	return lifted, nil
}

// stillHolds re-tests one hold against the records as they are now.
func (d *Dispatch) stillHolds(ctx context.Context, held Hold) (bool, error) {
	on := On{
		ItemID: held.ItemID, Stage: item.Stage(held.Stage), IntentID: held.IntentID,
		ProjectID: held.ProjectID, ServiceID: held.ServiceID,
		AreaID: held.area(), AreaChain: held.AreaChain,
	}
	switch held.Condition {
	case HoldNoEntryCoversTheStage:
		_, found, err := d.c.Fleet.EntryFor(ctx, Role(held.Role), on)
		return !found, err
	case HoldNoRolePromptInForce:
		_, found, err := d.c.Prompts.InForce(ctx, Role(held.Role))
		return !found, err
	case HoldTheIntentStops:
		stopped, err := d.intentStops(ctx, on)
		return stopped != "", err
	default:
		// A condition this component does not compute is left standing:
		// closing a hold whose condition nothing here can re-test would say
		// the condition is gone on no evidence.
		return true, nil
	}
}

// intentStops is the intent's state where it stops work on the item, and the
// empty string where it does not. An item naming no intent, and an intent that
// cannot be read, are not a stop: the state is a reason to hold and its absence
// is not.
func (d *Dispatch) intentStops(ctx context.Context, on On) (string, error) {
	intentID := on.IntentID
	if intentID == "" && on.ItemID != "" {
		it, err := item.Get(ctx, d.c.Pool, on.ItemID)
		if err != nil {
			return "", err
		}
		intentID = it.IntentID
	}
	if intentID == "" {
		return "", nil
	}
	in, err := intent.Get(ctx, d.c.Pool, intentID)
	if err != nil {
		return "", err
	}
	return stops(string(in.State)), nil
}
