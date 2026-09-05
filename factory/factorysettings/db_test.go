// The database tests of this package are in factorysettings_test rather than in
// factorysettings, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as
// "test factorysettings -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package factorysettings_test

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

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *factorysettings.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_fs_" + hex.EncodeToString(suffix[:])

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
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, factorysettings.NewWriter(pool, token)
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

// TestThereIsOneRecordAndEnsureIsIdempotent: the record exists before any
// project does, so whatever reaches it first creates it — and the store, not the
// caller that looked first, is what keeps there being one.
func TestThereIsOneRecordAndEnsureIsIdempotent(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := factorysettings.Get(ctx, pool); !errors.Is(err, factorysettings.ErrNotFound) {
		t.Fatalf("Get before the record exists = %v, want ErrNotFound", err)
	}

	first, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	second, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if first.ID != second.ID {
		t.Errorf("two ensures left two records: %s and %s", first.ID, second.ID)
	}
	if first.AllowedPredicateKinds != nil || first.RolePromptOrSkillThreshold.Present {
		t.Errorf("a freshly created record carries something authored: %+v", first)
	}

	if _, err := pool.Exec(ctx, `insert into `+factorysettings.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, only_row, allowed_predicate_kinds)
		values ('fs_second', '`+factorysettings.FormatVersion+`', 'human', 'person:owner', 'claimed', $1, true, '')`, record.Now()); err == nil {
		t.Error("the store accepted a second factory-wide settings record")
	}
}

// TestTheAttemptLimitIsOneParameterAndNotThree: the limit is per stage, the
// interview's rounds and decomposition's re-decompositions count against the same
// parameter, and a subject no attempt is counted at is refused — the store has no
// foreign key to refuse it, and a value nothing will ever read is worse than an
// error. A stage that is not retried is refused at [factorysettings.OfStage].
func TestTheAttemptLimitIsOneParameterAndNotThree(t *testing.T) {
	ctx, pool, w := newTable(t)

	policy, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	authored, err := factorysettings.AttemptLimit(ctx, pool, policy.ID, factorysettings.SubjectImplementation)
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	if authored.Present {
		t.Errorf("an unauthored limit reads back as %+v", authored)
	}

	for _, authoring := range []struct {
		subject factorysettings.AttemptLimitSubject
		limit   int
	}{
		{factorysettings.SubjectImplementation, 5},
		{factorysettings.SubjectSpec, 2},
		{factorysettings.SubjectInterview, 4},
		{factorysettings.SubjectDecomposition, 6},
	} {
		inTx(t, ctx, pool, func(tx pgx.Tx) error {
			return factorysettings.SetAttemptLimit(ctx, tx, owner, policy.ID, authoring.subject, authoring.limit)
		})
		read, err := factorysettings.AttemptLimit(ctx, pool, policy.ID, authoring.subject)
		if err != nil {
			t.Fatalf("AttemptLimit(%s): %v", authoring.subject, err)
		}
		if read.Number != float64(authoring.limit) {
			t.Errorf("the limit at %s reads back as %v, want %d", authoring.subject, read.Number, authoring.limit)
		}
	}

	// The stage an item is at resolves to the subject where attempts are counted
	// there, and is refused where they are not: nothing is dispatched to queued or
	// merged, so no limit is ever read for them.
	if subject, err := factorysettings.OfStage(item.StageImplementation); err != nil || subject != factorysettings.SubjectImplementation {
		t.Errorf("OfStage(implementation) = %q, %v", subject, err)
	}
	for _, stage := range []item.Stage{item.StageQueued, item.StageMerged, item.StageSuperseded} {
		if _, err := factorysettings.OfStage(stage); !errors.Is(err, factorysettings.ErrSubjectUnknown) {
			t.Errorf("OfStage(%s) = %v, want ErrSubjectUnknown", stage, err)
		}
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := factorysettings.SetAttemptLimit(ctx, tx, owner, policy.ID, "review", 3); !errors.Is(err, factorysettings.ErrSubjectUnknown) {
		t.Errorf("a limit on something no attempt is counted at = %v, want ErrSubjectUnknown", err)
	}
	if err := factorysettings.SetAttemptLimit(ctx, tx, owner, policy.ID, factorysettings.SubjectSpec, 0); !errors.Is(err, factorysettings.ErrLimitNotPositive) {
		t.Errorf("a limit of zero = %v, want ErrLimitNotPositive", err)
	}
}

// TestDDLListsEveryAttemptLimitSubject keeps the CHECK constraint and
// [factorysettings.AttemptLimitSubjects] from disagreeing.
func TestDDLListsEveryAttemptLimitSubject(t *testing.T) {
	const open = "subject in ("
	var statement string
	for _, s := range factorysettings.DDL {
		if strings.Contains(s, open) {
			statement = s
		}
	}
	if statement == "" {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := statement[strings.Index(statement, open)+len(open):]
	listed := strings.Split(rest[:strings.Index(rest, ")")], ",")
	if len(listed) != len(factorysettings.AttemptLimitSubjects) {
		t.Fatalf("the constraint lists %d subjects, AttemptLimitSubjects has %d",
			len(listed), len(factorysettings.AttemptLimitSubjects))
	}
	for n, subject := range factorysettings.AttemptLimitSubjects {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(subject)+"'"; got != want {
			t.Errorf("the constraint lists %s where AttemptLimitSubjects has %s", got, want)
		}
	}
}

// TestReAuthoringALimitIsOneRow: the unique constraint on the record and the
// stage is what an authoring write conflicts on.
func TestReAuthoringALimitIsOneRow(t *testing.T) {
	ctx, pool, w := newTable(t)

	policy, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	for _, limit := range []int{5, 3} {
		inTx(t, ctx, pool, func(tx pgx.Tx) error {
			return factorysettings.SetAttemptLimit(ctx, tx, owner, policy.ID, factorysettings.SubjectImplementation, limit)
		})
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from `+factorysettings.LimitTable).Scan(&rows); err != nil {
		t.Fatalf("counting the limit rows: %v", err)
	}
	if rows != 1 {
		t.Errorf("two authorings of one stage left %d rows, want 1", rows)
	}
	authored, err := factorysettings.AttemptLimit(ctx, pool, policy.ID, factorysettings.SubjectImplementation)
	if err != nil {
		t.Fatalf("AttemptLimit: %v", err)
	}
	if authored.Number != 3 {
		t.Errorf("the limit reads back as %v, want the second authoring's 3", authored.Number)
	}
}

