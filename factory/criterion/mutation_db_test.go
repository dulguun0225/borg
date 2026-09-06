// The database tests of the mutation score: the reading the deployer records
// beside a run's criteria results, and what the Merge to master gate reads off
// it. They share db_test.go's newSet and result_db_test.go's deployer and
// onEnvironment.
package criterion_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/record"
)

// TestRecordMutationKeepsEveryRunAndTheGateReadsTheLatest: a second run over
// one build is a new run and so a new row, and what the gate reads is the
// highest run's.
func TestRecordMutationKeepsEveryRunAndTheGateReadsTheLatest(t *testing.T) {
	ctx, pool, token := newSet(t)
	const buildID = "bl_a"

	first, err := criterion.RecordMutation(ctx, pool, token, deployer, onEnvironment(buildID, 1, "seed@1"),
		criterion.Mutation{
			Toolchain: "go", Tool: "mutate", Coverage: "go test -cover over the checkout: 1 package(s) reported 80% of statements covered",
			MutantsTested: 10, MutantsDetected: 4,
		})
	if err != nil {
		t.Fatalf("RecordMutation: %v", err)
	}
	if first.Mutation.Score() != 0.4 || !first.Mutation.Derived() {
		t.Errorf("the first reading is %+v, want a derived score of 0.4", first.Mutation)
	}

	if _, err := criterion.RecordMutation(ctx, pool, token, deployer, onEnvironment(buildID, 2, "seed@1"),
		criterion.Mutation{Toolchain: "go", Tool: "mutate", MutantsTested: 10, MutantsDetected: 9}); err != nil {
		t.Fatalf("RecordMutation again: %v", err)
	}

	readings, err := criterion.MutationsForBuild(ctx, pool, buildID)
	if err != nil {
		t.Fatalf("MutationsForBuild: %v", err)
	}
	if len(readings) != 2 || readings[0].Run != 1 || readings[1].Run != 2 {
		t.Fatalf("MutationsForBuild = %+v, want both runs in run order", readings)
	}
	if readings[0].Mutation.Coverage != first.Mutation.Coverage || readings[0].Actor != deployer {
		t.Errorf("the stored reading is %+v, want what the deployer recorded", readings[0])
	}

	latest, found, err := criterion.LatestMutation(ctx, pool, buildID)
	if err != nil || !found {
		t.Fatalf("LatestMutation = %+v, %v, %v", latest, found, err)
	}
	if latest.Run != 2 || latest.Mutation.Score() != 0.9 {
		t.Errorf("LatestMutation = %+v, want run 2 at a score of 0.9", latest)
	}
	if latest.Mutation.Blocks(0.8) {
		t.Error("a score above the floor blocked at the gate")
	}

	// A build nothing mutated is not a reading of zero.
	if _, found, err := criterion.LatestMutation(ctx, pool, "bl_never_mutated"); err != nil || found {
		t.Errorf("LatestMutation over a build nothing mutated = %v, %v, want no reading", found, err)
	}
}

// TestACouldNotDeriveMutationCountsNothingAndNeverPasses: a service the factory
// cannot mutate reads could not derive, which counts no mutants and takes the
// treatment that outcome takes at Merge to master.
func TestACouldNotDeriveMutationCountsNothingAndNeverPasses(t *testing.T) {
	ctx, pool, token := newSet(t)

	recorded, err := criterion.RecordMutation(ctx, pool, token, deployer, onEnvironment("bl_b", 1, "seed@1"),
		criterion.Mutation{
			Toolchain:      "go",
			Coverage:       "go test -cover over the checkout: 1 package(s) reported 80% of statements covered",
			CouldNotDerive: "the checkout names no mutation tool in a tool directive of go.mod",
		})
	if err != nil {
		t.Fatalf("RecordMutation: %v", err)
	}
	if recorded.Mutation.Derived() || !recorded.Mutation.Blocks(0) {
		t.Errorf("a could-not-derive reading is %+v, want one that never passes", recorded.Mutation)
	}

	read, found, err := criterion.LatestMutation(ctx, pool, "bl_b")
	if err != nil || !found {
		t.Fatalf("LatestMutation = %+v, %v, %v", read, found, err)
	}
	if read.Mutation.CouldNotDerive != recorded.Mutation.CouldNotDerive || read.Mutation.MutantsTested != 0 {
		t.Errorf("the stored reading is %+v, want the reason and no counts", read.Mutation)
	}
}

