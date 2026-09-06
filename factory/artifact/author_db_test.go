// The database tests of who authored a version and of what it was authored
// from: the author a per-author prior is kept on, the two reads over it, and
// the input manifest every submission takes. They share db_test.go's newStore
// and the actors and drafts it declares.
package artifact_test

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/record"
)

// TestTheAuthorIsWhatAPriorIsKeptOn: two roles on one model are one author, and
// the store keeps the author beside the authorship rather than instead of it.
func TestTheAuthorIsWhatAPriorIsKeptOn(t *testing.T) {
	ctx, pool, s := newStore(t)

	spec, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "a spec", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	implementation, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a commit", "")
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

	second, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a second commit", "")
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
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_zebra", "a commit", ""); err != nil {
		t.Fatalf("SubmitImplementation on the first item: %v", err)
	}
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_apple", "a commit", ""); err != nil {
		t.Fatalf("SubmitImplementation on the second item: %v", err)
	}
	// A second version of the first item moves it nowhere.
	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_zebra", "another commit", ""); err != nil {
		t.Fatalf("SubmitImplementation again on the first item: %v", err)
	}
	// A fleet version by the same author names no item.
	if _, err := s.SubmitFleet(ctx, specAuthor, byAgent, artifact.KindRolePrompt, "spec_author", "", "words", ""); err != nil {
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

// TestTheInputManifestIsWrittenWhereTheCallerSuppliesOne: the submission takes
// what the run was handed as its last argument and writes it on the row, and
// an entry nobody wrote reads no manifest — the DDL refuses one there.
func TestTheInputManifestIsWrittenWhereTheCallerSuppliesOne(t *testing.T) {
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

	// A caller that dispatched no manifest writes none, which is what every
	// caller does until context assembly exists.
	none, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_b", "a commit", "")
	if err != nil {
		t.Fatalf("SubmitImplementation with no manifest: %v", err)
	}
	if none.InputManifestID != "" {
		t.Errorf("a submission with no manifest wrote %q, want none", none.InputManifestID)
	}

	// An entry nobody wrote reads no manifest, and the store refuses one.
	entered, err := s.EnterShipped(ctx, factoryStart, artifact.KindRolePrompt, "spec_author", "",
		"shipped words", artifact.EnteredByInstall, "bundle-1")
	if err != nil {
		t.Fatalf("EnterShipped: %v", err)
	}
	if entered.InputManifestID != "" {
		t.Errorf("the shipped entry names manifest %q, want none", entered.InputManifestID)
	}
	_, err = pool.Exec(ctx, `insert into artifact
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, shipped_bundle_identity, entered_by,
		input_manifest_id)
		values ($1, $2, 'component', 'factory.start', '', $3, '', 'reviewer', '', 'role_prompt', 1,
		'', '', '', 'text', 'x', 'bundle-1', 'install', 'im_three')`,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "input_manifest_only_when_authored") {
		t.Errorf("inserting a shipped entry naming a manifest = %v, want a violation of input_manifest_only_when_authored", err)
	}
}
