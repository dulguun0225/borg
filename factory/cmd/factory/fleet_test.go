// The install's first-start step for what an agent is told: what shipped
// enters the chain, the install's entry in force ungated and an upgrade's
// entry awaiting the gate every version fires.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
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
	prompts, entered, err := enterShippedPrompts(ctx, store, d.pool, d.token, installActor, "bundle-2")
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

// TestComposeWritesAnEntryPerRoleAndLeavesAnOwnersOwn is the terminal's
// stand-in for the owner's first act at Factory: a fresh install holds no fleet
// entry, so compose writes one per role from the flags — the effort among them,
// which is the field a fleet entry has for how long the model works before it
// answers — and a role an owner already covered is left alone.
func TestComposeWritesAnEntryPerRoleAndLeavesAnOwnersOwn(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	d.effort = "high"

	// One entry an owner wrote first, on another model, for one of the roles.
	owner, err := humanNamed(ctx, d.pool, d.token, d.human)
	if err != nil {
		t.Fatalf("humanNamed: %v", err)
	}
	mine, err := fleetentry.NewWriter(d.pool, d.token).Write(ctx, owner, fleetentry.New{
		ModelVersion: "another/model", Effort: "low", Role: string(dispatch.RoleImplementer),
		CredentialName: "model.mine", ProcessingLocation: "elsewhere",
		MaterialClasses: fleetentry.MaterialClasses, ReadsAtOnce: 1000,
		DispatchesBetweenEvaluationRuns: 1,
	})
	if err != nil {
		t.Fatalf("writing the owner's own entry: %v", err)
	}

	written, err := ensureFleetEntries(ctx, d, owner)
	if err != nil {
		t.Fatalf("ensureFleetEntries: %v", err)
	}
	if len(written) != len(dispatch.Roles)-1 {
		t.Fatalf("entries written for %v, want one per role but the one an owner covered", written)
	}

	inForce, err := fleetentry.InForce(ctx, d.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != len(dispatch.Roles) {
		t.Fatalf("%d entries in force, want one per role", len(inForce))
	}
	for _, one := range inForce {
		if one.ID == mine.ID {
			if one.ModelVersion != "another/model" || one.Effort != "low" {
				t.Errorf("the owner's own entry reads back as %+v, want what the owner wrote", one)
			}
			continue
		}
		if one.ModelVersion != d.modelName || one.Effort != "high" {
			t.Errorf("the composed entry for %s is %+v, want the model and effort the flags name", one.Role, one)
		}
		if one.CredentialName != d.modelCredentialName || one.ProcessingLocation == "" {
			t.Errorf("the composed entry for %s names credential %q at %q, want the flag's credential and a location",
				one.Role, one.CredentialName, one.ProcessingLocation)
		}
		if len(one.MaterialClasses) != len(fleetentry.MaterialClasses) {
			t.Errorf("the composed entry for %s names classes %v, want every class", one.Role, one.MaterialClasses)
		}
	}

	// A second call writes nothing: every role is covered.
	again, err := ensureFleetEntries(ctx, d, owner)
	if err != nil {
		t.Fatalf("ensureFleetEntries again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a second call wrote entries for %v, and every role was already covered", again)
	}
}

// TestTheReadinessReadingPerRole is
// ../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md's
// readiness reading: per role, whether an entry in force covers it, whether a
// role prompt version is in force, and the age of the oldest item dispatch
// holds unmatched. A first install shows a row for every role before anything
// is wrong.
func TestTheReadinessReadingPerRole(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("compose: %v", err)
	}

	reading, err := p.readiness(ctx)
	if err != nil {
		t.Fatalf("readiness: %v", err)
	}
	if len(reading) != len(dispatch.Roles) {
		t.Fatalf("%d rows, want one per role", len(reading))
	}
	for _, one := range reading {
		if !one.entry {
			t.Errorf("no entry covers %s after compose wrote one per role", one.role)
		}
		if !one.rolePrompt {
			t.Errorf("no role prompt version is in force for %s after the first start entered one", one.role)
		}
		if one.oldestUnmatched != 0 {
			t.Errorf("%s reads an unmatched item %v old, and nothing is held", one.role, one.oldestUnmatched)
		}
	}

	// Every entry withdrawn is the install that has written none: the reading
	// says so per role, and a dispatch onto a stage holds and is aged here.
	entries := fleetentry.NewWriter(d.pool, d.token)
	inForce, err := fleetentry.InForce(ctx, d.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	for _, one := range inForce {
		if _, err := entries.Withdraw(ctx, p.human, one.ID); err != nil {
			t.Fatalf("withdrawing %s: %v", one.ID, err)
		}
	}
	if _, _, err := p.dispatch.Interviewer(ctx, dispatch.On{IntentID: "in_readiness", ProjectID: p.projectID},
		nil, agent.Interviewing{Statement: "a statement"}); !errors.Is(err, dispatch.ErrHeld) {
		t.Fatalf("Interviewer with no entry in force = %v, want ErrHeld", err)
	}

	reading, err = p.readiness(ctx)
	if err != nil {
		t.Fatalf("readiness after the withdrawal: %v", err)
	}
	for _, one := range reading {
		if one.entry {
			t.Errorf("an entry covers %s after every entry was withdrawn", one.role)
		}
		if one.role == dispatch.RoleInterviewer && one.oldestUnmatched <= 0 {
			t.Errorf("the interviewer's oldest unmatched row reads %v, want the age of the hold", one.oldestUnmatched)
		}
	}
}

// TestAProviderIsReadOffTheCredentialAnEntryNames: the fleet entry names the
// credential and no record names the provider, so the client one entry runs on
// is built from the credential name — and a credential no provider of this
// install reads is refused rather than sent to whichever came first.
func TestAProviderIsReadOffTheCredentialAnEntryNames(t *testing.T) {
	for credential, want := range map[string]string{
		anthropicCredentialName:  "anthropic",
		openRouterCredentialName: "openrouter",
	} {
		named, err := providerOf(credential)
		if err != nil || named != want {
			t.Errorf("providerOf(%q) = %q, %v; want %q", credential, named, err, want)
		}
	}
	if _, err := providerOf("model.nobody"); err == nil {
		t.Error("a credential no provider of this install reads was answered rather than refused")
	}

	// One client per entry and kept: the pace is the time since the last call,
	// which a client rebuilt per dispatch would not know.
	secrets := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(secrets, []byte(openRouterCredentialName+"=unused\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(secrets)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	clientFor := modelsPerEntry(resolver, time.Second)
	first, err := clientFor(theModel, openRouterCredentialName)
	if err != nil {
		t.Fatalf("the client for an entry: %v", err)
	}
	again, err := clientFor(theModel, openRouterCredentialName)
	if err != nil {
		t.Fatalf("the client for the same entry: %v", err)
	}
	if first != again {
		t.Error("a second dispatch on one entry was given a second client, and the pace it holds would start over")
	}
	if _, err := clientFor(theModel, "model.nobody"); err == nil {
		t.Error("an entry naming a credential no provider reads was given a client")
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

	if _, _, err := enterShippedPrompts(ctx, store, d.pool, d.token, installActor, "bundle-1"); err != nil {
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

	_, entered, err := enterShippedPrompts(ctx, store, d.pool, d.token, installActor, "bundle-1")
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

	prompts := &rolePrompts{pool: d.pool, token: d.token}
	if _, found, err := prompts.InForce(ctx, role); err != nil || found {
		t.Errorf("InForce over a chain an upgrade started = %v, %v; want nothing in force", found, err)
	}
}
