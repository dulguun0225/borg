package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/principal"
)

// A run is [path.takeIn] — every intent taken in, refined, decomposed, and its
// set ratified at Decomposition — and then [path.advance] repeated until nothing
// moves. One pass performs what it can and stops each item at the first row a
// human decides, so the pass is repeatable and what the next one does is read
// out of the records, per ../../../end-goal/one-process.md's rule that every
// component's restart is a read of its own records.
//
// [path.rehydrate] is that read: one item's candidate built from the records
// alone — the item, the intent and the requirements it answers, the four artifact
// versions and their text, the screens the spec introduced, every build, the
// candidate environment and what it was composed from, the criterion runs, the
// release, the deploy, the window — and [path.stepFrom] is where the pass enters
// it, derived from which of the item's subjects the rows above it have approved.
// The item's stage cannot be that reading and is not used as one: dispatch writes
// the stage when it puts an agent on the next stage, so an item whose Spec row has
// just approved still stands at spec.
//
// No field is kept in memory across a pass. Five have no record of their own
// and each is derived. The build's diff is taken again from the repository, the
// way the stage that built took it. What the environment was composed from at
// the run that passed at Merge to master is read off that build's criterion
// result rows, where the composition is copied at every run — the environment
// record's own composed-from field is rewritten at each recomposition and cannot
// answer it. Which of an item's builds the rows below the implementation stage
// decide is the newest one the release does not name, the release naming the
// build the merge queue verified. The seven firings are read back off the log's
// own rows. What a release published is read back the way [contract.Publish]
// wrote it, the change diffed against the version in force below that release's
// number. And what a rejection sent the item back with is read off the close
// event that rejected it.
//
// One field is deliberately empty: a compile failure is one attempt's reading,
// and the stage that re-enters takes its own.
//
// A verdict a human left between two passes reaches exactly the code the
// factory's own auto-pass reached. At the three document rows and the
// implementation row [path.sendBack] returns the item to the stage the row names,
// with an attempt counted there, and hands the stage the reason and the version;
// at the two deploy rows and Merge to master [path.decideOrResume] answers with
// the verdict already on the row instead of firing it again, so what an approval
// performs — the environment composed, the candidate admitted to the queue, the
// release deployed — is performed by the pass that finds it. A hold is a stop on
// the event: the row is closed, the item reads as held, and nothing here fires it
// again, a holder releasing it at Work being what does.
//
// What it costs: the log is read whole once per pass, because the verdict on a
// row lives in a payload the log's own store knows nothing about, and once per
// pass rather than once per item because a read per item is quadratic in a log
// that grows by a read event at every read; a candidate is rewritten
// from the records at every pass, so nothing survives in memory that a record
// does not hold; and what says a pass moved is whether a row fired or a verdict
// was acted on, which is what ends the loop.

// The pass is resumable, and this is what makes it one: one item's candidate
// read back out of the records, and the step the next pass performs derived from
// the rows its gates have closed. A row a human decides is left pending in Work
// by the pass that fired it, so every field the stage below it reads has to come
// from a record rather than from the pass that authored it.

// step is how far one item has got on the path, derived from records alone. The
// steps are in the order the path performs them, so a pass entering at one
// performs it and every step after it, and a comparison against a later step is
// how the pass skips what is already done.
type step int

const (
	stepSpec step = iota
	stepPlan
	stepTasks
	stepImplementation
	stepCandidateEnvironment
	stepMerge
	// stepQueue is the merge queue's and the production deploy's: nothing per
	// item is performed above the merge row, and what happens next is the
	// service's queue rather than this item's own stage.
	stepQueue
	// stepSentBack is an item the merge queue rejected at its own
	// re-verification. It goes back to the implementation stage with an attempt
	// counted there, which the queue's rejection already performed, and building
	// it again is not part of this milestone — so the pass performs nothing on
	// it and does not read the merge row's approval again.
	stepSentBack
	// stepHeld is an item a human held at a deploy row. The row is closed and
	// fires again only when a holder of its duty releases the hold at Work, so
	// the pass performs nothing on it and does not fire it again.
	stepHeld
	// stepEnded is an item that stops for good — dropped, escalated, or
	// superseded — and stepWaiting one whose row is pending in Work.
	stepEnded
	stepWaiting
)

