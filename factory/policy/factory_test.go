package policy_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// TestGatePolicyIsAuthoredByAHuman: duty 8 is an owner's, so a component
// authoring a parameter would be the factory setting its own bounds — which is
// the one thing kept apart from what the score supplies.
func TestGatePolicyIsAuthoredByAHuman(t *testing.T) {
	ctx, in := newFactory(t)

	component := record.Actor{Kind: record.KindComponent, Key: "score"}
	if _, err := in.factory.AuthorWindowLimit(ctx, component, in.service.ID, 2); !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component authoring the window limit = %v, want ErrNotAnOwner", err)
	}
	if _, _, err := in.factory.AddSafeguard(ctx, component, gatepolicy.WindowLimit,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: in.service.ID}, safeguard.Bound{Number: 2}); !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component placing a safeguard = %v, want ErrNotAnOwner", err)
	}
	if _, err := in.factory.Install(ctx, component, "acme", []string{"/srv"}, credential, 8); !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component installing = %v, want ErrNotAnOwner", err)
	}
	if _, err := in.factory.AuthorWindowLimit(ctx, record.Actor{}, in.service.ID, 2); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("authoring with no actor = %v, want ErrKindUnknown", err)
	}
}
