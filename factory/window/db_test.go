// The database tests of this package are in window_test rather than in
// window, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as "test window ->
// postgres".
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
//
// This file is [window.Writer.Open], the validation it refuses, and the
// fixtures the other files of this package share. close_test.go is
// [window.Writer.Close]; read_test.go is the reads and the mark.
package window_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// healthMonitor is the one writer of analysis windows, the way doc.go names it.
var healthMonitor = record.Actor{Kind: record.KindComponent, Key: "health_monitor", Basis: record.BasisClaimed}

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *window.Writer, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m3_win_" + hex.EncodeToString(suffix[:])

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
		t.Fatalf("acquiring the lease: %v", err)
	}
	return ctx, pool, window.NewWriter(pool, token), token
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

// opening is a complete OpenEvent over ids of its own, so a test that needs one
// or several does not repeat every field a window names at the open.
func opening() window.OpenEvent {
	return window.OpenEvent{
		DeployID:        record.NewID("dep"),
		ReleaseID:       record.NewID("rel"),
		BuildID:         record.NewID("bld"),
		ServiceID:       record.NewID("svc"),
		PassedAvailable: true,
		Size: map[gatepolicy.Quantity]float64{
			gatepolicy.QuantityRequestRate: 0.1,
			gatepolicy.QuantityErrorRate:   0.05,
			gatepolicy.QuantityLatency:     0.1,
		},
		Power: map[gatepolicy.Quantity]float64{
			gatepolicy.QuantityRequestRate: 0.8,
			gatepolicy.QuantityErrorRate:   0.8,
			gatepolicy.QuantityLatency:     0.8,
		},
		Confidence:             0.95,
		CapSeconds:             3600,
		BoundaryVersion:        boundary.Version,
		Targets:                []string{"one.example", "two.example"},
		OperationsReadAlone:    []string{"GET /items"},
		EmissionVersionRelease: "emission/1",
		EmissionVersionControl: "emission/1",
		OwnHistorySize:         map[gatepolicy.Quantity]float64{gatepolicy.QuantityErrorRate: 0.2},
		OwnHistoryRunLength:    10000,
		PolicyVersion:          "pv_1",
		ScoreVersion:           "sv_1",
	}
}

