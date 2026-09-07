// The two read-time reporting queries against a real store, in score_test
// rather than in score: this file applies the whole factory schema through
// package postgres, which reaches this package back through people and
// policy, so an internal test file importing postgres would make package
// score import itself. decisionlog's and window's own database tests take
// the same shape for the same reason.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package score_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/window"
)

// reportReading is the principal these tests read as: a human at the screen
// [score.RealizedAutoPass] and [score.HeldOutByBand] are read from, and never
// the package's own internal component actor, which is not what a screen's
// read is made as.
var reportReading = principal.OfHuman("person:report", record.BasisClaimed)

// newReportStore gives a test a schema of its own with the whole factory
// schema applied through package postgres — report.go's reads cross the
// decision log, the release table and the window table, so no narrower
// schema would serve every fixture this file needs — a lease acquired for
// it, and a writer for each of the three. The schema is dropped when the
// test ends, so a rerun on a database a previous run left dirty starts
// clean.
func newReportStore(t *testing.T) (context.Context, *pgxpool.Pool, *decisionlog.Writer, *window.Writer, *release.Writer, lease.Token) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "score_report_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inReportSchema(t, postgres.URL(), schema))
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
	return ctx, pool, decisionlog.NewWriter(pool, token), window.NewWriter(pool, token), release.NewWriter(pool, token), token
}

// inReportSchema points a connection URL at one schema and nothing else, so
// every unqualified name in the DDL and in the writers' statements resolves
// there.
func inReportSchema(t *testing.T, base, schema string) string {
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

// appendFiring writes one decision open and its close, the JSON payloads
// being exactly what package score's own reader reads back.
func appendFiring(t *testing.T, ctx context.Context, log *decisionlog.Writer,
	openActor, closeActor record.Actor, open score.OpenEvent, closeEvent score.CloseEvent) decisionlog.Row {
	t.Helper()

	openPayload, err := json.Marshal(open)
	if err != nil {
		t.Fatalf("encoding the open event: %v", err)
	}
	opened, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: openActor, Payload: string(openPayload), FormatVersion: "decision/1",
		PolicyVersion: "pv_1", ScoreVersion: "sv_1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}

	closePayload, err := json.Marshal(closeEvent)
	if err != nil {
		t.Fatalf("encoding the close event: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: closeActor, Payload: string(closePayload), FormatVersion: "decision/1",
		Closes: opened.ID, Verdict: closeEvent.Verdict, OpenedInWorkAt: record.Now(),
	}); err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
	}
	return opened
}

// gateComponent is the actor a gate row closes for itself, the way every
// other package's fixtures name a component.
func gateComponent(row string) record.Actor {
	return record.Actor{Kind: record.KindComponent, Key: "gate." + row, Basis: record.BasisClaimed}
}

// near is float comparison for a value arrived at by division, the same
// tolerance package score's own tests use for arithmetic on bands.
func near(got, want float64) bool { return got-want < 1e-9 && want-got < 1e-9 }

