// The database tests of this package are in halt_test rather than in halt,
// because they open the pool through package postgres, which imports this one
// to apply its DDL. deps.txt records the edge as "test halt -> postgres lease".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package halt_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_hlt_" + hex.EncodeToString(suffix[:])

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
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, token
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

// TestAHaltStandsUntilAnApprovedWithdrawalNamesIt: setting one is immediate,
// and it stands until the second write approves a withdrawal of it — never on
// the withdrawal's own write.
func TestAHaltStandsUntilAnApprovedWithdrawalNamesIt(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := halt.NewWriter(pool, token)

	set, err := w.Insert(ctx, owner, "an owner who has lost confidence in the factory itself")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	if set.Reason == "" {
		t.Fatalf("the halt reads back with no reason: %+v", set)
	}

	standing, err := halt.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 || standing[0].ID != set.ID {
		t.Fatalf("the halts in force are %v, want the one just set", standing)
	}

	pending, err := w.InsertWithdrawal(ctx, owner, set.ID)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	if pending.Approved {
		t.Fatalf("a fresh withdrawal reads back approved: %+v", pending)
	}

	// A withdrawal nobody approved leaves the halt standing.
	standing, err = halt.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 {
		t.Errorf("a halt with a pending withdrawal is not standing: %v", standing)
	}

	if err := w.ApproveWithdrawal(ctx, pending.ID); err != nil {
		t.Fatalf("ApproveWithdrawal: %v", err)
	}

	standing, err = halt.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 0 {
		t.Errorf("a halt with an approved withdrawal is still standing: %v", standing)
	}

	if err := w.ApproveWithdrawal(ctx, pending.ID); !errors.Is(err, halt.ErrAlreadyApproved) {
		t.Errorf("approving an already-approved withdrawal = %v, want ErrAlreadyApproved", err)
	}
	if err := w.ApproveWithdrawal(ctx, "hltw_nothing"); !errors.Is(err, halt.ErrWithdrawalNotFound) {
		t.Errorf("approving a withdrawal that does not exist = %v, want ErrWithdrawalNotFound", err)
	}
}

// TestAHaltNamesAReasonAndAnActor: the two fields the design gives it, and the
// refusals around them.
func TestAHaltNamesAReasonAndAnActor(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := halt.NewWriter(pool, token)

	if _, err := w.Insert(ctx, owner, ""); !errors.Is(err, halt.ErrReasonEmpty) {
		t.Errorf("a halt with no reason = %v, want ErrReasonEmpty", err)
	}
	if _, err := w.Insert(ctx, record.Actor{}, "a reason"); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("a halt with no actor = %v, want ErrKindUnknown", err)
	}
	if _, err := w.InsertWithdrawal(ctx, owner, ""); !errors.Is(err, halt.ErrHaltIDEmpty) {
		t.Errorf("a withdrawal naming no halt = %v, want ErrHaltIDEmpty", err)
	}
}

// TestMultipleHaltsEachStandUntilTheirOwnWithdrawal: the factory may be halted
// for more than one reason at once, and withdrawing one leaves the other
// standing.
func TestMultipleHaltsEachStandUntilTheirOwnWithdrawal(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := halt.NewWriter(pool, token)

	first, err := w.Insert(ctx, owner, "the first reason")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}
	second, err := w.Insert(ctx, owner, "the second reason")
	if err != nil {
		t.Fatalf("Insert: %v", err)
	}

	pending, err := w.InsertWithdrawal(ctx, owner, first.ID)
	if err != nil {
		t.Fatalf("InsertWithdrawal: %v", err)
	}
	if err := w.ApproveWithdrawal(ctx, pending.ID); err != nil {
		t.Fatalf("ApproveWithdrawal: %v", err)
	}

	standing, err := halt.Standing(ctx, pool)
	if err != nil {
		t.Fatalf("Standing: %v", err)
	}
	if len(standing) != 1 || standing[0].ID != second.ID {
		t.Fatalf("the halts standing are %v, want only the second", standing)
	}
}
