// The database tests of this package are in build_test and open the pool
// through package postgres, the way decisionlog's do; deps.txt records the
// test edge. They apply this package's DDL themselves rather than calling
// postgres.Apply, which does not know this package until integration wires it
// in.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package build_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/exposure"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newTable gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *build.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range build.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, build.NewWriter(pool, token)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writer's statements resolves there.
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

var dispatch = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}

// draftOf is one draft naming itemID, serviceID and commit, with an artifact
// digest every draft this file writes needs and does not otherwise care about.
func draftOf(itemID, serviceID, commit string) build.Draft {
	return build.Draft{ItemID: itemID, ServiceID: serviceID, CommitHash: commit, ArtifactDigest: "sha256:" + commit}
}

func TestCreateWritesTheRecordOnce(t *testing.T) {
	ctx, pool, w := newTable(t)

	itemID, serviceID := record.NewID("it"), record.NewID("svc")
	created, err := w.Create(ctx, dispatch, draftOf(itemID, serviceID, "0badc0de0badc0de0badc0de0badc0de0badc0de"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.ItemID != itemID || created.ServiceID != serviceID || created.CommitHash != "0badc0de0badc0de0badc0de0badc0de0badc0de" {
		t.Errorf("Create returned %+v, which does not name what it was given", created)
	}
	if _, err := time.Parse(record.TimeLayout, created.At); err != nil {
		t.Errorf("the record has timestamp %q: %v", created.At, err)
	}

	read, err := build.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read, created) {
		t.Errorf("Get = %+v, want the record Create returned, %+v", read, created)
	}
}

func TestASecondBuildOfOneCommitIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	itemID, serviceID := record.NewID("it"), record.NewID("svc")

	if _, err := w.Create(ctx, dispatch, draftOf(itemID, serviceID, "aaaa")); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := w.Create(ctx, dispatch, draftOf(itemID, serviceID, "aaaa")); err == nil {
		t.Error("a second record of the same item and commit was accepted")
	}
	if _, err := w.Create(ctx, dispatch, draftOf(itemID, serviceID, "bbbb")); err != nil {
		t.Errorf("a second commit of the same item was refused: %v", err)
	}
	if _, err := w.Create(ctx, dispatch, draftOf(record.NewID("it"), serviceID, "aaaa")); err != nil {
		t.Errorf("the same commit for another item was refused: %v", err)
	}
}

func TestAnEmptyCommitHashIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Create(ctx, dispatch, draftOf(record.NewID("it"), record.NewID("svc"), "")); !errors.Is(err, build.ErrCommitHashEmpty) {
		t.Errorf("Create = %v, want %v", err, build.ErrCommitHashEmpty)
	}

	// Around the writer, the CHECK constraint is what refuses it.
	_, err := pool.Exec(ctx, `insert into build (id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, commit_hash, artifact_digest, resolved_set_coverage, resolved_set_could_not_derive, notice_file, design_system_constraint_id, shipped_bundle_identity)
		values ($1, $2, 'component', 'dispatch', 'claimed', $3, $4, $5, '', 'sha256:x', '', '', '', '', '')`,
		record.NewID(build.IDPrefix), build.FormatVersion, record.Now(), record.NewID("it"), record.NewID("svc"))
	if err == nil {
		t.Error("the store accepted a build with no commit hash")
	}
}

// TestAnEmptyServiceIDIsRefusedTwice is this package's required link column.
// item_id may be empty — a search build names a service and no item — but
// service_id is required on every build, refused by the writer and by the
// store, the way every other required field is; record's doc.go states what a
// link is checked for.
func TestAnEmptyServiceIDIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Create(ctx, dispatch, draftOf(record.NewID("it"), "", "aaaa")); !errors.Is(err, build.ErrServiceIDEmpty) {
		t.Errorf("Create = %v, want %v", err, build.ErrServiceIDEmpty)
	}

	_, err := pool.Exec(ctx, `insert into build (id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id, commit_hash, artifact_digest, resolved_set_coverage, resolved_set_could_not_derive, notice_file, design_system_constraint_id, shipped_bundle_identity, declares_schema_change)
		values ($1, $2, 'component', 'dispatch', 'claimed', $3, $4, '', 'aaaa', 'sha256:x', '', '', '', '', '', false)`,
		record.NewID(build.IDPrefix), build.FormatVersion, record.Now(), record.NewID("it"))
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a build naming no service = %v, want a violation of service_id_present", err)
	}
}

func TestABadActorIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	if _, err := w.Create(ctx, record.Actor{}, draftOf(record.NewID("it"), record.NewID("svc"), "aaaa")); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create = %v, want %v", err, record.ErrKindUnknown)
	}
}

func TestGetOfNothingIsNotFound(t *testing.T) {
	ctx, pool, _ := newTable(t)
	if _, err := build.Get(ctx, pool, "bl_00000000000000000000000000000000"); !errors.Is(err, build.ErrNotFound) {
		t.Errorf("Get = %v, want %v", err, build.ErrNotFound)
	}
}

// TestForCommitAnswersWhichBuildIsAlreadyThere: a rebuild is a new build, so a
// re-verification that produced the commit already built produced no build. The
// caller asks before it writes one, rather than being refused by the unique
// constraint and left without the record that is there.
func TestForCommitAnswersWhichBuildIsAlreadyThere(t *testing.T) {
	ctx, pool, w := newTable(t)
	const itemID, serviceID, commit = "it_a", "svc_a", "8bd35e6a5b0f1ee5f0f2f6f39c5d0f0f6a2b1c3d"

	if _, found, err := build.ForCommit(ctx, pool, itemID, serviceID, commit); err != nil || found {
		t.Fatalf("ForCommit before anything was built = found %v, %v", found, err)
	}

	made, err := w.Create(ctx, dispatch, draftOf(itemID, serviceID, commit))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	found, ok, err := build.ForCommit(ctx, pool, itemID, serviceID, commit)
	if err != nil || !ok {
		t.Fatalf("ForCommit = ok %v, %v", ok, err)
	}
	if !reflect.DeepEqual(found, made) {
		t.Errorf("ForCommit = %+v, want the build that was made, %+v", found, made)
	}

	// Another item at the same commit is another build, the record being one per
	// commit built for an item.
	if _, ok, err := build.ForCommit(ctx, pool, "it_b", serviceID, commit); err != nil || ok {
		t.Errorf("ForCommit for another item = ok %v, %v", ok, err)
	}
}

// TestTheExposureListIsStoredAndAnEmptyOneIsNotNothing: what the build runner
// derived from its own checkout is on the record, and a build no extractor ran
// for is told from a diff that reached nothing new. The two call for opposite
// responses at a gate, so the column keeps them apart rather than an empty list
// standing for both.
func TestTheExposureListIsStoredAndAnEmptyOneIsNotNothing(t *testing.T) {
	ctx, pool, w := newTable(t)
	itemID, serviceID := record.NewID("it"), record.NewID("svc")

	reached := exposure.Evidence{
		OutboundCalls:     []string{"main.go:3 — a new import of net/http"},
		DependencyChanges: []string{"go.mod:5 — example.com/x v1.2.3, licence MIT"},
	}
	draft := draftOf(itemID, serviceID, "aaaa")
	draft.Exposure = &reached
	draft.DeclaresSchemaChange = true
	created, err := w.Create(ctx, dispatch, draft)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if !created.DeclaresSchemaChange {
		t.Error("the record does not say the build declares a schema change")
	}
	read, found, err := build.Exposure(ctx, pool, created.ID)
	if err != nil || !found {
		t.Fatalf("Exposure = found %v, %v", found, err)
	}
	if !reflect.DeepEqual(read, reached) {
		t.Errorf("Exposure = %+v, want %+v", read, reached)
	}

	// A diff that reached nothing new is a reading and answers found.
	empty := exposure.Evidence{}
	nothing := draftOf(itemID, serviceID, "bbbb")
	nothing.Exposure = &empty
	made, err := w.Create(ctx, dispatch, nothing)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if read, found, err := build.Exposure(ctx, pool, made.ID); err != nil || !found || len(read.List()) != 0 {
		t.Errorf("Exposure of a diff that reached nothing = %+v, found %v, %v", read, found, err)
	}

	// A build no extractor ran for holds none, which is what resolves the factor.
	unread, err := w.Create(ctx, dispatch, draftOf(itemID, serviceID, "cccc"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if read, found, err := build.Exposure(ctx, pool, unread.ID); err != nil || found {
		t.Errorf("Exposure of a build nobody read = %+v, found %v, %v", read, found, err)
	}
	if got, err := build.Get(ctx, pool, unread.ID); err != nil || got.DeclaresSchemaChange {
		t.Errorf("a build declaring no schema change reads %+v, %v", got, err)
	}
}
