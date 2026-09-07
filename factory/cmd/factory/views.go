package main

import (
	"context"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
)

// views is [screens.Views] over the composition: every read the four screens
// make, answered from the records this process already composes readers for.
// Package screens imports no record package — what crosses that seam is the
// screen's own view of a record and never the record — so the mapping is here,
// in the one place that already imports most of the module.
//
// Nothing here writes. A view that reads the log reads it as the calling
// principal, so the read event names the human at the screen and not a
// component: no pass asked.
type views struct{ p *path }

var _ screens.Views = (*views)(nil)

// sinceTheInstall is the one factory-owned span every number on Factory and
// the digest are computed over: the life of the install. Nobody authors it,
// which is what the design asks of that span, and an empty lower bound is
// every row, every timestamp being fixed-width UTC text that sorts as a time.
const sinceTheInstall = ""

// Home is the home view: the badge and its parts, a row per last check record
// past the interval it carries with a further pass owed, the readiness reading
// per role, and the digest where the badge is zero.
func (v *views) Home(ctx context.Context, who principal.Principal) (screens.Home, error) {
	// The readiness reading is taken once and handed to the badge: it reads two
	// records and the log, and a role with no matching entry reaches the badge,
	// so a second reading would be the same three reads again.
	reading, err := v.p.readiness(ctx)
	if err != nil {
		return screens.Home{}, err
	}
	badge, err := v.badge(ctx, reading)
	if err != nil {
		return screens.Home{}, err
	}
	checks, err := v.lastChecks(ctx)
	if err != nil {
		return screens.Home{}, err
	}
	rows := make([]screens.RoleReadiness, 0, len(reading))
	for _, one := range reading {
		rows = append(rows, screens.RoleReadiness{
			Role:                          string(one.role),
			EntryCovers:                   one.entry,
			RolePromptInForce:             one.rolePrompt,
			OldestUnmatchedHoldAgeSeconds: int64(one.oldestUnmatched / time.Second),
		})
	}
	home := screens.Home{Badge: badge, LastChecks: checks, Readiness: rows}
	if badge.Total != 0 {
		return home, nil
	}
	digest, err := v.digest(ctx, who)
	if err != nil {
		return screens.Home{}, err
	}
	home.Digest = &digest
	return home, nil
}

// badge is what waits on a human, counted into the parts the design lists. A
// UAT assignment is counted as one and not twice: the pending rows at Merge to
// master are that part, and PendingGates is every other pending row, so the
// total is the sum of the parts.
//
// reading is the readiness the caller has already taken, a role with no
// matching entry being a wait on a human and so a part of the badge.
func (v *views) badge(ctx context.Context, reading []roleReadiness) (screens.Badge, error) {
	var badge screens.Badge
	pending, err := v.p.gate.Pending(ctx)
	if err != nil {
		return badge, err
	}
	for _, opened := range pending {
		if !opened.HumanDecides {
			continue
		}
		if opened.Gate.Kind == gate.KindMergeToMaster {
			badge.UATAssignments++
			continue
		}
		badge.PendingGates++
	}

	questions, err := v.openQuestions(ctx)
	if err != nil {
		return badge, err
	}
	badge.InterviewQuestions = int64(len(questions))

	escalated, err := v.escalations(ctx)
	if err != nil {
		return badge, err
	}
	badge.Escalations = escalated

	held, _, err := v.p.dispatch.Open(ctx)
	if err != nil {
		return badge, err
	}
	for _, one := range held {
		switch one.Condition {
		case dispatch.HoldNoEntryCoversTheStage, dispatch.HoldNoRolePromptInForce,
			dispatch.HoldCredentialUnreachable, dispatch.HoldCredentialAtCeiling:
			badge.FactoryHoldsForAHuman++
		case dispatch.HoldConstraintRequiresSeam5:
			badge.ConstraintCausedStops++
		}
	}

	// A role no entry covers is one of the factory's own holds — a fleet entry
	// that does not exist is a record only a human writes — and it is counted
	// once: where dispatch has already written a hold naming that role the row
	// above counted it, and where nothing has been dispatched yet the readiness
	// row is the only reading of the same wait.
	for _, one := range reading {
		if !one.entry && one.oldestUnmatched == 0 {
			badge.FactoryHoldsForAHuman++
		}
	}

	badge.Total = badge.PendingGates + badge.UATAssignments + badge.InterviewQuestions +
		badge.Escalations + badge.FactoryHoldsForAHuman + badge.ConstraintCausedStops
	return badge, nil
}

