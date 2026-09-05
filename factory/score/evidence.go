package score

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

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
// out well, and what says so is a release with a window that closed without
// harm, no rollback naming it, no incident on it, and no human having rejected it
// anywhere.
type Outcome string

const (
	// OutcomeUnknown is an item nothing downstream has decided yet: no release,
	// or a window still open. It is evidence for nothing and is not counted
	// against anything — the same distinction the factor readings keep between an
	// empty history and an unavailable factor.
	OutcomeUnknown Outcome = "unknown"
	// OutcomeWell is an item that shipped and was neither failed nor
	// complained about.
	OutcomeWell Outcome = "well"
	// OutcomeBadly is an item a human rejected, or one whose release a rollback
	// failed, or one whose release an incident was raised against.
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
}

// Evidence is every outcome the score learns from, read once. It is a value and
// not a set of methods over a pool, because the six rules ask about the same
// records from different angles and a factory that read them per rule would read
// the log six times.
//
// What it reads is five whole tables and the whole log. That is what learning over
// every outcome costs while the store is small, and it is the same cost the
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

	releaseOfItem   map[string]release.Release
	windowOfRelease map[string]window.Window
	failed          map[string]bool
	swept           map[string]bool
	incidentOn      map[string]bool
	rejected        map[string]bool
}

// ReadEvidence reads every outcome the score learns from. It reads the log
// through token, appending one read event as the score's own component actor,
// and writes nothing else.
func ReadEvidence(ctx context.Context, pool *pgxpool.Pool, token lease.Token) (*Evidence, error) {
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
		f := Firing{OpenEvent: opening, CloseEvent: closing, HumanClosed: d.CloseEvent.Actor.Kind == record.KindHuman}
		e.firings = append(e.firings, f)
		if f.HumanClosed && closing.Verdict == VerdictRejected && opening.ItemID != "" {
			e.rejected[opening.ItemID] = true
		}
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

	e.index()
	return e, nil
}

// newEvidence is an empty [Evidence] with its maps made.
func newEvidence() *Evidence {
	return &Evidence{
		releaseOfItem:   map[string]release.Release{},
		windowOfRelease: map[string]window.Window{},
		failed:          map[string]bool{},
		swept:           map[string]bool{},
		incidentOn:      map[string]bool{},
		rejected:        map[string]bool{},
	}
}

// index builds what the rules read across the tables: which release an item has,
// which window watched it, and which releases a rollback or an incident named. It
// is a step of its own so that a test can assemble the slices and index them the
// way a read of the store does, rather than filling six maps by hand and getting
// one of them wrong.
func (e *Evidence) index() {
	for _, r := range e.releases {
		e.releaseOfItem[r.ItemID] = r
	}
	for _, w := range e.windows {
		e.windowOfRelease[w.ReleaseID] = w
	}
	for _, d := range e.rollbacks {
		e.failed[d.Undoing.FailedReleaseID] = true
		for _, id := range d.Undoing.SweptReleaseIDs {
			e.swept[id] = true
		}
	}
	for _, i := range e.incidents {
		e.incidentOn[i.ReleaseID] = true
	}
}

