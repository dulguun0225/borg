package score

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// The rules are arithmetic over records, so these tests are arithmetic: the
// evidence is assembled here and indexed the way a read of the store indexes it,
// which is what keeps a rule testable without a database. What the rules read out
// of a real graph is score_test's, one file along.

// TestRulesStateEveryBound holds the published text and the source together. A
// rule an owner reads that does not say the number the code applies is worse than
// no published rule at all.
func TestRulesStateEveryBound(t *testing.T) {
	for _, bound := range []struct {
		what  string
		value string
	}{
		{"the threshold's band", "0.05"},
		{"the threshold's floor", "0.05"},
		{"the threshold's ceiling", "0.90"},
		{"how many held-out firings raise it a band", "3"},
		{"the attempt bound's ceiling", "6"},
		{"the item-size target's floor", "50"},
		{"the window size's floor", "0.002"},
		{"the confidence's ceiling", "0.999"},
		{"the cap's floor", "86400"},
		{"K's ceiling", "5"},
		{"how many windows raise K", "3"},
	} {
		if !strings.Contains(Rules, bound.value) {
			t.Errorf("the published rules do not state %s (%s)", bound.what, bound.value)
		}
	}
	if !strings.Contains(Rules, "one in ten") && !strings.Contains(Rules, "One firing in\nten") &&
		!strings.Contains(Rules, "One firing in ten") {
		t.Error("the published rules do not state the sample's rate")
	}
	for _, d := range gatepolicy.Definitions {
		if !strings.Contains(Rules, ruleName(d.Parameter)) {
			t.Errorf("the published rules do not name %s", d.Parameter)
		}
	}
}

// ruleName is how the published text names one parameter, which is the words an
// owner reads and not the column name.
func ruleName(p gatepolicy.Parameter) string {
	switch p {
	case gatepolicy.RiskThreshold:
		return "risk threshold"
	case gatepolicy.AttemptBound:
		return "attempt bound"
	case gatepolicy.ItemSizeTarget:
		return "item-size target"
	case gatepolicy.WindowSize:
		return "watch window size"
	case gatepolicy.WindowConfidence:
		return "window confidence"
	case gatepolicy.WindowCap:
		return "watch window cap"
	case gatepolicy.K:
		return "K"
	default:
		return "predicate"
	}
}

// TestLearningIsIdempotent: every rule is a function of the whole graph and never
// a step from the value in force, so a pass that runs twice moves nothing twice.
// learn.go's own comment names this test.
func TestLearningIsIdempotent(t *testing.T) {
	e := someEvidence(t)
	first, err := LearnFrom(e)
	if err != nil {
		t.Fatalf("LearnFrom: %v", err)
	}
	second, err := LearnFrom(e)
	if err != nil {
		t.Fatalf("LearnFrom again: %v", err)
	}
	if encode(t, first) != encode(t, second) {
		t.Errorf("two passes over one graph supply two tables:\n%s\n%s", encode(t, first), encode(t, second))
	}
	// And a pass over the same graph read again — the same evidence assembled a
	// second time — supplies the same table, which is what makes a version appended
	// by one process readable by the next.
	third, err := LearnFrom(someEvidence(t))
	if err != nil {
		t.Fatalf("LearnFrom a second reading: %v", err)
	}
	if encode(t, first) != encode(t, third) {
		t.Error("two readings of one store supply two tables")
	}
}

