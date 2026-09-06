// driftpass_test.go is the notifier's own last check and its reads of the
// drift detector's store: the one wait nothing calls it about, and the page
// event a delivery made while the factory's process was down. Split out of
// db_test.go, which fixtures_test.go's helpers are shared with, to keep both
// files under the line bound.
package notifier_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
)

// TestRecordOwnLastCheckWritesTheNotifiersRecord is 07-pages.md's "the
// notifier writes a last check record too, beside the health monitor's and
// the deployer's."
func TestRecordOwnLastCheckWritesTheNotifiersRecord(t *testing.T) {
	ctx, pool, _, n, _ := newNotifier(t)

	if err := n.RecordOwnLastCheck(ctx, time.Minute); err != nil {
		t.Fatalf("RecordOwnLastCheck: %v", err)
	}
	check, found, err := lastcheck.Get(ctx, pool, lastcheck.ComponentNotifier, "")
	if err != nil || !found {
		t.Fatalf("lastcheck.Get for the notifier: found %t, err %v", found, err)
	}
	if check.Interval != time.Minute {
		t.Errorf("the recorded interval = %v, want %v", check.Interval, time.Minute)
	}
}

// TestSweepDriftDetectorPagesWidensAndAnswers is the notifier reading the
// drift detector's own store, since that store calls nothing: an
// unreached mismatch is paged, a still-uncleared one widens once, and a
// cleared one is answered.
func TestSweepDriftDetectorPagesWidensAndAnswers(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	drift := driftTestPool(t, ctx)
	dw := driftdetector.NewWriter(drift)
	recorded, err := dw.Record(ctx, driftdetector.Pass{
		ServiceID: "svc_1", Target: "t1", Reached: true, RunningBuild: "b_running",
		RecordedBuildID: "b_recorded", Interval: time.Minute,
	})
	if err != nil {
		t.Fatalf("recording a mismatch: %v", err)
	}
	if recorded.Raised == "" {
		t.Fatalf("no mismatch was raised")
	}

	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (page): %v", err)
	}
	events, err := n.EventsFor(ctx, recorded.Raised)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached || events[0].Reached != "hk_sre" {
		t.Fatalf("after the first sweep: %+v, want one reached event to hk_sre", events)
	}

	// A second sweep with the mismatch still uncleared widens, once.
	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (widen): %v", err)
	}
	events, err = n.EventsFor(ctx, recorded.Raised)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 2 || notifier.Event(events[1].Event) != notifier.EventWidened {
		t.Fatalf("after the second sweep: %+v, want a widened event too", events)
	}

	// Clearing it at the detector and sweeping again answers it.
	if _, err := driftdetector.NewWriter(drift).Clear(ctx, recorded.Raised, "a-human"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (answer): %v", err)
	}
	events, err = n.EventsFor(ctx, recorded.Raised)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered || last.Reached != "a-human" {
		t.Errorf("after clearing and sweeping: last event %+v, want answered by a-human", last)
	}
}

// TestSweepDriftDetectorStalePagesAndAnswersItself is the notifier's own
// half of "each of the two processes watches the other": a stale last
// check pages, a still-stale one widens, and a check that catches up again
// answers the wait itself, nothing else calling that closing act.
func TestSweepDriftDetectorStalePagesAndAnswersItself(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	drift := driftTestPool(t, ctx)
	if _, err := driftdetector.NewWriter(drift).Record(ctx, driftdetector.Pass{
		ServiceID: "svc_1", Target: "t1", Reached: true, RunningBuild: "b1",
		RecordedBuildID: "b1", Interval: time.Second,
	}); err != nil {
		t.Fatalf("recording a last check: %v", err)
	}
	backdate(t, ctx, drift, driftdetector.LastCheckTable, time.Now().Add(-time.Hour))

	if err := n.SweepDriftDetectorStale(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetectorStale (page): %v", err)
	}
	events, err := n.EventsFor(ctx, "driftdetector_own_last_check")
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 1 || notifier.Event(events[0].Event) != notifier.EventReached || events[0].Reached != "hk_sre" {
		t.Fatalf("after the first sweep: %+v, want one reached event to hk_sre", events)
	}

	if err := n.SweepDriftDetectorStale(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetectorStale (widen): %v", err)
	}
	events, err = n.EventsFor(ctx, "driftdetector_own_last_check")
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 2 || notifier.Event(events[1].Event) != notifier.EventWidened {
		t.Fatalf("after the second sweep: %+v, want a widened event too", events)
	}

	// The detector catching up again answers the wait itself: nothing else
	// calls the closing act for a wait inside a process reading its own
	// health.
	if _, err := driftdetector.NewWriter(drift).Record(ctx, driftdetector.Pass{
		ServiceID: "svc_1", Target: "t1", Reached: true, RunningBuild: "b1",
		RecordedBuildID: "b1", Interval: time.Hour,
	}); err != nil {
		t.Fatalf("recording a fresh last check: %v", err)
	}
	if err := n.SweepDriftDetectorStale(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetectorStale (answer): %v", err)
	}
	events, err = n.EventsFor(ctx, "driftdetector_own_last_check")
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	last := events[len(events)-1]
	if notifier.Event(last.Event) != notifier.EventAnswered {
		t.Errorf("after the check caught up: last event %+v, want answered", last)
	}
}

