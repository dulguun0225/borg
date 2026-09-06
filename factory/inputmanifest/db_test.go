// The database tests of this package are in inputmanifest_test rather than in
// inputmanifest, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as "test
// inputmanifest -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package inputmanifest_test

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

	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// contextAssembly is the actor every test writes as: context assembly is not
// built, so the caller that dispatches stands in for it, the way doc.go says.
var contextAssembly = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *inputmanifest.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "inputmanifest_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, inputmanifest.NewWriter(pool, token)
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

// onItem is a complete New over an item id of its own, so a test that needs
// one does not repeat every field.
func onItem() inputmanifest.New {
	bound := int64(200000)
	return inputmanifest.New{
		ItemID: record.NewID("itm"),
		Stage:  "implementation",
		Materials: []inputmanifest.Material{
			{Class: "repository", Reference: "repo@abc123", Bytes: 4096},
			{Class: "constraint", Reference: "con_1", Bytes: 128},
		},
		ReadAtOnceBound:      &bound,
		SelectionRuleVersion: "selection/1",
		Excluded: []inputmanifest.Exclusion{
			{What: "incident_report", Reason: "the fleet entry does not name that class"},
		},
	}
}

func TestWriteWritesTheManifestAndGetReadsItBack(t *testing.T) {
	ctx, pool, w := newTable(t)
	n := onItem()

	written, err := w.Write(ctx, contextAssembly, n)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written.ItemID != n.ItemID || written.Stage != n.Stage {
		t.Errorf("Write = %+v, which does not name what it was written for", written)
	}
	if _, err := time.Parse(record.TimeLayout, written.At); err != nil {
		t.Errorf("the manifest has timestamp %q: %v", written.At, err)
	}

	read, err := inputmanifest.Get(ctx, pool, written.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(read.Materials) != 2 || read.Materials[0].Reference != "repo@abc123" ||
		read.Materials[1].Bytes != 128 {
		t.Errorf("Get materials = %+v, want the two written", read.Materials)
	}
	if read.ReadAtOnceBound == nil || *read.ReadAtOnceBound != 200000 {
		t.Errorf("Get bound = %v, want 200000", read.ReadAtOnceBound)
	}
	if read.SelectionRuleVersion != "selection/1" {
		t.Errorf("Get selection rule version = %q, want selection/1", read.SelectionRuleVersion)
	}
	if len(read.Excluded) != 1 || read.Excluded[0].What != "incident_report" {
		t.Errorf("Get excluded = %+v, want the one written", read.Excluded)
	}
}

func TestGetOnAnUnknownIDIsNotFound(t *testing.T) {
	ctx, pool, _ := newTable(t)
	if _, err := inputmanifest.Get(ctx, pool, "im_00000000000000000000000000000000"); !errors.Is(err, inputmanifest.ErrNotFound) {
		t.Errorf("Get on an unknown id = %v, want ErrNotFound", err)
	}
}

func TestANoBoundAndNoSelectionRuleVersionRoundTripAsEmpty(t *testing.T) {
	ctx, pool, w := newTable(t)
	n := inputmanifest.New{IntentID: record.NewID("int")}

	written, err := w.Write(ctx, contextAssembly, n)
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if written.ReadAtOnceBound != nil {
		t.Errorf("Write with no bound = %v, want nil", written.ReadAtOnceBound)
	}

	read, err := inputmanifest.Get(ctx, pool, written.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ReadAtOnceBound != nil || read.SelectionRuleVersion != "" || len(read.Materials) != 0 || len(read.Excluded) != 0 {
		t.Errorf("Get = %+v, want every optional field empty", read)
	}
}

func TestAManifestNamingNeitherAnItemNorAnIntentIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	if _, err := w.Write(ctx, contextAssembly, inputmanifest.New{}); !errors.Is(err, inputmanifest.ErrNamedNothing) {
		t.Errorf("Write naming neither = %v, want ErrNamedNothing", err)
	}
}

func TestAStageWithoutAnItemIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	n := inputmanifest.New{IntentID: record.NewID("int"), Stage: "spec"}
	if _, err := w.Write(ctx, contextAssembly, n); !errors.Is(err, inputmanifest.ErrStageWithoutAnItem) {
		t.Errorf("Write with a stage and no item = %v, want ErrStageWithoutAnItem", err)
	}
}

func TestANegativeReadAtOnceBoundIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	bound := int64(-1)
	n := onItem()
	n.ReadAtOnceBound = &bound
	if _, err := w.Write(ctx, contextAssembly, n); !errors.Is(err, inputmanifest.ErrReadAtOnceBoundNegative) {
		t.Errorf("Write with a negative bound = %v, want ErrReadAtOnceBoundNegative", err)
	}
}

func TestAnIncompleteMaterialIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	n := onItem()
	n.Materials = []inputmanifest.Material{{Class: "repository", Reference: ""}}
	if _, err := w.Write(ctx, contextAssembly, n); !errors.Is(err, inputmanifest.ErrMaterialIncomplete) {
		t.Errorf("Write with a material missing a reference = %v, want ErrMaterialIncomplete", err)
	}
}

func TestAnIncompleteExclusionIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	n := onItem()
	n.Excluded = []inputmanifest.Exclusion{{What: "incident_report", Reason: ""}}
	if _, err := w.Write(ctx, contextAssembly, n); !errors.Is(err, inputmanifest.ErrExclusionIncomplete) {
		t.Errorf("Write with an exclusion missing a reason = %v, want ErrExclusionIncomplete", err)
	}
}