func (s step) String() string {
	switch s {
	case stepSpec:
		return "spec"
	case stepPlan:
		return "implementation plan"
	case stepTasks:
		return "tasks"
	case stepImplementation:
		return "implementation"
	case stepCandidateEnvironment:
		return "deploy to candidate environment"
	case stepMerge:
		return "merge to master"
	case stepQueue:
		return "the merge queue"
	case stepSentBack:
		return "sent back by the merge queue"
	case stepHeld:
		return "held by a human"
	case stepEnded:
		return "ended"
	default:
		return "waiting in Work"
	}
}

// decided is the newest closed decision at one gate row of one subject: the
// firing as its open event recorded it, the verdict it reached, and the reason a
// rejection carries. It is read off the log because a verdict given at Work is
// written there and nowhere else, and the pass that finds it is what performs
// what it causes.
type decided struct {
	opened  gate.Opened
	closing decisionlog.Row
	verdict gate.Verdict
	reason  string
}

// subject is what the row decided over: the version at an artifact row and the
// build at an event row, which is what says whether the verdict is over what the
// item holds now or over something a later attempt replaced.
func (d decided) subject() string {
	if d.opened.ArtifactID != "" {
		return d.opened.ArtifactID
	}
	return d.opened.Subject.BuildID
}

// resumeReader is who the pass reads the log as. Reading which rows closed is
// how the component that puts an agent on a stage knows which stage to put one
// on next, so the read event names it and not the owner: no human asked.
var resumeReader = principal.OfComponent("dispatch")

// read is the log as one pass read it: every row in order, the decisions still
// pending, and the decisions that have closed. It is held for the length of a
// pass because every item of the pass asks the same questions of it — a read per
// item is quadratic in the log, which grows by a read event at every read.
//
// The pairing and the pending predicate are made here rather than through
// [decisionlog.Reader.ClosedDecisions] and [gate.Gate.Pending], which each read
// the whole log again: one read answers all three, and what the pass needs
// beyond the two is the queue's own rejection rows, which neither of those
// returns.
type read struct {
	rows    []decisionlog.Row
	pending []gate.Opened
	closed  []decisionlog.Closed
}

// readLog reads the log once and answers with what the pass already read where
// it has. [path.advance] is what sets and clears [path.logRead], so a caller
// outside a pass — a test driving one item — reads the log as it stands.
func (p *path) readLog(ctx context.Context) (*read, error) {
	if held := p.heldLog(); held != nil {
		return held, nil
	}
	rows, err := decisionlog.NewReader(p.d.pool, p.d.token).Read(ctx, resumeReader)
	if err != nil {
		return nil, err
	}
	held := &read{rows: rows}
	ended := make(map[string]bool, len(rows))
	closings := make(map[string]decisionlog.Row, len(rows))
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision {
			continue
		}
		switch row.Part {
		case decisionlog.PartClose:
			ended[row.Closes], closings[row.Closes] = true, row
		case decisionlog.PartAbandonment:
			ended[row.Closes] = true
		}
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		if closing, closed := closings[row.ID]; closed {
			held.closed = append(held.closed, decisionlog.Closed{OpenEvent: row, CloseEvent: closing})
			continue
		}
		if ended[row.ID] {
			continue
		}
		// A row this package cannot read is one some other component wrote in a
		// shape it does not know, and it is passed over the way every other
		// reader of this log treats one.
		opened, err := gate.OpenedFrom(row)
		if err != nil {
			continue
		}
		held.pending = append(held.pending, opened)
	}
	return held, nil
}

