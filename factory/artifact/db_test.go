// The database tests of this package are in artifact_test rather than in
// artifact, because they open the pool through package postgres, and an
// external test package keeps that a test edge rather than an import of the
// package that will one day apply this schema. Both packages' DDL is applied
// here with pool.Exec, statement by statement — criterion's first, because
// the store writes into that table too — since postgres.Apply does not know
// either package until integration.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package artifact_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newStore gives a test a schema of its own with the criterion and artifact
// DDL applied inside it, and a store over it. The schema is dropped when the
// test ends, so a rerun on a database a previous run left dirty starts
// clean.
func newStore(t *testing.T) (context.Context, *pgxpool.Pool, *artifact.Store) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1art_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})

	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	for n, statement := range criterion.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying criterion statement %d: %v", n+1, err)
		}
	}
	for n, statement := range artifact.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying artifact statement %d: %v", n+1, err)
		}
	}
	return ctx, pool, artifact.NewStore(pool)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the store's statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

var specAuthor = record.Actor{Kind: record.KindComponent, Name: "agent.spec_author"}
var implementer = record.Actor{Kind: record.KindComponent, Name: "agent.implementer"}

// modelVersion is the author both roles write as here, which is the point of the
// field: the prior is kept per model version, so two agents in two roles on one
// model are one author under two actors.
const modelVersion = "claude-opus-5"

var byAgent = artifact.By{Authorship: artifact.AuthorshipAgent, Author: modelVersion}

// TestSubmitSpecWritesTheSpecAndItsCriteriaTogether is the one call: the
// artifact row and each criterion row committed in one transaction, the
// criteria naming the version that introduced them.
func TestSubmitSpecWritesTheSpecAndItsCriteriaTogether(t *testing.T) {
	ctx, pool, s := newStore(t)

	spec, criteria, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a",
		"The service opens an intent per report.",
		[]artifact.Draft{
			{Sentence: "When a report arrives, the system shall open an intent."},
			{Sentence: "The checkout page loads fast.", EscapeReason: "no pattern fits a latency feel"},
		})
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	if spec.Kind != artifact.KindSpec || spec.Version != 1 || spec.Supersedes != "" {
		t.Errorf("the first spec version is %+v, want kind spec, version 1, superseding nothing", spec)
	}
	if len(criteria) != 2 {
		t.Fatalf("SubmitSpec wrote %d criteria, want 2", len(criteria))
	}
	for _, c := range criteria {
		if c.SpecArtifactID != spec.ID {
			t.Errorf("criterion %s names spec version %s, want %s", c.ID, c.SpecArtifactID, spec.ID)
		}
		if c.ServiceID != "svc_a" {
			t.Errorf("criterion %s belongs to %s, want svc_a", c.ID, c.ServiceID)
		}
	}
	if criteria[0].Pattern != criterion.PatternEvent || criteria[1].Pattern != criterion.PatternEscape {
		t.Errorf("the criteria classified as %s and %s, want %s and %s",
			criteria[0].Pattern, criteria[1].Pattern, criterion.PatternEvent, criterion.PatternEscape)
	}

	read, err := artifact.Get(ctx, pool, spec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != spec {
		t.Errorf("Get returned %+v, want %+v", read, spec)
	}
	inForce, err := criterion.InForce(ctx, pool, "svc_a", []string{spec.ItemID})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 2 {
		t.Errorf("InForce returned %d criteria, want 2", len(inForce))
	}
}