// TestKRisesWithWindowsAndFallsWithASweepingRollback is the one rule the design
// states in as many words, and the one value that moves both ways.
func TestKRisesWithWindowsAndFallsWithASweepingRollback(t *testing.T) {
	start, _ := Starting(gatepolicy.K)

	// Three windows closing without harm: K rises by one.
	rising := evidenceFor("svc_a", closes(3, window.ExitCap), nil)
	if k := valueOf(t, rising, gatepolicy.K, "svc_a"); k != start.Value+1 {
		t.Errorf("three windows without harm supply K = %v, want %v", k, start.Value+1)
	}

	// Two are not enough: the rise is per three, and a service that rises on two
	// would be one taking throughput it has not earned.
	if k := valueOf(t, evidenceFor("svc_a", closes(2, window.ExitCap), nil), gatepolicy.K, "svc_a"); k != start.Value {
		t.Errorf("two windows without harm supply K = %v, want the starting %v", k, start.Value)
	}

	// Three windows and then a rollback that swept: back to the floor. The fold is
	// in order, which is why the same two facts the other way round do not cancel.
	after := evidenceFor("svc_a", closes(3, window.ExitCap), []serviceEvent{{at: record.FormatTime(time.Date(2026, 8, 20, 0, 0, 9, 0, time.UTC)), sweeping: true}})
	if k := valueOf(t, after, gatepolicy.K, "svc_a"); k != start.Value {
		t.Errorf("a rollback that swept leaves K = %v, want the floor %v", k, start.Value)
	}

	// Six windows, then the sweep: from two rather than from one, which is the
	// whole reason the rule is a fold and not a count.
	sixThenSweep := evidenceFor("svc_a", closes(6, window.ExitCap), []serviceEvent{{at: record.FormatTime(time.Date(2026, 8, 20, 0, 0, 9, 0, time.UTC)), sweeping: true}})
	if k := valueOf(t, sixThenSweep, gatepolicy.K, "svc_a"); k != start.Value+1 {
		t.Errorf("six windows and one sweep leave K = %v, want %v", k, start.Value+1)
	}
}

// TestAMissMakesTheWindowFinerAndACleanMissAlsoRaisesTheConfidence: a window that
// closed without harm over a release an incident was raised against is the
// crossing the comparison could have seen and did not.
func TestAMissMakesTheWindowFinerAndACleanMissAlsoRaisesTheConfidence(t *testing.T) {
	size, _ := Starting(gatepolicy.WindowSize)
	confidence, _ := Starting(gatepolicy.WindowConfidence)

	// One miss at the cap: the size halves and the confidence does not move. A
	// window that ended at the cap ruled nothing out, so it says nothing about how
	// sure the boundary was.
	missed := withIncident(evidenceFor("svc_a", closes(1, window.ExitCap), nil), "rel_svc_a_0")
	if got := valueOf(t, missed, gatepolicy.WindowSize, "svc_a"); got != size.Value/2 {
		t.Errorf("one miss supplies a size of %v, want %v", got, size.Value/2)
	}
	if got := valueOf(t, missed, gatepolicy.WindowConfidence, "svc_a"); got != confidence.Value {
		t.Errorf("a miss at the cap moved the confidence to %v", got)
	}

	// One miss at the clean exit: the size halves and the confidence closes half
	// the distance to one, because the boundary said it had ruled out what it had
	// not.
	falseClean := withIncident(evidenceFor("svc_a", closes(1, window.ExitClean), nil), "rel_svc_a_0")
	if got := valueOf(t, falseClean, gatepolicy.WindowConfidence, "svc_a"); got != 1-(1-confidence.Value)/2 {
		t.Errorf("a false clean supplies a confidence of %v, want %v", got, 1-(1-confidence.Value)/2)
	}

	// A harm exit is not a miss and never can be: the comparison rolls a release
	// back at that exit, so it caught what it was watching for.
	caught := withIncident(evidenceFor("svc_a", closes(1, window.ExitHarm), nil), "rel_svc_a_0")
	if got := valueOf(t, caught, gatepolicy.WindowSize, "svc_a"); got != size.Value {
		t.Errorf("a window that closed at harm moved the size to %v, and it caught what it watched for", got)
	}
}

