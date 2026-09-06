// The database tests of this package are in screenstatemachine_test rather
// than in screenstatemachine, because they open the pool through package
// postgres, and an external test package keeps that a test edge rather than
// an import of the package that will one day apply this schema. The DDL is
// applied here with pool.Exec, statement by statement, because postgres.Apply
// does not know this package until integration.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package screenstatemachine_test

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

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// newSchema gives a test a schema of its own with this package's DDL applied
// inside it. The schema is dropped when the test ends, so a rerun on a
// database a previous run left dirty starts clean.
func newSchema(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1ssm_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range screenstatemachine.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying statement %d: %v", n+1, err)
		}
	}
	return ctx, pool
}

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

var store = record.Actor{Kind: record.KindComponent, Key: "artifact.store", Basis: record.BasisClaimed}

func simpleDraft() screenstatemachine.Draft {
	return screenstatemachine.Draft{
		Initial: "empty",
		States:  []string{"empty", "loaded"},
		Events:  []string{"load"},
		Transitions: []screenstatemachine.Transition{
			{From: "empty", Event: "load", To: "loaded"},
		},
		Terminal: []string{"loaded"},
	}
}

func TestInsertNamesTheMachineItsOwnScreen(t *testing.T) {
	ctx, pool := newSchema(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	m, err := screenstatemachine.Insert(ctx, tx, store,
		screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_a"}, simpleDraft())
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if m.Screen != m.ID {
		t.Errorf("Screen = %q, want the machine's own id %q", m.Screen, m.ID)
	}
	if m.Supersedes != "" {
		t.Errorf("Supersedes = %q, want empty", m.Supersedes)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

func TestASupersedingMachineInheritsTheScreen(t *testing.T) {
	ctx, pool := newSchema(t)

	first := insert(t, ctx, pool, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_a"}, simpleDraft())

	d := simpleDraft()
	d.Supersedes = first.ID
	second := insert(t, ctx, pool, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_b"}, d)

	if second.Screen != first.Screen {
		t.Errorf("the superseding machine's screen = %q, want %q", second.Screen, first.Screen)
	}
	if second.Screen != first.ID {
		t.Errorf("the screen = %q, want the introducing machine's id %q", second.Screen, first.ID)
	}
}

func TestInsertRefusesASupersedesThatDoesNotExist(t *testing.T) {
	ctx, pool := newSchema(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	d := simpleDraft()
	d.Supersedes = "ssm_00000000000000000000000000000000"
	_, err = screenstatemachine.Insert(ctx, tx,
		store, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_a"}, d)
	if !errors.Is(err, screenstatemachine.ErrSupersedesNotFound) {
		t.Errorf("Insert = %v, want %v", err, screenstatemachine.ErrSupersedesNotFound)
	}
}

func TestInsertRefusesAnIllFormedDraft(t *testing.T) {
	ctx, pool := newSchema(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	d := simpleDraft()
	d.States = append(d.States, "orphan")
	_, err = screenstatemachine.Insert(ctx, tx,
		store, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_a"}, d)
	var unreachable *screenstatemachine.UnreachableStateError
	if !errors.As(err, &unreachable) {
		t.Errorf("Insert = %v, want a *UnreachableStateError", err)
	}
}

func TestAnEmptyServiceIDIsRefused(t *testing.T) {
	ctx, pool := newSchema(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = screenstatemachine.Insert(ctx, tx,
		store, screenstatemachine.Of{SpecArtifactID: "art_a", ItemID: "it_a"}, simpleDraft())
	if !errors.Is(err, screenstatemachine.ErrServiceIDEmpty) {
		t.Errorf("Insert = %v, want %v", err, screenstatemachine.ErrServiceIDEmpty)
	}
}

// TestDDLRefusesAnEmptyRequiredColumn is this package's link columns, checked
// twice: the writer above, and here the store around it. record's doc.go
// states what a link is checked for.
func TestDDLRefusesAnEmptyRequiredColumn(t *testing.T) {
	ctx, pool := newSchema(t)
	_, err := pool.Exec(ctx, `insert into `+screenstatemachine.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
		screen, supersedes, initial, states, events, transitions, terminal)
		values ($1, $2, 'component', 'artifact.store', 'claimed', $3, '', 'art_a', 'it_a',
		'ssm_x', '', 'empty', '{empty}', '{}', '[]', '{}')`,
		record.NewID(screenstatemachine.IDPrefix), screenstatemachine.FormatVersion, record.Now())
	if err == nil || !strings.Contains(err.Error(), "service_id_present") {
		t.Errorf("inserting a machine naming no service = %v, want a violation of service_id_present", err)
	}
}

func TestInForceExcludesASupersededMachine(t *testing.T) {
	ctx, pool := newSchema(t)

	first := insert(t, ctx, pool, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_a", ItemID: "it_a"}, simpleDraft())
	other := insert(t, ctx, pool, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_b", ItemID: "it_b"}, simpleDraft())

	d := simpleDraft()
	d.Supersedes = first.ID
	revision := insert(t, ctx, pool, screenstatemachine.Of{ServiceID: "svc_a", SpecArtifactID: "art_c", ItemID: "it_c"}, d)

	inForce, err := screenstatemachine.InForce(ctx, pool, "svc_a", []string{"it_a", "it_b", "it_c"})
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	ids := map[string]bool{}
	for _, m := range inForce {
		ids[m.ID] = true
	}
	if ids[first.ID] {
		t.Errorf("InForce includes %s, which %s supersedes", first.ID, revision.ID)
	}
	if !ids[other.ID] || !ids[revision.ID] {
		t.Errorf("InForce = %+v, want %s and %s", inForce, other.ID, revision.ID)
	}
}

func TestInForceOfNoItemsIsEmpty(t *testing.T) {
	ctx, pool := newSchema(t)
	inForce, err := screenstatemachine.InForce(ctx, pool, "svc_a", nil)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 0 {
		t.Errorf("InForce of no items = %+v, want none", inForce)
	}
}

func insert(t *testing.T, ctx context.Context, pool *pgxpool.Pool, of screenstatemachine.Of, draft screenstatemachine.Draft) screenstatemachine.Machine {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	m, err := screenstatemachine.Insert(ctx, tx, store, of, draft)
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return m
}