// TestTheCatalogAndTheRolePromptThreshold: both are fields of this record and neither
// is read by anything at this milestone, which is what makes storing them the
// whole of what can be demonstrated about them.
func TestTheCatalogAndTheRolePromptThreshold(t *testing.T) {
	ctx, pool, w := newTable(t)

	policy, err := w.Ensure(ctx, owner)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	allowed := []string{"read", "populated", "called"}
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetAllowedPredicateKinds(ctx, tx, policy.ID, allowed)
	})
	inTx(t, ctx, pool, func(tx pgx.Tx) error {
		return factorysettings.SetRolePromptOrSkillThreshold(ctx, tx, policy.ID, 0.15)
	})

	read, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !slices.Equal(read.AllowedPredicateKinds, allowed) {
		t.Errorf("the allowed reads back as %v, want %v", read.AllowedPredicateKinds, allowed)
	}
	if !read.RolePromptOrSkillThreshold.Present || read.RolePromptOrSkillThreshold.Number != 0.15 {
		t.Errorf("the role-prompt-or-skill threshold reads back as %+v, want 0.15 present", read.RolePromptOrSkillThreshold)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := factorysettings.SetAllowedPredicateKinds(ctx, tx, policy.ID, nil); !errors.Is(err, factorysettings.ErrAllowedPredicateKindsEmpty) {
		t.Errorf("an empty authored allowed = %v, want ErrAllowedPredicateKindsEmpty", err)
	}
	if err := factorysettings.SetRolePromptOrSkillThreshold(ctx, tx, policy.ID, 2); !errors.Is(err, factorysettings.ErrThresholdOutOfRange) {
		t.Errorf("a threshold of 2 = %v, want ErrThresholdOutOfRange", err)
	}
	if err := factorysettings.SetRolePromptOrSkillThreshold(ctx, tx, "fs_nothing", 0.2); !errors.Is(err, factorysettings.ErrNotFound) {
		t.Errorf("authoring on a record that does not exist = %v, want ErrNotFound", err)
	}
}

// inTx runs one authoring write in its own transaction, which is how its one
// real caller runs it — inside the transaction that appends the policy version.
func inTx(t *testing.T, ctx context.Context, pool *pgxpool.Pool, write func(pgx.Tx) error) {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := write(tx); err != nil {
		t.Fatalf("the authoring write: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
}

// begin is a transaction a refusal is attempted in and never committed, rolled
// back when the test ends.
func begin(t *testing.T, ctx context.Context, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	return tx
}
