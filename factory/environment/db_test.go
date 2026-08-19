// The database tests of this package are in environment_test rather than in
// environment, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as
// "test environment -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package environment_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

var credential = secretref.MustNew("deploy.local")

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *environment.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_env_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
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
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	return ctx, pool, environment.NewWriter(pool)
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

// TestProductionIsCreatedWithItsTargetsAndItsCredential: an environment is a
// record and not a name in code, and what it names is where a deploy into it is
// performed and what it is performed with.
func TestProductionIsCreatedWithItsTargetsAndItsCredential(t *testing.T) {
	ctx, pool, w := newTable(t)

	targets := []string{"/srv/targets/one", "/srv/targets/two"}
	created, err := w.Create(ctx, owner, environment.KindProduction, environment.ProductionName, targets, credential)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if created.Kind != environment.KindProduction {
		t.Errorf("the record's kind is %q, want production", created.Kind)
	}

	read, err := environment.Get(ctx, pool, created.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.Targets, targets) {
		t.Errorf("the targets read back as %v, want %v", read.Targets, targets)
	}
	if read.Credential != credential {
		t.Errorf("the credential reads back as %v, want the reference %v", read.Credential, credential)
	}

	byName, found, err := environment.ByName(ctx, pool, environment.ProductionName)
	if err != nil || !found || byName.ID != created.ID {
		t.Fatalf("ByName = %+v, %v, %v", byName, found, err)
	}
}

