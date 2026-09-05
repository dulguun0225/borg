package environment_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/record"
)

// TestCycleHoursAndEnvironmentHoursSumAcrossCycles is arithmetic over the
// stored fields and needs no database: environment-hours is composition start
// to teardown, summed across every compose-and-reclaim cycle.
func TestCycleHoursAndEnvironmentHoursSumAcrossCycles(t *testing.T) {
	began := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	one := environment.Cycle{
		ID:         "ecy_one",
		BeganAt:    record.FormatTime(began),
		TornDownAt: record.FormatTime(began.Add(time.Hour)),
	}
	two := environment.Cycle{
		ID:         "ecy_two",
		BeganAt:    record.FormatTime(began.Add(2 * time.Hour)),
		TornDownAt: record.FormatTime(began.Add(4 * time.Hour)),
	}

	hours, err := one.Hours(time.Time{})
	if err != nil {
		t.Fatalf("Hours of a closed cycle: %v", err)
	}
	if hours != 1 {
		t.Errorf("a one-hour cycle reads as %v hours, want 1", hours)
	}
	if one.Open() {
		t.Error("a cycle with a teardown time reads as open")
	}

	total, err := environment.EnvironmentHours([]environment.Cycle{one, two}, time.Time{})
	if err != nil {
		t.Fatalf("EnvironmentHours: %v", err)
	}
	if total != 3 {
		t.Errorf("EnvironmentHours of a one-hour and a two-hour cycle = %v, want 3", total)
	}

	// An open cycle is measured to now rather than to a teardown it has none of.
	open := environment.Cycle{ID: "ecy_open", BeganAt: record.FormatTime(began)}
	if !open.Open() {
		t.Error("a cycle with no teardown time reads as closed")
	}
	hours, err = open.Hours(began.Add(30 * time.Minute))
	if err != nil {
		t.Fatalf("Hours of an open cycle: %v", err)
	}
	if hours != 0.5 {
		t.Errorf("an open cycle measured 30 minutes on reads as %v hours, want 0.5", hours)
	}
}

// TestDDLListsEveryReason keeps the CHECK constraint and [environment.Reasons]
// from disagreeing: the constraint is SQL text rather than built from the
// slice, so this is what says they still name the same reasons.
func TestDDLListsEveryReason(t *testing.T) {
	const open = "constraint reason_matches_teardown check ((torn_down_at <> '') = (reason in ("
	var statement string
	for _, s := range environment.DDL {
		if strings.Contains(s, open) {
			statement = s
		}
	}
	if statement == "" {
		t.Fatalf("no statement has %q", open)
	}
	i := strings.Index(statement, open)
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(environment.Reasons) {
		t.Fatalf("the constraint lists %d reasons, Reasons has %d", len(listed), len(environment.Reasons))
	}
	for n, r := range environment.Reasons {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(r)+"'"; got != want {
			t.Errorf("the constraint lists %s where Reasons has %s", got, want)
		}
	}
}

// TestAReclamationLeavesTheEnvironmentComposableAgain: torn down for good on
// the three events, an environment is also reclaimed meanwhile from an item
// running nothing — the branch and the builds persist, and the environment is
// the one part discarded and composed again.
func TestAReclamationLeavesTheEnvironmentComposableAgain(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)

	env, err := candidates.Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/candidate"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	if err := candidates.TearDown(ctx, deployer, env.ID, environment.ReasonReclaimed, environment.Rate{}); err != nil {
		t.Fatalf("TearDown reclaiming: %v", err)
	}

	read, found, err := environment.ForItem(ctx, pool, "it_a")
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if !read.Live() {
		t.Error("an environment reclaimed and not torn down for good reads as not live")
	}
	if read.TornDownReason != "" {
		t.Errorf("a reclaimed environment carries the teardown reason %q, want none", read.TornDownReason)
	}

	cycles, err := environment.Cycles(ctx, pool, env.ID)
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}
	if len(cycles) != 1 || cycles[0].Open() || cycles[0].Reason != environment.ReasonReclaimed {
		t.Fatalf("Cycles after one reclamation = %+v, want one closed cycle reclaimed", cycles)
	}

	// No cycle is open until the item next reaches Deploy to candidate
	// environment and the deployer composes again.
	if err := candidates.RunCouldStart(ctx, env.ID); !errors.Is(err, environment.ErrNoOpenCycle) {
		t.Errorf("RunCouldStart on a reclaimed environment = %v, want ErrNoOpenCycle", err)
	}

	if err := candidates.Recompose(ctx, deployer, env.ID, environment.Composition{}); err != nil {
		t.Fatalf("Recompose after a reclamation: %v", err)
	}
	cycles, err = environment.Cycles(ctx, pool, env.ID)
	if err != nil {
		t.Fatalf("Cycles after Recompose: %v", err)
	}
	if len(cycles) != 2 || !cycles[1].Open() {
		t.Fatalf("Cycles after Recompose = %+v, want a second cycle open", cycles)
	}

	if err := candidates.RunCouldStart(ctx, env.ID); err != nil {
		t.Fatalf("RunCouldStart on the reopened cycle: %v", err)
	}
	cycles, err = environment.Cycles(ctx, pool, env.ID)
	if err != nil {
		t.Fatalf("Cycles after RunCouldStart: %v", err)
	}
	if cycles[1].RunCouldStartAt == "" {
		t.Error("RunCouldStart wrote no time on the open cycle")
	}

	// Torn down for good this time, and not put back.
	if err := candidates.TearDown(ctx, deployer, env.ID, environment.ReasonMerged, environment.Rate{}); err != nil {
		t.Fatalf("TearDown for good: %v", err)
	}
	if read, _, err = environment.ForItem(ctx, pool, "it_a"); err != nil {
		t.Fatalf("ForItem after the final teardown: %v", err)
	}
	if read.Live() {
		t.Error("the environment reads as live after a teardown for good")
	}
	if err := candidates.TearDown(ctx, deployer, env.ID, environment.ReasonReclaimed, environment.Rate{}); !errors.Is(err, environment.ErrAlreadyTornDown) {
		t.Errorf("reclaiming after a teardown for good = %v, want ErrAlreadyTornDown", err)
	}
}