func TestAWindowOpensWithEveryFieldIntact(t *testing.T) {
	ctx, pool, w, _ := newTable(t)
	o := opening()

	opened, err := w.Open(ctx, healthMonitor, o)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if opened.DeployID != o.DeployID || opened.ReleaseID != o.ReleaseID ||
		opened.BuildID != o.BuildID || opened.ServiceID != o.ServiceID {
		t.Errorf("Open = %+v, which does not name what it was opened over", opened)
	}
	if !reflect.DeepEqual(opened.Size, o.Size) || !reflect.DeepEqual(opened.Power, o.Power) ||
		opened.Confidence != o.Confidence || opened.CapSeconds != o.CapSeconds ||
		opened.BoundaryVersion != o.BoundaryVersion ||
		!reflect.DeepEqual(opened.Targets, o.Targets) ||
		!reflect.DeepEqual(opened.OperationsReadAlone, o.OperationsReadAlone) ||
		opened.EmissionVersionRelease != o.EmissionVersionRelease ||
		!reflect.DeepEqual(opened.OwnHistorySize, o.OwnHistorySize) ||
		opened.OwnHistoryRunLength != o.OwnHistoryRunLength ||
		opened.PolicyVersion != o.PolicyVersion || opened.ScoreVersion != o.ScoreVersion {
		t.Errorf("Open = %+v, does not carry the parameters it was given, %+v", opened, o)
	}
	if !opened.Open() {
		t.Error("a freshly opened window reads as closed")
	}
	if opened.Exit != "" || opened.ClosedAt != "" {
		t.Errorf("a freshly opened window has exit %q closed at %q, want both empty", opened.Exit, opened.ClosedAt)
	}
	if _, err := time.Parse(record.TimeLayout, opened.At); err != nil {
		t.Errorf("the window has timestamp %q: %v", opened.At, err)
	}

	read, err := window.Get(ctx, pool, opened.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !reflect.DeepEqual(read, opened) {
		t.Errorf("Get = %+v, want %+v", read, opened)
	}
}

// TestTheBoundaryIsAllocatedOverTheWholeSet is the confidence held over the set
// rather than per reading: the quantities on every operation read alone plus the
// pooled one, on every target the rollout is planned to reach.
func TestTheBoundaryIsAllocatedOverTheWholeSet(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	opened, err := w.Open(ctx, healthMonitor, opening())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if got, want := opened.Comparisons(), 2*2*3; got != want {
		t.Errorf("Comparisons() = %d, want two targets by two series by three quantities = %d", got, want)
	}
	b, carried := opened.Boundary(gatepolicy.QuantityErrorRate)
	if !carried {
		t.Fatal("the window carries no boundary for the error rate")
	}
	if b.Comparisons != opened.Comparisons() || b.Confidence != opened.Confidence {
		t.Errorf("Boundary = %+v, which is not the window's own confidence over its own set", b)
	}
	if b.Worse != boundary.WorseHigher {
		t.Errorf("the error rate reads %q as worse, want higher", b.Worse)
	}
	if rate, _ := opened.Boundary(gatepolicy.QuantityRequestRate); rate.Worse != boundary.WorseLower {
		t.Errorf("the request rate reads %q as worse, want lower", rate.Worse)
	}
}

func TestASecondWindowOverOneDeployIsRefused(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	o := opening()
	if _, err := w.Open(ctx, healthMonitor, o); err != nil {
		t.Fatalf("Open: %v", err)
	}

	again := opening()
	again.DeployID = o.DeployID
	if _, err := w.Open(ctx, healthMonitor, again); err == nil {
		t.Error("a second window over one deploy was accepted")
	}
}

func TestASecondWindowOverOneReleaseIsRefused(t *testing.T) {
	ctx, _, w, _ := newTable(t)
	o := opening()
	if _, err := w.Open(ctx, healthMonitor, o); err != nil {
		t.Fatalf("Open: %v", err)
	}

	again := opening()
	again.ReleaseID = o.ReleaseID
	if _, err := w.Open(ctx, healthMonitor, again); err == nil {
		t.Error("a second window over one release was accepted")
	}
}

// TestASearchsWindowNamesABuildAndNoRelease is the window over a deploy the
// search called for: it names the build alone, and two of them do not collide on
// the release the neither of them has.
func TestASearchsWindowNamesABuildAndNoRelease(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	for i := 0; i < 2; i++ {
		o := opening()
		o.ReleaseID = ""
		opened, err := w.Open(ctx, healthMonitor, o)
		if err != nil {
			t.Fatalf("Open over a search's deploy: %v", err)
		}
		if opened.ReleaseID != "" || opened.BuildID == "" {
			t.Errorf("a search's window = %+v, want a build and no release", opened)
		}
	}
}

// TestAnOpeningMissingAFieldIsIncomplete covers every required field the same
// way, one OpenEvent with exactly one of them passed per case.
func TestAnOpeningMissingAFieldIsIncomplete(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*window.OpenEvent)
	}{
		{"deploy", func(o *window.OpenEvent) { o.DeployID = "" }},
		{"build", func(o *window.OpenEvent) { o.BuildID = "" }},
		{"service", func(o *window.OpenEvent) { o.ServiceID = "" }},
		{"boundary version", func(o *window.OpenEvent) { o.BoundaryVersion = "" }},
		{"policy version", func(o *window.OpenEvent) { o.PolicyVersion = "" }},
		{"score version", func(o *window.OpenEvent) { o.ScoreVersion = "" }},
		{"size", func(o *window.OpenEvent) { o.Size = nil }},
		{"power", func(o *window.OpenEvent) { o.Power = nil }},
		{"target set", func(o *window.OpenEvent) { o.Targets = nil }},
		{"emission version", func(o *window.OpenEvent) { o.EmissionVersionRelease = "" }},
	} {
		o := opening()
		c.mut(&o)
		if _, err := w.Open(ctx, healthMonitor, o); !errors.Is(err, window.ErrOpeningIncomplete) {
			t.Errorf("Open missing %s = %v, want ErrOpeningIncomplete", c.what, err)
		}
	}
}

// TestASizeConfidencePowerOrCapOutOfRangeIsIncomplete covers the shares: a size
// is above nothing and at most one, a power and the confidence are above nothing
// and below one, and the cap is above nothing.
func TestASizeConfidencePowerOrCapOutOfRangeIsIncomplete(t *testing.T) {
	ctx, _, w, _ := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*window.OpenEvent)
	}{
		{"size at zero", func(o *window.OpenEvent) { o.Size[gatepolicy.QuantityErrorRate] = 0 }},
		{"size above one", func(o *window.OpenEvent) { o.Size[gatepolicy.QuantityErrorRate] = 1.5 }},
		{"power at one", func(o *window.OpenEvent) { o.Power[gatepolicy.QuantityErrorRate] = 1 }},
		{"a quantity with a size and no power", func(o *window.OpenEvent) {
			delete(o.Power, gatepolicy.QuantityErrorRate)
		}},
		{"confidence at zero", func(o *window.OpenEvent) { o.Confidence = 0 }},
		{"confidence at one", func(o *window.OpenEvent) { o.Confidence = 1 }},
		{"cap at zero", func(o *window.OpenEvent) { o.CapSeconds = 0 }},
	} {
		o := opening()
		c.mut(&o)
		if _, err := w.Open(ctx, healthMonitor, o); !errors.Is(err, window.ErrOpeningIncomplete) {
			t.Errorf("Open with %s = %v, want ErrOpeningIncomplete", c.what, err)
		}
	}
}

// closedOn is the read a test closes a window on: counts per quantity with a
// baseline in them and one series beside them, which is what an exit other than
// skipped always has. The numbers are not what these tests assert over — what
// they assert is the exit — but a read is what makes an exit recomputable.
