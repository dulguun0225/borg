package score

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// ThresholdRealized is the realized auto-pass rate at one risk threshold, over
// one factor set, as [RealizedAutoPass] reads it.
//
// RecordedRate is left at zero here and is the caller's to fill: it is the
// rate the policy version that set this threshold froze at the write, read
// back by matching [ThresholdRealized.FactorSet] against the rows package
// policy's own reader returns for this Subject. It is a field the caller
// fills rather than a map argument keyed by policy's own scope, because this
// package does not import policy — deps.txt states the direction the other
// way, policy -> score — and a map keyed by policy's Scope type would need
// that import too.
type ThresholdRealized struct {
	FactorSet FactorSet
	// Subject is the gate row the threshold governs — [Supplied]'s own doc
	// comment states that a risk threshold's subject is the row and not the
	// environment its authored value is a field of, and this is that same
	// key.
	Subject   string
	Threshold float64
	// Decisions is every closed decision at this factor set, this row and
	// this threshold, since the span [RealizedAutoPass] was asked over —
	// what the threshold governed, whichever verdict closed it.
	Decisions int
	// RealizedRate is the share of Decisions the factory approved on its
	// own because the number read under the threshold: [AutoPassThreshold],
	// and not [AutoPassSample], which is the held-out sample overriding a
	// would-gate decision rather than the threshold clearing it.
	RealizedRate float64
	// RecordedRate is the rate recorded on the policy version that set this
	// threshold; see the type's doc comment for why it is the caller's to
	// fill.
	RecordedRate float64
}

// RealizedAutoPass is the realized auto-pass rate at each risk threshold in
// force, per factor set and per gate row, over every decision closed since
// since. Reading it beside the rate a policy version recorded at the write —
// which the caller supplies onto [ThresholdRealized.RecordedRate] — is what
// [_What the factory auto-approved, and what was undone_] uses to tell a
// score that got better from one whose meaning moved under the threshold an
// owner set.
//
// p is the principal the read is made as, recorded on the log's own read
// event. It is the caller's and never [componentPrincipal]: this is a
// screen's own read and not an internal pass, so the human or agent asking
// is who the log names.
//
// ../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
func RealizedAutoPass(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	p principal.Principal, since string) ([]ThresholdRealized, error) {

	firings, err := readClosedFirings(ctx, pool, token, p)
	if err != nil {
		return nil, err
	}

	type key struct {
		set       FactorSet
		subject   string
		threshold float64
	}
	rows := map[key]*ThresholdRealized{}
	for _, f := range firings {
		if f.At < since {
			continue
		}
		k := key{set: f.OpenEvent.FactorSet, subject: f.OpenEvent.Gate, threshold: f.OpenEvent.Threshold}
		row := rows[k]
		if row == nil {
			row = &ThresholdRealized{FactorSet: k.set, Subject: k.subject, Threshold: k.threshold}
			rows[k] = row
		}
		row.Decisions++
		if f.CloseEvent.WhyItAutoPassed == AutoPassThreshold {
			row.RealizedRate++
		}
	}

	published := make([]ThresholdRealized, 0, len(rows))
	for _, row := range rows {
		row.RealizedRate /= float64(row.Decisions)
		published = append(published, *row)
	}
	sort.Slice(published, func(i, j int) bool {
		if published[i].FactorSet != published[j].FactorSet {
			return published[i].FactorSet < published[j].FactorSet
		}
		if published[i].Subject != published[j].Subject {
			return published[i].Subject < published[j].Subject
		}
		return published[i].Threshold < published[j].Threshold
	})
	return published, nil
}

// BandOutcome is one band of the number, factory-wide, as [HeldOutByBand]
// reads it. It is [Band] without the per-service breakdown, which
// [_What the factory auto-approved, and what was undone_] does not ask for:
// that screen reads the bands per factor set alone.
type BandOutcome struct {
	FactorSet FactorSet
	From      float64
	To        float64
	// Windows is how many held-out releases' windows resolved inside this
	// band since the span [HeldOutByBand] was asked over.
	Windows int
	// FailedShare is what share of Windows failed.
	FailedShare float64
}