// TestTheThresholdFallsBelowWhatItPassedAndRisesOnlyOnTheSample is the
// calibration, and the second half is why the sample exists at all.
func TestTheThresholdFallsBelowWhatItPassedAndRisesOnlyOnTheSample(t *testing.T) {
	start, _ := Starting(gatepolicy.RiskThreshold)
	const row = "merge_to_master"

	// A change the score auto-passed on the number, condemned by its own window:
	// the threshold falls one band below what that change scored.
	fell := firingEvidence(t, []Firing{autoPassed(row, 0.20, AutoPassedByThreshold, "it_bad")})
	fell = condemn(fell, "it_bad")
	if got := valueOf(t, fell, gatepolicy.RiskThreshold, row); !near(got, 0.20-thresholdBand) {
		t.Errorf("the threshold reads %v after a bad auto-pass at 0.20, want %v", got, 0.20-thresholdBand)
	}

	// Three held-out firings that turned out well: one band up. Nothing else
	// raises it, and a gated change a human approved is not evidence — the human's
	// own scrutiny is part of why it turned out well.
	rose := firingEvidence(t, []Firing{
		autoPassed(row, 0.9, AutoPassedBySample, "it_a"),
		autoPassed(row, 0.9, AutoPassedBySample, "it_b"),
		autoPassed(row, 0.9, AutoPassedBySample, "it_c"),
	})
	if got := valueOf(t, rose, gatepolicy.RiskThreshold, row); !near(got, start.Value+thresholdBand) {
		t.Errorf("three good held-out firings supply %v, want %v", got, start.Value+thresholdBand)
	}

	// Three approvals by a human at a gate that gated them move nothing.
	approved := firingEvidence(t, []Firing{
		humanApproved(row, 0.9, "it_a"), humanApproved(row, 0.9, "it_b"), humanApproved(row, 0.9, "it_c"),
	})
	if got := valueOf(t, approved, gatepolicy.RiskThreshold, row); got != start.Value {
		t.Errorf("three human approvals moved the threshold to %v, and a gated change a human approved is not evidence the gate was unnecessary", got)
	}

	// A fall outranks a rise: it names a number the score is known to have got
	// wrong.
	both := firingEvidence(t, []Firing{
		autoPassed(row, 0.9, AutoPassedBySample, "it_a"),
		autoPassed(row, 0.9, AutoPassedBySample, "it_b"),
		autoPassed(row, 0.9, AutoPassedBySample, "it_c"),
		autoPassed(row, 0.20, AutoPassedByThreshold, "it_bad"),
	})
	both = condemn(both, "it_bad")
	if got := valueOf(t, both, gatepolicy.RiskThreshold, row); !near(got, 0.20-thresholdBand) {
		t.Errorf("a fall and a rise together supply %v, want the fall %v", got, 0.20-thresholdBand)
	}
}

// TestTheAttemptBoundRisesToOneAboveWhatSucceeded and nothing lowers it, an
// escalation saying the bound was reached and not that it was too high.
func TestTheAttemptBoundRisesToOneAboveWhatSucceeded(t *testing.T) {
	start, _ := Starting(gatepolicy.AttemptBound)
	e := newEvidence()
	e.items = []item.Item{{ID: "it_a", Stage: item.StageMerged, AreaID: "ar_a"}}
	e.stages = []item.StageTotals{{ItemID: "it_a", Stage: item.StageImplementation, Attempts: 4}}
	e.index()
	if got := valueOf(t, e, gatepolicy.AttemptBound, string(item.StageImplementation)); got != 5 {
		t.Errorf("a stage that succeeded on attempt 4 supplies a bound of %v, want 5", got)
	}

	// A stage that has only ever succeeded on the first attempt leaves the bound
	// where it started: the evidence is one-sided and there is none for lowering it.
	quiet := newEvidence()
	quiet.items = []item.Item{{ID: "it_a", Stage: item.StageMerged}}
	quiet.stages = []item.StageTotals{{ItemID: "it_a", Stage: item.StageImplementation, Attempts: 1}}
	quiet.index()
	if got := valueOf(t, quiet, gatepolicy.AttemptBound, string(item.StageImplementation)); got != start.Value {
		t.Errorf("a stage that succeeds first time supplies %v, want the starting %v", got, start.Value)
	}
}