// TestTheKindIsTheSeamAndOnlyOneIsWritten: the kind is fixed at creation and two
// writers never write a record of the other's kind. This milestone writes
// production, and a kind it does not build is refused by the writer and by the
// store.
func TestTheKindIsTheSeamAndOnlyOneIsWritten(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Create(ctx, owner, environment.Kind("candidate"), "cand", []string{"/srv"}, credential); !errors.Is(err, environment.ErrKindUnknown) {
		t.Errorf("Create of a candidate environment = %v, want ErrKindUnknown", err)
	}
	if _, err := w.Create(ctx, owner, environment.KindProduction, "production", nil, credential); !errors.Is(err, environment.ErrTargetsEmpty) {
		t.Errorf("Create with no target = %v, want ErrTargetsEmpty", err)
	}
	if _, err := w.Create(ctx, record.Actor{}, environment.KindProduction, "production", []string{"/srv"}, credential); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want ErrKindUnknown", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+environment.Table+`
		(id, actor_kind, actor_name, at, kind, name, targets, credential)
		values ('env_x', 'human', 'owner', $1, 'candidate', 'cand', '/srv', 'deploy.local')`,
		record.Now()); err == nil {
		t.Error("the store accepted a kind written around the writer")
	}
}

// TestDDLListsEveryKind keeps the CHECK constraint and [environment.Kinds] from
// disagreeing: the constraint is SQL text rather than built from the slice, so
// this is what says they still name the same kinds.
func TestDDLListsEveryKind(t *testing.T) {
	// The constraint is named in the search, because record's own actor_kind
	// constraint is a "kind in (" earlier in the same statement.
	const open = "constraint kind_known check (kind in ("
	statement := environment.DDL[0]
	i := strings.Index(statement, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[i+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(environment.Kinds) {
		t.Fatalf("the constraint lists %d kinds, Kinds has %d", len(listed), len(environment.Kinds))
	}
	for n, k := range environment.Kinds {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(k)+"'"; got != want {
			t.Errorf("the constraint lists %s where Kinds has %s", got, want)
		}
	}
}

// TestAThresholdExistsOnlyWhereAnOwnerAuthoredOne: an absent row is the score
// supplying the value, and re-authoring is one row rather than two.
func TestAThresholdExistsOnlyWhereAnOwnerAuthoredOne(t *testing.T) {
	ctx, pool, w := newTable(t)

	production, err := w.Create(ctx, owner, environment.KindProduction, environment.ProductionName, []string{"/srv"}, credential)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	authored, err := environment.GateThreshold(ctx, pool, production.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("GateThreshold: %v", err)
	}
	if authored.Present {
		t.Errorf("an unauthored threshold reads back as %+v", authored)
	}

	for _, threshold := range []float64{0.4, 0.2} {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("Begin: %v", err)
		}
		if err := environment.SetGateThreshold(ctx, tx, owner, production.ID, "merge_to_master", threshold); err != nil {
			t.Fatalf("SetGateThreshold(%v): %v", threshold, err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("Commit: %v", err)
		}
	}

	authored, err = environment.GateThreshold(ctx, pool, production.ID, "merge_to_master")
	if err != nil {
		t.Fatalf("GateThreshold: %v", err)
	}
	if !authored.Present || authored.Number != 0.2 {
		t.Errorf("the re-authored threshold reads back as %+v, want 0.2 present", authored)
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from `+environment.ThresholdTable).Scan(&rows); err != nil {
		t.Fatalf("counting the threshold rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("two authorings of one row left %d rows, want 1", rows)
	}

	// Another row on the same environment is another threshold, not the same one.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := environment.SetGateThreshold(ctx, tx, owner, production.ID, "deploy_to_production", 0.5); err != nil {
		t.Fatalf("SetGateThreshold on a second row: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	deployRow, err := environment.GateThreshold(ctx, pool, production.ID, "deploy_to_production")
	if err != nil {
		t.Fatalf("GateThreshold: %v", err)
	}
	if !deployRow.Present || deployRow.Number != 0.5 {
		t.Errorf("the second row's threshold reads back as %+v, want 0.5 present", deployRow)
	}
}

// TestAThresholdOffTheScaleIsRefusedTwice: the score's number is between nothing
// and one, so a threshold outside that compares against nothing the score can
// produce. The writer refuses it and so does the store.
func TestAThresholdOffTheScaleIsRefusedTwice(t *testing.T) {
	ctx, pool, w := newTable(t)

	production, err := w.Create(ctx, owner, environment.KindProduction, environment.ProductionName, []string{"/srv"}, credential)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := environment.SetGateThreshold(ctx, tx, owner, production.ID, "merge_to_master", 1.5); !errors.Is(err, environment.ErrThresholdOutOfRange) {
		t.Errorf("SetGateThreshold(1.5) = %v, want ErrThresholdOutOfRange", err)
	}
	if err := environment.SetGateThreshold(ctx, tx, owner, production.ID, "", 0.5); !errors.Is(err, environment.ErrGateRowEmpty) {
		t.Errorf("SetGateThreshold naming no row = %v, want ErrGateRowEmpty", err)
	}

	if _, err := pool.Exec(ctx, `insert into `+environment.ThresholdTable+`
		(id, actor_kind, actor_name, at, environment_id, gate_row, threshold)
		values ('egt_x', 'human', 'owner', $1, $2, 'merge_to_master', 1.5)`,
		record.Now(), production.ID); err == nil {
		t.Error("the store accepted a threshold off the scale written around the writer")
	}
}

// TestTheCredentialIsAReferenceAndNoValue: nothing that renders this record
// renders a secret, which is the seam the store carries from the first record.
func TestTheCredentialIsAReferenceAndNoValue(t *testing.T) {
	ctx, pool, w := newTable(t)

	created, err := w.Create(ctx, owner, environment.KindProduction, environment.ProductionName, []string{"/srv"}, credential)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	var stored string
	if err := pool.QueryRow(ctx, `select `+environment.Table+`::text from `+environment.Table+` where id = $1`, created.ID).Scan(&stored); err != nil {
		t.Fatalf("reading the row as text: %v", err)
	}
	if !strings.Contains(stored, credential.Name()) {
		t.Errorf("the row does not name the credential reference: %s", stored)
	}
	if strings.Contains(stored, "sk-") {
		t.Errorf("the row holds something that reads like a secret value: %s", stored)
	}
}
