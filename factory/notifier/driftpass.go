package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/people"
)

// driftObligation is whose wait a mismatch and the detector's own staleness
// both are: installing the drift detector is substrate outside the twelve,
// so the page reaches whoever People records as having installed it.
var driftObligation = people.OfObligation(people.ObligationDriftDetector)

// SweepDriftDetector is the notifier reading the drift detector's store
// itself, the one wait nothing calls it about: that store writes into
// nothing of the factory's and calls nothing, so both ends of its page are
// read rather than told. It replaces cmd/factory's own former copy of this
// pass, moved here so the composition depends on the notifier for it
// instead of duplicating the read.
//
// A mismatch nobody has been reached about is paged. One still uncleared on
// a later pass widens, once, to the owner. One a human has cleared is
// answered — here, at the pass that finds it cleared, because clearing it
// happened where nothing calls.
func (n *Notifier) SweepDriftDetector(ctx context.Context, driftPool *pgxpool.Pool) error {
	all, err := driftdetector.All(ctx, driftPool)
	if err != nil {
		return err
	}
	for _, m := range all {
		w := Wait{
			Row: m.ID, Kind: KindDriftMismatch, Waiting: m.Why(),
			Holding: driftObligation, Worse: true,
		}
		events, err := n.EventsFor(ctx, m.ID)
		if err != nil {
			return err
		}
		var reachedIt, widened, answered bool
		for _, e := range events {
			switch Event(e.Event) {
			case EventReached:
				reachedIt = true
			case EventWidened:
				widened = true
			case EventAnswered:
				answered = true
			}
		}

		switch {
		case !reachedIt:
			if _, err := n.Notify(ctx, w); err != nil {
				return err
			}
		case m.Cleared() && !answered:
			if _, err := n.Answered(ctx, w, m.ClearedBy); err != nil {
				return err
			}
		case !m.Cleared() && !widened:
			if _, err := n.Widen(ctx, w); err != nil {
				return err
			}
		}
	}
	return nil
}

// driftDetectorStaleRow is the fixed row [SweepDriftDetectorStale] pages,
// widens and answers on — the wait is the detector's own process, singular,
// and not one row per target, so unlike a mismatch there is one id to name
// rather than one minted per finding.
const driftDetectorStaleRow = "driftdetector_own_last_check"

// SweepDriftDetectorStale is the notifier's own half of "each of the two
// processes watches the other": it reads the detector's last check per
// target and pages whoever installed it when any has missed a pass, the
// same way [SweepDriftDetector] pages a mismatch. It answers itself once
// every target's last check is within its interval again, since nothing
// else calls the closing act for a wait inside a process that reads its
// own health.
func (n *Notifier) SweepDriftDetectorStale(ctx context.Context, driftPool *pgxpool.Pool) error {
	checks, err := driftdetector.LastChecks(ctx, driftPool, "")
	if err != nil {
		return err
	}
	now := time.Now()
	stale := false
	for _, c := range checks {
		missed, err := c.Stale(now)
		if err != nil {
			return err
		}
		if missed {
			stale = true
			break
		}
	}

	w := Wait{
		Row: driftDetectorStaleRow, Kind: KindDriftDetectorStale,
		Waiting: "the drift detector's own last check has missed a pass",
		Holding: driftObligation, Worse: true,
	}
	events, err := n.EventsFor(ctx, driftDetectorStaleRow)
	if err != nil {
		return err
	}
	// A fixed row can stale, answer, and stale again — unlike a mismatch,
	// which mints a new id each time — so only the events since the last
	// answer describe the episode this call is deciding about.
	for i, e := range events {
		if Event(e.Event) == EventAnswered {
			events = events[i+1:]
		}
	}
	var reachedIt, widened, answered bool
	for _, e := range events {
		switch Event(e.Event) {
		case EventReached:
			reachedIt = true
		case EventWidened:
			widened = true
		case EventAnswered:
			answered = true
		}
	}

	switch {
	case stale && !reachedIt:
		_, err = n.Notify(ctx, w)
	case stale && !widened:
		_, err = n.Widen(ctx, w)
	case !stale && reachedIt && !answered:
		_, err = n.Answered(ctx, w, "")
	}
	return err
}

// CatchUpDriftDetectorDelivery is the factory's next start appending the
// page event for the detector's own delivery: the log is inside the
// stopped process, so the event could not be written when the delivery
// happened, and this reads the detector's store as it already does for a
// cleared mismatch and writes the event with the detector's own time on it
// — never the row's own append time, which would understate how long the
// process was down.
//
// It writes one reached event per delivery not yet caught up, to whoever
// holds the obligation of installing the detector — the same routing every
// other drift page uses — rather than the raw address the delivery itself
// went to, which is not a per-person key and is not this package's to
// resolve into one.
func (n *Notifier) CatchUpDriftDetectorDelivery(ctx context.Context, driftPool *pgxpool.Pool) error {
	deliveries, err := driftdetector.OwnDeliveries(ctx, driftPool)
	if err != nil {
		return err
	}
	if len(deliveries) == 0 {
		return nil
	}
	reach, err := n.routeTo(ctx, Wait{Holding: driftObligation})
	if err != nil {
		return err
	}
	for _, d := range deliveries {
		events, err := n.EventsFor(ctx, d.ID)
		if err != nil {
			return err
		}
		if len(events) > 0 {
			continue
		}
		for _, human := range reach {
			if err := n.appendCaughtUpEvent(ctx, d, human); err != nil {
				return err
			}
		}
	}
	return nil
}

// appendCaughtUpEvent appends the one page event a caught-up delivery
// writes, carrying d's own time in the payload rather than going through
// [Notifier.deliver] — the delivery already happened at the detector, so
// there is no channel to invoke again and no delivery record to write for
// an attempt this process never made.
func (n *Notifier) appendCaughtUpEvent(ctx context.Context, d driftdetector.OwnDelivery, reached string) error {
	payload, err := json.Marshal(Payload{
		Kind: PageEventKind, Row: d.ID, WaitKind: "drift_detector_own_delivery",
		Waiting: d.Why, Event: string(EventReached), Reached: reached, At: d.At,
	})
	if err != nil {
		return fmt.Errorf("notifier: marshalling the caught-up page event for %s: %w", d.ID, err)
	}
	_, err = n.log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: PageEventFormatVersion,
	})
	return err
}

// RecordOwnLastCheck writes the notifier's own last check, beside the
// health monitor's and the deployer's: what makes a fault in this one
// component visible even though it is the channel that would otherwise
// carry that page. It promises the next pass within interval.
func (n *Notifier) RecordOwnLastCheck(ctx context.Context, interval time.Duration) error {
	_, err := lastcheck.NewWriter(n.pool, n.token).Record(ctx, Actor, lastcheck.LastCheck{
		Component: lastcheck.ComponentNotifier, Interval: interval,
	})
	return err
}
