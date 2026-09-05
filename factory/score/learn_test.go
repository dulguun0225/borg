package score

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// The rules are arithmetic over records, so these tests are arithmetic: the
// evidence is assembled here and indexed the way a read of the store indexes it,
// which is what keeps a rule testable without a database.

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
		{"the attempt limit's ceiling", "6"},
		{"the attempt limit's floor", "2"},
		{"the item-size target's floor", "1 requirement"},
		{"the window size's floor", "0.002"},
		{"the power's ceiling", "0.99"},
		{"the power's floor", "0.50"},
		{"the power's step", "0.05"},
		{"the cap's floor", "60"},
		{"the window limit's ceiling", "5"},
		{"how many windows raise the window limit", "3"},
		{"the drift bound", "0.25"},
		{"how many decisions a drift reading needs", "8"},
		{"how many held-out decisions a fit needs", "10"},
	} {
		if !strings.Contains(Rules, bound.value) {
			t.Errorf("the published rules do not state %s (%s)", bound.what, bound.value)
		}
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
	case gatepolicy.ExposureBound:
		return "the exposure bound"
	case gatepolicy.AdvisorySeverity:
		return "advisory"
	case gatepolicy.AttemptLimit:
		return "attempt limit"
	case gatepolicy.ItemSizeTarget:
		return "item-size target"
	case gatepolicy.WindowSize:
		return "analysis window size"
	case gatepolicy.WindowConfidence:
		return "window confidence"
	case gatepolicy.WindowPower:
		return "analysis window power"
	case gatepolicy.WindowCap:
		return "analysis window cap"
	case gatepolicy.WindowLimit:
		return "window limit"
	case gatepolicy.HeldOutSampleRate:
		return "held-out sample"
	case gatepolicy.ReviewSampleRate:
		return "review sample rate"
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
	third, err := LearnFrom(someEvidence(t))
	if err != nil {
		t.Fatalf("LearnFrom a second reading: %v", err)
	}
	if encode(t, first) != encode(t, third) {
		t.Error("two readings of one store supply two tables")
	}
}

// TestTheWindowLimitRisesOnPassedAndNotOnTimedOut is the rule stated exit by
// exit: a release nothing ruled anything out on is not a release that behaved,
// and counting one would let a service whose instrumentation stopped ratchet its
// own limit upward while measuring nothing at all.
func TestTheWindowLimitRisesOnPassedAndNotOnTimedOut(t *testing.T) {
	start, _ := Starting(gatepolicy.WindowLimit)

	if limit := valueOf(t, evidenceFor("svc_a", closes(3, window.ExitPassed), nil), gatepolicy.WindowLimit, "svc_a"); limit != start.Value+1 {
		t.Errorf("three windows that closed passed supply a window limit of %v, want %v", limit, start.Value+1)
	}
	for _, exit := range []window.Exit{window.ExitTimedOut, window.ExitSkipped} {
		e := evidenceFor("svc_a", closes(9, exit), nil)
		if limit := valueOf(t, e, gatepolicy.WindowLimit, "svc_a"); limit != start.Value {
			t.Errorf("nine windows that closed %s supply a window limit of %v, want the starting %v", exit, limit, start.Value)
		}
	}

	// Two passed closes are not enough: the rise is per three.
	if limit := valueOf(t, evidenceFor("svc_a", closes(2, window.ExitPassed), nil), gatepolicy.WindowLimit, "svc_a"); limit != start.Value {
		t.Errorf("two windows that closed passed supply a window limit of %v, want the starting %v", limit, start.Value)
	}

	// Three and then a rollback that undid more than its target: back to the
	// floor. The fold is in order, which is why the same two facts the other way
	// round do not cancel.
	after := evidenceFor("svc_a", closes(3, window.ExitPassed), undidMoreThanItsTarget())
	if limit := valueOf(t, after, gatepolicy.WindowLimit, "svc_a"); limit != start.Value {
		t.Errorf("a rollback that undid more than its target leaves a window limit of %v, want the floor %v", limit, start.Value)
	}
	sixThenARollback := evidenceFor("svc_a", closes(6, window.ExitPassed), undidMoreThanItsTarget())
	if limit := valueOf(t, sixThenARollback, gatepolicy.WindowLimit, "svc_a"); limit != start.Value+1 {
		t.Errorf("six windows and one rollback leave a window limit of %v, want %v", limit, start.Value+1)
	}
}

