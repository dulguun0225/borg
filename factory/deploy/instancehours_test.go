// The three fleets' spans on a production deploy's target rows, and the backfill
// mark enforcement reads off a completed deploy record.
package deploy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

// TestTheThreeFleetsSpansAreSummedIntoInstanceHours: three sets of instances run
// over spans this record dates — the release's own, the control's, and the
// instances kept for the release a rollback would return to — and instance-hours
// per release is that count across that span for the three together, converted
// at the rate in force at each write and never repriced.
func TestTheThreeFleetsSpansAreSummedIntoInstanceHours(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	d, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithControl,
		ControlTarget: "/srv/one", ControlReleaseID: "rel_below",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	completeOn(t, ctx, w, d.ID, "/srv/one", "/srv/two")

	priced := deploy.Priced{Rate: 0.5, InForce: true}
	if err := w.TearDownControl(ctx, d.ID, "/srv/one", 2, priced); err != nil {
		t.Fatalf("TearDownControl: %v", err)
	}
	if err := w.TearDownKept(ctx, d.ID, "/srv/one", 4, priced); err != nil {
		t.Fatalf("TearDownKept: %v", err)
	}
	if err := w.TearDownRelease(ctx, d.ID, "/srv/one", 10, priced); err != nil {
		t.Fatalf("TearDownRelease: %v", err)
	}

	targets, err := deploy.Targets(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Targets: %v", err)
	}
	one := targets[0]
	if one.Fleets.Control.Hours != 2 || one.Fleets.Kept.Hours != 4 || one.Fleets.Release.Hours != 10 {
		t.Errorf("the fleets ran %+v, want each fleet's own hours", one.Fleets)
	}
	if one.InstanceHours != 16 {
		t.Errorf("the target ran %v instance-hours, want the three fleets added up", one.InstanceHours)
	}
	if !one.Priced.InForce || one.Priced.Amount != 8 || one.Priced.Rate != 0.5 {
		t.Errorf("the target converted to %+v, want each span converted at the rate in force at its write", one.Priced)
	}
	for _, fleet := range []deploy.Fleet{one.Fleets.Release, one.Fleets.Control, one.Fleets.Kept} {
		if fleet.TornDownAt == "" {
			t.Errorf("a fleet torn down names no date: %+v", fleet)
		}
	}

	// The second target's fleets still run, and a service whose record carried
	// no instance-hour rate names no amount at all, which is not an amount of
	// zero.
	if err := w.TearDownKept(ctx, d.ID, "/srv/two", 3, deploy.Priced{}); err != nil {
		t.Fatalf("TearDownKept with no rate in force: %v", err)
	}
	if targets, err = deploy.Targets(ctx, pool, d.ID); err != nil {
		t.Fatalf("Targets: %v", err)
	}
	two := targets[1]
	if two.InstanceHours != 3 || two.Priced.InForce {
		t.Errorf("the second target reads %v hours priced %+v, want the hours and no amount",
			two.InstanceHours, two.Priced)
	}
	if two.Fleets.Release.TornDownAt != "" {
		t.Error("the release's own fleet on the second target reads as torn down, and nothing tore it down")
	}
}

// TestHoursIsTheSpanTimesTheInstances: the hours are a fact of fields and
// timestamps the record already carries, computed one way so that the caller
// writing them and the reader checking them read one function.
func TestHoursIsTheSpanTimesTheInstances(t *testing.T) {
	hours, err := deploy.Hours("2026-01-01T00:00:00.000000000Z", "2026-01-01T03:00:00.000000000Z", 4)
	if err != nil {
		t.Fatalf("Hours: %v", err)
	}
	if hours != 12 {
		t.Errorf("Hours over three hours of four instances = %v, want 12", hours)
	}
	if _, err := deploy.Hours("not a time", "2026-01-01T03:00:00.000000000Z", 4); err == nil {
		t.Error("Hours read a timestamp nothing wrote")
	}
}

// TestABackfillsCompletedRecordIsWhatMarksItComplete: a backfill item's release
// declares no schema diff and opens no contract version, only the element it
// fills and the element it fills from, and the deployer completes its record
// only once every row the old form holds is present in the new. Enforcement
// reads that record before it admits the item that moves reads and the drop
// after it.
func TestABackfillsCompletedRecordIsWhatMarksItComplete(t *testing.T) {
	ctx, pool, w, token := newTableWithToken(t)
	const serviceID = "svc_a"
	r := mintRelease(t, ctx, pool, token, serviceID)

	d, err := w.Start(ctx, deployer, deploy.Beginning{
		ServiceID: serviceID, EnvironmentID: productionID,
		What: deploy.OfRelease(r.ID, r.BuildID), Targets: twoTargets,
		IntoProduction: true, StrategyPicked: deploy.StrategyWithoutControl,
		Backfill: deploy.Backfill{Contract: "ledger", Element: "AmountMinor", FromElement: "Amount"},
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// While the copy runs the record is started, and nothing is marked.
	for _, element := range []string{"AmountMinor", "Amount"} {
		if _, complete, err := deploy.BackfillComplete(ctx, pool, serviceID, "ledger", element); err != nil || complete {
			t.Errorf("the backfill of %s reads complete %v (%v) while the copy runs", element, complete, err)
		}
	}

	completeOn(t, ctx, w, d.ID, "/srv/one", "/srv/two")
	if err := w.Complete(ctx, d.ID); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Either side of the pair reads the same record: the one it filled and the
	// one it filled from are one backfill.
	for _, element := range []string{"AmountMinor", "Amount"} {
		id, complete, err := deploy.BackfillComplete(ctx, pool, serviceID, "ledger", element)
		if err != nil || !complete || id != d.ID {
			t.Errorf("the backfill of %s reads %q complete %v (%v), want the completed record", element, id, complete, err)
		}
	}
	if _, complete, err := deploy.BackfillComplete(ctx, pool, serviceID, "ledger", "SomethingElse"); err != nil || complete {
		t.Errorf("an element no backfill filled reads complete %v (%v)", complete, err)
	}
	if _, complete, err := deploy.BackfillComplete(ctx, pool, "svc_b", "ledger", "AmountMinor"); err != nil || complete {
		t.Errorf("another service's store reads complete %v (%v)", complete, err)
	}

	read, err := deploy.Get(ctx, pool, d.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !read.Backfill.Any() || read.Backfill.FromElement != "Amount" {
		t.Errorf("the record names %+v, want the pair the backfill copies between", read.Backfill)
	}
}