// HeldOutByBand is the share of held-out releases whose windows failed within
// each band of the number, per factor set, over every held-out release whose
// window closed since since. It reads the held-out selections off the log's
// open events and the window outcomes off [window.ClosedAtTheVersionInForce]
// the way [Learn]'s own pass does, through [Evidence.bands] — the same bands
// [bandWidth] divides the scale into — rather than restating that arithmetic.
//
// It excludes a release a human marked as not caused by the release, the same
// exclusion [Learn]'s own pass applies: the screen's number is read against
// the same evidence the score learns from, so the two have to agree. The mark
// is read through [markedReleases], the same join a [Marks] implementation
// makes, rather than through that interface: this read is made for a screen
// and not for the pass, and the interface exists so the pass does not import
// [deploy] on its own account, which this file already does for other reasons.
//
// p is the principal the read is made as, recorded on the log's own read
// event, the same reason [RealizedAutoPass] takes one.
//
// ../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
func HeldOutByBand(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	p principal.Principal, since string) ([]BandOutcome, error) {

	firings, err := readClosedFirings(ctx, pool, token, p)
	if err != nil {
		return nil, err
	}
	releases, err := release.All(ctx, pool)
	if err != nil {
		return nil, err
	}
	windows, err := window.ClosedAtTheVersionInForce(ctx, pool)
	if err != nil {
		return nil, err
	}
	marked, err := markedReleases(ctx, pool)
	if err != nil {
		return nil, err
	}

	var kept []window.Window
	for _, w := range windows {
		if w.ClosedAt >= since {
			kept = append(kept, w)
		}
	}

	e := newEvidence()
	e.firings = firings
	e.releases = releases
	e.windows = kept
	e.marked = marked
	e.index()

	var published []BandOutcome
	for _, b := range e.bands() {
		if b.Service != "" {
			// The per-service rows are [Evidence.bands]'s own breakdown, not
			// asked for here: this report is factory-wide per factor set.
			continue
		}
		published = append(published, BandOutcome{
			FactorSet: b.FactorSet, From: b.From, To: b.To,
			Windows: b.Windows, FailedShare: b.FailedShare,
		})
	}
	return published, nil
}

// markedReleases is every release whose rollback a named human at Ops marked
// as not caused by the release, keyed for [Evidence.marked]. The mark is
// package window's, over the rollback's own deploy record, and the release
// this excludes is that record's failed release — the same join
// [cmd/factory]'s own [Marks] implementation performs, composed here instead
// because this package already imports [deploy] and [window] and a screen's
// read is not the pass [markedSet] serves.
//
// A mark whose deploy cannot be read is skipped rather than failing the read:
// a mark the factory cannot resolve leaves the rollback counted, which is the
// direction that teaches more rather than less, the same reading
// [cmd/factory]'s composition gives it.
func markedReleases(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	marks, err := window.Marks(ctx, pool)
	if err != nil {
		return nil, err
	}
	excluded := map[string]bool{}
	for _, m := range marks {
		dep, err := deploy.Get(ctx, pool, m.DeployID)
		if err != nil {
			continue
		}
		if dep.Undoing.FailedReleaseID != "" {
			excluded[dep.Undoing.FailedReleaseID] = true
		}
	}
	return excluded, nil
}

// readClosedFirings reads every closed decision through the log, the way
// [ReadEvidence] does, but naming p rather than [componentPrincipal]: both
// readers in this file are made on a screen's behalf and not by this
// package's own pass, so the principal the read event names is the caller's.
func readClosedFirings(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	p principal.Principal) ([]Firing, error) {

	closed, err := decisionlog.NewReader(pool, token).ClosedDecisions(ctx, p)
	if err != nil {
		return nil, err
	}
	var firings []Firing
	for _, d := range closed {
		var opening OpenEvent
		var closing CloseEvent
		if json.Unmarshal([]byte(d.OpenEvent.Payload), &opening) != nil ||
			json.Unmarshal([]byte(d.CloseEvent.Payload), &closing) != nil {
			// A payload this package cannot read is a row some other
			// component wrote in a shape it does not know, the same reading
			// [ReadEvidence] gives it.
			continue
		}
		firings = append(firings, Firing{
			OpenEvent: opening, CloseEvent: closing,
			HumanClosed: d.CloseEvent.Actor.Kind == record.KindHuman,
			ClosedBy:    d.CloseEvent.Actor, At: d.CloseEvent.At,
		})
	}
	return firings, nil
}