func TestRealizedAutoPassReadsOneRowPerFactorSetAndThreshold(t *testing.T) {
	ctx, pool, log, _, _, token := newReportStore(t)
	gate := gateComponent("implementation")
	reviewer := record.Actor{Kind: record.KindHuman, Key: "person:reviewer", Basis: record.BasisClaimed}

	appendFiring(t, ctx, log, gate, gate,
		score.OpenEvent{ItemID: "it_a", Gate: "implementation", FactorSet: score.SetWithABuild, Number: 0.1, Threshold: 0.3},
		score.CloseEvent{Verdict: score.VerdictApproved, WhyItAutoPassed: score.AutoPassThreshold})
	appendFiring(t, ctx, log, gate, reviewer,
		score.OpenEvent{ItemID: "it_b", Gate: "implementation", FactorSet: score.SetWithABuild, Number: 0.5, Threshold: 0.3},
		score.CloseEvent{Verdict: score.VerdictApproved})

	since := record.FormatTime(time.Now().Add(-time.Hour))
	rows, err := score.RealizedAutoPass(ctx, pool, token, reportReading, since)
	if err != nil {
		t.Fatalf("RealizedAutoPass: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("RealizedAutoPass = %d rows, want 1: %+v", len(rows), rows)
	}
	row := rows[0]
	if row.FactorSet != score.SetWithABuild || row.Subject != "implementation" || row.Threshold != 0.3 {
		t.Errorf("row = %+v, wrong key", row)
	}
	if row.Decisions != 2 {
		t.Errorf("Decisions = %d, want 2", row.Decisions)
	}
	if !near(row.RealizedRate, 0.5) {
		t.Errorf("RealizedRate = %v, want 0.5", row.RealizedRate)
	}
}

func TestHeldOutByBandReadsAFailedAndAPassedBandApart(t *testing.T) {
	ctx, pool, log, win, rel, token := newReportStore(t)
	gate := gateComponent("deploy_to_production")
	owner := record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}
	healthMonitor := record.Actor{Kind: record.KindComponent, Key: "health_monitor", Basis: record.BasisClaimed}

	appendFiring(t, ctx, log, gate, gate,
		score.OpenEvent{ItemID: "it_c", Gate: "deploy_to_production", FactorSet: score.SetWithABuild, Number: 0.15, Threshold: 0.3, HeldOut: true},
		score.CloseEvent{Verdict: score.VerdictApproved, WhyItAutoPassed: score.AutoPassSample})
	appendFiring(t, ctx, log, gate, gate,
		score.OpenEvent{ItemID: "it_d", Gate: "deploy_to_production", FactorSet: score.SetWithABuild, Number: 0.35, Threshold: 0.3, HeldOut: true},
		score.CloseEvent{Verdict: score.VerdictApproved, WhyItAutoPassed: score.AutoPassSample})

	relC, err := rel.Mint(ctx, owner, release.Minting{ServiceID: "svc_a", BuildID: "bld_1", Commit: "commit_c", ItemID: "it_c"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	relD, err := rel.Mint(ctx, owner, release.Minting{ServiceID: "svc_a", BuildID: "bld_1", Commit: "commit_d", ItemID: "it_d"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	shares := map[gatepolicy.Quantity]float64{
		gatepolicy.QuantityRequestRate: 0.1, gatepolicy.QuantityErrorRate: 0.05, gatepolicy.QuantityLatency: 0.1,
	}
	powers := map[gatepolicy.Quantity]float64{
		gatepolicy.QuantityRequestRate: 0.8, gatepolicy.QuantityErrorRate: 0.8, gatepolicy.QuantityLatency: 0.8,
	}
	windowOpen := func(releaseID string) window.OpenEvent {
		return window.OpenEvent{
			DeployID: record.NewID("dep"), ReleaseID: releaseID, BuildID: "bld_1", ServiceID: "svc_a",
			PassedAvailable: true, Size: shares, Power: powers, Confidence: 0.95, CapSeconds: 3600,
			BoundaryVersion: boundary.Version, Targets: []string{"one.example"},
			EmissionVersionRelease: "emission/1", PolicyVersion: "pv_1", ScoreVersion: "sv_1",
		}
	}

	openedC, err := win.Open(ctx, healthMonitor, windowOpen(relC.ID))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := win.Close(ctx, openedC.ID, window.ExitFailed, window.Closing{}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	openedD, err := win.Open(ctx, healthMonitor, windowOpen(relD.ID))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := win.Close(ctx, openedD.ID, window.ExitPassed, window.Closing{}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	since := record.FormatTime(time.Now().Add(-time.Hour))
	bands, err := score.HeldOutByBand(ctx, pool, token, reportReading, since)
	if err != nil {
		t.Fatalf("HeldOutByBand: %v", err)
	}
	if len(bands) != 2 {
		t.Fatalf("HeldOutByBand = %d bands, want 2: %+v", len(bands), bands)
	}
	var failedBand, passedBand *score.BandOutcome
	for i, b := range bands {
		if b.FactorSet != score.SetWithABuild {
			t.Errorf("band %+v is on the wrong factor set", b)
		}
		switch {
		case near(b.FailedShare, 1.0):
			failedBand = &bands[i]
		case near(b.FailedShare, 0.0):
			passedBand = &bands[i]
		}
	}
	if failedBand == nil || failedBand.Windows != 1 || !near(failedBand.From, 0.1) || !near(failedBand.To, 0.2) {
		t.Errorf("the failed band = %+v", failedBand)
	}
	if passedBand == nil || passedBand.Windows != 1 || !near(passedBand.From, 0.3) || !near(passedBand.To, 0.4) {
		t.Errorf("the passed band = %+v", passedBand)
	}
}

// TestHeldOutByBandExcludesAMarkedRelease: a held-out window that failed
// still teaches the band nothing where a named human at Ops marked its
// release's rollback as not caused by the release — the same exclusion
// [Learn]'s own pass applies, which [HeldOutByBand] has to agree with because
// the screen's number is read against the same evidence the score learns
// from.
func TestHeldOutByBandExcludesAMarkedRelease(t *testing.T) {
	ctx, pool, log, win, rel, token := newReportStore(t)
	gate := gateComponent("deploy_to_production")
	owner := record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}
	healthMonitor := record.Actor{Kind: record.KindComponent, Key: "health_monitor", Basis: record.BasisClaimed}

	appendFiring(t, ctx, log, gate, gate,
		score.OpenEvent{ItemID: "it_e", Gate: "deploy_to_production", FactorSet: score.SetWithABuild, Number: 0.15, Threshold: 0.3, HeldOut: true},
		score.CloseEvent{Verdict: score.VerdictApproved, WhyItAutoPassed: score.AutoPassSample})

	relE, err := rel.Mint(ctx, owner, release.Minting{ServiceID: "svc_a", BuildID: "bld_1", Commit: "commit_e", ItemID: "it_e"})
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}

	shares := map[gatepolicy.Quantity]float64{
		gatepolicy.QuantityRequestRate: 0.1, gatepolicy.QuantityErrorRate: 0.05, gatepolicy.QuantityLatency: 0.1,
	}
	powers := map[gatepolicy.Quantity]float64{
		gatepolicy.QuantityRequestRate: 0.8, gatepolicy.QuantityErrorRate: 0.8, gatepolicy.QuantityLatency: 0.8,
	}
	opened, err := win.Open(ctx, healthMonitor, window.OpenEvent{
		DeployID: record.NewID("dep"), ReleaseID: relE.ID, BuildID: "bld_1", ServiceID: "svc_a",
		PassedAvailable: true, Size: shares, Power: powers, Confidence: 0.95, CapSeconds: 3600,
		BoundaryVersion: boundary.Version, Targets: []string{"one.example"},
		EmissionVersionRelease: "emission/1", PolicyVersion: "pv_1", ScoreVersion: "sv_1",
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := win.Close(ctx, opened.ID, window.ExitFailed, window.Closing{}); err != nil {
		t.Fatalf("Close: %v", err)
	}

	rollback, err := deploy.NewWriter(pool, token).StartUndoing(ctx, owner, deploy.Beginning{
		ServiceID: "svc_a", EnvironmentID: "env_a",
		Targets: []deploy.Reaching{{Address: "one.example"}},
	}, deploy.Undoing{FailedReleaseID: relE.ID, Source: deploy.SourceHealthMonitorAtFailed})
	if err != nil {
		t.Fatalf("StartUndoing: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := window.WriteMark(ctx, tx, token, owner, rollback.ID, "svc_a", "a confounded comparison"); err != nil {
		t.Fatalf("WriteMark: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("committing the mark: %v", err)
	}

	since := record.FormatTime(time.Now().Add(-time.Hour))
	bands, err := score.HeldOutByBand(ctx, pool, token, reportReading, since)
	if err != nil {
		t.Fatalf("HeldOutByBand: %v", err)
	}
	if len(bands) != 0 {
		t.Errorf("HeldOutByBand with the only held-out release marked = %+v, want no bands", bands)
	}
}

func TestAnEmptyStoreReadsAsNoRowsAndNoError(t *testing.T) {
	ctx, pool, _, _, _, token := newReportStore(t)
	since := record.FormatTime(time.Now().Add(-time.Hour))

	rows, err := score.RealizedAutoPass(ctx, pool, token, reportReading, since)
	if err != nil {
		t.Fatalf("RealizedAutoPass: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("RealizedAutoPass over an empty store = %+v, want none", rows)
	}

	bands, err := score.HeldOutByBand(ctx, pool, token, reportReading, since)
	if err != nil {
		t.Fatalf("HeldOutByBand: %v", err)
	}
	if len(bands) != 0 {
		t.Errorf("HeldOutByBand over an empty store = %+v, want none", bands)
	}
}
