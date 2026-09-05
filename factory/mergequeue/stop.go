package mergequeue

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// The conditions that stop a fast-forward. None of them is a decision: no gate
// fires, nothing is decided, no attempt is counted and the score learns nothing,
// so each is a wait the factory could not compute at a firing, written into the
// log with the queue as caller and actor and closed by the queue when the
// condition is found gone.

// WaitKind is what a wait row of the queue's says it is, so a reader can tell
// the queue's waits from every other kind of payload sharing the log's wait
// shape.
type WaitKind string

const (
	// WaitMasterHoldsACommitTheQueueDidNotMake is a commit on master past the
	// newest release's that no build of a candidate approved at Merge to master
	// names: a human's push, an agent's, or a merge whose records a restore lost.
	// It ends when master returns to the commit the newest release names, or when
	// a human accepts the commit through [Queue.AcceptCommit].
	WaitMasterHoldsACommitTheQueueDidNotMake WaitKind = "master holds a commit the queue did not make"
	// WaitAReleaseNamesACommitMasterDoesNotHold is git restored behind the graph:
	// the members of the recovery unit landed apart. It ends only when master
	// holds the commit again, because a release is written once and cannot be
	// unwritten to match.
	WaitAReleaseNamesACommitMasterDoesNotHold WaitKind = "a release names a commit master does not hold"
	// WaitHalt is the one authored record whose subject is the factory. While one
	// stands the queue fast-forwards no candidate, the two exceptions apart.
	WaitHalt WaitKind = "a halt stands, and the factory is stopped"
	// WaitBacklogCap is as many releases waiting behind a rollback hold as the
	// backlog cap allows. It ends when the rollback hold lifts, which is when the
	// revert ships.
	WaitBacklogCap WaitKind = "as many releases wait behind a rollback hold as the backlog cap allows"
	// WaitIntentStops is the state of the intent the item was decomposed from:
	// unrefined, re-decomposing, escalated, or dropped. It is closed when the
	// state clears.
	WaitIntentStops WaitKind = "the item's intent stops every component that could move it"
)

// WaitKinds is every kind of wait the queue writes, in the order a pass reads
// the conditions: the two readings of master first, because a service held at
// either mints nothing whatever else stands, then the two stops over every
// candidate, then the one over a single item.
var WaitKinds = []WaitKind{
	WaitAReleaseNamesACommitMasterDoesNotHold, WaitMasterHoldsACommitTheQueueDidNotMake,
	WaitHalt, WaitBacklogCap, WaitIntentStops,
}

// WaitPayload is what the queue writes into a wait's opening. Every wait names
// the service, because every one of them stops one service's merges, and the
// fields beside it are what the condition is keyed by: the item for a wait about
// one candidate, the commit and the release for the two readings of master.
type WaitPayload struct {
	Kind      WaitKind `json:"kind"`
	ServiceID string   `json:"service_id"`
	ItemID    string   `json:"item_id,omitempty"`
	Commit    string   `json:"commit,omitempty"`
	ReleaseID string   `json:"release_id,omitempty"`
	// IntentState is the state that stopped the item, on a wait of kind
	// [WaitIntentStops] and empty on every other.
	IntentState string `json:"intent_state,omitempty"`
	// Releases and Cap are the two numbers the backlog cap's stop was decided
	// from, and are zero on every other kind.
	Releases int `json:"releases,omitempty"`
	Cap      int `json:"cap,omitempty"`
}

// waitFormatVersion is the format version every wait the queue appends is
// written with. It names [decisionlog.ShapeWait] through decisionlog.Formats.
const waitFormatVersion = "wait/1"

// subject is what tells one of the queue's waits from another: the kind and the
// thing the condition is about. Two openings with one subject are one wait, so a
// pass that meets a standing condition again writes no second row.
type subject struct {
	Kind      WaitKind
	ServiceID string
	ItemID    string
	Commit    string
	ReleaseID string
}

func (p WaitPayload) subject() subject {
	return subject{Kind: p.Kind, ServiceID: p.ServiceID, ItemID: p.ItemID,
		Commit: p.Commit, ReleaseID: p.ReleaseID}
}

// standing is every wait of the queue's that no closing has ended, by subject.
// It is read from the log rather than kept in the queue, so a restart finds the
// waits its predecessor opened: what outlasts a run of the queue is a record,
// and its speculative re-verifications are the only state that is not.
func (q *Queue) standing(ctx context.Context) (map[subject]decisionlog.Row, error) {
	rows, err := decisionlog.NewReader(q.pool, q.token).ByShape(ctx, Actor, decisionlog.ShapeWait)
	if err != nil {
		return nil, err
	}
	ended := make(map[string]bool)
	for _, row := range rows {
		if row.Part == decisionlog.PartClose {
			ended[row.Closes] = true
		}
	}
	open := make(map[subject]decisionlog.Row)
	for _, row := range rows {
		if row.Part != decisionlog.PartOpen || ended[row.ID] {
			continue
		}
		var payload WaitPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			// A payload is unconstrained bytes by decisionlog's contract, so a
			// row this package cannot read is a wait some other component wrote
			// in a shape it does not know. It is not one of the queue's.
			continue
		}
		if !slices.Contains(WaitKinds, payload.Kind) {
			continue
		}
		open[payload.subject()] = row
	}
	return open, nil
}