// TestTearDownConvertsAtTheRateInForce: where the service record's
// environment-hour rate is in force at the write, the converted amount for the
// cycle's span is computed and fixed there, never repriced. Where none is in
// force the amount is nothing rather than a rate of nothing priced.
func TestTearDownConvertsAtTheRateInForce(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)

	env, err := candidates.Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/candidate"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	// The composition began two hours ago, set directly so the span is exact
	// rather than however long the test took to run.
	twoHoursAgo := record.FormatTime(time.Now().Add(-2 * time.Hour))
	if _, err := pool.Exec(ctx, `update `+environment.CycleTable+`
		set began_at = $1 where environment_id = $2`, twoHoursAgo, env.ID); err != nil {
		t.Fatalf("backdating the cycle: %v", err)
	}

	if err := candidates.TearDown(ctx, deployer, env.ID, environment.ReasonMerged,
		environment.Rate{PerHour: 3, InForce: true}); err != nil {
		t.Fatalf("TearDown priced: %v", err)
	}
	cycles, err := environment.Cycles(ctx, pool, env.ID)
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}
	if len(cycles) != 1 {
		t.Fatalf("Cycles = %+v, want one", cycles)
	}
	c := cycles[0]
	if !c.Rate.InForce || c.Rate.PerHour != 3 {
		t.Errorf("the cycle's rate reads back as %+v, want 3 in force", c.Rate)
	}
	// Roughly six: two hours at a rate of three, allowing for the moment TearDown
	// itself took to run.
	if c.ConvertedAmount < 5.9 || c.ConvertedAmount > 6.1 {
		t.Errorf("the converted amount reads back as %v, want about 6", c.ConvertedAmount)
	}

	// A second environment torn down with no rate in force converts to nothing.
	other, err := candidates.Compose(ctx, deployer, "it_b", theProject,
		oneTarget("/srv/other"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if err := candidates.TearDown(ctx, deployer, other.ID, environment.ReasonMerged, environment.Rate{}); err != nil {
		t.Fatalf("TearDown unpriced: %v", err)
	}
	otherCycles, err := environment.Cycles(ctx, pool, other.ID)
	if err != nil {
		t.Fatalf("Cycles: %v", err)
	}
	if otherCycles[0].Rate.InForce || otherCycles[0].ConvertedAmount != 0 {
		t.Errorf("an unpriced teardown reads back as %+v, want no rate and no amount", otherCycles[0])
	}
}

// TestTearDownRefusesAReasonNotAmongTheFour: a cycle ends for one of the four
// reasons and none other.
func TestTearDownRefusesAReasonNotAmongTheFour(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)

	env, err := candidates.Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/candidate"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if err := candidates.TearDown(ctx, deployer, env.ID, environment.Reason("cancelled"), environment.Rate{}); !errors.Is(err, environment.ErrReasonUnknown) {
		t.Errorf("TearDown with an unknown reason = %v, want ErrReasonUnknown", err)
	}

	if _, err := pool.Exec(ctx, `update `+environment.CycleTable+`
		set torn_down_at = $1, reason = 'cancelled' where environment_id = $2`,
		record.Now(), env.ID); err == nil {
		t.Error("the store accepted a reason not among the four")
	}
}
