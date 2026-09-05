package score

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// Outcome is what the graph says became of one item. It is the score's own
// reading and not a field of any record: nothing writes down that an item turned
// out well, and what says so is a release whose window closed passed, no
// rollback naming it, no incident on it, and no rejection of it that resolved as
// a gate the factory needed.
type Outcome string

const (
	// OutcomeUnknown is an item nothing downstream has decided yet: no release,
	// a window still open, or a window that ruled nothing out. It is evidence
	// for nothing and is not counted against anything — the same distinction the
	// factor readings keep between an empty history and a resolved factor.
	OutcomeUnknown Outcome = "unknown"
	// OutcomeWell is an item whose release a window closed passed, and which
	// nothing failed or complained about.
	OutcomeWell Outcome = "well"
	// OutcomeBadly is an item a human's rejection resolved against, or one whose
	// release a window failed, or one whose release an incident was raised
	// against.
	OutcomeBadly Outcome = "badly"
)

// Firing is one closed decision as the learning pass reads it: what the opening
// row said about the change and what the close event decided. It is the pair and
// not the two rows, because every question the pass asks of a decision needs both
// — the number the score gave it and what became of it.
type Firing struct {
	OpenEvent  OpenEvent
	CloseEvent CloseEvent
	// HumanClosed is whether the close event's actor was a human, which is what
	// separates a verdict from the factory agreeing with itself.
	HumanClosed bool
	// ClosedBy is the actor that closed it, which is the human a false alarm is
	// published against.
	ClosedBy record.Actor
	// At is when the close was appended, which orders one item's firings.
	At string
}

// Evidence is every outcome the score learns from, read once. It is a value and
// not a set of methods over a pool, because the rules ask about the same records
// from different angles and a factory that read them per rule would read the
// records once per rule.
//
// What it reads is five whole tables and the whole log. That is what learning
// over every outcome costs while the store is small, and it is the same cost the
// per-author prior already carries — a query narrowed by what a payload names
// would put the payload's shape inside the log.
type Evidence struct {
	firings   []Firing
	windows   []window.Window
	rollbacks []deploy.Deploy
	incidents []incident.Incident
	items     []item.Item
	stages    []item.StageTotals
	releases  []release.Release
	// digests is the content digest of every artifact version a human decided
	// on, which is what a re-authored version is compared against.
	digests map[string]string
	// marked is every release whose rollback a human marked as not caused by the
	// release. It is read by everything here that learns and by nothing that
	// acts.
	marked map[string]bool

	releaseOfItem   map[string]release.Release
	itemOfRelease   map[string]string
	serviceOfItem   map[string]string
	windowOfRelease map[string]window.Window
	failed          map[string]bool
	skipped         map[string]bool
	incidentOn      map[string]bool
	rejectedNeeded  map[string]bool
}

// ReadEvidence reads every outcome the score learns from. It reads the log
// through token, appending one read event as the score's own component actor,
// reads the marks through marks, and writes nothing else.
func ReadEvidence(ctx context.Context, pool *pgxpool.Pool, token lease.Token, marks Marks) (*Evidence, error) {
	e := newEvidence()

	closed, err := decisionlog.NewReader(pool, token).ClosedDecisions(ctx, component)
	if err != nil {
		return nil, err
	}
	for _, d := range closed {
		var opening OpenEvent
		var closing CloseEvent
		if json.Unmarshal([]byte(d.OpenEvent.Payload), &opening) != nil ||
			json.Unmarshal([]byte(d.CloseEvent.Payload), &closing) != nil {
			// A payload this package cannot read is a row some other component
			// wrote in a shape it does not know, which is not an outcome and is
			// not an error either.
			continue
		}
		e.firings = append(e.firings, Firing{
			OpenEvent: opening, CloseEvent: closing,
			HumanClosed: d.CloseEvent.Actor.Kind == record.KindHuman,
			ClosedBy:    d.CloseEvent.Actor, At: d.CloseEvent.At,
		})
	}

	if e.windows, err = window.Closed(ctx, pool); err != nil {
		return nil, err
	}
	if e.rollbacks, err = deploy.Rollbacks(ctx, pool); err != nil {
		return nil, err
	}
	if e.incidents, err = incident.All(ctx, pool); err != nil {
		return nil, err
	}
	if e.items, err = item.All(ctx, pool); err != nil {
		return nil, err
	}
	if e.stages, err = item.AllStages(ctx, pool); err != nil {
		return nil, err
	}
	if e.releases, err = release.All(ctx, pool); err != nil {
		return nil, err
	}
	if e.marked, err = markedSet(ctx, marks); err != nil {
		return nil, err
	}
	if err := e.readDigests(ctx, pool); err != nil {
		return nil, err
	}

	e.index()
	return e, nil
}

// newEvidence is an empty [Evidence] with its maps made.
func newEvidence() *Evidence {
	return &Evidence{
		digests:         map[string]string{},
		marked:          map[string]bool{},
		releaseOfItem:   map[string]release.Release{},
		itemOfRelease:   map[string]string{},
		serviceOfItem:   map[string]string{},
		windowOfRelease: map[string]window.Window{},
		failed:          map[string]bool{},
		skipped:         map[string]bool{},
		incidentOn:      map[string]bool{},
		rejectedNeeded:  map[string]bool{},
	}
}