// TestAMissOnATimedOutWindowMovesTheSizeAndOneOnAPassedWindowMovesThePower is
// the rule that which parameter an event moves is a fact of the record: a window
// that closed passed cleared what it should have caught, and one that timed out
// ruled nothing out at all.
func TestAMissOnATimedOutWindowMovesTheSizeAndOneOnAPassedWindowMovesThePower(t *testing.T) {
	size, _ := Starting(gatepolicy.WindowSize)
	power, _ := Starting(gatepolicy.WindowPower)
	subject := QuantitySubject("svc_a", gatepolicy.QuantityErrorRate)

	timedOut := withIncident(evidenceFor("svc_a", closes(1, window.ExitTimedOut), nil), "rel_svc_a_0")
	if got := valueOf(t, timedOut, gatepolicy.WindowSize, subject); got != size.Value/2 {
		t.Errorf("a miss on a timed-out window supplies a size of %v, want %v", got, size.Value/2)
	}
	if got := valueOf(t, timedOut, gatepolicy.WindowPower, subject); got != power.Value {
		t.Errorf("a miss on a timed-out window moved the power to %v, and nothing was ruled out there", got)
	}

	falsePass := withIncident(evidenceFor("svc_a", closes(1, window.ExitPassed), nil), "rel_svc_a_0")
	want := 1 - (1-power.Value)/2
	if got := valueOf(t, falsePass, gatepolicy.WindowPower, subject); !near(got, want) {
		t.Errorf("a false pass supplies a power of %v, want %v", got, want)
	}
	if got := valueOf(t, falsePass, gatepolicy.WindowSize, subject); got != size.Value {
		t.Errorf("a false pass moved the size to %v, and the size is what a timed-out window moves", got)
	}

	// A failed exit is not a miss and never can be: the health monitor rolls a
	// release back at that exit, so it caught what it was watching for.
	caught := withIncident(evidenceFor("svc_a", closes(1, window.ExitFailed), nil), "rel_svc_a_0")
	if got := valueOf(t, caught, gatepolicy.WindowSize, subject); got != size.Value {
		t.Errorf("a window that closed failed moved the size to %v, and it caught what it watched for", got)
	}
}

// TestNothingMovesTheConfidence: no outcome says the confidence a comparison
// must reach was too high, and the one thing that establishes a failed release
// was fine says the comparison was confounded and nothing about how sure it
// should have been.
func TestNothingMovesTheConfidence(t *testing.T) {
	confidence, _ := Starting(gatepolicy.WindowConfidence)
	for _, exit := range []window.Exit{window.ExitPassed, window.ExitTimedOut, window.ExitFailed} {
		e := withIncident(evidenceFor("svc_a", closes(3, exit), nil), "rel_svc_a_0")
		if got := valueOf(t, e, gatepolicy.WindowConfidence, "svc_a"); got != confidence.Value {
			t.Errorf("three windows closing %s moved the confidence to %v, want the starting %v", exit, got, confidence.Value)
		}
	}
}

// TestAMarkedRollbackTeachesNothing: a rollback a human marked as not caused by
// the release measured something other than the release, so it is excluded from
// the window limit and from the prior alike.
func TestAMarkedRollbackTeachesNothing(t *testing.T) {
	start, _ := Starting(gatepolicy.WindowLimit)
	six := evidenceFor("svc_a", closes(6, window.ExitPassed), undidMoreThanItsTarget())
	marked := withMark(evidenceFor("svc_a", closes(6, window.ExitPassed), undidMoreThanItsTarget()), "rel_svc_a_failed_0")

	if got := valueOf(t, six, gatepolicy.WindowLimit, "svc_a"); got != start.Value+1 {
		t.Fatalf("six windows and one rollback leave %v, want %v", got, start.Value+1)
	}
	if got := valueOf(t, marked, gatepolicy.WindowLimit, "svc_a"); got != start.Value+2 {
		t.Errorf("the marked rollback still lowered the limit: %v, want %v", got, start.Value+2)
	}
}

