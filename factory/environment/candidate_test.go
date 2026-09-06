package environment_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/record"
)

// deployer is the candidate kind's writer: the one component that reaches a
// deploy target at all is the one that reaches environments.
var deployer = record.Actor{Kind: record.KindComponent, Key: "deployer"}

// composition is what a candidate's environment was composed from: each
// dependency at the release that was current then, and the versions of the seed
// and of the non-production value set.
var composition = environment.Composition{
	From:            []environment.Composed{{ServiceID: "svc_dep", ReleaseID: "rel_one"}},
	SeedVersion:     "seed_one",
	ValueSetVersion: "values_one",
}

// TestACandidatesEnvironmentIsComposedRecomposedAndTornDown is the candidate
// kind's whole life: composed at the approval of the gate that decides its
// deploy, named for the item so two candidates of one service cannot collide,
// recomposed at a re-verification, and torn down for good at the merge with the
// row kept — because the deploy records naming it would otherwise point at
// nothing.
func TestACandidatesEnvironmentIsComposedRecomposedAndTornDown(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)
	const itemID = "it_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

	env, err := candidates.Compose(ctx, deployer, itemID, theProject,
		oneTarget("/srv/candidate"), credential, composition)
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if env.Kind != environment.KindCandidate || env.Name != environment.NameForItem(itemID) {
		t.Errorf("the environment is kind %s named %q, want a candidate's named %q",
			env.Kind, env.Name, environment.NameForItem(itemID))
	}
	if !env.Live() {
		t.Error("a freshly composed environment is not live")
	}

	read, found, err := environment.ForItem(ctx, pool, itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if read.ID != env.ID || !read.Composition.Equal(composition) {
		t.Errorf("ForItem = %+v, want %s composed from %+v", read, env.ID, composition)
	}

	// A second call for one item is refused: the environment is the item's and
	// persists across a rebuild, so a rebuild recomposes rather than composing
	// again.
	if _, err := candidates.Compose(ctx, deployer, itemID, theProject,
		oneTarget("/srv/candidate"), credential, environment.Composition{}); err == nil {
		t.Error("a second Compose for one item was accepted, and the name is derived from the item")
	}

	// Recomposed: the dependencies' current releases have moved since, and so has
	// the seed the store was built from.
	moved := environment.Composition{
		From:            []environment.Composed{{ServiceID: "svc_dep", ReleaseID: "rel_two"}},
		SeedVersion:     "seed_two",
		ValueSetVersion: "values_one",
	}
	if composition.Equal(moved) {
		t.Fatal("the two compositions in this test are equal, and the comparison proves nothing")
	}
	if err := candidates.Recompose(ctx, deployer, env.ID, moved); err != nil {
		t.Fatalf("Recompose: %v", err)
	}
	if read, _, err = environment.ForItem(ctx, pool, itemID); err != nil {
		t.Fatalf("ForItem after Recompose: %v", err)
	}
	if !read.Composition.Equal(moved) {
		t.Errorf("the environment was recomposed from %+v, want %+v", read.Composition, moved)
	}

	if err := candidates.TearDown(ctx, deployer, env.ID, environment.ReasonMerged, environment.Rate{}); err != nil {
		t.Fatalf("TearDown: %v", err)
	}
	if read, _, err = environment.ForItem(ctx, pool, itemID); err != nil {
		t.Fatalf("ForItem after TearDown: %v", err)
	}
	if read.Live() {
		t.Error("the environment reads as live after a teardown for good")
	}
	if read.TornDownReason != environment.ReasonMerged {
		t.Errorf("the teardown reason reads back as %q, want merged", read.TornDownReason)
	}

	// Teardown for good does not run twice and nothing puts one back.
	if err := candidates.TearDown(ctx, deployer, env.ID, environment.ReasonMerged, environment.Rate{}); !errors.Is(err, environment.ErrAlreadyTornDown) {
		t.Errorf("TearDown again = %v, want ErrAlreadyTornDown", err)
	}
	if err := candidates.Recompose(ctx, deployer, env.ID, moved); !errors.Is(err, environment.ErrAlreadyTornDown) {
		t.Errorf("Recompose after a teardown for good = %v, want ErrAlreadyTornDown", err)
	}
}