// TestAStallHalvesTheItemSizeTarget: an item that reached the bound at a stage and
// never shipped is work spent and thrown away, which is what a cut too coarse
// shows as.
func TestAStallHalvesTheItemSizeTarget(t *testing.T) {
	start, _ := Starting(gatepolicy.ItemSizeTarget)
	e := newEvidence()
	e.items = []item.Item{{ID: "it_stalled", AreaID: "ar_a", Stage: item.StageImplementation}}
	e.stages = []item.StageTotals{{ItemID: "it_stalled", Stage: item.StageImplementation, Attempts: int(start.Value)}}
	e.stages[0].Attempts = 3
	e.index()
	if got := valueOf(t, e, gatepolicy.ItemSizeTarget, "ar_a"); got != start.Value/2 {
		t.Errorf("one stall supplies a target of %v, want %v", got, start.Value/2)
	}

	// An item that reached the bound and then shipped is not a stall: nothing was
	// thrown away.
	shipped := newEvidence()
	shipped.items = []item.Item{{ID: "it_a", AreaID: "ar_a", Stage: item.StageMerged}}
	shipped.stages = []item.StageTotals{{ItemID: "it_a", Stage: item.StageImplementation, Attempts: 3}}
	shipped.releases = []release.Release{{ID: "rel_a", ItemID: "it_a", ServiceID: "svc_a", Number: 1}}
	shipped.index()
	if got := valueOf(t, shipped, gatepolicy.ItemSizeTarget, "ar_a"); got != start.Value {
		t.Errorf("an item that reached the bound and shipped supplies %v, want the starting %v", got, start.Value)
	}
}

// TestTheCapIsSetAboveWhatAWindowActuallyNeeded: a cap under the time a window of
// this service took to resolve closes unresolved one that would have resolved.
func TestTheCapIsSetAboveWhatAWindowActuallyNeeded(t *testing.T) {
	start, _ := Starting(gatepolicy.WindowCap)
	e := newEvidence()
	opened := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	e.windows = []window.Window{{
		ID: "win_a", ServiceID: "svc_a", ReleaseID: "rel_a", Exit: window.ExitClean,
		At: record.FormatTime(opened), ClosedAt: record.FormatTime(opened.Add(20 * time.Hour)),
	}}
	e.index()
	// Twenty hours doubled is forty, which is above the day the score starts at.
	if got := valueOf(t, e, gatepolicy.WindowCap, "svc_a"); got != (40 * time.Hour).Seconds() {
		t.Errorf("the cap reads %v, want twice the twenty hours the window needed", got)
	}

	// A window that closed inside the starting cap leaves it alone: the floor is
	// the starting value, and the rule only ever raises.
	quick := newEvidence()
	quick.windows = []window.Window{{
		ID: "win_a", ServiceID: "svc_a", ReleaseID: "rel_a", Exit: window.ExitHarm,
		At: record.FormatTime(opened), ClosedAt: record.FormatTime(opened.Add(time.Minute)),
	}}
	quick.index()
	if got := valueOf(t, quick, gatepolicy.WindowCap, "svc_a"); got != start.Value {
		t.Errorf("a window that resolved in a minute supplies a cap of %v, want the starting %v", got, start.Value)
	}
}

