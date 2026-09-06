// The install's first-start step for what an agent is told: what shipped
// enters the chain, the install's entry in force ungated and an upgrade's
// entry awaiting the gate every version fires.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAnUpgradesShippedPromptIsNotInForceUntilItsGate is
// ../../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md's
// "a version not approved is not in force, so where it extends a chain the
// words the install ran on stand until the gate decides". The first start
// enters what shipped; a start whose shipped words differ enters a second
// version, and what a dispatch reads is still the first.
func TestAnUpgradesShippedPromptIsNotInForceUntilItsGate(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	store := artifact.NewStore(d.pool, d.token)
	role := dispatch.RoleSpecAuthor

	// What the install ran on. It is entered as the install's own, which is the
	// one entry that stands in force with nothing decided.
	installed, err := store.EnterShipped(ctx, installActor, artifact.KindRolePrompt, string(role), "",
		"the words the install ran on", artifact.EnteredByInstall, "bundle-1")
	if err != nil {
		t.Fatalf("entering the install's own prompt: %v", err)
	}

	// The first start on a version whose shipped words differ. It enters a
	// version and puts nothing in force.
	prompts, entered, err := enterShippedPrompts(ctx, store, d.pool, installActor, "bundle-2")
	if err != nil {
		t.Fatalf("enterShippedPrompts: %v", err)
	}
	if len(entered) != len(dispatch.Roles) {
		t.Fatalf("the first start entered %v, want a version per role", entered)
	}

	head, found, err := artifact.Newest(ctx, d.pool, artifact.KindRolePrompt, string(role), "")
	if err != nil || !found {
		t.Fatalf("Newest = %v, %v", found, err)
	}
	if head.EnteredBy != artifact.EnteredByUpgradeFirstStart {
		t.Errorf("the head of the chain entered by %q, want %q", head.EnteredBy, artifact.EnteredByUpgradeFirstStart)
	}

	inForce, found, err := prompts.InForce(ctx, role)
	if err != nil || !found {
		t.Fatalf("InForce = %v, %v; want the install's entry still standing", found, err)
	}
	if inForce.ID != installed.ID {
		t.Errorf("what is in force is %s, the version an upgrade entered; want %s, the words the install ran on",
			inForce.ID, installed.ID)
	}
}

// TestTheComposedEntryNamesTheEffort: -effort is the field a fleet entry has
// for how long the model works before it answers, and the one entry this
// interface composes names it for every role — so what dispatch hands the role,
// what the role sends the provider, and what the agent run record carries all
// come from the same value. Empty is an entry naming none.
func TestTheComposedEntryNamesTheEffort(t *testing.T) {
	for _, effort := range []string{"", "high"} {
		fleet := oneModelFleet{modelName: "a-model", effort: effort, credential: "model.openrouter"}
		for _, role := range dispatch.Roles {
			entry, matched, err := fleet.EntryFor(t.Context(), role, dispatch.On{})
			if err != nil || !matched {
				t.Fatalf("EntryFor(%s) = %v, %v; the composed fleet answers for every role", role, matched, err)
			}
			if entry.Effort != effort {
				t.Errorf("the entry for %s names effort %q, want %q", role, entry.Effort, effort)
			}
		}
	}
}

// TestASecondStartOnOneVersionEntersNothing is
// ../../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md's
// "at the factory's first start on the new version, what shipped enters the
// chain": the trigger is the version's identity, so once an agent has authored
// a version over what shipped, a later start under the same identity enters
// nothing — the words differing from the head of the chain is not an upgrade.
func TestASecondStartOnOneVersionEntersNothing(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	store := artifact.NewStore(d.pool, d.token)
	role := dispatch.RoleSpecAuthor

	if _, _, err := enterShippedPrompts(ctx, store, d.pool, installActor, "bundle-1"); err != nil {
		t.Fatalf("the install's own entry: %v", err)
	}
	// An agent authors a version over what shipped, which is what leaves the
	// head of the chain holding words the shipped constant does not.
	author := record.Actor{Kind: record.KindAgent, Key: "a-model-version", Basis: record.BasisClaimed}
	if _, err := store.SubmitFleet(ctx, author, artifact.By{
		Authorship: artifact.AuthorshipAgent, Author: "a-model-version",
	}, artifact.KindRolePrompt, string(role), "", "the words an agent authored", ""); err != nil {
		t.Fatalf("authoring a version over what shipped: %v", err)
	}

	_, entered, err := enterShippedPrompts(ctx, store, d.pool, installActor, "bundle-1")
	if err != nil {
		t.Fatalf("enterShippedPrompts: %v", err)
	}
	if len(entered) != 0 {
		t.Errorf("a second start on one version entered %v, and the version's own entry is already in the chain", entered)
	}

	head, found, err := artifact.Newest(ctx, d.pool, artifact.KindRolePrompt, string(role), "")
	if err != nil || !found {
		t.Fatalf("Newest = %v, %v", found, err)
	}
	if head.Authorship != artifact.AuthorshipAgent {
		t.Errorf("the head of the chain is %+v, want the version the agent authored", head)
	}
}

// TestAChainAnUpgradeStartedHasNothingInForce is the other half of the same
// sentence: where the upgrade starts a chain, nothing is in force until the
// gate decides, and the work that would have used it waits.
func TestAChainAnUpgradeStartedHasNothingInForce(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	store := artifact.NewStore(d.pool, d.token)
	role := dispatch.RoleSpecAuthor

	if _, err := store.EnterShipped(ctx, installActor, artifact.KindRolePrompt, string(role), "",
		"the words a later start ships", artifact.EnteredByUpgradeFirstStart, "bundle-2"); err != nil {
		t.Fatalf("entering an upgrade's prompt onto an empty chain: %v", err)
	}

	prompts := rolePrompts{pool: d.pool}
	if _, found, err := prompts.InForce(ctx, role); err != nil || found {
		t.Errorf("InForce over a chain an upgrade started = %v, %v; want nothing in force", found, err)
	}
}
