// The database tests of [artifact.Store.SubmitFleet], [artifact.Store.EnterShipped]
// and [artifact.InForce] — the fleet kinds' chains and the ungated shipped
// entry. They share db_test.go's newStore and the actors and drafts it
// declares; the erasure's are in redact_db_test.go.
package artifact_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/record"
)

// TestSubmitFleetChainsByRoleAndBySubject is the two named chains: a role
// prompt's version chain by role, a skill's by subject, and the one
// selection rule naming neither.
func TestSubmitFleetChainsByRoleAndBySubject(t *testing.T) {
	ctx, _, s := newStore(t)

	first, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version one", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet: %v", err)
	}
	second, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version two", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet again: %v", err)
	}
	if second.Version != 2 || second.Supersedes != first.ID {
		t.Errorf("the second role prompt version is %+v, want version 2 superseding %s", second, first.ID)
	}

	otherRole, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "implementer", "", "a different role", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet for another role: %v", err)
	}
	if otherRole.Version != 1 {
		t.Errorf("a new role's chain starts at %d, want 1", otherRole.Version)
	}

	skill, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSkill, "", "svc_a", "a procedure for svc_a", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet for a skill: %v", err)
	}
	if skill.Subject != "svc_a" || skill.Role != "" {
		t.Errorf("the skill is %+v, want subject svc_a and no role", skill)
	}

	rule, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSelectionRule, "", "", "the one selection rule", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet for the selection rule: %v", err)
	}
	if rule.Role != "" || rule.Subject != "" || rule.ItemID != "" {
		t.Errorf("the selection rule is %+v, want none of item, role or subject named", rule)
	}
}

func TestSubmitFleetRefusesAMissingDiscriminator(t *testing.T) {
	ctx, _, s := newStore(t)

	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "", "", "text", theManifest); !errors.Is(err, artifact.ErrRoleEmpty) {
		t.Errorf("SubmitFleet for a role prompt naming no role = %v, want ErrRoleEmpty", err)
	}
	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSkill, "", "", "text", theManifest); !errors.Is(err, artifact.ErrSubjectEmpty) {
		t.Errorf("SubmitFleet for a skill naming no subject = %v, want ErrSubjectEmpty", err)
	}
	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSpec, "", "", "text", theManifest); !errors.Is(err, artifact.ErrFleetKindUnknown) {
		t.Errorf("SubmitFleet for an item kind = %v, want ErrFleetKindUnknown", err)
	}
}

// TestEnterShippedWritesTheEmptyAuthorPair is "One pipeline"'s one exception:
// the factory's own start authors nothing, and the pair is empty together
// rather than a fourth authorship.
func TestEnterShippedWritesTheEmptyAuthorPair(t *testing.T) {
	ctx, pool, s := newStore(t)

	shipped, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "",
		"the shipped role prompt", artifact.EnteredByInstall, "bundle-2026.1")
	if err != nil {
		t.Fatalf("EnterShipped: %v", err)
	}
	if shipped.Authorship != "" || shipped.Author != "" {
		t.Errorf("EnterShipped wrote authorship %q and author %q, want both empty", shipped.Authorship, shipped.Author)
	}
	if shipped.EnteredBy != artifact.EnteredByInstall {
		t.Errorf("the entry was written by %q, want the install's", shipped.EnteredBy)
	}
	if shipped.ShippedBundleIdentity != "bundle-2026.1" {
		t.Errorf("the shipped bundle identity is %q, want bundle-2026.1", shipped.ShippedBundleIdentity)
	}

	read, err := artifact.Get(ctx, pool, shipped.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != shipped {
		t.Errorf("Get returned %+v, want %+v", read, shipped)
	}
}

func TestEnterShippedRefusesAnEmptyShippedBundleIdentity(t *testing.T) {
	ctx, _, s := newStore(t)
	_, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "", "text",
		artifact.EnteredByInstall, "")
	if !errors.Is(err, artifact.ErrShippedBundleIdentityEmpty) {
		t.Errorf("EnterShipped with no shipped bundle identity = %v, want ErrShippedBundleIdentityEmpty", err)
	}
	_, err = s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "", "text",
		"a later start", "bundle-2026.1")
	if !errors.Is(err, artifact.ErrEnteredByUnknown) {
		t.Errorf("EnterShipped with an event outside the two = %v, want ErrEnteredByUnknown", err)
	}
}

