// The database tests of [artifact.Store.SubmitFleet], [artifact.Store.EnterShipped],
// [artifact.InForce] and [artifact.Store.Redact] — the fleet kinds' chains, the
// ungated shipped entry, and the one exception to insert-and-never-update. They
// share db_test.go's newStore and the actors and drafts it declares.
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

	first, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version one", "")
	if err != nil {
		t.Fatalf("SubmitFleet: %v", err)
	}
	second, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version two", "")
	if err != nil {
		t.Fatalf("SubmitFleet again: %v", err)
	}
	if second.Version != 2 || second.Supersedes != first.ID {
		t.Errorf("the second role prompt version is %+v, want version 2 superseding %s", second, first.ID)
	}

	otherRole, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "implementer", "", "a different role", "")
	if err != nil {
		t.Fatalf("SubmitFleet for another role: %v", err)
	}
	if otherRole.Version != 1 {
		t.Errorf("a new role's chain starts at %d, want 1", otherRole.Version)
	}

	skill, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSkill, "", "svc_a", "a procedure for svc_a", "")
	if err != nil {
		t.Fatalf("SubmitFleet for a skill: %v", err)
	}
	if skill.Subject != "svc_a" || skill.Role != "" {
		t.Errorf("the skill is %+v, want subject svc_a and no role", skill)
	}

	rule, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSelectionRule, "", "", "the one selection rule", "")
	if err != nil {
		t.Fatalf("SubmitFleet for the selection rule: %v", err)
	}
	if rule.Role != "" || rule.Subject != "" || rule.ItemID != "" {
		t.Errorf("the selection rule is %+v, want none of item, role or subject named", rule)
	}
}

func TestSubmitFleetRefusesAMissingDiscriminator(t *testing.T) {
	ctx, _, s := newStore(t)

	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "", "", "text", ""); !errors.Is(err, artifact.ErrRoleEmpty) {
		t.Errorf("SubmitFleet for a role prompt naming no role = %v, want ErrRoleEmpty", err)
	}
	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSkill, "", "", "text", ""); !errors.Is(err, artifact.ErrSubjectEmpty) {
		t.Errorf("SubmitFleet for a skill naming no subject = %v, want ErrSubjectEmpty", err)
	}
	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSpec, "", "", "text", ""); !errors.Is(err, artifact.ErrFleetKindUnknown) {
		t.Errorf("SubmitFleet for an item kind = %v, want ErrFleetKindUnknown", err)
	}
}

// TestEnterShippedWritesTheEmptyAuthorPair is "One pipeline"'s one exception:
// the factory's own start authors nothing, and the pair is empty together
// rather than a fourth authorship.
func TestEnterShippedWritesTheEmptyAuthorPair(t *testing.T) {
	ctx, pool, s := newStore(t)

	shipped, err := s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "spec_author", "",
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
	_, err := s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "spec_author", "", "text",
		artifact.EnteredByInstall, "")
	if !errors.Is(err, artifact.ErrShippedBundleIdentityEmpty) {
		t.Errorf("EnterShipped with no shipped bundle identity = %v, want ErrShippedBundleIdentityEmpty", err)
	}
	_, err = s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "spec_author", "", "text",
		"a later start", "bundle-2026.1")
	if !errors.Is(err, artifact.ErrEnteredByUnknown) {
		t.Errorf("EnterShipped with an event outside the two = %v, want ErrEnteredByUnknown", err)
	}
}

// TestDDLRefusesAnAuthorWithNoAuthorship is the one partial pair the CHECK
// refuses beside the two full ones and the empty one it allows.
func TestDDLRefusesAnAuthorWithNoAuthorship(t *testing.T) {
	ctx, pool, _ := newStore(t)
	_, err := pool.Exec(ctx, `insert into artifact
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, shipped_bundle_identity, entered_by,
		input_manifest_id)
		values ($1, $2, 'component', 'factory.start', '', $3, '', 'spec_author', '', 'role_prompt', 1,
		'', '', 'claude-opus-5', 'text', 'x', 'bundle-1', 'install', '')`,
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

	first, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version one", "")
	if err != nil {
		t.Fatalf("SubmitFleet: %v", err)
	}
	second, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "version two", "")
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

// TestRedactDestroysASpanAndRecomputesTheDigest is the one exception to
// insert-and-never-update.
func TestRedactDestroysASpanAndRecomputesTheDigest(t *testing.T) {
	ctx, pool, s := newStore(t)

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "the secret is 12345 and nothing else", "")
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}

	if err := s.Redact(ctx, factoryStart, impl.ID, []artifact.Span{{Start: 14, End: 19}}); err != nil {
		t.Fatalf("Redact: %v", err)
	}

	read, err := artifact.Get(ctx, pool, impl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Content != "the secret is xxxxx and nothing else" {
		t.Errorf("the redacted content is %q", read.Content)
	}
	if read.ContentDigest == impl.ContentDigest {
		t.Error("the content digest did not change after redaction")
	}
}

func TestRedactRefusesASpanOutsideTheContent(t *testing.T) {
	ctx, _, s := newStore(t)

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "short", "")
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	err = s.Redact(ctx, factoryStart, impl.ID, []artifact.Span{{Start: 0, End: 50}})
	if !errors.Is(err, artifact.ErrSpanOutOfRange) {
		t.Errorf("Redact with an out-of-range span = %v, want ErrSpanOutOfRange", err)
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

	installed, err := s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "spec_author", "",
		"the shipped role prompt", artifact.EnteredByInstall, "bundle-2026.1")
	if err != nil {
		t.Fatalf("EnterShipped at install: %v", err)
	}

	// Nothing is approved, and the install's entry is in force anyway.
	found, ok, err := artifact.InForce(ctx, pool, artifact.KindRolePrompt, "spec_author", "", nil)
	if err != nil || !ok || found.ID != installed.ID {
		t.Fatalf("InForce with nothing approved = %+v, %v, %v; want the install's entry %s", found, ok, err, installed.ID)
	}

	upgraded, err := s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "spec_author", "",
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
	started, err := s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "reviewer", "",
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
		supersedes, authorship, author, content, content_digest, shipped_bundle_identity, entered_by,
		input_manifest_id)
		values ($1, $2, 'component', 'factory.start', '', $3, '', 'spec_author', '', 'role_prompt', 1,
		'', $4, $5, 'text', 'x', $6, $7, '')`

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
