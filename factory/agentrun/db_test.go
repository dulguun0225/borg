// The database tests of this package are in agentrun_test rather than in
// agentrun, because they open the pool through package postgres, which
// imports this one to apply its DDL. deps.txt records the edge as "test
// agentrun -> postgres".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package agentrun_test

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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// dispatcher is the actor every test writes as: dispatch as a component is
// not built, so the caller that performed the run stands in for it.
var dispatcher = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *agentrun.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "agentrun_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, agentrun.NewWriter(pool, token)
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

// runOnItem is a complete New over ids of its own, so a test that needs one or
// several does not repeat every required field.
func runOnItem() agentrun.New {
	return agentrun.New{
		Role:               "implementation",
		ModelVersion:       "claude-opus-4.8-20260101",
		Effort:             "high",
		CredentialName:     "anthropic.owner",
		ProcessingLocation: "anthropic/us",
		AccountKind:        agentrun.AccountPerson,
		ItemID:             record.NewID("itm"),
		Stage:              "implementation",
		UnitsByKind:        map[string]int64{"input": 1000, "output": 200},
		Sources:            []string{"repo@abc123"},
		RatesByKind:        map[string]float64{"input": 0.01, "output": 0.03},
		ConvertedAmount:    16.0,
		Priced:             true,
		Currency:           "USD",
		Outcome:            "advanced",
	}
}

func TestRecordWritesTheRunAndGetReadsItBack(t *testing.T) {
	ctx, pool, w := newTable(t)
	n := runOnItem()

	recorded, err := w.Record(ctx, dispatcher, n)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.ItemID != n.ItemID || recorded.Stage != n.Stage || recorded.Role != n.Role {
		t.Errorf("Record = %+v, which does not name what it ran on", recorded)
	}
	if _, err := time.Parse(record.TimeLayout, recorded.At); err != nil {
		t.Errorf("the run has timestamp %q: %v", recorded.At, err)
	}
	// The prefix names the record kind and is the only part of an id a reader
	// may interpret, so it is this kind's alone: "ar" is package area's.
	if agentrun.IDPrefix != "agr" || !strings.HasPrefix(recorded.ID, "agr_") {
		t.Errorf("the run has id %q under prefix %q, want an id of the agent run's own prefix agr",
			recorded.ID, agentrun.IDPrefix)
	}

	read, err := agentrun.Get(ctx, pool, recorded.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ID != recorded.ID || read.CredentialName != recorded.CredentialName ||
		read.ConvertedAmount != recorded.ConvertedAmount || read.Priced != recorded.Priced ||
		read.UnitsByKind["input"] != 1000 || read.RatesByKind["output"] != 0.03 {
		t.Errorf("Get = %+v, want the fields Record wrote", read)
	}
}

func TestGetOnAnUnknownIDIsNotFound(t *testing.T) {
	ctx, pool, _ := newTable(t)
	if _, err := agentrun.Get(ctx, pool, "agr_00000000000000000000000000000000"); !errors.Is(err, agentrun.ErrNotFound) {
		t.Errorf("Get on an unknown id = %v, want ErrNotFound", err)
	}
}

func TestARunNamingNeitherAnItemNorAnIntentIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	n := runOnItem()
	n.ItemID, n.Stage = "", ""

	if _, err := w.Record(ctx, dispatcher, n); !errors.Is(err, agentrun.ErrServedNothing) {
		t.Errorf("Record naming neither = %v, want ErrServedNothing", err)
	}
}

func TestARunOnAnIntentNamesNoItem(t *testing.T) {
	ctx, pool, w := newTable(t)
	n := runOnItem()
	n.ItemID, n.Stage = "", ""
	n.IntentID = record.NewID("int")

	recorded, err := w.Record(ctx, dispatcher, n)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.ItemID != "" || recorded.IntentID != n.IntentID {
		t.Errorf("Record = %+v, want IntentID %s and no item", recorded, n.IntentID)
	}
	found, err := agentrun.ForIntent(ctx, pool, n.IntentID)
	if err != nil || len(found) != 1 || found[0].ID != recorded.ID {
		t.Errorf("ForIntent = %+v, %v, want [%s]", found, err, recorded.ID)
	}
}

func TestAStageWithoutAnItemIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	n := runOnItem()
	n.ItemID = ""

	if _, err := w.Record(ctx, dispatcher, n); !errors.Is(err, agentrun.ErrStageWithoutAnItem) {
		t.Errorf("Record with a stage and no item = %v, want ErrStageWithoutAnItem", err)
	}
}

func TestAnIncompleteRunIsRefused(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*agentrun.New)
		want error
	}{
		{"role", func(n *agentrun.New) { n.Role = "" }, agentrun.ErrRoleEmpty},
		{"model version", func(n *agentrun.New) { n.ModelVersion = "" }, agentrun.ErrModelVersionEmpty},
		{"credential name", func(n *agentrun.New) { n.CredentialName = "" }, agentrun.ErrCredentialNameEmpty},
		{"outcome", func(n *agentrun.New) { n.Outcome = "" }, agentrun.ErrOutcomeEmpty},
		{"currency", func(n *agentrun.New) { n.Currency = "" }, agentrun.ErrCurrencyEmpty},
		{"account kind", func(n *agentrun.New) { n.AccountKind = "unheard-of" }, agentrun.ErrAccountKindUnknown},
	} {
		n := runOnItem()
		c.mut(&n)
		if _, err := w.Record(ctx, dispatcher, n); !errors.Is(err, c.want) {
			t.Errorf("Record missing %s = %v, want %v", c.what, err, c.want)
		}
	}
}