// TestAHumanBackstopsAStageAndTheseRecordsBelongToNone: a role prompt, a
// skill and the selection rule reach past any one stage, so the writers are
// the agent that authors a version and the gate where a human takes Edit in
// place. A human backstopping a stage authors none of the three.
func TestAHumanBackstopsAStageAndTheseRecordsBelongToNone(t *testing.T) {
	ctx, _, s := newStore(t)

	byHuman := artifact.By{Authorship: artifact.AuthorshipHuman, Author: "the owner"}
	for _, of := range []struct {
		kind          artifact.Kind
		role, subject string
	}{
		{artifact.KindRolePrompt, "spec_author", ""},
		{artifact.KindSkill, "", "svc_a"},
		{artifact.KindSelectionRule, "", ""},
	} {
		if _, err := s.SubmitFleet(ctx, specAuthor, byHuman, of.kind, of.role, of.subject,
			"words a human wrote", ""); !errors.Is(err, artifact.ErrHumanAuthorsNoFleetVersion) {
			t.Errorf("SubmitFleet of a %s authored by a human = %v, want ErrHumanAuthorsNoFleetVersion", of.kind, err)
		}
	}

	// The gate is the path a human's words take, and it is admitted.
	atTheGate := artifact.By{Authorship: artifact.AuthorshipGate, Author: "gate.role_prompt_or_skill"}
	if _, err := s.SubmitFleet(ctx, specAuthor, atTheGate, artifact.KindRolePrompt, "spec_author", "",
		"what the human wrote at the gate", ""); err != nil {
		t.Errorf("SubmitFleet of a version edited in place at the gate: %v", err)
	}
}

// TestOnlyTheFactorysOwnStartEntersWhatShipped: the entry names no authorship
// and no author, so the actor is the whole of who wrote it, and the install's
// half of it enters in force with nothing decided. An actor the caller chose
// would let any component write such a row.
func TestOnlyTheFactorysOwnStartEntersWhatShipped(t *testing.T) {
	ctx, _, s := newStore(t)

	_, err := s.EnterShipped(ctx, specAuthor, artifact.KindRolePrompt, "spec_author", "",
		"words a component entered", artifact.EnteredByInstall, "bundle-1")
	if !errors.Is(err, artifact.ErrNotTheFactorysStart) {
		t.Errorf("EnterShipped as a component other than the start = %v, want ErrNotTheFactorysStart", err)
	}
	almost := artifact.FactoryStart
	almost.Basis = record.BasisVerified
	if _, err := s.EnterShipped(ctx, almost, artifact.KindRolePrompt, "spec_author", "",
		"words", artifact.EnteredByInstall, "bundle-1"); !errors.Is(err, artifact.ErrNotTheFactorysStart) {
		t.Errorf("EnterShipped as an actor that is the start's key on another basis = %v, want ErrNotTheFactorysStart", err)
	}
	if _, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "",
		"the words that shipped", artifact.EnteredByInstall, "bundle-1"); err != nil {
		t.Errorf("EnterShipped as the factory's own start: %v", err)
	}
}

