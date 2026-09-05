package policy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestTheAttemptLimitIsReadThroughTheSameThreeReads: the one parameter besides
// the threshold that a mechanism reads at this milestone.
func TestTheAttemptLimitIsReadThroughTheSameThreeReads(t *testing.T) {
	ctx, in := newFactory(t)

	limit, err := in.reader.AttemptLimit(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	supplied := startingValue(t, gatepolicy.AttemptLimit)
	if limit.Source != policy.FromSupplied || limit.Number != supplied {
		t.Errorf("the limit reads %v from %s, want the supplied %v", limit.Number, limit.Source, supplied)
	}

	if _, err := in.factory.AuthorAttemptLimit(ctx, owner, item.StageImplementation, 6); err != nil {
		t.Fatalf("AuthorAttemptLimit: %v", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, owner, gatepolicy.AttemptLimit,
		safeguard.Subject{Kind: safeguard.SubjectStage, ID: "implementation", Key: "implementation"},
		safeguard.Bound{Number: 4}, safeguard.Routing{}); err != nil {
		t.Fatalf("AddSafeguard: %v", err)
	}
	limit, err = in.reader.AttemptLimit(ctx, in.subjects("merge_to_master"))
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	if limit.Number != 4 || !limit.Clamped {
		t.Errorf("the limit reads %v clamped %v, want the safeguard's ceiling of 4", limit.Number, limit.Clamped)
	}

	// A limit authored on another stage is that stage's and not this one's.
	other := in.subjects("merge_to_master")
	other.Stage = item.StageSpec
	spec, err := in.reader.AttemptLimit(ctx, other)
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	if spec.Source != policy.FromSupplied {
		t.Errorf("the spec stage's limit reads from %s, want the supplied value", spec.Source)
	}
	// The safeguard is drawn on the implementation stage and reaches that stage
	// alone: a safeguard on a stage reaches that stage and no other, the way a
	// safeguard on a gate row reaches that row and no other.
	if spec.Number != supplied || spec.Clamped {
		t.Errorf("the spec stage's limit reads %v clamped %v, want the supplied %v untouched",
			spec.Number, spec.Clamped, supplied)
	}
	if len(spec.Safeguards) != 0 {
		t.Errorf("a safeguard on the implementation stage reached the spec stage: %v", spec.Safeguards)
	}
}
