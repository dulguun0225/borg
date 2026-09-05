package mergequeue_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
)

// TestTheThreeReadingsOfAFailure: a criterion that passed on the candidate
// environment and fails here is run once more, over the re-verification's own
// build and composition. A repeat that disagrees makes the criterion undecided
// and the score learns as from a reject; a failure that repeats with the two
// compositions matching is the candidate failing against the master it will
// actually merge into, learned as from a reject; a failure that repeats with a
// dependency's release moved between the runs is learned as from a hold, and the
// per-author prior does not move. The attempt counts on every one of the three.
func TestTheThreeReadingsOfAFailure(t *testing.T) {
	moved := environment.Composition{
		From:        []environment.Composed{{ServiceID: "svc_dependency", ReleaseID: "rel_two"}},
		SeedVersion: "seed-1",
	}
	approved := environment.Composition{
		From:        []environment.Composed{{ServiceID: "svc_dependency", ReleaseID: "rel_one"}},
		SeedVersion: "seed-1",
	}

	for _, c := range []struct {
		name         string
		verified     mergequeue.Verified
		confirmation mergequeue.Confirmation
		reading      mergequeue.Reading
		learnsAs     gate.Verdict
		priorMoves   bool
		namesMoved   bool
	}{
		{
			name: "an encoding that could not hold its answer",
			verified: mergequeue.Verified{
				Commit: "commit-one", BuildID: "bl_one", Why: "criterion cr_a failed",
				FailedCriteria: []string{"cr_a"}, ApprovedComposition: approved, Composition: approved,
			},
			confirmation: mergequeue.Confirmation{Disagreed: []string{"cr_a"}},
			reading:      mergequeue.ReadingAnEncodingCouldNotHoldItsAnswer,
			learnsAs:     gate.VerdictReject,
			priorMoves:   true,
		},
		{
			name: "against the master it merges into",
			verified: mergequeue.Verified{
				Commit: "commit-one", BuildID: "bl_one", Why: "criterion cr_a failed",
				FailedCriteria: []string{"cr_a"}, ApprovedComposition: approved, Composition: approved,
			},
			confirmation: mergequeue.Confirmation{Repeated: []string{"cr_a"}, Why: "criterion cr_a failed again"},
			reading:      mergequeue.ReadingAgainstTheMasterItMerges,
			learnsAs:     gate.VerdictReject,
			priorMoves:   true,
		},
		{
			name: "a dependency's release moved between the runs",
			verified: mergequeue.Verified{
				Commit: "commit-one", BuildID: "bl_one", Why: "criterion cr_a failed",
				FailedCriteria: []string{"cr_a"}, ApprovedComposition: approved, Composition: moved,
			},
			confirmation: mergequeue.Confirmation{Repeated: []string{"cr_a"}},
			reading:      mergequeue.ReadingADependencysReleaseMoved,
			learnsAs:     gate.VerdictHold,
			priorMoves:   false,
			namesMoved:   true,
		},
		{
			name: "a failure no criterion decided is read against the master it merges into",
			verified: mergequeue.Verified{
				Commit: "commit-one", BuildID: "bl_one",
				Why: "the candidate does not merge cleanly onto master",
			},
			reading:    mergequeue.ReadingAgainstTheMasterItMerges,
			learnsAs:   gate.VerdictReject,
			priorMoves: true,
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			repo := newRepository()
			ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
			it := queued(ctx, t, pool, token, 1)
			repo.verified[it.ID] = c.verified
			repo.confirmed[it.ID] = c.confirmation

			pass, err := q.Run(ctx, serviceID)
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			if len(pass.Outcomes) != 1 || pass.Outcomes[0].Merged {
				t.Fatalf("the outcomes are %+v, want the one candidate rejected", pass.Outcomes)
			}
			r := pass.Outcomes[0].Rejection
			if r.Reading != c.reading {
				t.Errorf("the rejection reads %q, want %q", r.Reading, c.reading)
			}
			if r.LearnsAs != c.learnsAs || r.PriorMoves != c.priorMoves {
				t.Errorf("the score learns as %q with the prior moving %v, want %q and %v",
					r.LearnsAs, r.PriorMoves, c.learnsAs, c.priorMoves)
			}
			if !r.CountsAnAttempt || r.ReturnsTo != gate.ReturnsToImplementation {
				t.Errorf("the rejection counts an attempt %v and returns to %q, and the attempt counts on every reading",
					r.CountsAnAttempt, r.ReturnsTo)
			}
			if c.namesMoved {
				if len(r.Moved) != 1 || r.Moved[0].ServiceID != "svc_dependency" ||
					r.Moved[0].From != "rel_one" || r.Moved[0].To != "rel_two" {
					t.Errorf("the rejection names moved %+v, want the dependency's release", r.Moved)
				}
			} else if len(r.Moved) != 0 {
				t.Errorf("the rejection names moved %+v, and nothing moved", r.Moved)
			}
			// The confirming run happens once, and only where a criterion failed.
			wantConfirmations := 1
			if len(c.verified.FailedCriteria) == 0 {
				wantConfirmations = 0
			}
			if len(repo.confirmations) != wantConfirmations {
				t.Errorf("the confirming run was asked %d times, want %d — once, and never until green",
					len(repo.confirmations), wantConfirmations)
			}
		})
	}
}