// queueSentBack is the merge queue's own rejection of this item after the Merge
// to master row that approved it: the row it was written as, the reason it gave,
// and whether it gave one.
//
// The queue's rejection is a shape of its own in the log and no gate fired for
// it — the merge row's own having closed as an approval — so what says that
// approval is spent is a rejection row after it. Without that reading the pass
// would read the approval again and re-admit an item the queue has already sent
// back.
func queueSentBack(rows []decisionlog.Row, itemID, afterCloseRow string) (row, why string, sent bool) {
	after := afterCloseRow == ""
	for _, one := range rows {
		if one.ID == afterCloseRow {
			after = true
			continue
		}
		if !after || one.Shape != decisionlog.ShapeQueueRejection {
			continue
		}
		var payload mergequeue.RejectionPayload
		if err := json.Unmarshal([]byte(one.Payload), &payload); err != nil {
			continue
		}
		if payload.Kind == mergequeue.RejectionKind && payload.ItemID == itemID {
			return one.ID, payload.Why, true
		}
	}
	return "", "", false
}

// on is one item's rows in this read: the newest closed decision per gate row,
// and the row pending on it where one is pending. What the pass needs is the
// verdict on a row, which lives in a payload the log's own store knows nothing
// about, so both answers are read out of the log rather than queried.
func (r *read) on(itemID string) (map[gate.Kind]decided, *gate.Opened) {
	var waiting *gate.Opened
	for n, open := range r.pending {
		if open.Subject.ItemID == itemID {
			waiting = &r.pending[n]
		}
	}
	return closedOn(r.closed, func(o gate.Opened) bool { return o.Subject.ItemID == itemID }), waiting
}

