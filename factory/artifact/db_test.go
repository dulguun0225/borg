// The database tests of this package are in artifact_test rather than in
// artifact, because they open the pool through package postgres, and an
// external test package keeps that a test edge rather than an import of the
// package that will one day apply this schema. Every DDL this package's
// writer reaches is applied here with pool.Exec, statement by statement —
// criterion's and screenstatemachine's first, because the store writes into
// those tables too — since postgres.Apply does not know any of the three
// until integration.
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
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// newStore gives a test a schema of its own with the criterion,
// screenstatemachine and artifact DDL applied inside it, and a store over it.
// The schema is dropped when the test ends, so a rerun on a database a
// previous run left dirty starts clean.
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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range criterion.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying criterion statement %d: %v", n+1, err)
		}
	}
	for n, statement := range screenstatemachine.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying screenstatemachine statement %d: %v", n+1, err)
		}
	}
	for n, statement := range artifact.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying artifact statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, artifact.NewStore(pool, token)
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

var specAuthor = record.Actor{Kind: record.KindComponent, Key: "agent.spec_author"}
var implementer = record.Actor{Kind: record.KindComponent, Key: "agent.implementer"}
var factoryStart = record.Actor{Kind: record.KindComponent, Key: "factory.start"}

// modelVersion is the author both roles write as here, which is the point of the
// field: the prior is kept per model version, so two agents in two roles on one
// model are one author under two actors.
const modelVersion = "claude-opus-5"

var byAgent = artifact.By{Authorship: artifact.AuthorshipAgent, Author: modelVersion}

// TestSubmitSpecWritesTheSpecItsCriteriaItsWithdrawalsAndItsMachinesTogether
// is the one call: the artifact row, each criterion row, each withdrawal, and
// each screen state machine, all committed in one transaction, each naming
// the version that introduced or withdrew it.
func TestSubmitSpecWritesTheSpecItsCriteriaItsWithdrawalsAndItsMachinesTogether(t *testing.T) {
	ctx, pool, s := newStore(t)

	_, firstCriteria, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a",
		"The service opens an intent per report.",
		[]criterion.Draft{
			{Sentence: "When a report arrives, the system shall open an intent.", RequirementID: "req_a"},
		}, nil, nil)
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	if len(firstCriteria) != 1 {
		t.Fatalf("the first SubmitSpec wrote %d criteria, want 1", len(firstCriteria))
	}

	spec, criteria, machines, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_b", "svc_a",
		"The service also shows the checkout page.",
		[]criterion.Draft{
			{Sentence: "When checkout opens, the system shall render the page.", RequirementID: "req_b"},
			{Sentence: "The checkout page loads fast.", NoPatternReason: "no pattern fits a latency feel", RequirementID: "req_c"},
		},
		[]string{firstCriteria[0].ID},
		[]screenstatemachine.Draft{{
			Initial: "empty", States: []string{"empty", "loaded"}, Events: []string{"load"},
			Transitions: []screenstatemachine.Transition{{From: "empty", Event: "load", To: "loaded"}},
			Terminal:    []string{"loaded"},
		}})
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	if spec.Kind != artifact.KindSpec || spec.Version != 1 || spec.Supersedes != "" {
		t.Errorf("the first spec version of it_b is %+v, want kind spec, version 1, superseding nothing", spec)
	}
	if spec.ContentDigest == "" {
		t.Error("the content digest is empty")
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
	if criteria[0].Pattern != criterion.PatternEvent || criteria[1].Pattern != criterion.PatternNoPattern {
		t.Errorf("the criteria classified as %s and %s, want %s and %s",
			criteria[0].Pattern, criteria[1].Pattern, criterion.PatternEvent, criterion.PatternNoPattern)
	}
	if len(machines) != 1 || machines[0].Screen != machines[0].ID {
		t.Fatalf("SubmitSpec wrote %+v, want one machine naming itself its own screen", machines)
	}

	read, err := artifact.Get(ctx, pool, spec.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != spec {
		t.Errorf("Get returned %+v, want %+v", read, spec)
	}
	inForce, err := criterion.InForce(ctx, pool, "svc_a", []string{"it_a", "it_b"})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 2 {
		t.Errorf("InForce returned %d criteria, want 2 — it_b's own two, it_a's one being withdrawn", len(inForce))
	}
}

// TestABadDraftTakesTheArtifactRowDownWithIt is the transaction: a draft the
// criterion package refuses rolls the whole call back, and no artifact row
// survives.
func TestABadDraftTakesTheArtifactRowDownWithIt(t *testing.T) {
	ctx, pool, s := newStore(t)

	_, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a",
		"A spec whose criteria do not all hold.",
		[]criterion.Draft{
			{Sentence: "When a report arrives, the system shall open an intent.", RequirementID: "req_a"},
			{Sentence: "The checkout page loads fast."}, // no pattern, no reason
		}, nil, nil)
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

	first, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "version one", nil, nil, nil)
	if err != nil {
		t.Fatalf("SubmitSpec: %v", err)
	}
	second, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "version two", nil, nil, nil)
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
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, shipped_bundle_identity, input_manifest_id)
		values ($1, $2, 'component', 'agent.implementer', '', $3, 'it_a', '', '', 'implementation', 1,
		'', 'reviewer', 'claude-opus-5', 'a commit', 'x', '', '')`,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now())
	if err == nil {
		t.Fatal("the store accepted an authorship outside the four")
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
	if _, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "", "svc_a", "a spec", nil, nil, nil); !errors.Is(err, artifact.ErrItemIDEmpty) {
		t.Errorf("SubmitSpec naming no item = %v, want ErrItemIDEmpty", err)
	}

	_, err := pool.Exec(ctx, `insert into artifact
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, shipped_bundle_identity, input_manifest_id)
		values ($1, $2, 'component', 'agent.implementer', '', $3, '', '', '', 'implementation', 1,
		'', 'agent', 'claude-opus-5', 'a commit', 'x', '', '')`,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "chain_key_matches_kind") {
		t.Errorf("inserting a version naming no item = %v, want a violation of chain_key_matches_kind", err)
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
	if _, _, _, err := s.SubmitSpec(ctx, specAuthor,
		artifact.By{Authorship: artifact.AuthorshipAgent}, "it_a", "svc_a", "a spec", nil, nil, nil); !errors.Is(err, artifact.ErrAuthorEmpty) {
		t.Errorf("SubmitSpec naming no author = %v, want ErrAuthorEmpty", err)
	}

	_, err := pool.Exec(ctx, `insert into artifact
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, role, subject, kind, version,
		supersedes, authorship, author, content, content_digest, shipped_bundle_identity, input_manifest_id)
		values ($1, $2, 'component', 'agent.implementer', '', $3, 'it_a', '', '', 'implementation', 1,
		'', 'agent', '', 'a commit', 'x', '', '')`,
		record.NewID(artifact.IDPrefix), artifact.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "author_pair_together") {
		t.Errorf("inserting a version naming no author = %v, want a violation of author_pair_together", err)
	}
}

// TestTheAuthorIsWhatAPriorIsKeptOn: two roles on one model are one author, and
// the store keeps the author beside the authorship rather than instead of it.
func TestTheAuthorIsWhatAPriorIsKeptOn(t *testing.T) {
	ctx, pool, s := newStore(t)

	spec, _, _, err := s.SubmitSpec(ctx, specAuthor, byAgent, "it_a", "svc_a", "a spec", nil, nil, nil)
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