// TestABadDraftTakesTheArtifactRowDownWithIt is the transaction: a draft the
// criterion package refuses rolls the whole call back, and no artifact row
// survives.
func TestABadDraftTakesTheArtifactRowDownWithIt(t *testing.T) {
	ctx, pool, s := newStore(t)

	_, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a",
		"A spec whose criteria do not all hold.",
		[]artifact.Draft{
			{Sentence: "When a report arrives, the system shall open an intent."},
			{Sentence: "The checkout page loads fast."}, // no pattern, no reason
		})
	if !errors.Is(err, criterion.ErrReasonMissing) {
		t.Fatalf("SubmitSpec = %v, want ErrReasonMissing", err)
	}

	var artifacts, criteria int
	if err := pool.QueryRow(ctx, `select count(*) from artifact`).Scan(&artifacts); err != nil {
		t.Fatalf("counting artifact rows: %v", err)
	}
	if err := pool.QueryRow(ctx, `select count(*) from criterion`).Scan(&criteria); err != nil {
		t.Fatalf("counting criterion rows: %v", err)
	}
	if artifacts != 0 || criteria != 0 {
		t.Errorf("the refused submission left %d artifact rows and %d criterion rows, want none of either", artifacts, criteria)
	}
}

// TestTheVersionChain is versioning per item and kind: the version
// increments, supersedes names the prior version's id, and an
// implementation's chain starts at 1 beside a spec already at 2.
func TestTheVersionChain(t *testing.T) {
	ctx, _, s := newStore(t)

	first, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "version one", nil)
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	second, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "version two", nil)
	if err != nil {
		t.Fatalf("SubmitSpec again: %v", err)
	}
	if second.Version != first.Version+1 {
		t.Errorf("the second version is %d, want %d", second.Version, first.Version+1)
	}
	if second.Supersedes != first.ID {
		t.Errorf("the second version supersedes %q, want %s", second.Supersedes, first.ID)
	}

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "3f786850e387550fdab836ed7e6dc881de23001b")
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	if impl.Version != 1 || impl.Supersedes != "" {
		t.Errorf("the first implementation version is %d superseding %q; the chain is per kind, want 1 superseding nothing", impl.Version, impl.Supersedes)
	}
}

// TestSubmitImplementation is the other kind: the content is the commit hash
// the stage produced, and the row reads back whole.
func TestSubmitImplementation(t *testing.T) {
	ctx, pool, s := newStore(t)

	commit := "3f786850e387550fdab836ed7e6dc881de23001b"
	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", commit)
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	if impl.Kind != artifact.KindImplementation {
		t.Errorf("the version is a %s, want %s", impl.Kind, artifact.KindImplementation)
	}
	if impl.Content != commit {
		t.Errorf("the content is %q, want the commit hash %q", impl.Content, commit)
	}
	if _, err := time.Parse(record.TimeLayout, impl.At); err != nil {
		t.Errorf("%s has timestamp %q: %v", impl.ID, impl.At, err)
	}

	read, err := artifact.Get(ctx, pool, impl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != impl {
		t.Errorf("Get returned %+v, want %+v", read, impl)
	}
}

// TestAnUnknownAuthorshipIsRefusedTwice is the store's validation and the
// CHECK constraint behind it: the method refuses an authorship outside the
// three, and a row inserted around the methods is refused by the store.
func TestAnUnknownAuthorshipIsRefusedTwice(t *testing.T) {
	ctx, pool, s := newStore(t)

	_, err := s.SubmitImplementation(ctx, implementer, artifact.By{Authorship: "reviewer", Author: modelVersion}, "it_a", "a commit")
	if !errors.Is(err, artifact.ErrAuthorshipUnknown) {
		t.Fatalf("SubmitImplementation = %v, want ErrAuthorshipUnknown", err)
	}

	_, err = pool.Exec(ctx, `insert into artifact
		(id, actor_kind, actor_name, at, item_id, kind, version, supersedes, authorship, author, content)
		values ($1, 'component', 'agent.implementer', $2, 'it_a', 'implementation', 1, '', 'reviewer', 'claude-opus-5', 'a commit')`,
		record.NewID(artifact.IDPrefix), record.Now())
	if err == nil {
		t.Fatal("the store accepted an authorship outside the three")
	}
}

// TestAnEmptyItemIDIsRefusedTwice is this package's link column. An empty link
// names nothing, so both submissions refuse it and the store refuses it again;
// record's doc.go states what a link is checked for.
func TestAnEmptyItemIDIsRefusedTwice(t *testing.T) {
	ctx, pool, s := newStore(t)

	if _, err := s.SubmitImplementation(ctx, implementer, byAgent, "", "a commit"); !errors.Is(err, artifact.ErrItemIDEmpty) {
		t.Errorf("SubmitImplementation naming no item = %v, want ErrItemIDEmpty", err)
	}
	if _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "", "svc_a", "a spec", nil); !errors.Is(err, artifact.ErrItemIDEmpty) {
		t.Errorf("SubmitSpec naming no item = %v, want ErrItemIDEmpty", err)
	}

	_, err := pool.Exec(ctx, `insert into artifact
		(id, actor_kind, actor_name, at, item_id, kind, version, supersedes, authorship, author, content)
		values ($1, 'component', 'agent.implementer', $2, '', 'implementation', 1, '', 'agent', 'claude-opus-5', 'a commit')`,
		record.NewID(artifact.IDPrefix), record.Now())
	if err == nil || !strings.Contains(err.Error(), "item_id_present") {
		t.Errorf("inserting a version naming no item = %v, want a violation of item_id_present", err)
	}
}

