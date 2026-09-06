package policy_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAGateWithNoRecordsToReadFallsBackToWhatTheScoreSupplies: a firing whose
// subjects name no environment reads the supplied threshold rather than failing,
// which is what keeps a factory with nothing authored running.
func TestAGateWithNoRecordsToReadFallsBackToWhatTheScoreSupplies(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorGateThreshold(ctx, owner, in.prod.ID, "merge_to_master", 0.5); err != nil {
		t.Fatalf("AuthorGateThreshold: %v", err)
	}
	applied, err := in.reader.AtGate(ctx, ownerReading, policy.Subjects{GateRow: "merge_to_master"})
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	supplied := startingValue(t, gatepolicy.RiskThreshold)
	if applied.ThresholdFrom != policy.FromSupplied || applied.Threshold != supplied {
		t.Errorf("a firing naming no environment reads %v from %s, want the supplied %v",
			applied.Threshold, applied.ThresholdFrom, supplied)
	}
}

// TestAGateBeforeTheFactoryIsInstalledHasNoVersionToName: an open event requires
// a policy version, so a factory nobody installed cannot fire a gate — which is
// better than a firing naming an empty version.
func TestAGateBeforeTheFactoryIsInstalledHasNoVersionToName(t *testing.T) {
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_policy_bare_" + hex.EncodeToString(suffix[:])
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
	if _, err := policy.NewReader(pool, token, score.Version{}).AtGate(ctx, ownerReading,
		policy.Subjects{GateRow: "merge_to_master"}); !errors.Is(err, policy.ErrNoVersion) {
		t.Errorf("AtGate on a factory nobody installed = %v, want ErrNoVersion", err)
	}
}