// readDigests reads the content digest of every artifact version a human decided
// on. A rejection resolves by what the re-authored version differs in, and a
// digest is what says two versions are not the same text.
func (e *Evidence) readDigests(ctx context.Context, pool *pgxpool.Pool) error {
	for _, f := range e.firings {
		id := f.OpenEvent.ArtifactID
		if !f.HumanClosed || id == "" {
			continue
		}
		if _, read := e.digests[id]; read {
			continue
		}
		a, err := artifact.Get(ctx, pool, id)
		if err != nil {
			// A decision naming a version the store no longer holds is a
			// truncated trail and not an error here: the rejection it belongs to
			// stays unresolved, which moves nothing.
			continue
		}
		e.digests[id] = a.ContentDigest
	}
	return nil
}

// index builds what the rules read across the tables: which release an item has,
// which window watched it, which releases a rollback or an incident named, and
// which rejections resolved as gates the factory needed. It is a step of its own
// so that a test can assemble the slices and index them the way a read of the
// store does, rather than filling the maps by hand and getting one of them wrong.
func (e *Evidence) index() {
	for _, it := range e.items {
		e.serviceOfItem[it.ID] = it.ServiceID
	}
	for _, r := range e.releases {
		e.releaseOfItem[r.ItemID] = r
		e.itemOfRelease[r.ID] = r.ItemID
	}
	for _, w := range e.windows {
		e.windowOfRelease[w.ReleaseID] = w
	}
	for _, d := range e.rollbacks {
		if e.marked[d.Undoing.FailedReleaseID] {
			continue
		}
		e.failed[d.Undoing.FailedReleaseID] = true
		for _, id := range d.Undoing.SkippedReleaseIDs {
			e.skipped[id] = true
		}
	}
	for _, i := range e.incidents {
		e.incidentOn[i.ReleaseID] = true
	}
	for _, r := range e.resolvedRejections() {
		if r.MovesTheThreshold() {
			e.rejectedNeeded[r.ItemID] = true
		}
	}
}

// Outcome is what became of one item. A rejection that resolved as a gate the
// factory needed, a window that failed its release, and an incident against its
// release are each enough to make it badly; a release whose window closed passed
// and none of those makes it well; anything else is unknown.
//
// timed out and skipped are neither, and that is the whole of the rule: a
// comparison the cap closed unresolved and a release a rollback undid before
// anything measured it each report no outcome. Counting one as good would put
// back exactly what the sample was built to remove, silence read as success.
func (e *Evidence) Outcome(itemID string) Outcome {
	if itemID == "" {
		return OutcomeUnknown
	}
	if e.rejectedNeeded[itemID] {
		return OutcomeBadly
	}
	r, released := e.releaseOfItem[itemID]
	if !released {
		return OutcomeUnknown
	}
	if e.failed[r.ID] || e.incidentOn[r.ID] {
		return OutcomeBadly
	}
	w, watched := e.windowOfRelease[r.ID]
	if !watched || w.Open() || w.Exit != window.ExitPassed {
		return OutcomeUnknown
	}
	return OutcomeWell
}

// falsePasses is every window of one service that closed passed over a release
// an incident was later raised against: the crossing the health monitor could
// have seen and did not, on a window that said it had ruled a regression out. It
// is evidence about the power, the exit that rules a regression out having
// cleared one it should have caught.
//
// A window a mark excluded is not one of these: what crossed the boundary was
// not the change, which is evidence about nothing.
func (e *Evidence) falsePasses(serviceID string) []window.Window {
	var missed []window.Window
	for _, w := range e.windows {
		if w.ServiceID == serviceID && w.Exit == window.ExitPassed &&
			e.incidentOn[w.ReleaseID] && !e.marked[w.ReleaseID] {
			missed = append(missed, w)
		}
	}
	return missed
}

// missesOnATimedOutWindow is every window of one service that timed out over a
// release an incident was later raised against. Nothing was ruled out at all
// there, so the event is evidence about the size and never about the power.
func (e *Evidence) missesOnATimedOutWindow(serviceID string) []window.Window {
	var missed []window.Window
	for _, w := range e.windows {
		if w.ServiceID == serviceID && w.Exit == window.ExitTimedOut &&
			e.incidentOn[w.ReleaseID] && !e.marked[w.ReleaseID] {
			missed = append(missed, w)
		}
	}
	return missed
}

// Services is every service any evidence names, ordered so that two passes over
// one store produce the same table. A service with no closed window and no
// rollback is not here, and nothing is supplied for it beyond the starting value.
func (e *Evidence) Services() []string {
	seen := map[string]bool{}
	for _, w := range e.windows {
		seen[w.ServiceID] = true
	}
	for _, d := range e.rollbacks {
		seen[d.ServiceID] = true
	}
	return sorted(seen)
}

// Areas is every area the items name, ordered. An item decomposed with no area
// declared names none, and those are left out: the item-size target is a field
// of an area record, so there is nothing for a value with no area to be supplied
// for.
func (e *Evidence) Areas() []string {
	seen := map[string]bool{}
	for _, it := range e.items {
		if it.AreaID != "" {
			seen[it.AreaID] = true
		}
	}
	return sorted(seen)
}

// Stages is every stage any item has reported an attempt at, ordered.
func (e *Evidence) Stages() []item.Stage {
	seen := map[string]bool{}
	for _, s := range e.stages {
		seen[string(s.Stage)] = true
	}
	var stages []item.Stage
	for _, s := range sorted(seen) {
		stages = append(stages, item.Stage(s))
	}
	return stages
}

// GateRows is every gate row a closed decision names, ordered.
func (e *Evidence) GateRows() []string {
	seen := map[string]bool{}
	for _, f := range e.firings {
		if f.OpenEvent.Gate != "" {
			seen[f.OpenEvent.Gate] = true
		}
	}
	return sorted(seen)
}