// TestRecordMutationRefusals: the mutation is read at a run on the candidate
// environment, and the counts and the could-not-derive are exclusive — a
// derivation that could not be made counts nothing, and one that was made
// tested at least one mutant.
func TestRecordMutationRefusals(t *testing.T) {
	ctx, pool, token := newSet(t)

	for _, refused := range []struct {
		name string
		run  criterion.Run
		m    criterion.Mutation
		want error
	}{
		{"no build", criterion.Run{Number: 1, Place: criterion.PlaceCandidateEnvironment, Composition: "s"},
			criterion.Mutation{MutantsTested: 1}, criterion.ErrBuildIDEmpty},
		{"the build's own process", criterion.Run{BuildID: "bl_c", Number: 0, Place: criterion.PlaceBuild},
			criterion.Mutation{MutantsTested: 1}, criterion.ErrMutationRunMismatch},
		{"a run numbered 0", onEnvironment("bl_c", 0, "seed@1"),
			criterion.Mutation{MutantsTested: 1}, criterion.ErrMutationRunMismatch},
		{"a derived reading with no mutants", onEnvironment("bl_c", 1, "seed@1"),
			criterion.Mutation{Toolchain: "go"}, criterion.ErrMutationCountsMismatch},
		{"a could-not-derive with counts", onEnvironment("bl_c", 1, "seed@1"),
			criterion.Mutation{CouldNotDerive: "no tool", MutantsTested: 4}, criterion.ErrMutationCountsMismatch},
		{"more detected than tested", onEnvironment("bl_c", 1, "seed@1"),
			criterion.Mutation{MutantsTested: 2, MutantsDetected: 3}, criterion.ErrMutationCountsMismatch},
	} {
		if _, err := criterion.RecordMutation(ctx, pool, token, deployer, refused.run, refused.m); !errors.Is(err, refused.want) {
			t.Errorf("RecordMutation with %s = %v, want %v", refused.name, err, refused.want)
		}
	}

	// The store refuses the same around the writer.
	insert := `insert into ` + criterion.MutationTable + `
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, build_id, run,
		toolchain, tool, coverage, mutants_tested, mutants_detected, could_not_derive)
		values ($1, $2, 'component', 'deployer', 'claimed', $3, 'bl_c', $4, 'go', 'mutate', '', $5, $6, $7)`

	_, err := pool.Exec(ctx, insert, record.NewID(criterion.MutationIDPrefix), criterion.FormatVersionMutation,
		record.Now(), 0, 4, 1, "")
	if err == nil || !strings.Contains(err.Error(), "run_is_a_candidate_environments") {
		t.Errorf("inserting a mutation at run 0 = %v, want a violation of run_is_a_candidate_environments", err)
	}
	_, err = pool.Exec(ctx, insert, record.NewID(criterion.MutationIDPrefix), criterion.FormatVersionMutation,
		record.Now(), 1, 4, 5, "")
	if err == nil || !strings.Contains(err.Error(), "detected_within_tested") {
		t.Errorf("inserting more detected than tested = %v, want a violation of detected_within_tested", err)
	}
	_, err = pool.Exec(ctx, insert, record.NewID(criterion.MutationIDPrefix), criterion.FormatVersionMutation,
		record.Now(), 1, 0, 0, "")
	if err == nil || !strings.Contains(err.Error(), "a_derivation_tested_a_mutant") {
		t.Errorf("inserting a derivation that tested nothing = %v, want a violation of a_derivation_tested_a_mutant", err)
	}
	_, err = pool.Exec(ctx, insert, record.NewID(criterion.MutationIDPrefix), criterion.FormatVersionMutation,
		record.Now(), 1, 4, 1, "no tool")
	if err == nil || !strings.Contains(err.Error(), "could_not_derive_counts_nothing") {
		t.Errorf("inserting a could-not-derive with counts = %v, want a violation of could_not_derive_counts_nothing", err)
	}
}