// TestTheCompositionComparesAllThree: a seed or a value set replaced between two
// runs is a composition that differs, which the merge queue reads as it reads a
// moved release. The comparison is arithmetic over the stored fields and needs no
// database.
func TestTheCompositionComparesAllThree(t *testing.T) {
	same := environment.Composition{
		From:            slices.Clone(composition.From),
		SeedVersion:     composition.SeedVersion,
		ValueSetVersion: composition.ValueSetVersion,
	}
	if !composition.Equal(same) {
		t.Error("two compositions naming the same three things are not equal")
	}
	for _, differs := range []environment.Composition{
		{From: []environment.Composed{{ServiceID: "svc_dep", ReleaseID: "rel_two"}}, SeedVersion: "seed_one", ValueSetVersion: "values_one"},
		{From: slices.Clone(composition.From), SeedVersion: "seed_two", ValueSetVersion: "values_one"},
		{From: slices.Clone(composition.From), SeedVersion: "seed_one", ValueSetVersion: "values_two"},
	} {
		if composition.Equal(differs) {
			t.Errorf("%+v compares equal to %+v", differs, composition)
		}
	}
	if !(environment.Composition{}).Empty() {
		t.Error("a composition of nothing does not read as empty")
	}
}

// TestTheCandidateWriterRefusesWhatIsNotACandidates: the two writers never write
// or touch a record of the other's kind, and a composition entry names both
// halves.
func TestTheCandidateWriterRefusesWhatIsNotACandidates(t *testing.T) {
	ctx, pool, w, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("creating production: %v", err)
	}
	err = candidates.TearDown(ctx, deployer, production.ID, environment.ReasonMerged, environment.Rate{})
	if !errors.Is(err, environment.ErrNotACandidate) {
		t.Errorf("tearing down production = %v, want ErrNotACandidate", err)
	}
	if err := candidates.Recompose(ctx, deployer, production.ID, environment.Composition{}); !errors.Is(err, environment.ErrNotACandidate) {
		t.Errorf("recomposing production = %v, want ErrNotACandidate", err)
	}
	err = candidates.TearDown(ctx, deployer, "env_missing", environment.ReasonMerged, environment.Rate{})
	if !errors.Is(err, environment.ErrNotFound) {
		t.Errorf("tearing down a missing environment = %v, want ErrNotFound", err)
	}

	if _, err := candidates.Compose(ctx, deployer, "", theProject, oneTarget("/srv"), credential, environment.Composition{}); !errors.Is(err, environment.ErrItemIDEmpty) {
		t.Errorf("Compose naming no item = %v, want ErrItemIDEmpty", err)
	}
	if _, err := candidates.Compose(ctx, deployer, "it_a", "", oneTarget("/srv"), credential, environment.Composition{}); !errors.Is(err, environment.ErrProjectIDEmpty) {
		t.Errorf("Compose naming no project = %v, want ErrProjectIDEmpty", err)
	}
	if _, err := candidates.Compose(ctx, deployer, "it_a", theProject, nil, credential, environment.Composition{}); !errors.Is(err, environment.ErrTargetsEmpty) {
		t.Errorf("Compose with no target = %v, want ErrTargetsEmpty", err)
	}
	if _, err := candidates.Compose(ctx, record.Actor{}, "it_a", theProject, oneTarget("/srv"), credential, environment.Composition{}); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Compose with no actor = %v, want ErrKindUnknown", err)
	}
	incomplete := environment.Composition{From: []environment.Composed{{ServiceID: "svc_dep"}}}
	if _, err := candidates.Compose(ctx, deployer, "it_a", theProject, oneTarget("/srv"), credential, incomplete); !errors.Is(err, environment.ErrCompositionIncomplete) {
		t.Errorf("Compose with a composition entry naming no release = %v, want ErrCompositionIncomplete", err)
	}

	// ForItem on an item with no environment is not an error: every item has none
	// until the candidate deploy gate approves.
	if _, found, err := environment.ForItem(ctx, pool, "it_none"); err != nil || found {
		t.Errorf("ForItem on an item with no environment = found %v, %v", found, err)
	}
}