// Outcome is what became of one item. A rejection by a human, a rollback that
// failed its release, and an incident against its release are each enough to
// make it badly; a release whose window closed without failing a release and none of those
// makes it well; anything else is unknown.
//
// A swept release is neither. Its own health monitor stopped because a rollback aimed
// below it undid it, so nothing was ever decided about the change — which is the
// same reading [window.Exit.Counts] gives that exit one level down.
func (e *Evidence) Outcome(itemID string) Outcome {
	if itemID == "" {
		return OutcomeUnknown
	}
	if e.rejected[itemID] {
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
	if !watched || w.Open() || !w.Exit.Counts() {
		return OutcomeUnknown
	}
	return OutcomeWell
}

// Misses is every window of one service that closed without failing a
// release over a release an incident was later raised against. That is the
// crossing the health monitor could have seen and did not: the window said it
// was done watching and the same quantity crossed afterwards, so the size it
// was watching at was too coarse.
//
// A rollback is not one of these and cannot be. The health monitor rolls a
// release back at the failed exit and nowhere else, so a rollback the
// factory performed always has a failed window under it and never a window
// that closed without failing a release; what happens outside a window is an
// incident and an item. A human's undo is not one either: the design counts
// only evidence traceable to the health monitor here, a human's reason is
// prose, and the factory does not judge prose — so an undo moves the per-author
// prior and not the window's size.
func (e *Evidence) Misses(serviceID string) []window.Window {
	var missed []window.Window
	for _, w := range e.windows {
		if w.ServiceID == serviceID && w.Exit.Counts() && e.incidentOn[w.ReleaseID] {
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

// Areas is every area the items name, ordered. An item decomposed with no area declared
// names none, and those are left out: the item-size target is a field of an area
// record, so there is nothing for a value with no area to be supplied for.
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

// serviceHistory is one service's closed windows and rollbacks in the order they
// happened, which is what the window limit is folded over. A window is placed at the time it
// closed and a rollback at the time its record was written, because what each is
// evidence about is the event and not the deploy that led to it.
type serviceEvent struct {
	at string
	// noHarm is a window that closed leaving a release the factory can return to.
	noHarm bool
	// sweeping is a rollback that undid more than the release it failed.
	sweeping bool
}

func (e *Evidence) serviceHistory(serviceID string) []serviceEvent {
	var events []serviceEvent
	for _, w := range e.windows {
		if w.ServiceID == serviceID && w.Exit.Counts() {
			events = append(events, serviceEvent{at: w.ClosedAt, noHarm: true})
		}
	}
	for _, d := range e.rollbacks {
		if d.ServiceID == serviceID && len(d.Undoing.SweptReleaseIDs) > 0 {
			events = append(events, serviceEvent{at: d.At, sweeping: true})
		}
	}
	sort.Slice(events, func(i, j int) bool { return events[i].at < events[j].at })
	return events
}

// Traffic is what one service's windows say about the traffic it actually
// receives: the units its release served per second, and the failure rate of the
// baseline it was read against. Both come off the freshest closed window that
// carries a read, because traffic changes and the newest reading is the honest
// estimate of what the next window will get.
//
// It is what says whether a size the score is asking for is reachable at all —
// which is arithmetic over volume and not a claim about harm, so it is evidence
// the analysis window's harm-only restriction does not speak to.
type Traffic struct {
	// UnitsPerSecond is what the release under watch served, over the seconds its
	// window was open.
	UnitsPerSecond float64
	// BaselineRate is the failure rate of the release it was read against, which is
	// what the units a window needs are computed at.
	BaselineRate float64
}

// traffic is [Traffic] for one service, and false where no closed window of it
// carries a read with a baseline in it. A service whose windows have never had a
// baseline has never had the passed exit available to them either, so there is
// nothing for
// reachability to constrain.
func (e *Evidence) traffic(serviceID string) (Traffic, bool, error) {
	for i := len(e.windows) - 1; i >= 0; i-- {
		w := e.windows[i]
		if w.ServiceID != serviceID || w.ClosedOn.Units <= 0 || w.ClosedOn.BaselineUnits <= 0 {
			continue
		}
		opened, err := record.ParseTime(w.At)
		if err != nil {
			return Traffic{}, false, fmt.Errorf("score: reading when window %s opened: %w", w.ID, err)
		}
		closed, err := record.ParseTime(w.ClosedAt)
		if err != nil {
			return Traffic{}, false, fmt.Errorf("score: reading when window %s closed: %w", w.ID, err)
		}
		seconds := closed.Sub(opened).Seconds()
		if seconds <= 0 {
			// A window opened and closed inside one timestamp says nothing about a
			// rate. It is not an error: the next one down will.
			continue
		}
		return Traffic{
			UnitsPerSecond: float64(w.ClosedOn.Units) / seconds,
			BaselineRate:   float64(w.ClosedOn.BaselineFailures) / float64(w.ClosedOn.BaselineUnits),
		}, true, nil
	}
	return Traffic{}, false, nil
}

// reachedStage is how many items have reported an attempt at one stage. It is the
// evidence count the attempt limit's own rule needs: one item that got past a
// stage first time is not grounds for supplying a limit the whole factory reads.
func (e *Evidence) reachedStage(stage item.Stage) int {
	n := 0
	for _, s := range e.stages {
		if s.Stage == stage {
			n++
		}
	}
	return n
}

// resolvedIn is how long each window of one service took to close on evidence —
// passed or failed, the two exits that are a reading of the quantity rather than a
// clock running out. It is what the cap is set above: a cap under the time a
// window of this service actually needed closes unresolved a window that would
// have resolved.
func (e *Evidence) resolvedIn(serviceID string) ([]time.Duration, error) {
	var took []time.Duration
	for _, w := range e.windows {
		if w.ServiceID != serviceID || (w.Exit != window.ExitPassed && w.Exit != window.ExitFailed) {
			continue
		}
		opened, err := record.ParseTime(w.At)
		if err != nil {
			return nil, fmt.Errorf("score: reading when window %s opened: %w", w.ID, err)
		}
		closed, err := record.ParseTime(w.ClosedAt)
		if err != nil {
			return nil, fmt.Errorf("score: reading when window %s closed: %w", w.ID, err)
		}
		took = append(took, closed.Sub(opened))
	}
	return took, nil
}

// stalls is every item of one area whose attempts at a stage reached the limit
// the score supplies for that stage and which has no release: work spent and
// thrown away, which is what a decomposition too coarse shows as.
//
// It reads the limit the score itself supplies and not the limit in force, which
// is package policy's read and would make this package a reader of what an owner
// authored. The two agree on a factory where nobody authored one; where an owner
// authored a different limit, the score is counting against its own default and
// the reason each moved value carries says so.
func (e *Evidence) stalls(areaID string, limit func(item.Stage) float64) []item.StageTotals {
	inArea := map[string]bool{}
	for _, it := range e.items {
		// A superseded item is left out of both this and succeededAt. It was
		// replaced by a re-decomposition rather than given up on, so what was spent on it says
		// something about the decomposition that rejected the set and nothing about the size
		// decomposition was aiming at.
		if it.AreaID == areaID && it.Stage != item.StageSuperseded {
			inArea[it.ID] = true
		}
	}
	var stalled []item.StageTotals
	for _, s := range e.stages {
		if !inArea[s.ItemID] {
			continue
		}
		if _, released := e.releaseOfItem[s.ItemID]; released {
			continue
		}
		if float64(s.Attempts) >= limit(s.Stage) {
			stalled = append(stalled, s)
		}
	}
	return stalled
}

// succeededAt is the highest attempt at which one stage produced work that got
// past it: the attempts recorded against a stage of an item that is no longer at
// that stage. Attempts accumulate and are never reset, so the number on the row
// of a stage the item has left is how many attempts that stage took.
func (e *Evidence) succeededAt(stage item.Stage) int {
	at := map[string]item.Stage{}
	for _, it := range e.items {
		at[it.ID] = it.Stage
	}
	highest := 0
	for _, s := range e.stages {
		// An item still at the stage has not got past it, and a superseded item
		// never will: a re-decomposition replaced it, so its attempts are not a retry that
		// worked.
		if s.Stage != stage || at[s.ItemID] == stage || at[s.ItemID] == item.StageSuperseded {
			continue
		}
		if s.Attempts > highest {
			highest = s.Attempts
		}
	}
	return highest
}

func sorted(seen map[string]bool) []string {
	var out []string
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
