package gate_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/gate"
)

// TestAnEventGateDoesNotFireBeforeTheRowAboveIsApproved: what fires an event
// gate is the event it decides becoming next, which needs the row above
// approved. The merge row's is the candidate deploy row.
func TestAnEventGateDoesNotFireBeforeTheRowAboveIsApproved(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	f := mergeFiring
	f.CandidateRunEnded = true
	if _, err := g.Fire(ctx, f); !errors.Is(err, gate.ErrRowAboveNotApproved) {
		t.Fatalf("the merge row fired with nothing approved above it = %v, want ErrRowAboveNotApproved", err)
	}
	pending, err := g.Pending(ctx)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("the refused firing left %d pending row(s), and it appends nothing", len(pending))
	}

	approvedAbove(t, ctx, pool, token, gate.DeployToCandidateEnvironment, f.ItemID)
	if _, err := g.Fire(ctx, f); err != nil {
		t.Fatalf("Fire once the candidate deploy row is approved: %v", err)
	}
}

// TestTheMergeRowDoesNotFireBeforeTheCandidatesRunEnded: the second thing that
// fires the merge row. What it decides is the candidate's own run, so a firing
// before that run ended would decide over nothing.
func TestTheMergeRowDoesNotFireBeforeTheCandidatesRunEnded(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)
	approvedAbove(t, ctx, pool, token, gate.DeployToCandidateEnvironment, mergeFiring.ItemID)

	if _, err := g.Fire(ctx, mergeFiring); !errors.Is(err, gate.ErrCandidateRunNotEnded) {
		t.Fatalf("the merge row fired before the candidate's run ended = %v, want ErrCandidateRunNotEnded", err)
	}

	// Every other row decides no candidate's run, so naming one there is
	// refused for the shape it is.
	above := mergeFiring
	above.Row, above.CandidateRunEnded = gate.Tasks, true
	above.ArtifactID = "art_00000000000000000000000000000a"
	if _, err := g.Fire(ctx, above); !errors.Is(err, gate.ErrFiringIncomplete) {
		t.Errorf("%s named a candidate's run = %v, want ErrFiringIncomplete", above.Row, err)
	}
}

// TestARejectSupersedesEveryApprovalBelowItsTarget: nothing below a reject's
// target stands as approved, so the event gate below it stops firing until what
// was sent back has been approved again.
func TestARejectSupersedesEveryApprovalBelowItsTarget(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.2)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	f := mergeRowFiring(t, ctx, pool, token)
	stands, err := g.ApprovalStands(ctx, f.ItemID, gate.DeployToCandidateEnvironment)
	if err != nil {
		t.Fatalf("ApprovalStands: %v", err)
	}
	if !stands {
		t.Fatal("the candidate deploy row was approved and does not stand as approved")
	}

	opened, err := g.Fire(ctx, f)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{
		Actor: owner, Verdict: gate.VerdictReject,
		Reason: "the spec says two things", ReturnsTo: gate.ReturnsToSpec,
	}); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	stands, err = g.ApprovalStands(ctx, f.ItemID, gate.DeployToCandidateEnvironment)
	if err != nil {
		t.Fatalf("ApprovalStands after the reject: %v", err)
	}
	if stands {
		t.Error("the candidate deploy row's approval stands below a reject that named Spec")
	}
	if _, err := g.Fire(ctx, f); !errors.Is(err, gate.ErrRowAboveNotApproved) {
		t.Errorf("the merge row fired again over a superseded approval = %v, want ErrRowAboveNotApproved", err)
	}

	// The row is fired again once what was sent back has been approved again,
	// which is the re-fire the design says opens fresh rows.
	approvedAbove(t, ctx, pool, token, gate.DeployToCandidateEnvironment, f.ItemID)
	if _, err := g.Fire(ctx, f); err != nil {
		t.Fatalf("Fire after the row above was approved again: %v", err)
	}
}

// TestImplementationIsTheOneArtifactGateWithNoEditInPlace: code is not editable
// at a gate, so a human who wants different code authors it upstream and sends
// the item back to have it built again.
func TestImplementationIsTheOneArtifactGateWithNoEditInPlace(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.9)}, &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	if gate.Implementation.OffersEditInPlace() {
		t.Error("Implementation offers Edit in place")
	}
	for _, row := range []gate.Row{gate.Spec, gate.ImplementationPlan, gate.Tasks, gate.RolePromptOrSkill} {
		if !row.OffersEditInPlace() {
			t.Errorf("%s is an artifact gate and offers no Edit in place", row)
		}
	}

	built, err := artifact.NewStore(pool, token).SubmitImplementation(ctx, owner,
		artifact.By{Authorship: artifact.AuthorshipHuman, Author: "the implementer"},
		mergeFiring.ItemID, "what was built", "")
	if err != nil {
		t.Fatalf("submitting the implementation version: %v", err)
	}
	f := mergeFiring
	f.Row, f.ArtifactID, f.Criteria = gate.Implementation, built.ID, nil
	opened, err := g.Fire(ctx, f)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.EditInPlace(ctx, opened, f); !errors.Is(err, gate.ErrEditInPlaceRefused) {
		t.Errorf("EditInPlace at Implementation = %v, want ErrEditInPlaceRefused", err)
	}
}