// backdate rewrites every row of table to look at seconds old, so a test
// does not have to sleep past a real interval to see a record read as
// stale.
func backdate(t *testing.T, ctx context.Context, pool *pgxpool.Pool, table string, at time.Time) {
	t.Helper()
	if _, err := pool.Exec(ctx, `update `+table+` set at = $1`, record.FormatTime(at)); err != nil {
		t.Fatalf("backdating %s: %v", table, err)
	}
}

// TestAnAcknowledgedMismatchStopsOnlyItsOwnWidening is what an acknowledgement
// does and all it does: it ends no wait and decides nothing, so the sweep goes
// on past it to every mismatch after it rather than stopping the whole pass on
// the widening it refuses.
func TestAnAcknowledgedMismatchStopsOnlyItsOwnWidening(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	drift := driftTestPool(t, ctx)
	dw := driftdetector.NewWriter(drift)
	var raised []string
	for _, target := range []string{"t1", "t2"} {
		recorded, err := dw.Record(ctx, driftdetector.Pass{
			ServiceID: "svc_1", Target: target, Reached: true, RunningBuild: "b_running",
			RecordedBuildID: "b_recorded", Interval: time.Minute,
		})
		if err != nil || recorded.Raised == "" {
			t.Fatalf("recording a mismatch on %s: raised %q, %v", target, recorded.Raised, err)
		}
		raised = append(raised, recorded.Raised)
	}
	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (page): %v", err)
	}

	// A human says they have the first row. It still waits and is still
	// uncleared; what the acknowledgement stops is its own widening.
	acknowledged := notifier.Wait{
		Row: raised[0], Kind: notifier.KindDriftMismatch,
		Waiting: "a record disagrees with what runs", Worse: true, ServiceID: "svc_1",
	}
	if _, err := n.Acknowledge(ctx, acknowledged, "hk_sre"); err != nil {
		t.Fatalf("Acknowledge: %v", err)
	}

	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector after an acknowledgement: %v", err)
	}
	first, err := n.EventsFor(ctx, raised[0])
	if err != nil {
		t.Fatalf("EventsFor the acknowledged mismatch: %v", err)
	}
	for _, e := range first {
		if notifier.Event(e.Event) == notifier.EventWidened {
			t.Errorf("the acknowledged mismatch widened: %+v", first)
		}
	}
	second, err := n.EventsFor(ctx, raised[1])
	if err != nil {
		t.Fatalf("EventsFor the mismatch after it: %v", err)
	}
	widened := false
	for _, e := range second {
		if notifier.Event(e.Event) == notifier.EventWidened {
			widened = true
		}
	}
	if !widened {
		t.Errorf("the mismatch after the acknowledged one is %+v, want it widened by the same pass", second)
	}
}

// TestCatchUpDriftDetectorDeliveryCarriesTheDetectorsTime is the page event
// for the detector's own delivery, appended at the factory's next start —
// this test's own call to it — carrying the detector's time and not the
// row's own append time.
func TestCatchUpDriftDetectorDeliveryCarriesTheDetectorsTime(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	drift := driftTestPool(t, ctx)
	if err := driftdetector.NewWriter(drift).SetAddress(ctx, "ops@example.com"); err != nil {
		t.Fatalf("SetAddress: %v", err)
	}
	delivered, err := driftdetector.NewWriter(drift).Deliver(ctx, "every factory last check is stale at once")
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}

	if err := n.CatchUpDriftDetectorDelivery(ctx, drift); err != nil {
		t.Fatalf("CatchUpDriftDetectorDelivery: %v", err)
	}
	events, err := n.EventsFor(ctx, delivered.ID)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 1 || events[0].At != delivered.At || events[0].Reached != "hk_sre" {
		t.Errorf("the caught-up event = %+v, want one reached event to hk_sre at %s", events, delivered.At)
	}

	// A second call finds it already caught up and appends nothing more.
	if err := n.CatchUpDriftDetectorDelivery(ctx, drift); err != nil {
		t.Fatalf("CatchUpDriftDetectorDelivery again: %v", err)
	}
	events, err = n.EventsFor(ctx, delivered.ID)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 1 {
		t.Errorf("a second catch-up appended %d event(s), want the one already there", len(events))
	}
}