// TestFiveOfTheSixMoveOnlyTowardProtection is the ratchet stated as a test. It is
// here so that the cost the published rules state is checkable and not only
// asserted: every value but K, given evidence, moves the protective way.
func TestFiveOfTheSixMoveOnlyTowardProtection(t *testing.T) {
	for _, of := range []struct {
		parameter gatepolicy.Parameter
		subject   string
		evidence  *Evidence
		// protective is the direction more protection lies in.
		protective func(before, after float64) bool
	}{
		{gatepolicy.WindowSize, "svc_a",
			withIncident(evidenceFor("svc_a", closes(1, window.ExitCap), nil), "rel_svc_a_0"),
			func(before, after float64) bool { return after < before }},
		{gatepolicy.WindowConfidence, "svc_a",
			withIncident(evidenceFor("svc_a", closes(1, window.ExitClean), nil), "rel_svc_a_0"),
			func(before, after float64) bool { return after > before }},
		{gatepolicy.WindowCap, "svc_a", longWindow(), func(before, after float64) bool { return after > before }},
	} {
		start, _ := Starting(of.parameter)
		after := valueOf(t, of.evidence, of.parameter, of.subject)
		if after == start.Value {
			t.Errorf("%s did not move on evidence that should move it", of.parameter)
			continue
		}
		if !of.protective(start.Value, after) {
			t.Errorf("%s moved from %v to %v, which is away from protection", of.parameter, start.Value, after)
		}
	}
}

// The evidence these tests are written over. Each is a graph assembled here and
// indexed the way ReadEvidence indexes one.

// closes is n windows of one service, closed at one exit, one second apart.
func closes(n int, exit window.Exit) []window.Window {
	var windows []window.Window
	for i := range n {
		windows = append(windows, window.Window{
			ID:        fmt.Sprintf("win_%d", i),
			ServiceID: "svc_a",
			ReleaseID: fmt.Sprintf("rel_svc_a_%d", i),
			Exit:      exit,
			At:        record.FormatTime(time.Date(2026, 8, 20, 0, 0, i, 0, time.UTC)),
			ClosedAt:  record.FormatTime(time.Date(2026, 8, 20, 0, 0, i, 0, time.UTC)),
		})
	}
	return windows
}

// evidenceFor is one service's windows and extra history. The extra events are
// rollbacks, which are the only history a rule folds beside a window.
func evidenceFor(serviceID string, windows []window.Window, extra []serviceEvent) *Evidence {
	e := newEvidence()
	e.windows = windows
	for i, event := range extra {
		if !event.sweeping {
			continue
		}
		e.rollbacks = append(e.rollbacks, deploy.Deploy{
			ID: fmt.Sprintf("dep_%d", i), ServiceID: serviceID, At: event.at,
			Undoing: deploy.Undoing{
				CondemnedReleaseID: fmt.Sprintf("rel_%s_condemned_%d", serviceID, i),
				SweptReleaseIDs:    []string{fmt.Sprintf("rel_%s_swept_%d", serviceID, i)},
				Source:             deploy.SourceComparisonAtHarm,
			},
		})
	}
	e.index()
	return e
}

// withIncident raises an incident against one release, which is what makes a
// window that closed without harm over it a miss.
func withIncident(e *Evidence, releaseID string) *Evidence {
	e.incidents = append(e.incidents, incident.Incident{ID: "inc_a", ReleaseID: releaseID, ServiceID: "svc_a"})
	e.index()
	return e
}

// longWindow is one window that took a day and a half to close on evidence.
func longWindow() *Evidence {
	e := newEvidence()
	opened := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	e.windows = []window.Window{{
		ID: "win_a", ServiceID: "svc_a", ReleaseID: "rel_a", Exit: window.ExitClean,
		At: record.FormatTime(opened), ClosedAt: record.FormatTime(opened.Add(36 * time.Hour)),
	}}
	e.index()
	return e
}

// autoPassed is one firing the factory closed itself, at a number, by one of the
// two things an auto-pass comes from.
func autoPassed(row string, number float64, by, itemID string) Firing {
	return Firing{
		Opening: Opening{ItemID: itemID, Gate: row, Number: number, Threshold: 0.3,
			HeldOut: by == AutoPassedBySample},
		Closing: Closing{Verdict: VerdictApproved, AutoPassedBy: by},
	}
}

// humanApproved is one firing a human approved at a gate the number gated.
func humanApproved(row string, number float64, itemID string) Firing {
	return Firing{
		Opening:     Opening{ItemID: itemID, Gate: row, Number: number, Threshold: 0.3},
		Closing:     Closing{Verdict: VerdictApproved},
		HumanClosed: true,
	}
}

