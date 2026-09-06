// The database tests of this package are in people_test rather than in
// people, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as "test people ->
// postgres". db_test.go is the holding table and the re-derivation and holds
// the fixtures; mapping_test.go is the key-to-name mapping and its deletion.
// The two are one external test package split by subject, each file held to
// 500 lines.
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

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// owner is the one writer of declarations, the way doc.go names it: a human,
// and never a component.
var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

// ownerReading is the same owner as a principal, which is what a read of the
// log takes.
var ownerReading = principal.OfHuman("person:owner", record.BasisClaimed)

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token, *people.Writer) {
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
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, token, people.NewWriter(pool, token, policy.NewFactory(pool, token))
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
	ctx, pool, _, w := newTable(t)

	duty, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(3))
	if err != nil {
		t.Fatalf("Declare of a duty: %v", err)
	}
	if duty.Key != "hk_alice" || duty.Duty != 3 || duty.Obligation != "" {
		t.Errorf("Declare of a duty = %+v, which does not name what it was declared with", duty)
	}
	if !duty.Holds() {
		t.Error("a freshly declared duty reads as not holding")
	}

	obligation, err := w.Declare(ctx, owner, "hk_bob", people.OfObligation(people.ObligationDriftDetector))
	if err != nil {
		t.Fatalf("Declare of an obligation: %v", err)
	}
	if obligation.Key != "hk_bob" || obligation.Duty != 0 || obligation.Obligation != people.ObligationDriftDetector {
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
}

func TestAComponentActorIsRefused(t *testing.T) {
	ctx, pool, _, w := newTable(t)
	component := record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}

	if _, err := w.Declare(ctx, component, "hk_alice", people.OfDuty(1)); !errors.Is(err, people.ErrNotAnOwner) {
		t.Errorf("Declare by a component = %v, want ErrNotAnOwner", err)
	}

	// Around the writer, the CHECK constraint refuses the same thing.
	_, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 withdrawn_at)
		values ($1, $2, 'component', 'dispatch', 'claimed', $3, 'hk_alice', 1, '', '')`,
		record.NewID(people.HoldingIDPrefix), people.FormatVersion, record.Now())
	if err == nil {
		t.Error("the store accepted a declaration written by a component")
	}
}

// TestAHoldingNamingBothOrNeitherOrOutsideTheirRangeIsUnknown covers every way
// a Holding can fail to name exactly one valid thing.
func TestAHoldingNamingBothOrNeitherOrOutsideTheirRangeIsUnknown(t *testing.T) {
	ctx, _, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "hk_alice", people.Holding{Duty: 1, Obligation: people.ObligationHosting}); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare naming both a duty and an obligation = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "hk_alice", people.Holding{}); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare naming neither = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(0)); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare of duty 0 = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(13)); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare of duty 13 = %v, want ErrHoldingUnknown", err)
	}
	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfObligation("catering")); !errors.Is(err, people.ErrHoldingUnknown) {
		t.Errorf("Declare of an unknown obligation = %v, want ErrHoldingUnknown", err)
	}
}

// TestDeclareRefusesAnEmptyKey is the identity's own requirement: a write
// names the per-person key it is about.
func TestDeclareRefusesAnEmptyKey(t *testing.T) {
	ctx, _, _, w := newTable(t)
	if _, err := w.Declare(ctx, owner, "", people.OfDuty(1)); !errors.Is(err, people.ErrKeyEmpty) {
		t.Errorf("Declare with no key = %v, want ErrKeyEmpty", err)
	}
}

// TestDeclaringTheSamePairTwiceIsOneRowAndRedeclaringClearsAWithdrawal is
// [Writer.Declare]'s idempotence: the insert conflicts on the key and the
// holding, so a second declaration of one pair is the first row again, and
// re-declaring a holding somebody gave up is how they take it back.
func TestDeclaringTheSamePairTwiceIsOneRowAndRedeclaringClearsAWithdrawal(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	first, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(5))
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	second, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(5))
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

	withdrawn, err := w.Withdraw(ctx, owner, first.ID)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if withdrawn.Holds() {
		t.Error("Withdraw left the holding standing")
	}

	redeclared, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(5))
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

// TestHoldersReturnsEveryHolderKeyInDeclarationOrderAndNoneForAnUnheldDuty is
// what the notifier routes on: every key holding a duty, in the order it
// declared it, and no holders is a routing answer and not an error.
func TestHoldersReturnsEveryHolderKeyInDeclarationOrderAndNoneForAnUnheldDuty(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(7)); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Declare(ctx, owner, "hk_bob", people.OfDuty(7)); err != nil {
		t.Fatalf("Declare: %v", err)
	}

	holders, err := people.Holders(ctx, pool, people.OfDuty(7))
	if err != nil {
		t.Fatalf("Holders: %v", err)
	}
	if len(holders) != 2 || holders[0] != "hk_alice" || holders[1] != "hk_bob" {
		t.Errorf("Holders(duty 7) = %v, want [hk_alice hk_bob] in declaration order", holders)
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
	ctx, pool, _, w := newTable(t)

	declared, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(9))
	if err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Withdraw(ctx, owner, declared.ID); err != nil {
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
// disagreeing: every duty in people.Duties inserts cleanly around the writer,
// and duty 0 with no obligation and duty 13 are both refused.
func TestDDLHoldsEveryDuty(t *testing.T) {
	ctx, pool, _, _ := newTable(t)

	for _, duty := range people.Duties {
		_, err := pool.Exec(ctx, `insert into `+people.Table+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
			 withdrawn_at)
			values ($1, $2, 'human', 'person:owner', 'claimed', $3, $4, $5, '', '')`,
			record.NewID(people.HoldingIDPrefix), people.FormatVersion, record.Now(), "hk_for_duty", int(duty))
		if err != nil {
			t.Errorf("inserting duty %d, one of people.Duties, was refused: %v", duty, err)
		}
	}

	if _, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 withdrawn_at)
		values ($1, $2, 'human', 'person:owner', 'claimed', $3, 'hk_nobody', 0, '', '')`,
		record.NewID(people.HoldingIDPrefix), people.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted duty 0 with no obligation")
	}
	if _, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 withdrawn_at)
		values ($1, $2, 'human', 'person:owner', 'claimed', $3, 'hk_nobody', 13, '', '')`,
		record.NewID(people.HoldingIDPrefix), people.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted duty 13, which is outside the twelve")
	}
}