// openQuestions is every round of the interview and every acceptance round
// waiting on an answer, on an intent still live. A question on a dropped or
// delivered intent waits on nobody.
func (v *views) openQuestions(ctx context.Context) ([]intent.Question, error) {
	ids, err := v.intentIDs(ctx)
	if err != nil {
		return nil, err
	}
	var open []intent.Question
	for _, id := range ids {
		in, err := intent.Get(ctx, v.p.d.pool, id)
		if err != nil {
			return nil, err
		}
		if in.State == intent.StateDropped || in.State == intent.StateDelivered {
			continue
		}
		questions, err := intent.Questions(ctx, v.p.d.pool, id)
		if err != nil {
			return nil, err
		}
		for _, q := range questions {
			if !q.Answered() {
				open = append(open, q)
			}
		}
	}
	return open, nil
}

// intentIDs is every intent the records name, read off the intents themselves
// where they have items and off the intent records otherwise. [path.intentIDs]
// reads them off the items alone, which is what a pass needs; a screen has to
// show an intent taken in at Work before decomposition has run.
func (v *views) intentIDs(ctx context.Context) ([]string, error) {
	fromItems, err := v.p.intentIDs(ctx)
	if err != nil {
		return nil, err
	}
	waiting, err := intent.InProject(ctx, v.p.d.pool, v.p.projectID)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	ids := make([]string, 0, len(fromItems)+len(waiting))
	for _, id := range fromItems {
		if !seen[id] {
			seen[id], ids = true, append(ids, id)
		}
	}
	for _, in := range waiting {
		if !seen[in.ID] {
			seen[in.ID], ids = true, append(ids, in.ID)
		}
	}
	return ids, nil
}

// escalations is what the factory admits it is stuck on: an item at the
// escalated stage, and an intent the interview's or decomposition's limit
// escalated.
func (v *views) escalations(ctx context.Context) (int64, error) {
	items, err := item.All(ctx, v.p.d.pool)
	if err != nil {
		return 0, err
	}
	var count int64
	for _, it := range items {
		if it.Stage == item.StageEscalated {
			count++
		}
	}
	ids, err := v.intentIDs(ctx)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		in, err := intent.Get(ctx, v.p.d.pool, id)
		if err != nil {
			return 0, err
		}
		if in.State == intent.StateEscalated {
			count++
		}
	}
	return count, nil
}

// lastChecks is a row per last check record past the interval it carries with a
// further pass owed, read one record at a time and never aggregated over a
// class — the health monitor still checking one service and stopped on another
// is what the aggregate hides. The drift detector's own records are read from
// its own store beside them.
func (v *views) lastChecks(ctx context.Context) ([]screens.LastCheck, error) {
	now := time.Now()
	stale, err := lastcheck.Stale(ctx, v.p.d.pool, now)
	if err != nil {
		return nil, err
	}
	rows := make([]screens.LastCheck, 0, len(stale))
	for _, one := range stale {
		if !one.FurtherPassOwed() {
			continue
		}
		rows = append(rows, screens.LastCheck{
			Component:       one.Component,
			Checks:          one.Subject,
			LastPass:        one.CheckedAt,
			IntervalSeconds: int64(one.Interval / time.Second),
			FurtherPassOwed: true,
		})
	}
	if v.p.d.driftdetector == nil {
		return rows, nil
	}
	own, err := driftdetector.LastChecks(ctx, v.p.d.driftdetector, "")
	if err != nil {
		return nil, err
	}
	for _, one := range own {
		past, err := one.Stale(now)
		if err != nil {
			return nil, err
		}
		if !past || !one.FurtherPassOwed {
			continue
		}
		rows = append(rows, screens.LastCheck{
			Component:       "drift detector",
			Checks:          one.Target,
			LastPass:        one.At,
			IntervalSeconds: int64(one.Interval / time.Second),
			FurtherPassOwed: true,
		})
	}
	return rows, nil
}