// TestTheThresholdRisesOnlyOnAHeldOutWindowThatPassed: the sample exists to
// produce an outcome, and the exit that reports no outcome reports none here
// either — counting a timed-out window as good would put back exactly what the
// sample was built to remove.
func TestTheThresholdRisesOnlyOnAHeldOutWindowThatPassed(t *testing.T) {
	start, _ := Starting(gatepolicy.RiskThreshold)
	const row = "merge_to_master"

	held := []Firing{
		autoPassed(row, 0.9, AutoPassSample, "it_a"),
		autoPassed(row, 0.9, AutoPassSample, "it_b"),
		autoPassed(row, 0.9, AutoPassSample, "it_c"),
	}
	rose := firingEvidence(t, held)
	if got := valueOf(t, rose, gatepolicy.RiskThreshold, row); !near(got, start.Value+thresholdBand) {
		t.Errorf("three held-out releases whose windows passed supply %v, want %v", got, start.Value+thresholdBand)
	}

	timedOut := firingEvidence(t, held)
	for _, id := range []string{"it_a", "it_b", "it_c"} {
		timedOut = exitOf(timedOut, id, window.ExitTimedOut)
	}
	if got := valueOf(t, timedOut, gatepolicy.RiskThreshold, row); got != start.Value {
		t.Errorf("three held-out releases whose windows timed out supply %v, want the starting %v", got, start.Value)
	}

	// A change the score auto-passed on the number, failed by its own window: the
	// threshold falls one band below what that change scored, and a fall outranks
	// a rise.
	fell := fail(firingEvidence(t, append(append([]Firing{}, held...),
		autoPassed(row, 0.20, AutoPassThreshold, "it_bad"))), "it_bad")
	if got := valueOf(t, fell, gatepolicy.RiskThreshold, row); !near(got, 0.20-thresholdBand) {
		t.Errorf("the threshold reads %v after a bad auto-pass at 0.20, want %v", got, 0.20-thresholdBand)
	}

	// Three approvals by a human at a gate that gated them move nothing.
	approved := firingEvidence(t, []Firing{
		humanApproved(row, 0.9, "it_a"), humanApproved(row, 0.9, "it_b"), humanApproved(row, 0.9, "it_c"),
	})
	if got := valueOf(t, approved, gatepolicy.RiskThreshold, row); got != start.Value {
		t.Errorf("three human approvals moved the threshold to %v, and a gated change a human approved is not evidence the gate was unnecessary", got)
	}
}

// TestTheAttemptLimitMovesBothWays: up to one above the highest attempt that ever
// got past the stage, down where nothing has ever needed more than one — which is
// the loose end gate policy's own table states, agent time spent before anybody
// sees the item.
func TestTheAttemptLimitMovesBothWays(t *testing.T) {
	start, _ := Starting(gatepolicy.AttemptLimit)

	if got := valueOf(t, stageEvidence(4, 3, 3), gatepolicy.AttemptLimit, string(item.StageImplementation)); got != 5 {
		t.Errorf("a stage that succeeded on attempt 4 supplies a limit of %v, want 5", got)
	}
	quick := valueOf(t, stageEvidence(1, 3, 3), gatepolicy.AttemptLimit, string(item.StageImplementation))
	if quick != attemptLimitFloor {
		t.Errorf("a stage that succeeds first time supplies %v, want the floor %v", quick, float64(attemptLimitFloor))
	}
	if quick >= start.Value {
		t.Errorf("the limit did not move down: %v against a starting %v", quick, start.Value)
	}
	if got := valueOf(t, stageEvidence(1, 2, 2), gatepolicy.AttemptLimit, string(item.StageImplementation)); got != start.Value {
		t.Errorf("two items at a stage supply %v, want the starting %v", got, start.Value)
	}
}