func TestNegativeUnitsAreRefused(t *testing.T) {
	ctx, _, w := newTable(t)
	n := runOnItem()
	n.UnitsByKind = map[string]int64{"input": -1}

	if _, err := w.Record(ctx, dispatcher, n); !errors.Is(err, agentrun.ErrUnitsNegative) {
		t.Errorf("Record with negative units = %v, want ErrUnitsNegative", err)
	}
}

// TestDDLListsEveryAccountKind keeps the CHECK constraint and
// agentrun.AccountKinds from disagreeing, the way incident's
// TestDDLListsEveryStatus does for incident.Statuses.
func TestDDLListsEveryAccountKind(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, kind := range agentrun.AccountKinds {
		n := runOnItem()
		n.AccountKind = kind
		if _, err := w.Record(ctx, dispatcher, n); err != nil {
			t.Errorf("Record with account kind %q, one of agentrun.AccountKinds, was refused: %v", kind, err)
		}
	}
	n := runOnItem()
	n.AccountKind = ""
	if _, err := w.Record(ctx, dispatcher, n); err != nil {
		t.Errorf("Record with an empty account kind was refused: %v", err)
	}
}

func TestForItemIsInOrder(t *testing.T) {
	ctx, pool, w := newTable(t)
	itemID := record.NewID("itm")

	var recorded []agentrun.Run
	for i := 0; i < 3; i++ {
		n := runOnItem()
		n.ItemID = itemID
		got, err := w.Record(ctx, dispatcher, n)
		if err != nil {
			t.Fatalf("Record: %v", err)
		}
		recorded = append(recorded, got)
	}

	got, err := agentrun.ForItem(ctx, pool, itemID)
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(got) != len(recorded) {
		t.Fatalf("ForItem returned %d runs, want %d", len(got), len(recorded))
	}
	for n, want := range recorded {
		if got[n].ID != want.ID {
			t.Errorf("ForItem[%d] = %s, want %s in the order recorded", n, got[n].ID, want.ID)
		}
	}
}

func TestByAuthorModelIsByModelVersion(t *testing.T) {
	ctx, pool, w := newTable(t)
	n := runOnItem()
	n.ModelVersion = "model-a"
	a, err := w.Record(ctx, dispatcher, n)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	n2 := runOnItem()
	n2.ModelVersion = "model-b"
	if _, err := w.Record(ctx, dispatcher, n2); err != nil {
		t.Fatalf("Record: %v", err)
	}

	got, err := agentrun.ByAuthorModel(ctx, pool, "model-a")
	if err != nil || len(got) != 1 || got[0].ID != a.ID {
		t.Errorf("ByAuthorModel(model-a) = %+v, %v, want [%s]", got, err, a.ID)
	}
}

func TestSpendByCredentialSinceSumsPricedRunsAndKeepsUnpricedApart(t *testing.T) {
	ctx, pool, w := newTable(t)
	credential := "anthropic.spend-test"
	start := record.Now()

	priced := runOnItem()
	priced.CredentialName = credential
	priced.ConvertedAmount = 10.0
	priced.Priced = true
	priced.Currency = "USD"
	if _, err := w.Record(ctx, dispatcher, priced); err != nil {
		t.Fatalf("Record: %v", err)
	}

	unpriced := runOnItem()
	unpriced.CredentialName = credential
	unpriced.RatesByKind = map[string]float64{"input": 0.01} // output has no rate
	unpriced.ConvertedAmount = 0
	unpriced.Priced = false
	unpriced.Currency = ""
	unpricedRun, err := w.Record(ctx, dispatcher, unpriced)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if kinds := unpricedRun.UnpricedKinds(); len(kinds) != 1 || kinds[0] != "output" {
		t.Errorf("UnpricedKinds = %v, want [output]", kinds)
	}

	spend, err := agentrun.SpendByCredentialSince(ctx, pool, credential, start)
	if err != nil {
		t.Fatalf("SpendByCredentialSince: %v", err)
	}
	if spend.Amount != 10.0 || spend.Currency != "USD" {
		t.Errorf("SpendByCredentialSince amount = %v %s, want 10 USD", spend.Amount, spend.Currency)
	}
	if len(spend.Unpriced) != 1 || spend.Unpriced[0].ID != unpricedRun.ID {
		t.Errorf("SpendByCredentialSince unpriced = %+v, want [%s]", spend.Unpriced, unpricedRun.ID)
	}
}

func TestSpendByCredentialSinceWithNoCredentialIsRefused(t *testing.T) {
	ctx, pool, _ := newTable(t)
	if _, err := agentrun.SpendByCredentialSince(ctx, pool, "", record.Now()); !errors.Is(err, agentrun.ErrCredentialNameEmpty) {
		t.Errorf("SpendByCredentialSince with no credential = %v, want ErrCredentialNameEmpty", err)
	}
}