// TestTheFleetReadsAreBoundToOneChain: the three reads answer for a role's, a
// subject's or the factory's chain, which is what a kind outside [FleetKinds]
// names none of. Given an item kind they would otherwise answer with the
// newest row of that kind across every item.
func TestTheFleetReadsAreBoundToOneChain(t *testing.T) {
	ctx, pool, s := newStore(t)

	if _, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "the spec of one item",
		nil, nil, nil, theManifest); err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	if _, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_b", "svc_a", "the spec of another",
		nil, nil, nil, theManifest); err != nil {
		t.Fatalf("SubmitSpec of a second item: %v", err)
	}

	for name, read := range map[string]func() (artifact.Artifact, bool, error){
		"Newest": func() (artifact.Artifact, bool, error) {
			return artifact.Newest(ctx, pool, artifact.KindSpec, "", "")
		},
		"NewestShipped": func() (artifact.Artifact, bool, error) {
			return artifact.NewestShipped(ctx, pool, artifact.KindSpec, "", "")
		},
		"InForce": func() (artifact.Artifact, bool, error) {
			return artifact.InForce(ctx, pool, artifact.KindSpec, "", "", nil)
		},
	} {
		found, ok, err := read()
		if !errors.Is(err, artifact.ErrFleetKindUnknown) {
			t.Errorf("%s of an item kind = %+v, %v, %v; want ErrFleetKindUnknown and no row of any item",
				name, found, ok, err)
		}
	}

	// A role prompt chain and a skill chain are named, and the read that names
	// neither is the one selection rule's.
	if _, _, err := artifact.Newest(ctx, pool, artifact.KindRolePrompt, "", ""); !errors.Is(err, artifact.ErrRoleEmpty) {
		t.Errorf("Newest of a role prompt naming no role = %v, want ErrRoleEmpty", err)
	}
	if _, _, err := artifact.InForce(ctx, pool, artifact.KindSkill, "", "", nil); !errors.Is(err, artifact.ErrSubjectEmpty) {
		t.Errorf("InForce of a skill naming no subject = %v, want ErrSubjectEmpty", err)
	}
	rule, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSelectionRule, "", "",
		"the one selection rule", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet of the selection rule: %v", err)
	}
	head, ok, err := artifact.Newest(ctx, pool, artifact.KindSelectionRule, "", "")
	if err != nil || !ok || head.ID != rule.ID {
		t.Errorf("Newest of the selection rule = %+v, %v, %v; want %s", head, ok, err, rule.ID)
	}
}

// TestDDLRefusesAnAuthorWithNoAuthorship is the one partial pair the CHECK
// refuses beside the two full ones and the empty one it allows.
func TestDDLRefusesAnAuthorWithNoAuthorship(t *testing.T) {
	ctx, pool, _ := newStore(t)
	_, err := pool.Exec(ctx, `insert into artifact
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, redacted_content_digest,
		shipped_bundle_identity, entered_by, input_manifest_id)
		values ($1, $2, 'component', 'install', 'claimed', $3, '', 'spec_author', '', 'role_prompt', 1,
		'', '', 'claude-opus-5', 'text', 'x', '', 'bundle-1', 'install', '')`,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "author_pair_together") {
		t.Errorf("inserting an author with no authorship = %v, want a violation of author_pair_together", err)
	}
}

// TestInForceReadsTheNewestApprovedVersion is [artifact.InForce]: the caller
// supplies the versions that are approved and not withdrawn, and it picks the
// newest of the chain among them.
func TestInForceReadsTheNewestApprovedVersion(t *testing.T) {
	ctx, pool, s := newStore(t)

	first, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version one", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet: %v", err)
	}
	second, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version two", theManifest)
	if err != nil {
		t.Fatalf("SubmitFleet again: %v", err)
	}

	found, ok, err := artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", []string{first.ID, second.ID})
	if err != nil || !ok {
		t.Fatalf("InForce = %+v, %v, %v", found, ok, err)
	}
	if found.ID != second.ID {
		t.Errorf("InForce = %s, want the newest, %s", found.ID, second.ID)
	}

	// The second version is not approved (or was withdrawn); the first still
	// stands.
	found, ok, err = artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", []string{first.ID})
	if err != nil || !ok || found.ID != first.ID {
		t.Errorf("InForce with only the first approved = %+v, %v, %v, want %s", found, ok, err, first.ID)
	}

	// Nothing approved is nothing in force.
	_, ok, err = artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", nil)
	if err != nil || ok {
		t.Errorf("InForce with nothing approved = ok %v, %v, want false", ok, err)
	}
}