// stageEvidence is n items that reported at Implementation, the first `past` of
// them having got past it, the first with `highest` attempts against it.
func stageEvidence(highest, past, n int) *Evidence {
	e := newEvidence()
	for i := range n {
		stage := item.StageImplementation
		if i < past {
			stage = item.StageMerged
		}
		id := fmt.Sprintf("it_%d", i)
		attempts := 1
		if i == 0 {
			attempts = highest
		}
		e.items = append(e.items, item.Item{ID: id, AreaID: "ar_a", Stage: stage})
		e.stages = append(e.stages, item.StageTotals{ItemID: id, Stage: item.StageImplementation, Attempts: attempts})
	}
	e.index()
	return e
}

// TestAStallHalvesTheItemSizeTargetInRequirements: the target is authored in the
// count of the intent's requirements an item answers, which is the unit
// decomposition sets, and a stall is work spent and thrown away.
func TestAStallHalvesTheItemSizeTargetInRequirements(t *testing.T) {
	start, _ := Starting(gatepolicy.ItemSizeTarget)
	if start.Value > 20 {
		t.Errorf("the item-size target starts at %v, which is a count of lines and not of requirements", start.Value)
	}

	e := newEvidence()
	e.items = []item.Item{{ID: "it_stalled", AreaID: "ar_a", Stage: item.StageImplementation}}
	e.stages = []item.StageTotals{{ItemID: "it_stalled", Stage: item.StageImplementation, Attempts: 3}}
	e.index()
	want := float64(int(start.Value/2 + 0.5))
	if got := valueOf(t, e, gatepolicy.ItemSizeTarget, "ar_a"); got != want {
		t.Errorf("one stall supplies a target of %v, want %v", got, want)
	}

	shipped := newEvidence()
	shipped.items = []item.Item{{ID: "it_a", AreaID: "ar_a", Stage: item.StageMerged}}
	shipped.stages = []item.StageTotals{{ItemID: "it_a", Stage: item.StageImplementation, Attempts: 3}}
	shipped.releases = []release.Release{{ID: "rel_a", ItemID: "it_a", ServiceID: "svc_a", Number: 1}}
	shipped.index()
	if got := valueOf(t, shipped, gatepolicy.ItemSizeTarget, "ar_a"); got != start.Value {
		t.Errorf("an item that reached the limit and shipped supplies %v, want the starting %v", got, start.Value)
	}
}

// TestTheCapMovesBothWaysWithWhatAWindowNeeded: a cap under the time a window of
// this service took to resolve closes unresolved one that would have resolved, and
// a cap far above it holds the next deploy for nothing.
func TestTheCapMovesBothWaysWithWhatAWindowNeeded(t *testing.T) {
	start, _ := Starting(gatepolicy.WindowCap)
	opened := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)

	long := resolvedIn(opened, 20*time.Hour, window.ExitPassed)
	if got := valueOf(t, long, gatepolicy.WindowCap, "svc_a"); got != (40 * time.Hour).Seconds() {
		t.Errorf("the cap reads %v, want twice the twenty hours the window needed", got)
	}

	quick := resolvedIn(opened, time.Minute, window.ExitFailed)
	got := valueOf(t, quick, gatepolicy.WindowCap, "svc_a")
	if got != (2 * time.Minute).Seconds() {
		t.Errorf("the cap reads %v, want twice the minute the window needed", got)
	}
	if got >= start.Value {
		t.Errorf("the cap did not move down: %v against a starting %v", got, start.Value)
	}

	atCap := resolvedIn(opened, time.Hour, window.ExitTimedOut)
	if got := valueOf(t, atCap, gatepolicy.WindowCap, "svc_a"); got != start.Value {
		t.Errorf("a service whose only window ended at the cap supplies %v, want the starting %v", got, start.Value)
	}
}

