// Tests of the policy subcommand: every parameter printed as it is in
// force, resolved against the records it is given.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/score"
)

// TestPolicyReadsWhatIsInForce is the print an owner reads before authoring
// anything: it resolves against the records it is given and does not fail on the
// ones it is not.
func TestPolicyReadsWhatIsInForce(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	decomposeService(t, ctx, pool, "checkout")
	if err := areaCommand([]string{"payments"}); err != nil {
		t.Fatalf("area: %v", err)
	}

	if err := policyCommand(nil); err != nil {
		t.Errorf("policy with no subjects: %v", err)
	}
	if err := policyCommand([]string{"-service", "checkout", "-area", "payments",
		"-gate", "deploy_to_production", "-stage", "spec"}); err != nil {
		t.Errorf("policy over every subject: %v", err)
	}
	if err := safeguardCommand([]string{"-parameter", "window_limit", "-subject", "service:checkout", "-bound", "2"}); err != nil {
		t.Fatalf("safeguard: %v", err)
	}
	if err := policyCommand([]string{"-service", "checkout"}); err != nil {
		t.Errorf("policy with a safeguard placed: %v", err)
	}
	if err := policyCommand([]string{"-service", "nothing"}); err == nil {
		t.Error("policy over a service nobody decomposed was accepted")
	}

	// What the print reads is what the reader reads, so the assertion over its
	// content is on the reader: every parameter resolves, and the two with a
	// mechanism at this milestone say so.
	effectives, err := policy.NewReader(pool, testToken(t, ctx, pool), score.Version{}).All(ctx, policy.Subjects{
		GateRow: "merge_to_master", Stage: item.StageImplementation,
	})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(effectives) != len(gatepolicy.Definitions) {
		t.Errorf("%d parameters resolved, want %d", len(effectives), len(gatepolicy.Definitions))
	}
}