// digest is what shipped, was decided, and was auto-approved over the one
// factory-owned span. It is the part of the home view that appears only at
// zero, so an empty screen means the factory is working.
func (v *views) digest(ctx context.Context, who principal.Principal) (screens.Digest, error) {
	releases, err := release.All(ctx, v.p.d.pool)
	if err != nil {
		return screens.Digest{}, err
	}
	closed, err := decisionlog.NewReader(v.p.d.pool, v.p.d.token).ClosedDecisions(ctx, who)
	if err != nil {
		return screens.Digest{}, err
	}
	digest := screens.Digest{Releases: int64(len(releases)), Decisions: int64(len(closed))}
	for _, one := range closed {
		closing, err := verdictOn(one.CloseEvent)
		if err != nil {
			continue
		}
		if closing.WhyItAutoPassed != "" {
			digest.AutoApprovals++
		}
	}
	return digest, nil
}

// Work is the board: one row per live item, narrowed to what waits on a human
// where the filter asks for it. Nothing here sorts what it renders — ordering
// is the client's.
func (v *views) Work(ctx context.Context, _ principal.Principal, filter screens.Filter) (screens.Work, error) {
	pending, err := v.p.gate.Pending(ctx)
	if err != nil {
		return screens.Work{}, err
	}
	held, _, err := v.p.dispatch.Open(ctx)
	if err != nil {
		return screens.Work{}, err
	}
	items, err := item.All(ctx, v.p.d.pool)
	if err != nil {
		return screens.Work{}, err
	}

	var board screens.Work
	for _, it := range items {
		switch it.Stage {
		case item.StageDropped, item.StageSuperseded:
			continue
		}
		row := screens.WorkRow{ItemID: it.ID, Stage: string(it.Stage), Priority: int64(it.Priority)}
		for _, opened := range pending {
			if opened.Subject.ItemID != it.ID {
				continue
			}
			row.Waiting = fmt.Sprintf("%s waits on %s", opened.Gate, waitedOn(opened.WaitsOn))
		}
		if it.Stage == item.StageEscalated && row.Waiting == "" {
			row.Waiting = "the factory gave up on this item and duty 12 takes it over"
		}
		row.Stop = stopOnItem(held, it.ID)
		// The home view's filter is what waits on a human, which is the same
		// reading the badge counts: a row pending at a gate, or a stop whose
		// named cause is a record only a human writes — which is what a stop
		// naming where at Factory it is lifted is. A stop the factory lifts
		// itself, an intent's own state among them, waits on nobody.
		if filter.WaitingOnAHuman && row.Waiting == "" &&
			(row.Stop == nil || row.Stop.LiftedAt == "") {
			continue
		}
		board.Rows = append(board.Rows, row)
	}
	return board, nil
}

// stopOnItem is the stop dispatch wrote onto one item, and nil where nothing
// holds it. Where the cause is a record only a human writes, the row carries
// the Factory address the link points at.
func stopOnItem(held []dispatch.Hold, itemID string) *screens.DispatchStop {
	for _, one := range held {
		if one.ItemID != itemID {
			continue
		}
		return &screens.DispatchStop{Cause: one.Condition, LiftedAt: liftedAt(one)}
	}
	return nil
}

// liftedAt is where at Factory the record that lifts one hold is written, and
// empty for the one cause no owner's write lifts — an intent's own state, which
// is lifted at Work on the intent.
func liftedAt(one dispatch.Hold) string {
	switch one.Condition {
	case dispatch.HoldNoEntryCoversTheStage, dispatch.HoldNoRolePromptInForce:
		return "/factory/fleet"
	case dispatch.HoldCredentialUnreachable, dispatch.HoldCredentialAtCeiling:
		return "/people"
	case dispatch.HoldConstraintRequiresSeam5:
		return "/factory/constraints"
	default:
		return ""
	}
}

// nameOf is one per-person key as a screen shows it: the name the People
// mapping gives it, and the key itself where the mapping was erased or never
// written — a key with no name is not an error, the mapping being the one
// record an erasure reaches and the holding it left behind still routing.
//
// Which of the two a view carries follows from the field: a field named for
// the key carries the key, every other human-facing field carries the name, and
// [screens.PersonRow] carries the pair because People is where the mapping is.
// So no view holds a name the mapping does not give it, and nothing resolves a
// name twice.
func (v *views) nameOf(ctx context.Context, key string) string {
	if key == "" {
		return ""
	}
	name, err := people.NameOf(ctx, v.p.d.pool, key)
	if err != nil {
		return key
	}
	return name
}