// firingEvidence is a graph where every named item shipped and its window closed
// at the cap, so each turned out well unless a later call condemns it.
func firingEvidence(t *testing.T, firings []Firing) *Evidence {
	t.Helper()
	e := newEvidence()
	e.firings = firings
	for i, f := range firings {
		if f.Opening.ItemID == "" {
			continue
		}
		releaseID := "rel_" + f.Opening.ItemID
		e.items = append(e.items, item.Item{ID: f.Opening.ItemID, ServiceID: "svc_a", Stage: item.StageMerged})
		e.releases = append(e.releases, release.Release{
			ID: releaseID, ItemID: f.Opening.ItemID, ServiceID: "svc_a", Number: int64(i + 1),
		})
		e.windows = append(e.windows, window.Window{
			ID: "win_" + f.Opening.ItemID, ServiceID: "svc_a", ReleaseID: releaseID,
			Exit:     window.ExitCap,
			At:       record.FormatTime(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)),
			ClosedAt: record.FormatTime(time.Date(2026, 8, 20, 0, 0, 1, 0, time.UTC)),
		})
	}
	e.index()
	return e
}

// condemn makes one item's release the release a rollback condemned, which is what
// makes that item's outcome badly.
func condemn(e *Evidence, itemID string) *Evidence {
	e.rollbacks = append(e.rollbacks, deploy.Deploy{
		ID: "dep_rollback", ServiceID: "svc_a", At: record.FormatTime(time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)),
		Undoing: deploy.Undoing{CondemnedReleaseID: "rel_" + itemID, Source: deploy.SourceComparisonAtHarm},
	})
	e.index()
	return e
}

// someEvidence is a graph with a little of every kind of outcome in it, which is
// what the idempotence test needs: a table with rows in it says more about two
// passes agreeing than a table of starting values does.
func someEvidence(t *testing.T) *Evidence {
	t.Helper()
	e := firingEvidence(t, []Firing{
		autoPassed("merge_to_master", 0.9, AutoPassedBySample, "it_a"),
		autoPassed("merge_to_master", 0.9, AutoPassedBySample, "it_b"),
		autoPassed("merge_to_master", 0.9, AutoPassedBySample, "it_c"),
		autoPassed("deploy_to_production", 0.2, AutoPassedByThreshold, "it_bad"),
	})
	e.items = append(e.items, item.Item{ID: "it_stalled", AreaID: "ar_a", Stage: item.StageImplementation})
	e.stages = append(e.stages,
		item.StageTotals{ItemID: "it_stalled", Stage: item.StageImplementation, Attempts: 3},
		item.StageTotals{ItemID: "it_a", Stage: item.StageSpec, Attempts: 2})
	for i := range e.items {
		e.items[i].AreaID = "ar_a"
	}
	e = condemn(e, "it_bad")
	e = withIncident(e, "rel_it_a")
	return e
}

// valueOf is what the pass supplies for one parameter on one subject.
func valueOf(t *testing.T, e *Evidence, parameter gatepolicy.Parameter, subject string) float64 {
	t.Helper()
	values, err := LearnFrom(e)
	if err != nil {
		t.Fatalf("LearnFrom: %v", err)
	}
	supplied, found := values.Value(parameter, subject)
	if !found {
		t.Fatalf("the score supplies no %s", parameter)
	}
	return supplied.Value
}

func encode(t *testing.T, values SuppliedValues) string {
	t.Helper()
	text, err := json.Marshal(values)
	if err != nil {
		t.Fatalf("encoding the table: %v", err)
	}
	return string(text)
}

// near is float comparison for a value the rules arrive at by arithmetic on
// bands. A band below 0.20 is 0.15 and reads as 0.15000000000000002, which is
// binary floating point and not a rule that missed.
func near(got, want float64) bool { return got-want < 1e-9 && want-got < 1e-9 }
