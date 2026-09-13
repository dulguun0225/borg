package decisionlog_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/principal"
)

func TestWithByShapeUsesOneConnectionAndReadsAfterTheCallersLock(t *testing.T) {
	ctx, pool, _, token := newLog(t)
	config := pool.Config()
	config.MaxConns = 1
	single, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer single.Close()
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	as := principal.OfAgent("model", "dispatch", "scope")
	reader := decisionlog.NewReader(single, token)
	var version decisionlog.Row
	err = reader.WithByShape(ctx, as, decisionlog.ShapePolicyVersion,
		func(ctx context.Context, tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock(42)`); err != nil {
				return err
			}
			var err error
			version, err = decisionlog.NewWriter(single, token).AppendPolicyVersionInTx(ctx, tx, decisionlog.Entry{
				Actor: gate, FormatVersion: "policy_version/1", Payload: "uncommitted version",
			})
			return err
		},
		func(ctx context.Context, tx pgx.Tx, rows []decisionlog.Row) error {
			if len(rows) != 1 || rows[0].ID != version.ID {
				t.Fatalf("transaction's version missing: %+v", rows)
			}
			if got := readEventCount(t, ctx, pool); got != 1 {
				t.Fatalf("read audit not durable before callback: %d", got)
			}
			key, payload := lastReadEvent(t, ctx, pool)
			if key != as.Actor.Key {
				t.Fatalf("read actor = %s", key)
			}
			var said struct{ Dispatch, Scope, Read string }
			if err := json.Unmarshal([]byte(payload), &said); err != nil {
				return err
			}
			if said.Dispatch != as.DispatchID || said.Scope != as.Scope || said.Read != "policy_version rows" {
				t.Fatalf("read payload: %+v", said)
			}
			return nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if got := readEventCount(t, ctx, pool); got != 1 {
		t.Fatalf("committed read events = %d, want 1", got)
	}
	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatal(err)
	}
}

func TestWithByShapeKeepsTheReadAuditWhenDependentWritesFail(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	refused := errors.New("dependent write refused")
	err := decisionlog.NewReader(pool, token).WithByShape(ctx, ownerReading, decisionlog.ShapeReadEvent, nil,
		func(ctx context.Context, tx pgx.Tx, rows []decisionlog.Row) error {
			if len(rows) != 1 || rows[0].Actor != owner {
				t.Fatalf("read did not include its audit: %+v", rows)
			}
			if _, err := log.AppendPolicyVersionInTx(ctx, tx, decisionlog.Entry{Actor: gate, FormatVersion: "policy_version/1", Payload: "rolled back"}); err != nil {
				return err
			}
			return refused
		})
	if !errors.Is(err, refused) {
		t.Fatalf("callback error = %v", err)
	}
	if got := readEventCount(t, ctx, pool); got != 1 {
		t.Fatalf("failed dependent write lost audit: %d", got)
	}
	var versions int
	if err := pool.QueryRow(ctx, `select count(*) from decision_log where shape = 'policy_version'`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if versions != 0 {
		t.Fatalf("failed callback committed %d versions", versions)
	}
}

func TestWithByShapeRefusesAnIncompletePrincipalAndAStaleFence(t *testing.T) {
	ctx, pool, _, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)
	called := false
	use := func(context.Context, pgx.Tx, []decisionlog.Row) error { called = true; return nil }
	if err := reader.WithByShape(ctx, principal.Principal{}, decisionlog.ShapePolicyVersion, nil, use); err == nil {
		t.Fatal("read accepted no principal")
	}
	if err := lease.Release(ctx, pool, token); err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Acquire(ctx, pool, "replacement", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := reader.WithByShape(ctx, ownerReading, decisionlog.ShapePolicyVersion, nil, use); !errors.Is(err, lease.ErrFenced) {
		t.Fatalf("stale read = %v, want ErrFenced", err)
	}
	if called {
		t.Fatal("refused read reached callback")
	}
	if got := readEventCount(t, ctx, pool); got != 0 {
		t.Fatalf("refused reads appended %d events", got)
	}
}