// closedRows is [read.on] over the log as it stands, for a caller outside a
// pass.
func (p *path) closedRows(ctx context.Context, itemID string) (map[gate.Kind]decided, *gate.Opened, error) {
	held, err := p.readLog(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, waiting := held.on(itemID)
	return rows, waiting, nil
}

// closedOn is the newest closed decision per gate row among the decisions the
// predicate selects. A row some other component wrote in a shape this pass does
// not know is passed over, the way every other reader of this log treats one.
func closedOn(closed []decisionlog.Closed, of func(gate.Opened) bool) map[gate.Kind]decided {
	rows := map[gate.Kind]decided{}
	for _, one := range closed {
		opened, err := gate.OpenedFrom(one.OpenEvent)
		if err != nil || !of(opened) {
			continue
		}
		var closing gate.ClosingPayload
		if err := json.Unmarshal([]byte(one.CloseEvent.Payload), &closing); err != nil {
			continue
		}
		rows[opened.Gate.Kind] = decided{
			opened:  opened,
			closing: one.CloseEvent,
			verdict: gate.Verdict(closing.Verdict),
			reason:  closing.Reason,
		}
	}
	return rows
}

// approvedOver reports whether the newest row of that kind approved the subject
// the item holds now. A row that approved an earlier version or an earlier build
// says nothing about this one, and an item holding no subject of that kind has
// not reached the row at all.
func approvedOver(rows map[gate.Kind]decided, kind gate.Kind, subject string) bool {
	if subject == "" {
		return false
	}
	row, closed := rows[kind]
	return closed && row.subject() == subject && row.verdict == gate.VerdictApprove
}

// heldOver is the same reading for a hold, which stops the event and leaves the
// row closed: it fires again when a holder of its duty releases the hold at
// Work, and nothing here releases one.
func heldOver(rows map[gate.Kind]decided, kind gate.Kind, subject string) bool {
	if subject == "" {
		return false
	}
	row, closed := rows[kind]
	return closed && row.subject() == subject && row.verdict == gate.VerdictHold
}

// rejectedOver is the same reading for a rejection, which is what sends the item
// back to a stage with an attempt counted there.
func rejectedOver(rows map[gate.Kind]decided, kind gate.Kind, subject string) (decided, bool) {
	if subject == "" {
		return decided{}, false
	}
	row, closed := rows[kind]
	if !closed || row.subject() != subject || row.verdict != gate.VerdictReject {
		return decided{}, false
	}
	return row, true
}

// stepFrom is where the pass enters one item, read from the item's record, the
// subjects it holds, and the rows its gates have closed. A row still pending is
// the first reading of all: nothing is performed on an item a human is deciding.
//
// The item's stage is not the reading, and cannot be: dispatch writes it when it
// puts an agent on the next stage, so an item whose Spec row has just approved
// still stands at spec. What says where the item is is which of its subjects the
// rows above it have approved.
func (p *path) stepFrom(it item.Item, c *candidate, rows map[gate.Kind]decided, waiting *gate.Opened) step {
	if waiting != nil {
		return stepWaiting
	}
	switch it.Stage {
	case item.StageDropped, item.StageEscalated, item.StageSuperseded:
		return stepEnded
	case item.StageQueued, item.StageMerged:
		return stepQueue
	}
	switch {
	case !approvedOver(rows, gate.KindSpec, c.specArtifactID):
		return stepSpec
	case !approvedOver(rows, gate.KindImplementationPlan, c.planArtifactID):
		return stepPlan
	case !approvedOver(rows, gate.KindTasks, c.tasksArtifactID):
		return stepTasks
	case !approvedOver(rows, gate.KindImplementation, c.implArtifactID):
		return stepImplementation
	}
	// Three rows send the item back to the implementation stage, and a rejection
	// at any of them over the build the item still holds is what the pass acts
	// on next, whatever the rows above approved.
	for _, kind := range []gate.Kind{gate.KindDeployToCandidateEnvironment, gate.KindMergeToMaster} {
		if _, sentBack := rejectedOver(rows, kind, c.buildID); sentBack {
			return stepImplementation
		}
	}
	if c.queueRejected {
		return stepSentBack
	}
	if heldOver(rows, gate.KindDeployToCandidateEnvironment, c.buildID) {
		return stepHeld
	}
	// The row approving is not the step done: what its approval performs is the
	// environment composed and the build put on it, and a pass that approved the
	// row and stopped left that owing.
	if !approvedOver(rows, gate.KindDeployToCandidateEnvironment, c.buildID) || c.candidateDeployBuild != c.buildID {
		return stepCandidateEnvironment
	}
	return stepMerge
}

// liveItemIDs is every item this composition may still perform a step on, in
// the order they were decomposed: an item of a service this install knows, whose
// intent exists and is refined, and which has not stopped for good.
//
// The intent's state is read here and not only at the firing because an item
// whose intent is unrefined, re-decomposing, escalated or dropped has no step to
// perform at all — the gate would refuse every firing on it — and because an
// item pointing at an intent that is not a record is not this factory's work.
func (p *path) liveItemIDs(ctx context.Context) ([]string, error) {
	all, err := item.All(ctx, p.d.pool)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, name := range p.d.serviceNames() {
		known[name] = true
	}
	var live []string
	for _, it := range all {
		switch it.Stage {
		case item.StageDropped, item.StageEscalated, item.StageSuperseded:
			continue
		}
		if it.IntentID == "" || it.ServiceID == "" {
			continue
		}
		svc, err := p.serviceOf(ctx, it.ServiceID)
		if err != nil || !known[svc.Name] {
			continue
		}
		in, err := intent.Get(ctx, p.d.pool, it.IntentID)
		if err != nil || in.State != intent.StateRefined {
			continue
		}
		live = append(live, it.ID)
	}
	return live, nil
}

// intentIDs is every intent the records name, in the order the items that were
// decomposed from them were: an intent with no item has reached no round and no
// step, so the items are where a process that holds no list of its own reads
// them from.
func (p *path) intentIDs(ctx context.Context) ([]string, error) {
	all, err := item.All(ctx, p.d.pool)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	for _, it := range all {
		if it.IntentID == "" || seen[it.IntentID] {
			continue
		}
		seen[it.IntentID] = true
		ids = append(ids, it.IntentID)
	}
	return ids, nil
}

// liveCandidates is every live item rehydrated, which is what one pass performs
// its steps over. The candidates the run already holds are rewritten in place
// rather than replaced, so what the run reports is the same value the records
// just filled and no field survives in memory that a record does not hold.
func (p *path) liveCandidates(ctx context.Context) ([]*candidate, error) {
	ids, err := p.liveItemIDs(ctx)
	if err != nil {
		return nil, err
	}
	live := make([]*candidate, 0, len(ids))
	for _, id := range ids {
		fresh, err := p.rehydrate(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("factory: reading item %s back out of the records: %w", id, err)
		}
		live = append(live, p.refreshCandidate(id, fresh))
	}
	return live, nil
}
