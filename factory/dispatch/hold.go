package dispatch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// HoldFormatVersion is what a hold row declares itself as. A hold is a wait
// and not a decision: no gate fired, nothing is decided, and the condition is
// not a record.
const HoldFormatVersion = "wait/1"

// The conditions that stop a dispatch, in the words the row stores. Six stop
// one in the design and five are computed here, in the order the design gives
// them; the sixth, and why, is in doc.go.
const (
	// HoldNoEntryCoversTheStage is a stage no fleet entry covers.
	HoldNoEntryCoversTheStage = "no fleet entry covers this stage on this item"
	// HoldNoRolePromptInForce is a stage whose role has no role prompt version
	// in force, which is what an upgrade that added a role leaves.
	HoldNoRolePromptInForce = "the role has no role prompt version in force"
	// HoldTheIntentStops is the intent's own state stopping every component
	// that could move the item.
	HoldTheIntentStops = "the intent's state stops work on this item"
	// HoldCredentialUnreachable is a credential a run has already failed to
	// reach, read off the credential row that failure left in the log.
	HoldCredentialUnreachable = "the credential this entry runs on is already known unreachable"
	// HoldCredentialAtCeiling is a credential whose spend since the period's
	// start has reached the ceiling authored on it, or whose spend the rates
	// do not cover, which fails closed the same way.
	HoldCredentialAtCeiling = "the credential this entry runs on is at its spend ceiling"
	// HoldConstraintRequiresSeam5 is a document-kind constraint in force over
	// the item requiring seam 5 enforced where the factory does not enforce
	// it.
	HoldConstraintRequiresSeam5 = "a constraint in force requires seam 5 enforced, and this factory does not enforce it"
)

// RoutedToTheOwner is who a hold row routes to where the design routes it away
// from whoever lent the credential: raising, clearing or lengthening a ceiling
// is the owner's, and a row reaching the lender would reach somebody who
// cannot act on it. The other two credential rows carry no routing — Work
// resolves the credential name against the People declaration to reach
// whoever lent it.
const RoutedToTheOwner = "owner"

// Hold is what a hold row says: the condition that held, the item and the
// stage, the intent and the role, or the project and the role it held, and the
// values the match was made on — so what would clear it is read off the row
// rather than followed from a pointer at an entry.
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
	// CredentialName is the credential where one of the two credential
	// conditions was the cause, and never a pointer at the fleet entry: an
	// owner may re-credential or delete an entry without changing what this
	// row says.
	CredentialName string `json:"credential_name,omitempty"`
	// WantsARate is the runs whose converted amount is absent where that is
	// why the ceiling failed closed, each naming the kinds, the model version
	// and the effort a rate is wanted for. Authoring the rate is what clears
	// it, rather than authorising an overage.
	WantsARate []WantsARate `json:"wants_a_rate,omitempty"`
	// ConstraintID is the constraint requiring seam 5 enforced where that was
	// the cause.
	ConstraintID string `json:"constraint_id,omitempty"`
	// RoutedTo is [RoutedToTheOwner] on the ceiling's row and empty on every
	// other, whose routing is the design's own.
	RoutedTo string `json:"routed_to,omitempty"`
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
// cause carries the condition and the values that condition names — the
// intent's state, the credential, the constraint, the routing — and this fills
// in the subject every row carries: the role, the item and its stage or the
// intent, and the values the match was made on.
//
// One row per item and stage: a hold already open for that item, that stage and
// that condition is returned as it stands rather than written again, so a stage
// retried against a condition that has not moved is one row and not one per
// retry. For a run on no item the row is per intent and role, and for a run on
// neither an item nor an intent it is per project and role, which is the same
// read one field along.
func (d *Dispatch) hold(ctx context.Context, run Run, on On, cause Hold) (Run, error) {
	standing, rows, err := d.Open(ctx)
	if err != nil {
		return run, err
	}
	for n, open := range standing {
		if open.Condition == cause.Condition && open.sameSubject(run.Role, on) {
			run.Held, run.HoldRow = cause.Condition, rows[n].ID
			return run, fmt.Errorf("%w: %s", ErrHeld, cause.Condition)
		}
	}

	cause.Kind, cause.Role = HoldKind, string(run.Role)
	cause.ItemID, cause.Stage, cause.IntentID = on.ItemID, string(on.Stage), on.IntentID
	cause.ProjectID, cause.ServiceID, cause.AreaChain = on.ProjectID, on.ServiceID, on.Areas()
	payload, err := json.Marshal(cause)
	if err != nil {
		return run, fmt.Errorf("dispatch: marshalling the hold on %s: %w", on.ItemID, err)
	}
	row, err := d.c.Log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: HoldFormatVersion,
	})
	if err != nil {
		return run, err
	}
	run.Held, run.HoldRow = cause.Condition, row.ID
	return run, fmt.Errorf("%w: %s", ErrHeld, cause.Condition)
}

// sameSubject reports whether an open hold is about what this dispatch is
// about: the item and the stage where there is an item, the intent and the role
// where the run is on an intent, and the project and the role where it is on
// neither. It is what makes a hold one row per item and stage rather than one
// per retry.
func (h Hold) sameSubject(role Role, on On) bool {
	if on.ItemID != "" {
		return h.ItemID == on.ItemID && h.Stage == string(on.Stage)
	}
	if h.ItemID != "" || h.Role != string(role) {
		return false
	}
	if on.IntentID != "" {
		return h.IntentID == on.IntentID
	}
	return h.IntentID == "" && h.ProjectID == on.ProjectID
}

