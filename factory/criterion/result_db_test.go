package criterion_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// deployer is who writes what a run on a candidate environment produced: the
// one component that reaches a deploy target is the one that reports what it
// observed there.
var deployer = record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed}

// buildRunner is the other writer of results: the encodings that declare the
// build are decided in the build's own process, run 0, against no environment.
var buildRunner = record.Actor{Kind: record.KindComponent, Key: "buildrunner", Basis: record.BasisClaimed}

// onEnvironment is a run of the given number against the given composition.
func onEnvironment(buildID string, number int, composition string) criterion.Run {
	return criterion.Run{
		BuildID: buildID, Number: number,
		Place: criterion.PlaceCandidateEnvironment, Composition: composition,
	}
}

// TestEachRunIsItsOwnRowsAndNothingIsOverwritten: the identity is the build,
// the run, and the criterion. Keyed by build and criterion alone, a second run
// would overwrite the first, and the disagreement undecided is computed from
// would be gone before anything read it.
func TestEachRunIsItsOwnRowsAndNothingIsOverwritten(t *testing.T) {
	ctx, pool, token := newSet(t)
	const buildID, composition = "bl_a", "svc_b@rl_1;seed@1"
	first, second := "cr_"+strings.Repeat("a", 32), "cr_"+strings.Repeat("b", 32)

	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 1, composition),
		map[string]criterion.Outcome{first: criterion.OutcomePassed, second: criterion.OutcomeFailed}); err != nil {
		t.Fatalf("RecordResults: %v", err)
	}
	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 2, composition),
		map[string]criterion.Outcome{first: criterion.OutcomeFailed}); err != nil {
		t.Fatalf("RecordResults again: %v", err)
	}

	results, err := criterion.ResultsForBuild(ctx, pool, buildID)
	if err != nil {
		t.Fatalf("ResultsForBuild: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("%d rows stand after two runs, want 3: %+v", len(results), results)
	}
	for _, r := range results {
		if r.BuildID != buildID || r.Composition != composition || r.Place != criterion.PlaceCandidateEnvironment {
			t.Errorf("a result reads %+v, want the build, the composition, and the environment", r)
		}
		if r.Actor != deployer {
			t.Errorf("a result's actor is %+v, want the deployer", r.Actor)
		}
		if _, err := time.Parse(record.TimeLayout, r.At); err != nil {
			t.Errorf("%s has timestamp %q: %v", r.ID, r.At, err)
		}
	}

	// A gate reads the latest run per criterion, and every earlier run stands
	// with the composition it was decided against.
	latest, err := criterion.Latest(ctx, pool, buildID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	got := map[string]criterion.Result{}
	for _, r := range latest {
		got[r.CriterionID] = r
	}
	if len(got) != 2 {
		t.Fatalf("Latest returned %d criteria, want 2: %+v", len(got), latest)
	}
	if got[first].Run != 2 || got[first].Outcome != criterion.OutcomeFailed {
		t.Errorf("the latest result of %s is %+v, want run 2 failed", first, got[first])
	}
	if got[second].Run != 1 || got[second].Outcome != criterion.OutcomeFailed {
		t.Errorf("the latest result of %s is %+v, want run 1, which is the last run that decided it", second, got[second])
	}
}

// TestUndecidedIsDerivedFromTwoRunsWhoseCompositionsMatch: an encoding that
// produced a failure and a pass over one build decided nothing, so the
// criterion is undecided for that build. Two runs against compositions that
// differ are two answers to two questions and make nothing undecided.
func TestUndecidedIsDerivedFromTwoRunsWhoseCompositionsMatch(t *testing.T) {
	ctx, pool, token := newSet(t)
	const buildID = "bl_a"
	repeated, recomposed := "cr_"+strings.Repeat("a", 32), "cr_"+strings.Repeat("b", 32)

	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 1, "seed@1"),
		map[string]criterion.Outcome{repeated: criterion.OutcomePassed, recomposed: criterion.OutcomePassed}); err != nil {
		t.Fatalf("RecordResults: %v", err)
	}
	// The confirming run repeats the same composition: a disagreement here is
	// the repetition and nothing else.
	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 2, "seed@1"),
		map[string]criterion.Outcome{repeated: criterion.OutcomeFailed}); err != nil {
		t.Fatalf("RecordResults again: %v", err)
	}
	// A dependency moved between the runs, so this is another question.
	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 3, "seed@2"),
		map[string]criterion.Outcome{recomposed: criterion.OutcomeFailed}); err != nil {
		t.Fatalf("RecordResults over the recomposed environment: %v", err)
	}

	undecided, err := criterion.Undecided(ctx, pool, buildID)
	if err != nil {
		t.Fatalf("Undecided: %v", err)
	}
	if len(undecided) != 1 || undecided[0] != repeated {
		t.Errorf("Undecided = %v, want [%s] alone", undecided, repeated)
	}
}

