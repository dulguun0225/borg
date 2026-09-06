package score

import (
	"context"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/area"
)

// The three resolutions this package reads without a record: what a spec version
// withdraws, a diff nothing could read for what it destroys, and an irreversible
// area whose production deploy has no control to run. Each is a rule over what
// the caller handed in, so each is tested as one.

// fakeWithdrawals answers with what one artifact removes, which is the shape the
// composition supplies and no package answers yet.
type fakeWithdrawals struct{ removed []ProtectionRemoved }

func (f fakeWithdrawals) ProtectionRemovedBy(context.Context, string) ([]ProtectionRemoved, error) {
	return f.removed, nil
}

// TestWithdrawingAProtectedCriterionResolvesAtSpec: withdrawal is not silent for
// the three provenances that name an authority, and the vector carries who the
// provenance names so the row can be routed to them rather than to the owner.
func TestWithdrawingAProtectedCriterionResolvesAtSpec(t *testing.T) {
	ctx := t.Context()
	withdrawn := fakeWithdrawals{removed: []ProtectionRemoved{{
		What: RemovedCriterion, SubjectID: "cri_a",
		Provenance: ProvenanceHumanConfirmed, RoutedTo: "hum_a",
	}}}

	s := &Score{withdrawals: withdrawn}
	r, err := s.protectionWithdrawn(ctx, Change{ArtifactID: "art_a", AtSpec: true})
	if err != nil {
		t.Fatalf("protectionWithdrawn: %v", err)
	}
	if r.resolved == "" || r.cause != CauseProtectionWithdrawn {
		t.Fatalf("the withdrawal reads %+v, want a resolution at Spec", r)
	}
	if len(r.evidence) != 1 || !strings.Contains(r.evidence[0], "cri_a") ||
		!strings.Contains(r.evidence[0], ProvenanceHumanConfirmed) ||
		!strings.Contains(r.evidence[0], "hum_a") {
		t.Errorf("the evidence is %v, want the criterion, its provenance and the human it routes to", r.evidence)
	}

	// A superseding machine that admits what a human-confirmed one forbade takes
	// the same treatment, which is what the design says of it.
	s = &Score{withdrawals: fakeWithdrawals{removed: []ProtectionRemoved{{
		What: RemovedScreenTransition, SubjectID: "ssm_a", Provenance: ProvenanceHumanConfirmed,
	}}}}
	r, err = s.protectionWithdrawn(ctx, Change{ArtifactID: "art_a", AtSpec: true})
	if err != nil {
		t.Fatalf("protectionWithdrawn over a screen revision: %v", err)
	}
	if r.cause != CauseProtectionWithdrawn {
		t.Errorf("the screen revision reads %+v, want the same resolution", r)
	}

	// A version that removes neither reads as nothing, and so does a factory
	// whose composition supplies no reader — an empty input and never an
	// unavailable one, which would resolve every Spec row there is.
	s = &Score{withdrawals: NoWithdrawals{}}
	r, err = s.protectionWithdrawn(ctx, Change{ArtifactID: "art_a", AtSpec: true})
	if err != nil {
		t.Fatalf("protectionWithdrawn with no reader: %v", err)
	}
	if r.resolved != "" || r.unavailable != "" || r.level != 0 {
		t.Errorf("a version withdrawing nothing reads %+v, want nothing", r)
	}
}

// TestADiffNobodyCouldReadForWhatItDestroysResolvesReversibility: the reading is
// per toolchain, so a toolchain with no derivation behind it resolves the factor
// rather than reading the diff as destroying nothing — the gate a failure would
// remove is the gate that failure is evidence for needing.
func TestADiffNobodyCouldReadForWhatItDestroysResolvesReversibility(t *testing.T) {
	s := &Score{}
	why := "whether the diff against master destroys stored data could not be read"
	r, err := s.reversibility(t.Context(), Change{
		AtImplementation: true,
		Measurement:      Measurement{DestroysStoredDataUnavailable: why},
	})
	if err != nil {
		t.Fatalf("reversibility: %v", err)
	}
	if r.unavailable != why {
		t.Errorf("the reading is %+v, want it unavailable with the reason on it", r)
	}
}

// TestAnIrreversibleAreaWithNoControlResolvesTheProductionDeploy: where the
// platform serves no share there is no schedule to pick and every deploy there
// goes without a control, so an irreversible area's deploy to production is a
// human's whatever the formula returns.
func TestAnIrreversibleAreaWithNoControlResolvesTheProductionDeploy(t *testing.T) {
	noShare := Change{AtDeployToProduction: true, EveryTargetServesAShare: false}
	if r := hazardReading(area.GradeIrreversible, noShare); r.cause != CauseNoControlInAnIrreversibleArea {
		t.Errorf("the production deploy of an irreversible area on a platform serving no share reads %+v", r)
	}

	// Where every target serves a share the row with a control is available, so
	// the value is weighed at the top of the scale and the row is not resolved.
	share := Change{AtDeployToProduction: true, EveryTargetServesAShare: true}
	if r := hazardReading(area.GradeIrreversible, share); r.resolved != "" || r.level != 1.0 {
		t.Errorf("the production deploy of an irreversible area that can run a control reads %+v", r)
	}

	// Implementation keeps the resolution it already had, on its own cause.
	atImplementation := Change{AtImplementation: true}
	if r := hazardReading(area.GradeIrreversible, atImplementation); r.cause != CauseIrreversibleHazard {
		t.Errorf("Implementation in an irreversible area reads %+v", r)
	}
}
