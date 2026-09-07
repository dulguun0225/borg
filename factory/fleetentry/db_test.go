// The database tests of this package are in fleetentry_test rather than in
// fleetentry, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as
// "test fleetentry -> postgres lease".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package fleetentry_test

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

	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

var owner = record.Actor{Kind: record.KindHuman, Key: "owner", Basis: record.BasisClaimed}
var component = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m8_fle_" + hex.EncodeToString(suffix[:])

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

func aNew() fleetentry.New {
	return fleetentry.New{
		ModelVersion:                    "anthropic/claude-opus-4.8",
		Effort:                          "high",
		Role:                            "implementer",
		Scope:                           fleetentry.Scope{ProjectID: "prj_1", ServiceID: "svc_1", AreaID: "area_1"},
		CredentialName:                  "anthropic-primary",
		ProcessingLocation:              "anthropic/us-east",
		MaterialClasses:                 []string{fleetentry.ClassRepository, fleetentry.ClassConstraints},
		ReadsAtOnce:                     200000,
		DispatchesBetweenEvaluationRuns: 50,
	}
}

// TestWriteAndReadBackAllNineFields: every field the design gives a fleet
// entry reads back the way it was written.
func TestWriteAndReadBackAllNineFields(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	n := aNew()
	set, err := w.Write(ctx, owner, n)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !set.InForce() {
		t.Fatalf("a freshly written entry reads back withdrawn: %+v", set)
	}

	got, err := fleetentry.Get(ctx, pool, set.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ModelVersion != n.ModelVersion {
		t.Errorf("ModelVersion = %q, want %q", got.ModelVersion, n.ModelVersion)
	}
	if got.Effort != n.Effort {
		t.Errorf("Effort = %q, want %q", got.Effort, n.Effort)
	}
	if got.Role != n.Role {
		t.Errorf("Role = %q, want %q", got.Role, n.Role)
	}
	if got.Scope != n.Scope {
		t.Errorf("Scope = %+v, want %+v", got.Scope, n.Scope)
	}
	if got.CredentialName != n.CredentialName {
		t.Errorf("CredentialName = %q, want %q", got.CredentialName, n.CredentialName)
	}
	if got.ProcessingLocation != n.ProcessingLocation {
		t.Errorf("ProcessingLocation = %q, want %q", got.ProcessingLocation, n.ProcessingLocation)
	}
	if len(got.MaterialClasses) != len(n.MaterialClasses) {
		t.Fatalf("MaterialClasses = %v, want %v", got.MaterialClasses, n.MaterialClasses)
	}
	for i, c := range n.MaterialClasses {
		if got.MaterialClasses[i] != c {
			t.Errorf("MaterialClasses[%d] = %q, want %q", i, got.MaterialClasses[i], c)
		}
	}
	if got.ReadsAtOnce != n.ReadsAtOnce {
		t.Errorf("ReadsAtOnce = %d, want %d", got.ReadsAtOnce, n.ReadsAtOnce)
	}
	if got.DispatchesBetweenEvaluationRuns != n.DispatchesBetweenEvaluationRuns {
		t.Errorf("DispatchesBetweenEvaluationRuns = %d, want %d",
			got.DispatchesBetweenEvaluationRuns, n.DispatchesBetweenEvaluationRuns)
	}
}

// TestAComponentActorIsRefused: the one writer is an owner at Factory, and no
// component in the pipeline writes back to this record.
func TestAComponentActorIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	if _, err := w.Write(ctx, component, aNew()); !errors.Is(err, fleetentry.ErrNotAnOwner) {
		t.Errorf("Write with a component actor = %v, want ErrNotAnOwner", err)
	}
}

// TestAnUnknownMaterialClassIsRefused: the writer refuses a class not among
// the seven this package defines.
func TestAnUnknownMaterialClassIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	n := aNew()
	n.MaterialClasses = []string{"a class nobody defined"}
	if _, err := w.Write(ctx, owner, n); !errors.Is(err, fleetentry.ErrMaterialClassUnknown) {
		t.Errorf("Write with an unknown material class = %v, want ErrMaterialClassUnknown", err)
	}
}

// TestADuplicateMaterialClassIsRefused: naming the same class twice is
// refused rather than stored.
func TestADuplicateMaterialClassIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	n := aNew()
	n.MaterialClasses = []string{fleetentry.ClassRepository, fleetentry.ClassRepository}
	if _, err := w.Write(ctx, owner, n); !errors.Is(err, fleetentry.ErrMaterialClassDuplicate) {
		t.Errorf("Write with a duplicate material class = %v, want ErrMaterialClassDuplicate", err)
	}
}