// openWait appends the wait's opening unless one with the same subject already
// stands, and answers with the row the condition stands as either way. The
// condition is met at every pass while it holds, and one row per pass would be a
// log that grows with how often the queue looked.
func (q *Queue) openWait(ctx context.Context, payload WaitPayload) (decisionlog.Row, error) {
	open, err := q.standing(ctx)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if row, found := open[payload.subject()]; found {
		return row, nil
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("mergequeue: marshalling the wait over %s: %w", payload.ServiceID, err)
	}
	return q.log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(encoded), FormatVersion: waitFormatVersion,
	})
}

// closeStale appends a closing for every wait of the queue's over this service
// whose kind the pass evaluated and whose subject it did not find standing. That
// is the condition found gone, and the pass that found it is what closes the
// wait rather than whatever ended the condition — the queue is the only reader
// of these rows.
//
// Only the kinds the pass evaluated are closed. A service held at either reading
// of master mints nothing and reaches no candidate, so a pass that stopped there
// has read neither the halt nor the backlog cap and closes neither.
func (q *Queue) closeStale(ctx context.Context, serviceID string, stood []subject, evaluated []WaitKind) error {
	open, err := q.standing(ctx)
	if err != nil {
		return err
	}
	for s, row := range open {
		if s.ServiceID != serviceID || !slices.Contains(evaluated, s.Kind) || slices.Contains(stood, s) {
			continue
		}
		if _, err := q.log.AppendWaitClose(ctx, decisionlog.Entry{
			Actor: Actor, Payload: row.Payload, FormatVersion: waitFormatVersion, Closes: row.ID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// halted reports whether a halt stands. While one does, the queue fast-forwards
// no candidate but the two the design excepts, and the stop is written into the
// log the way the backlog cap's already is. Nothing is decided, no attempt is
// counted, and the score learns nothing, which is the treatment a hold already
// gets and the reason a halt is one rather than a reject.
func (q *Queue) halted(ctx context.Context) (bool, error) {
	standing, err := halt.Standing(ctx, q.pool)
	if err != nil {
		return false, fmt.Errorf("mergequeue: reading whether a halt stands: %w", err)
	}
	return len(standing) > 0, nil
}

// stopFor is the condition that holds one candidate, and is empty where none
// does.
//
// The halt passes two candidates: a revert item and an item the health monitor
// raised. The backlog cap passes one, the revert's own candidate, because the
// rollback hold lifts only when the revert ships and a stop that held it would
// never end.
func stopFor(it item.Item, in intent.Intent, halted, capped bool, waiting Waiting) WaitKind {
	if halted && !raisedByTheHealthMonitor(in) {
		return WaitHalt
	}
	if capped && (waiting.RevertItemID == "" || waiting.RevertItemID != it.ID) {
		return WaitBacklogCap
	}
	return ""
}

// raisedByTheHealthMonitor reports whether the item's intent is one the health
// monitor raised, which covers both items the halt's stop passes: a revert is an
// item of the intent the health monitor raised at the rollback, and an item the
// health monitor raised on the service is an item of such an intent too. The
// reading is the intent's source and its actor, the two fields that say which of
// the three sources wrote it and which component called intake.
func raisedByTheHealthMonitor(in intent.Intent) bool {
	return in.Source == intent.SourceDetector &&
		in.Actor.Kind == record.KindComponent && in.Actor.Key == healthMonitorActorKey
}

// stop writes the wait one candidate's stop stands as and answers with the
// outcome that names it. Nothing else happens to the candidate: it is not
// re-verified, nothing is minted, and the item is not moved.
func (q *Queue) stop(ctx context.Context, it item.Item, kind WaitKind, waiting Waiting) (Outcome, subject, error) {
	payload := WaitPayload{Kind: kind, ServiceID: it.ServiceID, ItemID: it.ID}
	if kind == WaitBacklogCap {
		payload.Releases, payload.Cap = waiting.Releases, waiting.Cap
	}
	row, err := q.openWait(ctx, payload)
	if err != nil {
		return Outcome{}, subject{}, err
	}
	return Outcome{ItemID: it.ID, Stopped: string(kind), WaitRow: row.ID}, payload.subject(), nil
}

// intentWaits opens a wait for every member the intent's state stops and answers
// with the subjects that stand, so the pass closes the ones whose state has
// cleared. The row is opened by the component that met the state — three do, and
// this is the queue's — and closed by that component when the state clears.
func (q *Queue) intentWaits(ctx context.Context, m Membership) ([]subject, error) {
	stood := make([]subject, 0, len(m.Stopped))
	for _, it := range m.Stopped {
		payload := WaitPayload{
			Kind:        WaitIntentStops,
			ServiceID:   it.ServiceID,
			ItemID:      it.ID,
			IntentState: string(m.Intents[it.ID].State),
		}
		if _, err := q.openWait(ctx, payload); err != nil {
			return nil, err
		}
		stood = append(stood, payload.subject())
	}
	return stood, nil
}