// TestACandidatesEnvironmentHoldsNothingAnOwnerAuthored: the record is created at
// the gate that decides its deploy, so it cannot hold the threshold that decided
// it. Authoring one on it is refused where it is authored rather than left to a
// reader to notice a value nothing will ever compare against.
func TestACandidatesEnvironmentHoldsNothingAnOwnerAuthored(t *testing.T) {
	ctx, pool, _, token := newTable(t)
	env, err := environment.NewCandidates(pool, token).Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/candidate"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if env.Platform != (environment.Platform{}) {
		t.Errorf("a candidate's environment declares the platform %+v, and the platform is production's", env.Platform)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("beginning a transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = environment.SetGateThreshold(ctx, tx, token, owner, env.ID, "merge_to_master", 0.4)
	if !errors.Is(err, environment.ErrNotAnOwnersKind) {
		t.Errorf("authoring a threshold on a candidate's environment = %v, want ErrNotAnOwnersKind", err)
	}
	if err := environment.SetGateThreshold(ctx, tx, token, owner, "env_missing", "merge_to_master", 0.4); !errors.Is(err, environment.ErrNotFound) {
		t.Errorf("authoring a threshold on a missing environment = %v, want ErrNotFound", err)
	}
}

// TestTheCeilingIsCountedPerProject: the count the ceiling is compared against is
// scoped to the production environment named, so an install whose projects run on
// two platforms adds neither count across them.
func TestTheCeilingIsCountedPerProject(t *testing.T) {
	ctx, pool, w, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("creating production: %v", err)
	}
	const otherProject = "prj_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	otherSpec := productionSpec()
	otherSpec.ProjectID = otherProject
	otherProduction, err := w.Create(ctx, owner, otherSpec)
	if err != nil {
		t.Fatalf("creating the second project's production: %v", err)
	}

	first, err := candidates.Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/a"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if _, err := candidates.Compose(ctx, deployer, "it_b", theProject,
		oneTarget("/srv/b"), credential, environment.Composition{}); err != nil {
		t.Fatalf("Compose: %v", err)
	}
	if _, err := candidates.Compose(ctx, deployer, "it_c", otherProject,
		oneTarget("/srv/c"), credential, environment.Composition{}); err != nil {
		t.Fatalf("Compose in the second project: %v", err)
	}

	live, err := environment.CountLiveCandidates(ctx, pool, production.ID)
	if err != nil || live != 2 {
		t.Fatalf("CountLiveCandidates = %d, %v, want the two of this project", live, err)
	}
	if live, err := environment.CountLiveCandidates(ctx, pool, otherProduction.ID); err != nil || live != 1 {
		t.Fatalf("CountLiveCandidates of the second project = %d, %v, want 1", live, err)
	}

	if err := candidates.TearDown(ctx, deployer, first.ID, environment.ReasonMerged, environment.Rate{}); err != nil {
		t.Fatalf("TearDown: %v", err)
	}
	if live, err := environment.CountLiveCandidates(ctx, pool, production.ID); err != nil || live != 1 {
		t.Errorf("CountLiveCandidates after a teardown = %d, %v, want 1", live, err)
	}

	// The count is keyed by a production record and by no other.
	_, err = environment.CountLiveCandidates(ctx, pool, first.ID)
	if !errors.Is(err, environment.ErrNotAProductionEnvironment) {
		t.Errorf("counting against a candidate's environment = %v, want ErrNotAProductionEnvironment", err)
	}
}

// TestTheTornDownCandidatesAreWhatAFailedTeardownIsFoundAgainst: a candidate
// environment the platform holds and the records mark torn down is a teardown
// that failed. What the deployer's pass compares against what the platform
// reports holding is this read, scoped to the production record the room is
// declared on.
func TestTheTornDownCandidatesAreWhatAFailedTeardownIsFoundAgainst(t *testing.T) {
	ctx, pool, w, token := newTable(t)
	candidates := environment.NewCandidates(pool, token)

	production, err := w.Create(ctx, owner, productionSpec())
	if err != nil {
		t.Fatalf("creating production: %v", err)
	}
	standing, err := candidates.Compose(ctx, deployer, "it_a", theProject,
		oneTarget("/srv/a"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}
	torn, err := candidates.Compose(ctx, deployer, "it_b", theProject,
		oneTarget("/srv/b"), credential, environment.Composition{})
	if err != nil {
		t.Fatalf("Compose: %v", err)
	}

	if none, err := environment.TornDownCandidates(ctx, pool, production.ID); err != nil || len(none) != 0 {
		t.Fatalf("TornDownCandidates before any teardown = %+v, %v", none, err)
	}
	if err := candidates.TearDown(ctx, deployer, torn.ID, environment.ReasonMerged, environment.Rate{}); err != nil {
		t.Fatalf("TearDown: %v", err)
	}

	down, err := environment.TornDownCandidates(ctx, pool, production.ID)
	if err != nil {
		t.Fatalf("TornDownCandidates: %v", err)
	}
	if len(down) != 1 || down[0].ID != torn.ID {
		t.Fatalf("TornDownCandidates = %+v, want the one torn down", down)
	}
	if down[0].Live() {
		t.Error("a torn-down environment reads as live")
	}
	// A reclamation is not a teardown for good: the row still stands, and the
	// environment is composed again when the item next reaches the gate.
	if err := candidates.TearDown(ctx, deployer, standing.ID, environment.ReasonReclaimed, environment.Rate{}); err != nil {
		t.Fatalf("TearDown reclaiming: %v", err)
	}
	if down, err = environment.TornDownCandidates(ctx, pool, production.ID); err != nil || len(down) != 1 {
		t.Errorf("TornDownCandidates after a reclamation = %+v, %v, want the one torn down for good", down, err)
	}

	// The read is keyed by a production record and by no other, the way the
	// count of what stands is.
	if _, err := environment.TornDownCandidates(ctx, pool, standing.ID); !errors.Is(err, environment.ErrNotAProductionEnvironment) {
		t.Errorf("reading against a candidate's environment = %v, want ErrNotAProductionEnvironment", err)
	}
}
