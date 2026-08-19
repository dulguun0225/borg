// The database tests of this package are in item_test rather than in item,
// because they open the pool through package postgres. An external test
// package is a separate package to the compiler, so the edge is a test edge
// and not a cycle. deps.txt records it as "test item -> postgres".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package item_test

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
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newWriters gives a test a schema of its own, this package's DDL applied
// inside it, and both writers over it. The schema is dropped when the test
// ends, so a rerun on a database a previous run left dirty starts clean.
func newWriters(t *testing.T) (context.Context, *pgxpool.Pool, *item.Cut, *item.Dispatch) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1it_" + hex.EncodeToString(suffix[:])

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
	for n, statement := range item.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying item statement %d: %v", n+1, err)
		}
	}
	return ctx, pool, item.NewCut(pool), item.NewDispatch(pool)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
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

var cutActor = record.Actor{Kind: record.KindComponent, Name: "cut"}
var dispatchActor = record.Actor{Kind: record.KindComponent, Name: "dispatch"}

// oneItem is an item freshly cut, for the tests that need one to advance or
// report against.
func oneItem(ctx context.Context, t *testing.T, cut *item.Cut) item.Item {
	t.Helper()
	it, err := cut.Create(ctx, cutActor, item.New{
		IntentID:  "in_" + strings.Repeat("0", 32),
		ServiceID: "svc_" + strings.Repeat("0", 32),
		AreaID:    "ar_" + strings.Repeat("0", 32),
		Branch:    "item/checkout-retry",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	return it
}

func TestCutWritesOnceAtSpec(t *testing.T) {
	ctx, pool, cut, _ := newWriters(t)

	it := oneItem(ctx, t, cut)
	if it.Stage != item.StageSpec {
		t.Errorf("a new item is at %s, want spec", it.Stage)
	}
	if _, err := time.Parse(record.TimeLayout, it.At); err != nil {
		t.Errorf("the item's timestamp %q: %v", it.At, err)
	}

	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != it {
		t.Errorf("Get = %+v, want the item as cut, %+v", read, it)
	}

	if _, err := item.Get(ctx, pool, "it_missing"); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrNotFound", err)
	}
	if _, err := cut.Create(ctx, cutActor, item.New{IntentID: "in_x", ServiceID: "svc_x"}); !errors.Is(err, item.ErrBranchEmpty) {
		t.Errorf("Create with no branch = %v, want ErrBranchEmpty", err)
	}
	// An empty link names nothing, and the writer refuses it the way it
	// refuses every other required field. record's doc.go states what a link
	// is checked for.
	if _, err := cut.Create(ctx, cutActor, item.New{ServiceID: "svc_x", Branch: "item/x"}); !errors.Is(err, item.ErrIntentIDEmpty) {
		t.Errorf("Create naming no intent = %v, want ErrIntentIDEmpty", err)
	}
	if _, err := cut.Create(ctx, cutActor, item.New{IntentID: "in_x", Branch: "item/x"}); !errors.Is(err, item.ErrServiceIDEmpty) {
		t.Errorf("Create naming no service = %v, want ErrServiceIDEmpty", err)
	}
	if _, err := cut.Create(ctx, record.Actor{}, item.New{IntentID: "in_x", ServiceID: "svc_x", Branch: "item/x"}); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("Create with no actor = %v, want record.ErrKindUnknown", err)
	}
}

func TestAdvanceMovesOneStageForward(t *testing.T) {
	ctx, pool, cut, dispatch := newWriters(t)
	it := oneItem(ctx, t, cut)

	advanced, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation)
	if err != nil {
		t.Fatalf("Advance to implementation: %v", err)
	}
	if advanced.Stage != item.StageImplementation {
		t.Errorf("Advance returned stage %s, want implementation", advanced.Stage)
	}
	// The advance rewrites the stage and nothing else.
	advanced.Stage = it.Stage
	if advanced != it {
		t.Errorf("Advance rewrote more than the stage: %+v, cut as %+v", advanced, it)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageMerged); err != nil {
		t.Fatalf("Advance to merged: %v", err)
	}
	read, err := item.Get(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Stage != item.StageMerged {
		t.Errorf("the item is at %s, want merged", read.Stage)
	}

	// Merged is the last stage M1 writes; nothing advances past it.
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance past merged = %v, want ErrNotNextStage", err)
	}
}

func TestAdvanceRefusesSkipsAndBackwardsMoves(t *testing.T) {
	ctx, _, cut, dispatch := newWriters(t)
	it := oneItem(ctx, t, cut)

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageMerged); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance skipping implementation = %v, want ErrNotNextStage", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSpec); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance to the stage it is at = %v, want ErrNotNextStage", err)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		t.Fatalf("Advance to implementation: %v", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageSpec); !errors.Is(err, item.ErrNotNextStage) {
		t.Errorf("Advance backwards = %v, want ErrNotNextStage", err)
	}

	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.Stage("shipped")); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("Advance to a stage outside the three = %v, want ErrStageUnknown", err)
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, "it_missing", item.StageImplementation); !errors.Is(err, item.ErrNotFound) {
		t.Errorf("Advance on a missing item = %v, want ErrNotFound", err)
	}
}