// Open is every hold this component has open, and the row each stands as. It
// is what a re-match reads: a hold is a row and not a field, so a start finds
// the open ones by reading records and never by keeping a list.
//
// The rows come back whole rather than as their ids, because the readiness
// reading is the age of the oldest unmatched row and the time is the row's.
func (d *Dispatch) Open(ctx context.Context) ([]Hold, []decisionlog.Row, error) {
	rows, err := d.c.Reader.ByShape(ctx, componentPrincipal, decisionlog.ShapeWait)
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
	var standing []decisionlog.Row
	for _, row := range rows {
		if row.Part != decisionlog.PartOpen || closed[row.ID] {
			continue
		}
		var held Hold
		if err := json.Unmarshal([]byte(row.Payload), &held); err != nil || held.Kind != HoldKind {
			continue
		}
		open = append(open, held)
		standing = append(standing, row)
	}
	return open, standing, nil
}

// Rematch re-tests every open hold and writes the second row of each one the
// match now lifts, so no hold outlives its condition and none is left for a
// component that has stopped to close. It returns the rows it closed.
//
// It is called where a record able to clear one arrives. Four of the six
// records the design names have a caller: an owner writing or withdrawing a
// fleet entry at Factory, a credential reached again by the dispatch whose own
// failure opened its row, an owner clearing a ceiling, and the seam 5 field
// turning on. Two do not, so a hold on either outlives its condition until
// something else re-matches: the gate a version fires putting a role prompt in
// force, and an intent leaving the state that stopped it. It is also called at
// every start, a hold being a row and a start being a read of it, and by a
// dispatch that got through the conditions — which is what those two are
// covered by today, at whatever delay the next dispatch is.
func (d *Dispatch) Rematch(ctx context.Context) ([]string, error) {
	open, rows, err := d.Open(ctx)
	if err != nil {
		return nil, err
	}
	// The credential rows are read once for the whole re-match: every read of
	// the log appends a read event, and a hold that read them for itself would
	// write one per hold.
	credentials, err := d.credentialWaits(ctx)
	if err != nil {
		return nil, err
	}
	var lifted []string
	for n, held := range open {
		still, err := d.stillHolds(ctx, held, credentials)
		if err != nil {
			return lifted, err
		}
		if still {
			continue
		}
		if _, err := d.c.Log.AppendWaitClose(ctx, decisionlog.Entry{
			Actor: Actor, Payload: `{"kind":"` + HoldKind + `","lifted":"` + held.Condition + `"}`,
			FormatVersion: HoldFormatVersion, Closes: rows[n].ID,
		}); err != nil {
			return lifted, err
		}
		lifted = append(lifted, rows[n].ID)
	}
	return lifted, nil
}

// stillHolds re-tests one hold against the records as they are now.
func (d *Dispatch) stillHolds(ctx context.Context, held Hold, credentials credentialRows) (bool, error) {
	on := On{
		ItemID: held.ItemID, Stage: item.Stage(held.Stage), IntentID: held.IntentID,
		ProjectID: held.ProjectID, ServiceID: held.ServiceID,
		AreaID: held.area(), AreaChain: held.AreaChain,
	}
	switch held.Condition {
	case HoldNoEntryCoversTheStage:
		_, found, err := d.matchFor(ctx, Role(held.Role), on)
		return !found, err
	case HoldNoRolePromptInForce:
		_, found, err := d.c.Prompts.InForce(ctx, Role(held.Role))
		return !found, err
	case HoldTheIntentStops:
		stopped, err := d.intentStops(ctx, on)
		return stopped != "", err
	case HoldCredentialUnreachable:
		return credentials.standing(KindCredentialUnreachable, held.CredentialName), nil
	case HoldCredentialAtCeiling:
		reading, err := d.atCeiling(ctx, held.CredentialName, credentials)
		return reading.reached, err
	case HoldConstraintRequiresSeam5:
		requiring, err := d.constraintRequiringSeam5(ctx, on)
		return requiring != "", err
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

// constraintRequiringSeam5 is the id of a document-kind constraint in force
// over this dispatch that requires seam 5 enforced, where the factory does not
// enforce it, and the empty string otherwise. The first such constraint is the
// one the row names: one is enough to hold, and the row names what would
// clear it.
//
// A run on an intent reads the interview's own set — the factory's own
// constraints and the intent's — which is deliberately less than an item's: an
// intent's items may not share a project until decomposition has cut them.
//
// A factory with no settings record does not enforce seam 5: the field is off
// at install and turned on once, so an install that has not written the record
// has not turned it on.
func (d *Dispatch) constraintRequiringSeam5(ctx context.Context, on On) (string, error) {
	var inForce []constraint.Constraint
	var err error
	if on.ItemID != "" {
		inForce, err = constraint.InForce(ctx, d.c.Pool, constraint.Over{
			ProjectID: on.ProjectID, AreaChain: on.Areas(), IntentID: on.IntentID,
		}, time.Now())
	} else {
		inForce, err = constraint.InForceForInterview(ctx, d.c.Pool, on.IntentID, time.Now())
	}
	if err != nil {
		return "", err
	}
	var requiring string
	for _, one := range inForce {
		if one.RequiresSeam5Enforced {
			requiring = one.ID
			break
		}
	}
	if requiring == "" {
		return "", nil
	}
	settings, err := factorysettings.Get(ctx, d.c.Pool)
	if err != nil && !errors.Is(err, factorysettings.ErrNotFound) {
		return "", err
	}
	if settings.Seam5Enforced {
		return "", nil
	}
	return requiring, nil
}
