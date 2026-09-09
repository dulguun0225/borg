// The database tests of who authored a version and of what it was authored
// from: the author a per-author prior is kept on, the two reads over it, and
// the input manifest every submission takes. They share db_test.go's newStore
// and the actors and drafts it declares.
package artifact_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/record"
)

// TestTheAuthorIsWhatAPriorIsKeptOn: two roles on one model are one author, and
// the store keeps the author beside the authorship rather than instead of it.
func TestTheAuthorIsWhatAPriorIsKeptOn(t *testing.T) {
	ctx, pool, s := newStore(t)

	spec, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "a spec", nil, nil, nil, theManifest)
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	implementation, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a commit", theManifest)
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	if spec.Actor == implementation.Actor {
		t.Error("the two versions were written by one actor, and the roles differ")
	}
	if spec.Author != modelVersion || implementation.Author != modelVersion {
		t.Errorf("the authors are %q and %q, want the one model %q", spec.Author, implementation.Author, modelVersion)
	}

	authored, err := artifact.IDsByAuthor(ctx, pool, modelVersion)
	if err != nil {
		t.Fatalf("IDsByAuthor: %v", err)
	}
	if len(authored) != 2 {
		t.Errorf("%s wrote %d versions, want both", modelVersion, len(authored))
	}
	if others, err := artifact.IDsByAuthor(ctx, pool, "some-other-model"); err != nil || len(others) != 0 {
		t.Errorf("IDsByAuthor of a model that wrote nothing = %v, %v", others, err)
	}
	if none, err := artifact.IDsByAuthor(ctx, pool, ""); err != nil || len(none) != 0 {
		t.Errorf("IDsByAuthor of no author = %v, %v", none, err)
	}

	newest, found, err := artifact.NewestOfKind(ctx, pool, "it_a", artifact.KindImplementation)
	if err != nil || !found {
		t.Fatalf("NewestOfKind = %+v, %v, %v", newest, found, err)
	}
	if newest.ID != implementation.ID {
		t.Errorf("the newest implementation is %s, want %s", newest.ID, implementation.ID)
	}
	if _, found, err := artifact.NewestOfKind(ctx, pool, "it_nothing", artifact.KindImplementation); err != nil || found {
		t.Errorf("NewestOfKind on an item with no version = %v, %v", found, err)
	}

	second, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a second commit", theManifest)
	if err != nil {
		t.Fatalf("SubmitImplementation again: %v", err)
	}
	newest, _, err = artifact.NewestOfKind(ctx, pool, "it_a", artifact.KindImplementation)
	if err != nil {
		t.Fatalf("NewestOfKind: %v", err)
	}
	if newest.ID != second.ID || newest.Version != 2 {
		t.Errorf("the newest implementation is %s at version %d, want the second one", newest.ID, newest.Version)
	}
}

// TestItemsByAuthorIsInTheOrderTheVersionsWereWritten is what the read
// promises: every item this author wrote a version of, once each, standing
// where the author's first version of it stands. A fleet kind names no item
// and is not one of them.
func TestItemsByAuthorIsInTheOrderTheVersionsWereWritten(t *testing.T) {
	ctx, pool, s := newStore(t)

	// The items are written in an order the item ids do not sort in, so a read
	// that returned item-id order would fail here.
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_zebra", "a commit", theManifest); err != nil {
		t.Fatalf("SubmitImplementation on the first item: %v", err)
	}
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_apple", "a commit", theManifest); err != nil {
		t.Fatalf("SubmitImplementation on the second item: %v", err)
	}
	// A second version of the first item moves it nowhere.
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_zebra", "another commit", theManifest); err != nil {
		t.Fatalf("SubmitImplementation again on the first item: %v", err)
	}
	// A fleet version by the same author names no item.
	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "words", theManifest); err != nil {
		t.Fatalf("SubmitFleet: %v", err)
	}

	items, err := artifact.ItemsByAuthor(ctx, pool, modelVersion)
	if err != nil {
		t.Fatalf("ItemsByAuthor: %v", err)
	}
	if len(items) != 2 || items[0] != "it_zebra" || items[1] != "it_apple" {
		t.Errorf("ItemsByAuthor = %v, want [it_zebra it_apple] — once each, in the order the versions were written", items)
	}

	if none, err := artifact.ItemsByAuthor(ctx, pool, ""); err != nil || len(none) != 0 {
		t.Errorf("ItemsByAuthor of no author = %v, %v", none, err)
	}
}