// TestTheBuildsOwnProcessWritesRunZero: an encoding that declares the build is
// decided by the build runner as it builds, so the Implementation gate reads
// the result before any environment exists — and such a run has no composition,
// there being no environment to compose.
func TestTheBuildsOwnProcessWritesRunZero(t *testing.T) {
	ctx, pool, token := newSet(t)
	const buildID = "bl_a"
	unit := "cr_" + strings.Repeat("c", 32)

	if err := criterion.RecordResults(ctx, pool, token, buildRunner,
		criterion.Run{BuildID: buildID, Number: 0, Place: criterion.PlaceBuild},
		map[string]criterion.Outcome{unit: criterion.OutcomePassed}); err != nil {
		t.Fatalf("RecordResults over the build's own process: %v", err)
	}
	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 1, "seed@1"),
		map[string]criterion.Outcome{"cr_" + strings.Repeat("d", 32): criterion.OutcomePassed}); err != nil {
		t.Fatalf("RecordResults over the environment: %v", err)
	}

	// Latest is per criterion and not the build's highest run, so what the
	// build's own process decided is still read at the gate.
	latest, err := criterion.Latest(ctx, pool, buildID)
	if err != nil {
		t.Fatalf("Latest: %v", err)
	}
	found := false
	for _, r := range latest {
		if r.CriterionID == unit {
			found = true
			if r.Run != 0 || r.Place != criterion.PlaceBuild || r.Composition != "" {
				t.Errorf("what the build decided reads %+v, want run 0 in the build with no composition", r)
			}
		}
	}
	if !found {
		t.Errorf("Latest = %+v, and what the build's own process decided is not in it", latest)
	}

	// A criterion the build decided is never repeated over one build, so
	// undecided cannot arise for it.
	if undecided, err := criterion.Undecided(ctx, pool, buildID); err != nil || len(undecided) != 0 {
		t.Errorf("Undecided = %v, %v, want none", undecided, err)
	}
}

// TestARunIsRefusedWhereItDisagreesWithItsPlace: the build's own process is
// run 0 and carries no composition; a run on a candidate environment is
// numbered from 1 by the deployer and carries the composition it ran against.
// The store refuses each around the writer too.
func TestARunIsRefusedWhereItDisagreesWithItsPlace(t *testing.T) {
	ctx, pool, token := newSet(t)
	id := "cr_" + strings.Repeat("a", 32)
	passed := map[string]criterion.Outcome{id: criterion.OutcomePassed}

	for _, refused := range []struct {
		run  criterion.Run
		want error
	}{
		{criterion.Run{BuildID: "", Number: 1, Place: criterion.PlaceCandidateEnvironment, Composition: "s"}, criterion.ErrBuildIDEmpty},
		{criterion.Run{BuildID: "bl_a", Number: 1, Place: "elsewhere", Composition: "s"}, criterion.ErrPlaceUnknown},
		{criterion.Run{BuildID: "bl_a", Number: 1, Place: criterion.PlaceBuild}, criterion.ErrRunMismatch},
		{criterion.Run{BuildID: "bl_a", Number: 0, Place: criterion.PlaceBuild, Composition: "s"}, criterion.ErrCompositionMismatch},
		{criterion.Run{BuildID: "bl_a", Number: 0, Place: criterion.PlaceCandidateEnvironment, Composition: "s"}, criterion.ErrRunMismatch},
		{criterion.Run{BuildID: "bl_a", Number: 1, Place: criterion.PlaceCandidateEnvironment}, criterion.ErrCompositionMismatch},
	} {
		if err := criterion.RecordResults(ctx, pool, token, deployer, refused.run, passed); !errors.Is(err, refused.want) {
			t.Errorf("RecordResults over %+v = %v, want %v", refused.run, err, refused.want)
		}
	}

	_, err := pool.Exec(ctx, `insert into `+criterion.ResultTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, run, criterion_id, outcome, place, composition)
		values ($1, $2, 'component', 'deployer', 'claimed', $3, 'bl_a', 0, $4, 'passed', 'candidate_environment', 'seed@1')`,
		record.NewID(criterion.ResultIDPrefix), criterion.FormatVersionResult, record.Now(), id)
	if err == nil || !strings.Contains(err.Error(), "run_matches_place") {
		t.Errorf("inserting a candidate run numbered 0 = %v, want a violation of run_matches_place", err)
	}
	_, err = pool.Exec(ctx, `insert into `+criterion.ResultTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, run, criterion_id, outcome, place, composition)
		values ($1, $2, 'component', 'buildrunner', 'claimed', $3, 'bl_a', 0, $4, 'passed', 'build', 'seed@1')`,
		record.NewID(criterion.ResultIDPrefix), criterion.FormatVersionResult, record.Now(), id)
	if err == nil || !strings.Contains(err.Error(), "composition_matches_place") {
		t.Errorf("inserting a build-decided result carrying a composition = %v, want a violation of composition_matches_place", err)
	}
}

