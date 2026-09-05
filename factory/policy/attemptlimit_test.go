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
		safeguard.Subject{Kind: safeguard.SubjectFactorySettings, ID: in.settings.ID}, safeguard.Bound{Number: 4}); err != nil {
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
	// The safeguard over the factory-wide settings record reaches this stage too, and
	// clamps nothing: the supplied value is already under its ceiling, which is a
	// safeguard being a bound rather than a precedence on a stage nobody authored.
	if spec.Number != supplied || spec.Clamped {
		t.Errorf("the spec stage's limit reads %v clamped %v, want the supplied %v untouched",
			spec.Number, spec.Clamped, supplied)
	}
	if len(spec.Safeguards) != 1 {
		t.Errorf("the safeguard over the record does not reach the spec stage: %v", spec.Safeguards)
	}
}