// resolvedIn is one window of one service that closed at an exit after took.
func resolvedIn(opened time.Time, took time.Duration, exit window.Exit) *Evidence {
	e := newEvidence()
	e.windows = []window.Window{{
		ID: "win_a", ServiceID: "svc_a", ReleaseID: "rel_a", Exit: exit,
		At: record.FormatTime(opened), ClosedAt: record.FormatTime(opened.Add(took)),
	}}
	e.index()
	return e
}

// TestASizeFinerThanTheTrafficReachedIsNotAsked is the answer to the ratchet:
// what the evidence asks for and what the traffic reached are two different
// questions, and the size in force is the coarser of them. The finest size the
// traffic reached is the window's own arithmetic, reported on its record.
func TestASizeFinerThanTheTrafficReachedIsNotAsked(t *testing.T) {
	start, _ := Starting(gatepolicy.WindowSize)
	subject := QuantitySubject("svc_a", gatepolicy.QuantityErrorRate)

	thin := withFinestSize(withIncident(evidenceFor("svc_a", closes(1, window.ExitTimedOut), nil), "rel_svc_a_0"), start.Value)
	if got := valueOf(t, thin, gatepolicy.WindowSize, subject); got != start.Value {
		t.Errorf("the size reads %v on a service whose traffic reached no finer, want the starting %v", got, start.Value)
	}
	if row := rowFor(t, thin, gatepolicy.WindowSize, subject); row != nil {
		if !strings.Contains(row.Why, "timed out") || !strings.Contains(row.Why, "finest size") {
			t.Errorf("the row says %q, and both the miss and the traffic decided it", row.Why)
		}
	}

	busy := withFinestSize(withIncident(evidenceFor("svc_a", closes(1, window.ExitTimedOut), nil), "rel_svc_a_0"), start.Value/4)
	if got := valueOf(t, busy, gatepolicy.WindowSize, subject); got != start.Value/2 {
		t.Errorf("the size reads %v on a service whose traffic reached finer, want the halved %v", got, start.Value/2)
	}
}

// TestThePowerFallsWhereWindowsRunToTheCapOnTrafficThatReachedTheSize is the
// power's own second end, which the size's is not: a service whose windows all
// run to the cap is telling the score either that its size is finer than its
// traffic supports or that its power is, and only the second is what this reads.
func TestThePowerFallsWhereWindowsRunToTheCapOnTrafficThatReachedTheSize(t *testing.T) {
	power, _ := Starting(gatepolicy.WindowPower)
	size, _ := Starting(gatepolicy.WindowSize)
	subject := QuantitySubject("svc_a", gatepolicy.QuantityErrorRate)

	e := withFinestSize(evidenceFor("svc_a", closes(3, window.ExitTimedOut), nil), size.Value/2)
	if got := valueOf(t, e, gatepolicy.WindowPower, subject); !near(got, power.Value-windowPowerStep) {
		t.Errorf("three windows at the cap on traffic that reached the size supply a power of %v, want %v",
			got, power.Value-windowPowerStep)
	}

	// Two are not enough, and a service whose traffic did not reach the size in
	// force is telling the score about its size and not about its power.
	if got := valueOf(t, withFinestSize(evidenceFor("svc_a", closes(2, window.ExitTimedOut), nil), size.Value/2),
		gatepolicy.WindowPower, subject); got != power.Value {
		t.Errorf("two windows at the cap supply a power of %v, want the starting %v", got, power.Value)
	}
	if got := valueOf(t, withFinestSize(evidenceFor("svc_a", closes(3, window.ExitTimedOut), nil), size.Value*4),
		gatepolicy.WindowPower, subject); got != power.Value {
		t.Errorf("windows at the cap on traffic coarser than the size supply a power of %v, want the starting %v", got, power.Value)
	}
}