// TestUndecidedIsNeverRecorded: what is written is what was observed, and no
// run observes an undecided — it is the disagreement between two of them,
// derived at the read.
func TestUndecidedIsNeverRecorded(t *testing.T) {
	ctx, pool, token := newSet(t)
	id := "cr_" + strings.Repeat("a", 32)

	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment("bl_a", 1, "seed@1"),
		map[string]criterion.Outcome{id: criterion.OutcomeUndecided}); !errors.Is(err, criterion.ErrOutcomeNotObserved) {
		t.Errorf("RecordResults with an undecided = %v, want ErrOutcomeNotObserved", err)
	}
	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment("bl_a", 1, "seed@1"),
		map[string]criterion.Outcome{id: criterion.Outcome("flaky")}); !errors.Is(err, criterion.ErrOutcomeUnknown) {
		t.Errorf("RecordResults with an outcome outside the two = %v, want ErrOutcomeUnknown", err)
	}
	if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment("bl_a", 1, "seed@1"),
		map[string]criterion.Outcome{"": criterion.OutcomePassed}); !errors.Is(err, criterion.ErrCriterionIDEmpty) {
		t.Errorf("RecordResults naming no criterion = %v, want ErrCriterionIDEmpty", err)
	}
	if err := criterion.RecordResults(ctx, pool, token, record.Actor{}, onEnvironment("bl_a", 1, "seed@1"),
		map[string]criterion.Outcome{id: criterion.OutcomePassed}); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("RecordResults with no actor = %v, want ErrKindUnknown", err)
	}

	_, err := pool.Exec(ctx, `insert into `+criterion.ResultTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, run, criterion_id, outcome, place, composition)
		values ($1, $2, 'component', 'deployer', 'claimed', $3, 'bl_a', 1, $4, 'undecided', 'candidate_environment', 'seed@1')`,
		record.NewID(criterion.ResultIDPrefix), criterion.FormatVersionResult, record.Now(), id)
	if err == nil || !strings.Contains(err.Error(), "outcome_observed") {
		t.Errorf("inserting an undecided result = %v, want a violation of outcome_observed", err)
	}
}

// TestUnreliableIsTheDisagreementRateAcrossBuilds: a criterion carries an
// outcome history, derived and never authored, and above a bound it is
// unreliable. Undecided reads a disagreement inside one build's own repeated
// run; this reads one across builds that repeated nothing. The bound itself is
// [service.UnreliableBoundInForce]: an authored one, or, where an owner
// authored none, [service.ShippedUnreliableBound] — which the flapping
// criterion's rate crosses, the shipped default doing the work an authored
// bound would otherwise have to.
func TestUnreliableIsTheDisagreementRateAcrossBuilds(t *testing.T) {
	ctx, pool, token := newSet(t)
	steady := "cr_" + strings.Repeat("a", 32)
	flapping := "cr_" + strings.Repeat("b", 32)

	builds := []string{"bl_1", "bl_2", "bl_3", "bl_4"}
	flappingOutcomes := []criterion.Outcome{
		criterion.OutcomePassed, criterion.OutcomeFailed, criterion.OutcomePassed, criterion.OutcomePassed,
	}
	for n, buildID := range builds {
		if err := criterion.RecordResults(ctx, pool, token, deployer, onEnvironment(buildID, 1, "seed@1"),
			map[string]criterion.Outcome{
				steady:   criterion.OutcomePassed,
				flapping: flappingOutcomes[n],
			}); err != nil {
			t.Fatalf("RecordResults over %s: %v", buildID, err)
		}
	}

	authored := gatepolicy.Authored{Number: 0.2, Present: true}
	unauthored := gatepolicy.Authored{}

	steadiness, err := criterion.Unreliable(ctx, pool, steady, builds, authored)
	if err != nil {
		t.Fatalf("Unreliable: %v", err)
	}
	if steadiness.Unreliable || steadiness.Rate != 0 || steadiness.Builds != 4 {
		t.Errorf("the steady criterion reads %+v, want four builds and no disagreement", steadiness)
	}

	// No owner authored a bound, so this crosses the shipped default rather
	// than a number the test picked.
	flakiness, err := criterion.Unreliable(ctx, pool, flapping, builds, unauthored)
	if err != nil {
		t.Fatalf("Unreliable: %v", err)
	}
	if flakiness.Disagreements != 1 || flakiness.Rate != 0.25 || !flakiness.Unreliable {
		t.Errorf("the flapping criterion reads %+v, want one disagreement in four and above the shipped bound of %v",
			flakiness, service.ShippedUnreliableBound)
	}
	// An authored bound reads what was authored rather than the shipped
	// default, and a safeguard may only raise it, which is what takes a
	// criterion back out of the gate's way.
	raisedBound := gatepolicy.Authored{Number: 0.5, Present: true}
	if raised, err := criterion.Unreliable(ctx, pool, flapping, builds, raisedBound); err != nil || raised.Unreliable {
		t.Errorf("Unreliable against a raised bound = %+v, %v, want not unreliable", raised, err)
	}
	// One build is nothing for an outcome to disagree with.
	if one, err := criterion.Unreliable(ctx, pool, flapping, builds[:1], authored); err != nil || one.Unreliable {
		t.Errorf("Unreliable over one build = %+v, %v, want not unreliable", one, err)
	}
}
