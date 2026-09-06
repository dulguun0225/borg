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
	"github.com/dulguun0225/borg/factory/record"
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

// TestTheRowWithNoEnvironmentReadsTheThresholdOnTheSettingsRecord: the risk
// threshold is authorable at two scopes and a firing reads whichever its row
// names. The row that decides a version of what an agent is told belongs to no
// item, so it has no project and no production environment: its threshold is a
// field of the factory-wide settings record, and a firing there reads the
// number its owner authored and decides under the score version confirmed at
// that scope — not under one that redefined the number since.
func TestTheRowWithNoEnvironmentReadsTheThresholdOnTheSettingsRecord(t *testing.T) {
	ctx, in := newFactory(t)

	first, found, err := score.Newest(ctx, in.pool, in.token)
	if err != nil || !found {
		t.Fatalf("the score version in force: %v", err)
	}
	if _, err := in.factory.AuthorRolePromptOrSkillThreshold(ctx, owner, 0.15); err != nil {
		t.Fatalf("AuthorRolePromptOrSkillThreshold: %v", err)
	}

	subjects := policy.Subjects{GateRow: policy.RolePromptOrSkillRow}
	applied, err := in.reader.AtGate(ctx, ownerReading, subjects)
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if applied.ThresholdFrom != policy.FromAuthored || applied.Threshold != 0.15 {
		t.Errorf("the row reads %v from %s, want the authored 0.15", applied.Threshold, applied.ThresholdFrom)
	}

	// A version that redefines the number waits on the owner here, the way it
	// does at a row an owner authored a threshold on an environment record for.
	recalibrated, err := score.NewWriter(in.pool, in.token, score.NoMarks{}).EnterShipped(ctx,
		record.Actor{Kind: record.KindComponent, Key: "score", Basis: record.BasisClaimed}, "borg/2.0.0")
	if err != nil {
		t.Fatalf("EnterShipped: %v", err)
	}
	reader := policy.NewReader(in.pool, in.token, recalibrated)
	waiting, err := reader.AtGate(ctx, ownerReading, subjects)
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if waiting.ScoreVersion != first.ID || waiting.ScoreVersionWaiting != recalibrated.ID {
		t.Errorf("the row decides under %q and waits on %q, want the confirmed %q and the newest %q",
			waiting.ScoreVersion, waiting.ScoreVersionWaiting, first.ID, recalibrated.ID)
	}
	if waiting.Threshold != 0.15 {
		t.Errorf("the threshold reads %v while a version waits, want the owner's 0.15", waiting.Threshold)
	}

	// Re-authoring names the version in force, which is what puts it in force
	// at this scope: there is no confirmation call for a scope with no
	// environment.
	if _, err := in.factory.AuthorRolePromptOrSkillThreshold(ctx, owner, 0.2); err != nil {
		t.Fatalf("AuthorRolePromptOrSkillThreshold again: %v", err)
	}
	confirmed, err := reader.AtGate(ctx, ownerReading, subjects)
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if confirmed.ScoreVersion != recalibrated.ID || confirmed.ScoreVersionWaiting != "" {
		t.Errorf("after the re-authoring the row decides under %q and waits on %q, want %q and nothing",
			confirmed.ScoreVersion, confirmed.ScoreVersionWaiting, recalibrated.ID)
	}
	if confirmed.Threshold != 0.2 {
		t.Errorf("the threshold reads %v, want the re-authored 0.2", confirmed.Threshold)
	}
}