// TestASeedReplacedBetweenTheRunsIsACompositionThatDiffers: all three parts of a
// composition are compared together, so a seed or a value set replaced between
// two runs is read the way a moved release is.
func TestASeedReplacedBetweenTheRunsIsACompositionThatDiffers(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{
		Commit: "commit-one", BuildID: "bl_one", Why: "criterion cr_a failed",
		FailedCriteria:      []string{"cr_a"},
		ApprovedComposition: environment.Composition{SeedVersion: "seed-1", ValueSetVersion: "values-1"},
		Composition:         environment.Composition{SeedVersion: "seed-2", ValueSetVersion: "values-1"},
	}
	repo.confirmed[it.ID] = mergequeue.Confirmation{Repeated: []string{"cr_a"}}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	r := pass.Outcomes[0].Rejection
	if r.Reading != mergequeue.ReadingADependencysReleaseMoved {
		t.Fatalf("the rejection reads %q, want the composition that differs", r.Reading)
	}
	if len(r.Moved) != 1 || r.Moved[0].What != mergequeue.MovedSeed ||
		r.Moved[0].From != "seed-1" || r.Moved[0].To != "seed-2" {
		t.Errorf("the rejection names moved %+v, want the seed replaced", r.Moved)
	}
}

// TestADesignSystemMoveFailsTheCandidateWhateverItsCriteriaAnswered: the
// re-verification compares one field no criterion reads. Where the two builds
// name design system constraint records that differ on a component or a token the
// candidate's build uses, the candidate fails, the log entry names the replaced
// record, and the rejection takes the reading a moved dependency takes.
func TestADesignSystemMoveFailsTheCandidateWhateverItsCriteriaAnswered(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)
	built(ctx, t, pool, token, it, "commit-approved", "cs_before", nil)
	repo.verify = func(it item.Item) mergequeue.Verified {
		reverified := built(ctx, t, pool, token, it, "commit-one", "cs_after", nil)
		return mergequeue.Verified{Commit: "commit-one", BuildID: reverified.ID, Passed: true}
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || pass.Outcomes[0].Merged {
		t.Fatalf("the outcomes are %+v, want the candidate rejected on the move", pass.Outcomes)
	}
	r := pass.Outcomes[0].Rejection
	if r.ReplacedDesignSystemRecord != "cs_before" {
		t.Errorf("the rejection names the replaced record %q, want cs_before", r.ReplacedDesignSystemRecord)
	}
	if r.Reading != mergequeue.ReadingADependencysReleaseMoved || r.LearnsAs != gate.VerdictHold || r.PriorMoves {
		t.Errorf("the rejection reads %q, learns as %q, prior moves %v — an owner's move is no defect of the author's",
			r.Reading, r.LearnsAs, r.PriorMoves)
	}
	if !r.CountsAnAttempt {
		t.Error("the rejection counts no attempt, and the attempt counts on this reading too")
	}
	if len(repo.fastForwards) != 0 {
		t.Errorf("the queue fast-forwarded %v, and the candidate failed whatever its criteria answered", repo.fastForwards)
	}
}

// TestAMoveThatTouchesNothingTheBuildUsesDoesNotReject: the comparison is over a
// component or a token the candidate's build uses, so a factory with the
// constraint records to read lets a move that touched neither through.
func TestAMoveThatTouchesNothingTheBuildUsesDoesNotReject(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{
		Repository: repo, DesignSystem: sameDesignSystem{},
	})
	it := queued(ctx, t, pool, token, 1)
	built(ctx, t, pool, token, it, "commit-approved", "cs_before", nil)
	repo.verify = func(it item.Item) mergequeue.Verified {
		reverified := built(ctx, t, pool, token, it, "commit-one", "cs_after", nil)
		return mergequeue.Verified{Commit: "commit-one", BuildID: reverified.ID, Passed: true}
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("the outcomes are %+v, want the candidate merged", pass.Outcomes)
	}
}