func TestReportAttemptAccumulatesPerStage(t *testing.T) {
	ctx, pool, cut, dispatch := newWriters(t)
	it := oneItem(ctx, t, cut)

	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, 100); err != nil {
		t.Fatalf("ReportAttempt: %v", err)
	}
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, 50); err != nil {
		t.Fatalf("ReportAttempt again: %v", err)
	}
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageImplementation, 10); err != nil {
		t.Fatalf("ReportAttempt at implementation: %v", err)
	}

	stages, err := item.Stages(ctx, pool, it.ID)
	if err != nil {
		t.Fatalf("Stages: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("Stages returned %d rows, want 2", len(stages))
	}
	spec, implementation := stages[0], stages[1]
	if spec.Stage != item.StageSpec || implementation.Stage != item.StageImplementation {
		t.Fatalf("Stages = %+v, want spec then implementation, in first-report order", stages)
	}
	if spec.Attempts != 2 || spec.SpendTokens != 150 {
		t.Errorf("spec totals %d attempts and %d tokens, want 2 and 150", spec.Attempts, spec.SpendTokens)
	}
	if implementation.Attempts != 1 || implementation.SpendTokens != 10 {
		t.Errorf("implementation totals %d attempts and %d tokens, want 1 and 10", implementation.Attempts, implementation.SpendTokens)
	}

	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, -1); !errors.Is(err, item.ErrSpendNegative) {
		t.Errorf("ReportAttempt with negative spend = %v, want ErrSpendNegative", err)
	}
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.Stage("shipped"), 1); !errors.Is(err, item.ErrStageUnknown) {
		t.Errorf("ReportAttempt at a stage outside the three = %v, want ErrStageUnknown", err)
	}
}

// TestTheStoreRefusesAroundTheWriters inserts by raw SQL, so what it
// exercises is the CHECK and unique constraints and not the writers' own
// refusals.
func TestTheStoreRefusesAroundTheWriters(t *testing.T) {
	ctx, pool, cut, dispatch := newWriters(t)
	it := oneItem(ctx, t, cut)
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, 100); err != nil {
		t.Fatalf("ReportAttempt: %v", err)
	}

	insertItem := `insert into item (id, actor_kind, actor_name, at, intent_id, service_id, area_id, branch, stage)
		values ($1, 'component', 'cut', $2, 'in_x', 'svc_x', 'ar_x', $3, $4)`
	for _, refused := range []struct {
		name       string
		branch     string
		stage      string
		constraint string
	}{
		{"an empty branch", "", "spec", "branch_present"},
		{"a stage outside the three", "item/x", "shipped", "stage_known"},
	} {
		_, err := pool.Exec(ctx, insertItem, record.NewID(item.IDPrefix), record.Now(), refused.branch, refused.stage)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// An empty link, at the column representing this package's three: the
	// store refuses it around the writer too.
	if _, err := pool.Exec(ctx, `insert into item (id, actor_kind, actor_name, at, intent_id, service_id, area_id, branch, stage)
		values ($1, 'component', 'cut', $2, '', 'svc_x', 'ar_x', 'item/x', 'spec')`,
		record.NewID(item.IDPrefix), record.Now(),
	); err == nil || !strings.Contains(err.Error(), "intent_id_present") {
		t.Errorf("inserting an item naming no intent = %v, want a violation of intent_id_present", err)
	}

	insertStage := `insert into item_stage (id, actor_kind, actor_name, at, item_id, stage, attempts, spend_tokens)
		values ($1, 'component', 'dispatch', $2, $3, $4, $5, $6)`
	for _, refused := range []struct {
		name       string
		stage      string
		attempts   int
		spend      int64
		constraint string
	}{
		{"a stage outside the three", "shipped", 1, 1, "stage_known"},
		{"negative attempts", "implementation", -1, 1, "attempts_not_negative"},
		{"negative spend", "implementation", 1, -1, "spend_not_negative"},
	} {
		_, err := pool.Exec(ctx, insertStage, record.NewID(item.StageIDPrefix), record.Now(),
			it.ID, refused.stage, refused.attempts, refused.spend)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// A second row for one item and stage is refused by the unique
	// constraint, which is what ReportAttempt's upsert conflicts on.
	_, err := pool.Exec(ctx, insertStage, record.NewID(item.StageIDPrefix), record.Now(),
		it.ID, string(item.StageSpec), 1, 1)
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("a second row for one item and stage = %v, want a unique violation (23505)", err)
	}
}