// TestAVersionWithNoAuthorIsRefusedTwice: a per-author prior is computed from
// that author's own work, so a version whose author is not on the record is one
// no prior can read. The store refuses it around the writer too.
func TestAVersionWithNoAuthorIsRefusedTwice(t *testing.T) {
	ctx, pool, s := newStore(t)

	if _, err := s.SubmitImplementation(ctx, implementer,
		artifact.By{Authorship: artifact.AuthorshipAgent}, "it_a", "a commit"); !errors.Is(err, artifact.ErrAuthorEmpty) {
		t.Errorf("SubmitImplementation naming no author = %v, want ErrAuthorEmpty", err)
	}
	if _, _, err := s.SubmitSpec(ctx, specAuthor,
		artifact.By{Authorship: artifact.AuthorshipAgent}, "it_a", "svc_a", "a spec", nil); !errors.Is(err, artifact.ErrAuthorEmpty) {
		t.Errorf("SubmitSpec naming no author = %v, want ErrAuthorEmpty", err)
	}

	_, err := pool.Exec(ctx, `insert into artifact
		(id, actor_kind, actor_name, at, item_id, kind, version, supersedes, authorship, author, content)
		values ($1, 'component', 'agent.implementer', $2, 'it_a', 'implementation', 1, '', 'agent', '', 'a commit')`,
		record.NewID(artifact.IDPrefix), record.Now())
	if err == nil || !strings.Contains(err.Error(), "author_present") {
		t.Errorf("inserting a version naming no author = %v, want a violation of author_present", err)
	}
}

// TestTheAuthorIsWhatAPriorIsKeptOn: two roles on one model are one author, and
// the store keeps the author beside the authorship rather than instead of it.
func TestTheAuthorIsWhatAPriorIsKeptOn(t *testing.T) {
	ctx, pool, s := newStore(t)

	spec, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "a spec", nil)
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	implementation, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a commit")
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

	second, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "a second commit")
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

// TestDDLListsEveryAuthorship fails if [artifact.Authorships] and the CHECK
// constraint in the DDL stop agreeing.
func TestDDLListsEveryAuthorship(t *testing.T) {
	for _, authorship := range artifact.Authorships {
		if !strings.Contains(artifact.DDL[0], "'"+string(authorship)+"'") {
			t.Errorf("the DDL's authorship CHECK does not list %q", authorship)
		}
	}
}

// TestDDLListsEveryKind fails if [artifact.Kinds] and the CHECK constraint in the
// DDL stop agreeing, which is what a kind arriving with a milestone costs: a CHECK
// is a schema edit each time a value arrives.
func TestDDLListsEveryKind(t *testing.T) {
	for _, kind := range artifact.Kinds {
		if !strings.Contains(artifact.DDL[0], "'"+string(kind)+"'") {
			t.Errorf("the DDL's kind CHECK does not list %q", kind)
		}
	}
}
