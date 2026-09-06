// The supersession query's tests, beside db_test.go's, which holds the schema
// helper they share. None of them skips when the database is unreachable, for
// the reason db_test.go states.
package screenstatemachine_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// TestASupersedingMachineThatRemovesProtection: a machine is closed, so a
// superseding machine declaring a transition the human-confirmed machine it
// supersedes did not is what admits behaviour the confirmed one forbade. That
// is what the score reads at the Spec row.
func TestASupersedingMachineThatRemovesProtection(t *testing.T) {
	ctx, pool := newSchema(t)

	confirmed := insert(t, ctx, pool,
		screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_confirmed", ItemID: "it_a"}, simpleDraft())

	wider := simpleDraft()
	wider.Supersedes = confirmed.ID
	wider.States = []string{"empty", "loaded"}
	wider.Events = []string{"load", "reset"}
	wider.Transitions = []screenstatemachine.Transition{
		{From: "empty", Event: "load", To: "loaded"},
		{From: "loaded", Event: "reset", To: "empty"},
	}
	wider.Terminal = []string{}
	revision := insert(t, ctx, pool,
		screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_revision", ItemID: "it_b"}, wider)

	removing, err := screenstatemachine.SupersessionsRemovingProtection(ctx, pool,
		"art_revision", []string{"art_confirmed"})
	if err != nil {
		t.Fatalf("SupersessionsRemovingProtection: %v", err)
	}
	if len(removing) != 1 {
		t.Fatalf("SupersessionsRemovingProtection = %+v, want the one revision", removing)
	}
	if removing[0].Machine.ID != revision.ID || removing[0].Superseded.ID != confirmed.ID {
		t.Errorf("the revision is %+v, want %s superseding %s", removing[0], revision.ID, confirmed.ID)
	}
	if len(removing[0].Added) != 1 || removing[0].Added[0].Event != "reset" {
		t.Errorf("Added = %+v, want the one transition the confirmed machine did not declare", removing[0].Added)
	}

	// A machine whose superseded one no human confirmed is not routed anywhere:
	// human-confirmed is a query over the introducing version's decision, and
	// the caller passes what it read.
	none, err := screenstatemachine.SupersessionsRemovingProtection(ctx, pool, "art_revision", []string{"art_other"})
	if err != nil {
		t.Fatalf("SupersessionsRemovingProtection: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("SupersessionsRemovingProtection over a machine nobody confirmed = %+v, want none", none)
	}
}

// TestARevisionThatRemovesNoProtection: a revision that redirects a transition
// the confirmed machine already declared removes nothing — the closed reading
// forbade nothing there — and neither does one that declares fewer.
func TestARevisionThatRemovesNoProtection(t *testing.T) {
	ctx, pool := newSchema(t)

	confirmed := insert(t, ctx, pool,
		screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_confirmed", ItemID: "it_a"}, simpleDraft())

	redirected := simpleDraft()
	redirected.Supersedes = confirmed.ID
	redirected.States = []string{"empty"}
	redirected.Transitions = []screenstatemachine.Transition{
		{From: "empty", Event: "load", Screen: "ssm_00000000000000000000000000000002"},
	}
	redirected.Terminal = []string{}
	insert(t, ctx, pool,
		screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_revision", ItemID: "it_b"}, redirected)

	removing, err := screenstatemachine.SupersessionsRemovingProtection(ctx, pool,
		"art_revision", []string{"art_confirmed"})
	if err != nil {
		t.Fatalf("SupersessionsRemovingProtection: %v", err)
	}
	if len(removing) != 0 {
		t.Errorf("SupersessionsRemovingProtection = %+v, want none", removing)
	}
}

// TestASpecVersionThatSupersedesNothing: a version introducing a screen
// supersedes no machine, so it removes no protection and the query is empty
// without reading anything else.
func TestASpecVersionThatSupersedesNothing(t *testing.T) {
	ctx, pool := newSchema(t)
	insert(t, ctx, pool,
		screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_first", ItemID: "it_a"}, simpleDraft())

	removing, err := screenstatemachine.SupersessionsRemovingProtection(ctx, pool, "art_first", []string{"art_first"})
	if err != nil {
		t.Fatalf("SupersessionsRemovingProtection: %v", err)
	}
	if len(removing) != 0 {
		t.Errorf("SupersessionsRemovingProtection = %+v, want none", removing)
	}
	if none, err := screenstatemachine.SupersessionsRemovingProtection(ctx, pool, "", nil); err != nil || none != nil {
		t.Errorf("SupersessionsRemovingProtection with nothing named = %+v, %v", none, err)
	}
}