// TestTheVersionNamesTheManifestItWasAuthoredFrom: the submission takes what
// the run was handed as its last argument and writes it on the row, a version
// an agent authored names one or is refused, and a call authoring nothing
// reads no manifest — the DDL refuses one there.
func TestTheVersionNamesTheManifestItWasAuthoredFrom(t *testing.T) {
	ctx, pool, s := newStore(t)

	spec, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "a spec", nil, nil, nil, "im_one")
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	if spec.InputManifestID != "im_one" {
		t.Errorf("the spec version was authored from %q, want im_one", spec.InputManifestID)
	}
	read, err := artifact.Get(ctx, pool, spec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.InputManifestID != "im_one" {
		t.Errorf("the stored version names manifest %q, want im_one", read.InputManifestID)
	}

	for name, submit := range map[string]func() (artifact.Artifact, error){
		"the plan": func() (artifact.Artifact, error) {
			return s.SubmitPlan(ctx, implementer, byAgent, "it_a", "a plan", "im_two")
		},
		"the tasks": func() (artifact.Artifact, error) {
			return s.SubmitTasks(ctx, implementer, byAgent, "it_a", "a task", "im_two")
		},
		"the implementation": func() (artifact.Artifact, error) {
			return s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a commit", "im_two")
		},
		"a fleet version": func() (artifact.Artifact, error) {
			return s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindSkill, "", "svc_a", "a skill", "im_two")
		},
	} {
		written, err := submit()
		if err != nil {
			t.Fatalf("submitting %s: %v", name, err)
		}
		if written.InputManifestID != "im_two" {
			t.Errorf("%s was authored from %q, want im_two", name, written.InputManifestID)
		}
	}

	// A version an agent authored names the manifest the dispatch wrote, so a
	// submission naming none is refused by the store and by the writer.
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_b", "a commit", ""); !errors.Is(err, artifact.ErrInputManifestEmpty) {
		t.Errorf("an agent's submission naming no manifest = %v, want ErrInputManifestEmpty", err)
	}
	_, err = pool.Exec(ctx, insertAVersion,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"it_b", "", "implementation", "agent", modelVersion, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "input_manifest_names_the_dispatch") {
		t.Errorf("inserting an agent's version naming no manifest = %v, want a violation of input_manifest_names_the_dispatch", err)
	}

	// A human at a stage and a human at a gate author outside a dispatch, and
	// there is no manifest of theirs to name.
	for _, by := range []artifact.By{
		{Authorship: artifact.AuthorshipHuman, Author: "the implementer"},
		{Authorship: artifact.AuthorshipGate, Author: "the human at the gate"},
	} {
		written, err := s.SubmitImplementation(ctx, implementer, by, "it_c", "a commit", "")
		if err != nil {
			t.Fatalf("SubmitImplementation authored by %s outside a dispatch: %v", by.Authorship, err)
		}
		if written.InputManifestID != "" {
			t.Errorf("a version %s authored names manifest %q, want none", by.Authorship, written.InputManifestID)
		}
	}

	// An entry nobody wrote reads no manifest, and the store refuses one.
	entered, err := s.EnterShipped(ctx, artifact.FactoryStart, artifact.KindRolePrompt, "spec_author", "",
		"shipped words", artifact.EnteredByInstall, "bundle-1")
	if err != nil {
		t.Fatalf("EnterShipped: %v", err)
	}
	if entered.InputManifestID != "" {
		t.Errorf("the shipped entry names manifest %q, want none", entered.InputManifestID)
	}
	_, err = pool.Exec(ctx, insertAVersion,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"", "reviewer", "role_prompt", "", "", "bundle-1", "install", "im_three")
	if err == nil || !strings.Contains(err.Error(), "input_manifest_only_when_authored") {
		t.Errorf("inserting a shipped entry naming a manifest = %v, want a violation of input_manifest_only_when_authored", err)
	}
}

// TestTheOnlyItemKindNobodyAuthoredIsAConsumerContract: a version an
// authorship does not name is the factory's own start's, and on an item's
// chain the one such version is the contract the first-start step derived
// again. An unauthored spec, plan, tasks or implementation is nobody's work.
func TestTheOnlyItemKindNobodyAuthoredIsAConsumerContract(t *testing.T) {
	ctx, pool, _ := newStore(t)

	_, err := pool.Exec(ctx, insertAVersion,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"it_a", "", string(artifact.KindSpec), "", "", "bundle-1", string(artifact.EnteredByUpgradeFirstStart), "")
	if err == nil || !strings.Contains(err.Error(), "unauthored_item_kind_is_a_consumer_contract") {
		t.Errorf("inserting a spec nobody authored = %v, want a violation of unauthored_item_kind_is_a_consumer_contract", err)
	}
	if _, err := pool.Exec(ctx, insertAVersion,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now(),
		"it_a", "", string(artifact.KindConsumerContract), "", "", "bundle-1",
		string(artifact.EnteredByUpgradeFirstStart), ""); err != nil {
		t.Errorf("inserting the contract the first-start step derived again: %v", err)
	}
}

// insertAVersion is a row written around the writer, for the CHECK constraints
// the store holds a second time. The parameters after the timestamp are the
// item, the role, the kind, the authorship, the author, the shipped bundle
// identity, the event that entered it, and the manifest.
const insertAVersion = `insert into artifact
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
	supersedes, authorship, author, content, content_digest, redacted_content_digest,
	shipped_bundle_identity, entered_by, input_manifest_id)
	values ($1, $2, 'component', 'install', 'claimed', $3, $4, $5, '', $6, 1,
	'', $7, $8, 'text', 'x', '', $9, $10, $11)`