// TestReadsAtOnceOfZeroIsRefused: the CHECK and the writer's own guard agree
// that zero is not a bound either field's design gives a meaning to.
func TestReadsAtOnceOfZeroIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	n := aNew()
	n.ReadsAtOnce = 0
	if _, err := w.Write(ctx, owner, n); !errors.Is(err, fleetentry.ErrReadsAtOnceNotPositive) {
		t.Errorf("Write with ReadsAtOnce = 0 = %v, want ErrReadsAtOnceNotPositive", err)
	}
}

// TestWithdrawKeepsTheRowAndRemovesItFromInForceAndCoveredRoles: withdrawing
// an entry does not delete it, but it drops out of both readers that answer
// what is in force.
func TestWithdrawKeepsTheRowAndRemovesItFromInForceAndCoveredRoles(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	set, err := w.Write(ctx, owner, aNew())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	withdrawn, err := w.Withdraw(ctx, owner, set.ID)
	if err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if withdrawn.InForce() {
		t.Fatalf("a withdrawn entry reads back in force: %+v", withdrawn)
	}
	if withdrawn.WithdrawnAt == "" {
		t.Fatalf("a withdrawn entry carries no withdrawn_at: %+v", withdrawn)
	}

	// The row is kept: Get still finds it.
	got, err := fleetentry.Get(ctx, pool, set.ID)
	if err != nil {
		t.Fatalf("Get after withdrawal: %v", err)
	}
	if got.InForce() {
		t.Fatalf("Get reads back an entry as in force after withdrawal: %+v", got)
	}

	inForce, err := fleetentry.InForce(ctx, pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if len(inForce) != 0 {
		t.Fatalf("InForce = %v, want none after the only entry was withdrawn", inForce)
	}

	covered, err := fleetentry.CoveredRoles(ctx, pool)
	if err != nil {
		t.Fatalf("CoveredRoles: %v", err)
	}
	if covered[set.Role] {
		t.Fatalf("CoveredRoles still names %q after its only entry was withdrawn", set.Role)
	}
}

// TestWithdrawingTwiceIsRefused: a second withdrawal of the same entry is
// refused rather than silently accepted.
func TestWithdrawingTwiceIsRefused(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	set, err := w.Write(ctx, owner, aNew())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := w.Withdraw(ctx, owner, set.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}
	if _, err := w.Withdraw(ctx, owner, set.ID); !errors.Is(err, fleetentry.ErrAlreadyWithdrawn) {
		t.Errorf("withdrawing twice = %v, want ErrAlreadyWithdrawn", err)
	}
	if _, err := w.Withdraw(ctx, owner, "fle_nothing"); !errors.Is(err, fleetentry.ErrNotFound) {
		t.Errorf("withdrawing an entry that does not exist = %v, want ErrNotFound", err)
	}
}

// TestCoveredRolesOverTwoRolesWithOneWithdrawn: CoveredRoles names every role
// with at least one entry standing, and drops a role once its only entry is
// withdrawn.
func TestCoveredRolesOverTwoRolesWithOneWithdrawn(t *testing.T) {
	ctx, pool, token := newTable(t)
	w := fleetentry.NewWriter(pool, token)

	implementer := aNew()
	implementer.Role = "implementer"
	reviewer := aNew()
	reviewer.Role = "reviewer"

	first, err := w.Write(ctx, owner, implementer)
	if err != nil {
		t.Fatalf("Write implementer: %v", err)
	}
	if _, err := w.Write(ctx, owner, reviewer); err != nil {
		t.Fatalf("Write reviewer: %v", err)
	}

	covered, err := fleetentry.CoveredRoles(ctx, pool)
	if err != nil {
		t.Fatalf("CoveredRoles: %v", err)
	}
	if !covered["implementer"] || !covered["reviewer"] {
		t.Fatalf("CoveredRoles = %v, want both implementer and reviewer", covered)
	}

	if _, err := w.Withdraw(ctx, owner, first.ID); err != nil {
		t.Fatalf("Withdraw: %v", err)
	}

	covered, err = fleetentry.CoveredRoles(ctx, pool)
	if err != nil {
		t.Fatalf("CoveredRoles: %v", err)
	}
	if covered["implementer"] {
		t.Fatalf("CoveredRoles still names implementer after its only entry was withdrawn: %v", covered)
	}
	if !covered["reviewer"] {
		t.Fatalf("CoveredRoles no longer names reviewer: %v", covered)
	}

	inForceForRole, err := fleetentry.InForceForRole(ctx, pool, "reviewer")
	if err != nil {
		t.Fatalf("InForceForRole: %v", err)
	}
	if len(inForceForRole) != 1 {
		t.Fatalf("InForceForRole(reviewer) = %v, want one entry", inForceForRole)
	}
}