// driftTestPool is the drift detector's own store, on a schema of its own
// with its own schema applied — the notifier reads this pool and never the
// factory's for the drift detector's records.
func driftTestPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the drift detector's schema: %v", err)
	}
	schema := "notifier_dd_" + hex.EncodeToString(suffix[:])

	pool, err := driftdetector.Open(ctx, inSchema(t, driftdetector.DefaultURL, schema))
	if err != nil {
		t.Fatalf("the drift detector's store is not reachable, and these tests do not skip: %v", err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := driftdetector.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the drift detector's schema: %v", err)
	}
	return pool
}

// TestAStoppedHealthMonitorPagesTheWindowCapCondition is the fourth page
// condition reaching a human: the component that would have raised it is the one
// that stopped, so the drift detector's third comparison is what fires it and
// the notifier is what delivers it.
func TestAStoppedHealthMonitorPagesTheWindowCapCondition(t *testing.T) {
	ctx, pool, token, n, _ := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}

	drift := driftTestPool(t, ctx)
	raised, err := driftdetector.NewWriter(drift).RaiseStaleComponent(ctx, driftdetector.StaleComponent{
		Component: "health_monitor", ServiceID: "svc_quiet",
		Why: "the health monitor's own last check for this service is stale",
	})
	if err != nil || raised == "" {
		t.Fatalf("RaiseStaleComponent = %q, %v", raised, err)
	}

	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector: %v", err)
	}
	events, err := n.EventsFor(ctx, raised)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 1 || notifier.Kind(events[0].WaitKind) != notifier.KindWindowCapUnevaluated {
		t.Errorf("the page for a stopped health monitor is %+v, want one %s event",
			events, notifier.KindWindowCapUnevaluated)
	}
}

// TestAMismatchHeldToTheHoursIsNotRenotifiedAndIsNotPagedOnceCleared is what a
// wait with nothing waiting on it costs. A mismatch on a service whose paging
// hours are closed reaches mail and chat and holds the page channel back, so no
// reached event is written; without a second reading of that state every later
// sweep would deliver mail and chat again, and the pass that delivers a page
// when the hours come round would page about a mismatch a human had already
// cleared.
func TestAMismatchHeldToTheHoursIsNotRenotifiedAndIsNotPagedOnceCleared(t *testing.T) {
	ctx, pool, token, n, channels := newNotifier(t)
	if _, err := peopleWriter(pool, token).Declare(ctx, theHumanOwner, "hk_sre",
		people.OfObligation(people.ObligationDriftDetector)); err != nil {
		t.Fatalf("declaring who installed the drift detector: %v", err)
	}
	serviceID := aServiceWithNoPagingHoursNow(t, ctx, pool, token)

	drift := driftTestPool(t, ctx)
	recorded, err := driftdetector.NewWriter(drift).Record(ctx, driftdetector.Pass{
		ServiceID: serviceID, Target: "t1", Reached: true, RunningBuild: "b_running",
		RecordedBuildID: "b_recorded", Interval: time.Minute,
	})
	if err != nil || recorded.Raised == "" {
		t.Fatalf("recording a mismatch: raised %q, %v", recorded.Raised, err)
	}

	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (page): %v", err)
	}
	if channels.on(notifier.ChannelPage) != 0 {
		t.Fatalf("the mismatch paged outside the service's hours")
	}
	if channels.on(notifier.ChannelMail) != 1 || channels.on(notifier.ChannelChat) != 1 {
		t.Fatalf("the first sweep delivered mail %d time(s) and chat %d, want one each",
			channels.on(notifier.ChannelMail), channels.on(notifier.ChannelChat))
	}

	// A second sweep finds the same mismatch already delivered with its page
	// held to the hours, and delivers nothing again.
	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (second pass): %v", err)
	}
	if channels.on(notifier.ChannelMail) != 1 || channels.on(notifier.ChannelChat) != 1 {
		t.Errorf("a second sweep delivered mail %d time(s) and chat %d, want the one each already sent",
			channels.on(notifier.ChannelMail), channels.on(notifier.ChannelChat))
	}

	// Cleared where nothing calls: there is no page to answer, so the sweep
	// writes nothing and delivers nothing.
	if _, err := driftdetector.NewWriter(drift).Clear(ctx, recorded.Raised, "hk_sre"); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if err := n.SweepDriftDetector(ctx, drift); err != nil {
		t.Fatalf("SweepDriftDetector (after the clearing): %v", err)
	}
	events, err := n.EventsFor(ctx, recorded.Raised)
	if err != nil {
		t.Fatalf("EventsFor: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("the mismatch holds %+v, want no page event: its page never reached anybody", events)
	}

	// And when the hours come round, the pass that delivers what they held back
	// leaves a cleared mismatch alone.
	authorHoursCovering(t, ctx, pool, token, serviceID, time.Now())
	paged, err := n.PageDeferred(ctx, drift)
	if err != nil {
		t.Fatalf("PageDeferred: %v", err)
	}
	if len(paged) != 0 || channels.on(notifier.ChannelPage) != 0 {
		t.Errorf("PageDeferred paged %v about a mismatch a human had already cleared", paged)
	}
}