// TestTheReresolvedSetsDigestsAreComparedToTheApprovedBuilds: a version is not an
// identity for bytes, so the digests are what is compared, and a difference
// rejects on the terms a candidate that fails its own merits already does.
func TestTheReresolvedSetsDigestsAreComparedToTheApprovedBuilds(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)
	built(ctx, t, pool, token, it, "commit-approved", "", []build.ResolvedEntry{
		{Ecosystem: "go", Source: "proxy.golang.org", Package: "example.com/x", Version: "v1.2.3",
			Digest: "sha256:aaa", Licence: "MIT"},
	})
	repo.verify = func(it item.Item) mergequeue.Verified {
		reverified := built(ctx, t, pool, token, it, "commit-one", "", []build.ResolvedEntry{
			{Ecosystem: "go", Source: "proxy.golang.org", Package: "example.com/x", Version: "v1.2.3",
				Digest: "sha256:bbb", Licence: "MIT"},
		})
		return mergequeue.Verified{Commit: "commit-one", BuildID: reverified.ID, Passed: true}
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || pass.Outcomes[0].Merged {
		t.Fatalf("the outcomes are %+v, want the candidate rejected on the moved digest", pass.Outcomes)
	}
	r := pass.Outcomes[0].Rejection
	if r.Reading != mergequeue.ReadingAgainstTheMasterItMerges || !r.CountsAnAttempt {
		t.Errorf("the rejection reads %q and counts an attempt %v", r.Reading, r.CountsAnAttempt)
	}
	if r.Why == "" {
		t.Error("the rejection says nothing about what moved")
	}
	if len(repo.fastForwards) != 0 {
		t.Errorf("the queue fast-forwarded %v, and a difference rejects there", repo.fastForwards)
	}
}

// TestAnUnchangedResolvedSetMerges is the other half of that comparison: two
// builds that resolved the same bytes are no difference, and the candidate merges.
func TestAnUnchangedResolvedSetMerges(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})
	it := queued(ctx, t, pool, token, 1)
	entries := []build.ResolvedEntry{
		{Ecosystem: "go", Source: "proxy.golang.org", Package: "example.com/x", Version: "v1.2.3",
			Digest: "sha256:aaa", Licence: "MIT"},
	}
	built(ctx, t, pool, token, it, "commit-approved", "", entries)
	repo.verify = func(it item.Item) mergequeue.Verified {
		reverified := built(ctx, t, pool, token, it, "commit-one", "", entries)
		return mergequeue.Verified{Commit: "commit-one", BuildID: reverified.ID, Passed: true}
	}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || !pass.Outcomes[0].Merged {
		t.Fatalf("the outcomes are %+v, want the candidate merged", pass.Outcomes)
	}
}

// TestAFailureInvalidatesTheSpeculationBehindIt: a candidate entering the queue
// re-verifies against master plus every candidate ahead of it. A failure
// invalidates the speculation behind it, and those candidates re-verify against
// the master that actually resulted — counting nothing, because they were redone
// because of somebody else.
func TestAFailureInvalidatesTheSpeculationBehindIt(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{Repository: repo})

	first := queued(ctx, t, pool, token, 1)
	second := queued(ctx, t, pool, token, 2)
	third := queued(ctx, t, pool, token, 3)
	repo.verified[first.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one",
		Why: "criterion cr_a failed"}
	repo.confirmed[first.ID] = mergequeue.Confirmation{Repeated: []string{"cr_a"}}
	repo.verified[second.ID] = mergequeue.Verified{Commit: "commit-two", BuildID: "bl_two", Passed: true}
	repo.verified[third.ID] = mergequeue.Verified{Commit: "commit-three", BuildID: "bl_three", Passed: true}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 3 {
		t.Fatalf("the outcomes are %+v, want one per member", pass.Outcomes)
	}
	if pass.Outcomes[0].Merged || !pass.Outcomes[1].Merged || !pass.Outcomes[2].Merged {
		t.Fatalf("the outcomes are %+v, want the first rejected and the two behind it merged", pass.Outcomes)
	}

	// The speculation: each pending member was re-verified against the members
	// ahead of it before any of them fast-forwarded.
	if named := repo.speculations[third.ID]; len(named) != 1 || named[0] != second.ID {
		t.Errorf("the last speculation this candidate was re-verified against is %v, want the master that resulted", named)
	}
	// Three speculative runs and two re-runs behind the failure: the two candidates
	// behind it re-verify against the master that actually resulted.
	if len(repo.reverified) != 5 {
		t.Errorf("the queue re-verified %v, want three speculations and the two the failure invalidated",
			repo.reverified)
	}
	// The two behind the failure count nothing for it: only the rejected candidate
	// carries a rejection.
	for _, outcome := range pass.Outcomes[1:] {
		if outcome.Rejection.CountsAnAttempt {
			t.Errorf("the outcome %+v counts an attempt, and candidates behind a failure count nothing", outcome)
		}
	}
}