// TestDDLListsEveryObligation is TestDDLHoldsEveryDuty's mirror for
// people.Obligations: every obligation inserts cleanly with duty 0, and one
// outside the three does not.
func TestDDLListsEveryObligation(t *testing.T) {
	ctx, pool, _, _ := newTable(t)

	for _, obligation := range people.Obligations {
		_, err := pool.Exec(ctx, `insert into `+people.Table+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
			 withdrawn_at)
			values ($1, $2, 'human', 'person:owner', 'claimed', $3, $4, 0, $5, '')`,
			record.NewID(people.HoldingIDPrefix), people.FormatVersion, record.Now(), "hk_for_obligation", string(obligation))
		if err != nil {
			t.Errorf("inserting obligation %q, one of people.Obligations, was refused: %v", obligation, err)
		}
	}

	if _, err := pool.Exec(ctx, `insert into `+people.Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 withdrawn_at)
		values ($1, $2, 'human', 'person:owner', 'claimed', $3, 'hk_nobody', 0, 'catering', '')`,
		record.NewID(people.HoldingIDPrefix), people.FormatVersion, record.Now()); err == nil {
		t.Error("the store accepted an obligation outside people.Obligations")
	}
}

// TestDeclareAppendsAPolicyVersionBeforeTheDeclaration is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md's rule: a write to the
// declaration appends a policy version with People as caller, naming the
// duty by key, before the table itself changes.
func TestDeclareAppendsAPolicyVersionBeforeTheDeclaration(t *testing.T) {
	ctx, pool, token, w := newTable(t)
	reader := policy.NewReader(pool, token, score.Version{})

	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(4)); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	newest, err := reader.Newest(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	if newest.Caller != policy.CallerPeople {
		t.Errorf("the version's caller = %q, want %q", newest.Caller, policy.CallerPeople)
	}
	found := false
	for _, p := range newest.Declaration.People {
		if p.Key == "hk_alice" {
			for _, d := range p.Duties {
				if d == 4 {
					found = true
				}
			}
		}
	}
	if !found {
		t.Errorf("the version's declaration = %+v, want hk_alice holding duty 4", newest.Declaration)
	}
}

// TestRederiveWritesBackADutyTheVersionNamesThatTheTableLost is the
// factory's start finishing a write a stop interrupted: the table is put
// directly in that state, since nothing in the factory can write one
// without the other, and Rederive is what a version with no matching row
// repairs.
func TestRederiveWritesBackADutyTheVersionNamesThatTheTableLost(t *testing.T) {
	ctx, pool, token, w := newTable(t)
	reader := policy.NewReader(pool, token, score.Version{})

	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(6)); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	declared, err := people.ByHolding(ctx, pool, "hk_alice", people.OfDuty(6))
	if err != nil {
		t.Fatalf("ByHolding: %v", err)
	}
	if _, err := pool.Exec(ctx, `delete from `+people.Table+` where id = $1`, declared.ID); err != nil {
		t.Fatalf("deleting the row directly: %v", err)
	}
	if holders, err := people.Holders(ctx, pool, people.OfDuty(6)); err != nil || len(holders) != 0 {
		t.Fatalf("the row survived the direct delete: holders %v, err %v", holders, err)
	}

	rewritten, err := people.Rederive(ctx, pool, token, reader, ownerReading)
	if err != nil {
		t.Fatalf("Rederive: %v", err)
	}
	if len(rewritten) != 1 || rewritten[0] != "hk_alice" {
		t.Errorf("Rederive rewrote %v, want [hk_alice]", rewritten)
	}
	holders, err := people.Holders(ctx, pool, people.OfDuty(6))
	if err != nil {
		t.Fatalf("Holders: %v", err)
	}
	if len(holders) != 1 || holders[0] != "hk_alice" {
		t.Errorf("Holders after Rederive = %v, want [hk_alice]", holders)
	}

	// A re-derivation that finds the table already agreeing writes nothing.
	again, err := people.Rederive(ctx, pool, token, reader, ownerReading)
	if err != nil {
		t.Fatalf("Rederive again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a re-derivation over a table that agrees rewrote %v", again)
	}
}
