package gate

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// A decision that will never receive a verdict is ended in the log rather than
// left open. Its second row is an abandonment: it names the open event and why
// no verdict is coming, with the component that ended the decision as caller and
// actor, and it carries no verdict, a close event being reserved for one.

// Why no verdict is coming, in the words an abandonment stores. Three things
// write one, and these are the three.
const (
	// AbandonedByTheAttemptLimit is an item that exceeded the attempt limit and
	// stopped being retried, leaving a row nobody is deciding.
	AbandonedByTheAttemptLimit = "the item exceeded the attempt limit and stopped being retried"
	// AbandonedBySupersession is an item superseded by a re-decomposition or
	// dropped, or a row an Edit in place superseded by authoring a new version
	// while it waited.
	AbandonedBySupersession = "the row was superseded, and no verdict binds to what it decided over"
	// AbandonedByTheItemEnding is an item dropped for good, which ends its open
	// rows with it.
	AbandonedByTheItemEnding = "the item ended, so no verdict is coming"
)

// ErrAttemptLimit is returned by [Gate.EnforceAttemptLimit] where the item has
// exceeded the limit for the stage. The item is escalated, its pending rows are
// abandoned, and the wait goes to the notifier before it is returned.
var ErrAttemptLimit = errors.New("gate: the item exceeded the attempt limit for this stage")

// abandonmentPayload is what the abandonment row says: why no verdict is
// coming, in the words the caller gave. It is marshalled rather than written as
// text, because the reason is an arbitrary string and a chained row cannot be
// corrected afterwards.
type abandonmentPayload struct {
	Abandoned string `json:"abandoned"`
}

// Abandon ends a pending decision that will never receive a verdict, naming why.
// The actor is the gate component, which is the component that ended the
// decision.
func (g *Gate) Abandon(ctx context.Context, opened Opened, why string) (decisionlog.Row, error) {
	return g.abandon(ctx, opened, component(opened.Gate), why)
}

// abandon appends the abandonment.
func (g *Gate) abandon(ctx context.Context, opened Opened, actor record.Actor, why string) (decisionlog.Row, error) {
	if why == "" {
		return decisionlog.Row{}, fmt.Errorf("%w: the abandonment of %s says none",
			ErrReasonMissing, opened.Row.ID)
	}
	payload, err := json.Marshal(abandonmentPayload{Abandoned: why})
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("gate: marshalling the abandonment of %s: %w", opened.Row.ID, err)
	}
	return g.log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor:         actor,
		Payload:       string(payload),
		FormatVersion: decisionFormatVersion,
		Closes:        opened.Row.ID,
		Reason:        why,
	})
}

// Escalated is what the attempt limit left: whether the item exceeded it, the
// count and the limit it was compared against, and the rows abandoned with it.
type Escalated struct {
	Reached bool
	// Attempts is the item's own count for the stage since the mark a cleared
	// escalation left, which is what the limit is compared against.
	Attempts int
	Limit    int
	// Abandoned is every pending row of the item this call ended.
	Abandoned []decisionlog.Row
}

// EnforceAttemptLimit compares the item's own count for the stage against the
// limit in force. Over it, three things happen and in this order: dispatch writes
// the escalation onto the item, every pending row of the item is abandoned
// naming the limit, and the wait goes to the notifier — so the item stops being
// retried before anything says so, and a failure between the writes leaves the
// item stopped rather than a row nobody is deciding.
//
// What the limit is compared against is the item's own count for the stage it is
// at, kept per stage by dispatch: counting the rejects in the log instead would
// miss every attempt that failed before a gate fired. A hold counts nothing, and
// neither does a refer.
func (g *Gate) EnforceAttemptLimit(ctx context.Context, actor record.Actor, itemID string, stage item.Stage) (Escalated, error) {
	if g.dispatch == nil {
		return Escalated{}, fmt.Errorf("%w: %s at %s", ErrDispatchNotComposed, itemID, stage)
	}
	limit, err := g.policy.AttemptLimit(ctx, subjectsForStage(stage))
	if err != nil {
		return Escalated{}, fmt.Errorf("gate: reading the attempt limit for %s: %w", stage, err)
	}
	totals, err := item.Stages(ctx, g.pool, itemID)
	if err != nil {
		return Escalated{}, fmt.Errorf("gate: reading what %s has spent at %s: %w", itemID, stage, err)
	}
	attempts := 0
	for _, t := range totals {
		if t.Stage == stage {
			attempts = t.AttemptsSinceCleared()
		}
	}
	escalated := Escalated{Attempts: attempts, Limit: int(limit.Number)}
	if attempts <= escalated.Limit {
		return escalated, nil
	}
	escalated.Reached = true

	if _, err := g.dispatch.Escalate(ctx, actor, itemID); err != nil {
		return escalated, fmt.Errorf("gate: escalating %s: %w", itemID, err)
	}
	pending, err := g.Pending(ctx)
	if err != nil {
		return escalated, err
	}
	for _, open := range pending {
		if open.Subject.ItemID != itemID {
			continue
		}
		row, err := g.abandon(ctx, open, component(open.Gate), AbandonedByTheAttemptLimit)
		if err != nil {
			return escalated, err
		}
		escalated.Abandoned = append(escalated.Abandoned, row)
	}
	if err := g.notifier.Escalated(ctx, itemID, stage, AbandonedByTheAttemptLimit); err != nil {
		return escalated, fmt.Errorf("gate: reporting the escalation of %s: %w", itemID, err)
	}
	return escalated, nil
}

// subjectsForStage is what the attempt limit is read against: the stage alone.
// The limit is a field of the factory-wide settings record per stage, so no
// service, area or environment narrows it.
func subjectsForStage(stage item.Stage) policy.Subjects {
	return policy.Subjects{Stage: stage}
}
