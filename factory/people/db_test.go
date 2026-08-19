// The database tests of this package are in people_test rather than in
// people, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as "test people ->
// postgres".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package people_test

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

	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// owner is the one writer of declarations, the way doc.go names it: a human,
// and never a component.
var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *people.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m3_ppl_" + hex.EncodeToString(suffix[:])

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
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	return ctx, pool, people.NewWriter(pool)
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

func TestDeclaringADutyAndAnObligationReadBackAsHolding(t *testing.T) {
	ctx, pool, w := newTable(t)

	duty, err := w.Declare(ctx, owner, "alice", people.OfDuty(3))
	if err != nil {
		t.Fatalf("Declare of a duty: %v", err)
	}
	if duty.Human != "alice" || duty.Duty != 3 || duty.Obligation != "" {
		t.Errorf("Declare of a duty = %+v, which does not name what it was declared with", duty)
	}
	if !duty.Holds() {
		t.Error("a freshly declared duty reads as not holding")
	}

	obligation, err := w.Declare(ctx, owner, "bob", people.OfObligation(people.ObligationReconciler))
	if err != nil {
		t.Fatalf("Declare of an obligation: %v", err)
	}
	if obligation.Human != "bob" || obligation.Duty != 0 || obligation.Obligation != people.ObligationReconciler {
		t.Errorf("Declare of an obligation = %+v, which does not name what it was declared with", obligation)
	}
	if !obligation.Holds() {
		t.Error("a freshly declared obligation reads as not holding")
	}

	readDuty, err := people.Get(ctx, pool, duty.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if readDuty != duty {
		t.Errorf("Get = %+v, want %+v", readDuty, duty)
	}
	readObligation, err := people.Get(ctx, pool, obligation.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if readObligation != obligation {
		t.Errorf("Get = %+v, want %+v", readObligation, obligation)
	}
}

func TestAComponentActorIsRefused(t *testing.T) {
	ctx, pool, w := newTable(t)
	component := record.Actor{Kind: record.KindComponent, Name: "dispatch"}

	if _, err := w.Declare(ctx, component, "alice", people.OfDuty(1)); !errors.Is(err, people.ErrNotAnOwner) {
		t.Errorf("Declare by a component = %v, want ErrNotAnOwner", err)
	}

	// Around the writer, the CHECK constraint refuses the same thing.
	_, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
		values ($1, 'component', 'dispatch', $2, 'alice', 1, '', '')`,
		record.NewID(people.IDPrefix), record.Now())
	if err == nil {
		t.Error("the store accepted a declaration written by a component")
	}
}

// TestAHoldingNamingBothOrNeitherOrOutsideTheirRangeIsUnknown covers every way
// a Holding can fail to name exactly one valid thing.
func TestAHoldingNamingBothOrNeitherOrOutsideTheirRangeIsUnknown(t *testing.T) {
	ctx, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "alice", people.Holding{Duty: 1, Obligation: people.ObligationHosting}); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare naming both a duty and an obligation = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "alice", people.Holding{}); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare naming neither = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "alice", people.OfDuty(0)); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare of duty 0 = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "alice", people.OfDuty(13)); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare of duty 13 = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "alice", people.OfObligation("catering")); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare of an unknown obligation = %v, want ErrHoldingUnknown", err)
	}
}

// TestDeclaringTheSamePairTwiceIsOneRowAndRedeclaringClearsAWithdrawal is
// [Writer.Declare]'s idempotence: the insert conflicts on the human and the
// holding, so a second declaration of one pair is the first row again, and
// re-declaring a holding somebody gave up is how they take it back.
func TestDeclaringTheSamePairTwiceIsOneRowAndRedeclaringClearsAWithdrawal(t *testing.T) {
	ctx, pool, w := newTable(t)

	first, err := w.Declare(ctx, owner, "alice", people.OfDuty(5))
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	second, err := w.Declare(ctx, owner, "alice", people.OfDuty(5))
	if err != nil {
		t.Fatalf("Declare again: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("declaring the same pair twice made a new row %s, want %s again", second.ID, first.ID)
	}

	all, err := people.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("All = %d rows, want 1 for one pair declared twice", len(all))
	}

	withdrawn, err := w.Withdraw(ctx, first.ID)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if withdrawn.Holds() {
		t.Error("Withdraw left the holding standing")
	}

	redeclared, err := w.Declare(ctx, owner, "alice", people.OfDuty(5))
	if err != nil {
		t.Fatalf("Declare after Withdraw: %v", err)
	}
	if redeclared.ID != first.ID {
		t.Errorf("re-declaring made a new row %s, want %s again", redeclared.ID, first.ID)
	}
	if !redeclared.Holds() {
		t.Error("re-declaring did not clear the withdrawal")
	}
}

// TestHoldersReturnsEveryHolderInDeclarationOrderAndNoneForAnUnheldDuty is what
// the notifier routes on: every human who holds a duty, in the order they
// declared it, and no holders is a routing answer and not an error.
func TestHoldersReturnsEveryHolderInDeclarationOrderAndNoneForAnUnheldDuty(t *testing.T) {
	ctx, pool, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "alice", people.OfDuty(7)); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Declare(ctx, owner, "bob", people.OfDuty(7)); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	holders, err := people.Holders(ctx, pool, people.OfDuty(7))
	if err != nil {
		t.Fatalf("Holders: %v", err)
	}
	if len(holders) != 2 || holders[0] != "alice" || holders[1] != "bob" {
		t.Errorf("Holders(duty 7) = %v, want [alice bob] in declaration order", holders)
	}

	none, err := people.Holders(ctx, pool, people.OfDuty(8))
	if err != nil {
		t.Fatalf("Holders on a duty nobody holds: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("Holders(duty 8) = %v, want none and no error", none)
	}
}

// TestWithdrawKeepsTheRowAndExcludesItFromHolders is why the row is kept
// rather than deleted: a page delivered to a holder who has since stopped
// holding is still readable against the row that routed it, and Holders reads
// only what still stands.
func TestWithdrawKeepsTheRowAndExcludesItFromHolders(t *testing.T) {
	ctx, pool, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "alice", people.OfDuty(9))
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Withdraw(ctx, declared.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	holders, err := people.Holders(ctx, pool, people.OfDuty(9))
	if err != nil {
		t.Fatalf("Holders: %v", err)
	}
	if len(holders) != 0 {
		t.Errorf("Holders after Withdraw = %v, want none", holders)
	}

	all, err := people.All(ctx, pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 1 || all[0].ID != declared.ID {
		t.Errorf("All after Withdraw = %+v, want the withdrawn row kept", all)
	}
}

// TestDDLHoldsEveryDuty keeps the CHECK constraint and people.Duties from
// disagreeing, the way deploy/schema_test.go's
// TestDDLListsEveryStrategyAndStatus does for strategies and statuses: every
// duty in people.Duties inserts cleanly around the writer, and duty 0 with no
// obligation and duty 13 are both refused.
func TestDDLHoldsEveryDuty(t *testing.T) {
	ctx, pool, _ := newTable(t)

	for _, duty := range people.Duties {
		_, err := pool.Exec(ctx, `insert into `+people.Table+`
			(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
			values ($1, 'human', 'owner', $2, $3, $4, '', '')`,
			record.NewID(people.IDPrefix), record.Now(), "human_for_duty", int(duty))
		if err != nil {
			t.Errorf("inserting duty %d, one of people.Duties, was refused: %v", duty, err)
		}
	}

	if _, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
		values ($1, 'human', 'owner', $2, 'nobody', 0, '', '')`,
		record.NewID(people.IDPrefix), record.Now()); err == nil {
		t.Error("the store accepted duty 0 with no obligation")
	}
	if _, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
		values ($1, 'human', 'owner', $2, 'nobody', 13, '', '')`,
		record.NewID(people.IDPrefix), record.Now()); err == nil {
		t.Error("the store accepted duty 13, which is outside the twelve")
	}
}

// TestDDLListsEveryObligation is TestDDLHoldsEveryDuty's mirror for
// people.Obligations: every obligation inserts cleanly with duty 0, and one
// outside the three does not.
func TestDDLListsEveryObligation(t *testing.T) {
	ctx, pool, _ := newTable(t)

	for _, obligation := range people.Obligations {
		_, err := pool.Exec(ctx, `insert into `+people.Table+`
			(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
			values ($1, 'human', 'owner', $2, $3, 0, $4, '')`,
			record.NewID(people.IDPrefix), record.Now(), "human_for_obligation", string(obligation))
		if err != nil {
			t.Errorf("inserting obligation %q, one of people.Obligations, was refused: %v", obligation, err)
		}
	}

	if _, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
		values ($1, 'human', 'owner', $2, 'nobody', 0, 'catering', '')`,
		record.NewID(people.IDPrefix), record.Now()); err == nil {
		t.Error("the store accepted an obligation outside people.Obligations")
	}
}