// TestTheInstallsEntryIsInForceUngatedAndAnUpgradesIsNot is the design's split
// between the two events that enter shipped words: the install's entries alone
// enter in force ungated, a factory with nothing decided in it having to run,
// and an upgrade's first start enters a version awaiting the gate every
// version fires. Both write version 1 of a chain and both name a shipped
// bundle identity, so the event is what tells them apart.
func TestTheInstallsEntryIsInForceUngatedAndAnUpgradesIsNot(t *testing.T) {
	ctx, pool, s := newStore(t)

	installed, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "",
		"the shipped role prompt", artifact.EnteredByInstall, "bundle-2026.1")
	if err != nil {
		t.Fatalf("EnterShipped at install: %v", err)
	}

	// Nothing is approved, and the install's entry is in force anyway.
	found, ok, err := artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", nil)
	if err != nil || !ok || found.ID != installed.ID {
		t.Fatalf("InForce with nothing approved = %+v, %v, %v; want the install's entry %s", found, ok, err, installed.ID)
	}

	upgraded, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "",
		"the shipped role prompt, reworded", artifact.EnteredByUpgradeFirstStart, "bundle-2026.2")
	if err != nil {
		t.Fatalf("EnterShipped at an upgrade's first start: %v", err)
	}
	if upgraded.EnteredBy != artifact.EnteredByUpgradeFirstStart || upgraded.Version != 2 {
		t.Errorf("the upgrade's entry is %+v, want version 2 entered by an upgrade's first start", upgraded)
	}

	// The upgrade's entry awaits its gate, so the words the install ran on
	// still stand.
	found, ok, err = artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", nil)
	if err != nil || !ok || found.ID != installed.ID {
		t.Errorf("InForce after an upgrade entered = %+v, %v, %v; want the install's entry %s still", found, ok, err, installed.ID)
	}
	// Approved, it is in force, being the newest of the chain.
	found, ok, err = artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", []string{upgraded.ID})
	if err != nil || !ok || found.ID != upgraded.ID {
		t.Errorf("InForce with the upgrade's entry approved = %+v, %v, %v; want %s", found, ok, err, upgraded.ID)
	}

	// A chain an upgrade started has nothing in force until its gate decides.
	started, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "reviewer", "",
		"a role the upgrade adds", artifact.EnteredByUpgradeFirstStart, "bundle-2026.2")
	if err != nil {
		t.Fatalf("EnterShipped for a chain the upgrade starts: %v", err)
	}
	if _, ok, err := artifact.InForce(ctx, pool, artifact.KindRolePrompt, "reviewer", "", nil); err != nil || ok {
		t.Errorf("InForce on a chain an upgrade started = %v, %v, want nothing in force until %s is approved",
			ok, err, started.ID)
	}
}

// TestTheEnteredEventIsRefusedAroundTheWriter: the column says which event
// entered a row, so an entry naming none, and an authored version naming one,
// are both refused by the store as well as by the writer.
func TestTheEnteredEventIsRefusedAroundTheWriter(t *testing.T) {
	ctx, pool, _ := newStore(t)

	const insert = `insert into artifact
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, redacted_content_digest,
		shipped_bundle_identity, entered_by, input_manifest_id)
		values ($1, $2, 'component', 'install', 'claimed', $3, '', 'spec_author', '', 'role_prompt', 1,
		'', $4, $5, 'text', 'x', '', $6, $7, '')`

	// An entry nobody wrote, naming no event.
	_, err := pool.Exec(ctx, insert, record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"", "", "bundle-1", "")
	if err == nil || !strings.Contains(err.Error(), "entered_by_matches_authorship") {
		t.Errorf("inserting a shipped entry naming no event = %v, want a violation of entered_by_matches_authorship", err)
	}

	// An authored version naming one.
	_, err = pool.Exec(ctx, insert, record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"agent", "claude-opus-5", "", "install")
	if err == nil || !strings.Contains(err.Error(), "entered_by_matches_authorship") {
		t.Errorf("inserting an authored version naming an event = %v, want a violation of entered_by_matches_authorship", err)
	}

	// An event outside the two.
	_, err = pool.Exec(ctx, insert, record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"", "", "bundle-1", "a later start")
	if err == nil || !strings.Contains(err.Error(), "entered_by_known") {
		t.Errorf("inserting an event outside the two = %v, want a violation of entered_by_known", err)
	}
}
