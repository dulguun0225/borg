// The database tests of this package are in driftdetector_test rather than in
// driftdetector, the way every record package's are — except here it changes
// nothing in deps.txt: this store is not the factory's, so these tests open it
// through [driftdetector.Open] and [driftdetector.Apply] rather than through package
// postgres, and the only package this file imports besides driftdetector itself
// is record, which driftdetector's own line already allows.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package driftdetector_test

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

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/record"
)

func newTable(t *testing.T) (context.Context, *pgxpool.Pool, *driftdetector.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "driftdetector_" + hex.EncodeToString(suffix[:])

	pool, err := driftdetector.Open(ctx, inSchema(t, driftdetector.DefaultURL, schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", driftdetector.DefaultURL, err)
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
	if err := driftdetector.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	return ctx, pool, driftdetector.NewWriter(pool)
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

// pass is a complete Pass over ids of its own, so a test that needs one does
// not repeat the two required fields. It disagrees by default — its running
// build and its recorded build are two different ids — which is what most of
// these tests want; TestAPassWhereTheTargetAgreesWritesNoMismatch changes it.
func pass() driftdetector.Pass {
	return driftdetector.Pass{
		ServiceID:         record.NewID("svc"),
		Target:            record.NewID("tgt"),
		Reached:           true,
		RunningBuild:      record.NewID("bl"),
		RecordedReleaseID: record.NewID("rel"),
		RecordedBuildID:   record.NewID("bl"),
		Interval:          time.Minute,
	}
}

func TestAPassWhereTheTargetAgreesWritesNoMismatch(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()
	p.RunningBuild = p.RecordedBuildID

	recorded, err := w.Record(ctx, p)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !recorded.LastCheck.Agreed {
		t.Error("LastCheck.Agreed = false, want true where the target runs the recorded build")
	}
	if recorded.Raised != "" || recorded.Agreed != "" {
		t.Errorf("Record = %+v, want neither a raise nor an agreement recorded", recorded)
	}
	held, why, err := driftdetector.NewStore(pool).Mismatch(ctx, p.ServiceID)
	if err != nil || held {
		t.Errorf("Mismatch = %v %q, %v; a target running the recorded build raises none", held, why, err)
	}
}

// TestAPassWhereTheTargetDisagreesRaisesAMismatchTheGateReads is the one
// mismatch a pass writes: raised, found by [driftdetector.Uncleared], and read
// back through [driftdetector.Store.Mismatch] the way the production deploy
// gate does.
func TestAPassWhereTheTargetDisagreesRaisesAMismatchTheGateReads(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()

	recorded, err := w.Record(ctx, p)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.Raised == "" {
		t.Fatal("Record raised no mismatch, and the target ran something the factory did not record")
	}

	uncleared, err := driftdetector.Uncleared(ctx, pool, p.ServiceID)
	if err != nil || len(uncleared) != 1 || uncleared[0].ID != recorded.Raised {
		t.Fatalf("Uncleared = %+v, %v, want just %s", uncleared, err, recorded.Raised)
	}

	held, why, err := driftdetector.NewStore(pool).Mismatch(ctx, p.ServiceID)
	if err != nil || !held {
		t.Fatalf("Mismatch = %v %q, %v, want true", held, why, err)
	}
	for _, want := range []string{driftdetector.HoldWords, p.Target, p.RunningBuild, p.RecordedBuildID} {
		if !strings.Contains(why, want) {
			t.Errorf("Mismatch's words are %q, want them to contain %q", why, want)
		}
	}
}

// TestASecondDisagreeingPassRaisesNoSecondMismatch is doc.go's rule: a
// mismatch remains until a human clears it, so a second pass finding the same
// disagreement writes no second row.
func TestASecondDisagreeingPassRaisesNoSecondMismatch(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()

	first, err := w.Record(ctx, p)
	if err != nil || first.Raised == "" {
		t.Fatalf("Record: raised %q, %v, want a mismatch raised", first.Raised, err)
	}

	second, err := w.Record(ctx, p)
	if err != nil {
		t.Fatalf("Record again: %v", err)
	}
	if second.Raised != "" {
		t.Errorf("a second disagreeing pass raised %s, want none — a mismatch remains until a human clears it", second.Raised)
	}

	uncleared, err := driftdetector.Uncleared(ctx, pool, p.ServiceID)
	if err != nil || len(uncleared) != 1 {
		t.Fatalf("Uncleared = %+v, %v, want the one mismatch still standing", uncleared, err)
	}
}

// TestALaterAgreeingPassRecordsAnAgreementAndLeavesTheMismatchStanding is the
// design's own point: a mismatch remains even where a later comparison
// agrees, and the agreement is recorded on it as the evidence the human
// clearing reads.
func TestALaterAgreeingPassRecordsAnAgreementAndLeavesTheMismatchStanding(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()

	first, err := w.Record(ctx, p)
	if err != nil || first.Raised == "" {
		t.Fatalf("Record: raised %q, %v, want a mismatch raised", first.Raised, err)
	}

	agreeing := p
	agreeing.RunningBuild = p.RecordedBuildID
	second, err := w.Record(ctx, agreeing)
	if err != nil {
		t.Fatalf("Record the later agreement: %v", err)
	}
	if second.Agreed != first.Raised {
		t.Errorf("Record = %+v, want it recording an agreement on %s", second, first.Raised)
	}

	standing, err := driftdetector.Get(ctx, pool, first.Raised)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if standing.LaterAgreements != 1 {
		t.Errorf("LaterAgreements = %d, want 1", standing.LaterAgreements)
	}

	held, _, err := driftdetector.NewStore(pool).Mismatch(ctx, p.ServiceID)
	if err != nil || !held {
		t.Errorf("Mismatch = %v, %v, want true — a mismatch remains even where a later comparison agrees", held, err)
	}
}

func TestAnUnreachedTargetWritesTheLastCheckAndRaisesNoMismatch(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()
	p.Reached = false
	p.Why = "dial tcp: connection refused"

	recorded, err := w.Record(ctx, p)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if recorded.LastCheck.Reached || recorded.LastCheck.Why != p.Why {
		t.Errorf("LastCheck = %+v, want unreached with why %q", recorded.LastCheck, p.Why)
	}
	if recorded.Raised != "" || recorded.Agreed != "" {
		t.Errorf("Record = %+v, want neither — failing to reach a target is not a mismatch", recorded)
	}
	held, _, err := driftdetector.NewStore(pool).Mismatch(ctx, p.ServiceID)
	if err != nil || held {
		t.Errorf("Mismatch = %v, %v, want false", held, err)
	}
}

// TestAPassMissingSomethingEveryComparisonNamesIsIncomplete covers every
// required field the same way, one Pass with exactly one of them cleared per
// case — the way window/db_test.go's TestAnOpeningMissingAFieldIsIncomplete
// does for an OpenEvent.
func TestAPassMissingSomethingEveryComparisonNamesIsIncomplete(t *testing.T) {
	ctx, _, w := newTable(t)

	for _, c := range []struct {
		what string
		mut  func(*driftdetector.Pass)
	}{
		{"service", func(p *driftdetector.Pass) { p.ServiceID = "" }},
		{"target", func(p *driftdetector.Pass) { p.Target = "" }},
		{"why on an unreached target", func(p *driftdetector.Pass) { p.Reached = false; p.Why = "" }},
		{"interval", func(p *driftdetector.Pass) { p.Interval = 0 }},
	} {
		p := pass()
		c.mut(&p)
		if _, err := w.Record(ctx, p); !errors.Is(err, driftdetector.ErrPassIncomplete) {
			t.Errorf("Record missing %s = %v, want ErrPassIncomplete", c.what, err)
		}
	}
}

func TestTheLastCheckIsOverwrittenNotAppended(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()
	if _, err := w.Record(ctx, p); err != nil {
		t.Fatalf("Record: %v", err)
	}

	second := p
	second.RunningBuild = record.NewID("bl")
	if _, err := w.Record(ctx, second); err != nil {
		t.Fatalf("Record again: %v", err)
	}

	checks, err := driftdetector.LastChecks(ctx, pool, p.ServiceID)
	if err != nil {
		t.Fatalf("LastChecks: %v", err)
	}
	if len(checks) != 1 {
		t.Fatalf("LastChecks = %+v, want exactly one row", checks)
	}
	if checks[0].RunningBuild != second.RunningBuild {
		t.Errorf("the last check runs %q, want the second pass's %q", checks[0].RunningBuild, second.RunningBuild)
	}
}

func TestClearMarksAMismatchClearedAndKeepsTheRow(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()
	raised, err := w.Record(ctx, p)
	if err != nil || raised.Raised == "" {
		t.Fatalf("Record: raised %q, %v, want a mismatch raised", raised.Raised, err)
	}

	cleared, err := w.Clear(ctx, raised.Raised, "alice")
	if err != nil {
		t.Fatalf("Clear: %v", err)
	}
	if cleared.ClearedBy != "alice" || cleared.ClearedAt == "" || !cleared.Cleared() {
		t.Errorf("Clear = %+v, want it cleared by alice", cleared)
	}

	held, _, err := driftdetector.NewStore(pool).Mismatch(ctx, p.ServiceID)
	if err != nil || held {
		t.Errorf("Mismatch = %v, %v, want false — a cleared mismatch holds nothing", held, err)
	}

	all, err := driftdetector.All(ctx, pool)
	if err != nil || len(all) != 1 || all[0].ID != raised.Raised || !all[0].Cleared() {
		t.Errorf("All = %+v, %v, want the cleared row kept", all, err)
	}

	if _, err := w.Clear(ctx, raised.Raised, "bob"); !errors.Is(err, driftdetector.ErrAlreadyCleared) {
		t.Errorf("Clear again = %v, want ErrAlreadyCleared", err)
	}
}

func TestClearWithNoHumanIsClearedByEmpty(t *testing.T) {
	ctx, _, w := newTable(t)
	p := pass()
	raised, err := w.Record(ctx, p)
	if err != nil || raised.Raised == "" {
		t.Fatalf("Record: raised %q, %v, want a mismatch raised", raised.Raised, err)
	}
	if _, err := w.Clear(ctx, raised.Raised, ""); !errors.Is(err, driftdetector.ErrClearedByEmpty) {
		t.Errorf("Clear with no human = %v, want ErrClearedByEmpty", err)
	}
}

func TestGetOnAnUnknownIDIsNotFound(t *testing.T) {
	ctx, pool, _ := newTable(t)
	if _, err := driftdetector.Get(ctx, pool, "mis_00000000000000000000000000000000"); !errors.Is(err, driftdetector.ErrNotFound) {
		t.Errorf("Get on an unknown id = %v, want ErrNotFound", err)
	}
}

// TestAnExcusedPassAgreesDespiteADifferentRunningBuildAndRaisesNoMismatch is
// the exception for a build an open window accounts for: [driftdetector.Pass.Excused]
// makes a disagreement no mismatch.
func TestAnExcusedPassAgreesDespiteADifferentRunningBuildAndRaisesNoMismatch(t *testing.T) {
	ctx, pool, w := newTable(t)
	p := pass()
	p.Excused = true

	recorded, err := w.Record(ctx, p)
	if err != nil {
		t.Fatalf("Record: %v", err)
	}
	if !recorded.LastCheck.Agreed {
		t.Error("LastCheck.Agreed = false, want true — an open window's exception makes this no disagreement")
	}
	if recorded.Raised != "" {
		t.Errorf("Record raised %s, want none", recorded.Raised)
	}
	held, _, err := driftdetector.NewStore(pool).Mismatch(ctx, p.ServiceID)
	if err != nil || held {
		t.Errorf("Mismatch = %v, %v, want false", held, err)
	}
}

// TestDDLRefusesAMismatchClearedWithoutAHumanAndALastCheckUnreachedWithNoWhy
// keeps the two CHECK constraints and the writer's own rules from
// disagreeing, the way window/db_test.go's TestDDLListsEveryExit does for
// exits.
func TestDDLRefusesAMismatchClearedWithoutAHumanAndALastCheckUnreachedWithNoWhy(t *testing.T) {
	ctx, pool, _ := newTable(t)

	_, err := pool.Exec(ctx, `insert into `+driftdetector.MismatchTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, target, running_build,
		 recorded_release_id, recorded_build_id, later_agreements, cleared_at, cleared_by)
		values ($1, $2, 'component', 'driftdetector', '', $3, $4, $5, $6, $7, $8, 0, $9, '')`,
		record.NewID(driftdetector.MismatchIDPrefix), driftdetector.FormatVersionMismatch, record.Now(), record.NewID("svc"), record.NewID("tgt"),
		record.NewID("bl"), record.NewID("rel"), record.NewID("bl"), record.Now())
	if err == nil {
		t.Error("the store accepted a mismatch cleared with a time and no human")
	}

	_, err = pool.Exec(ctx, `insert into `+driftdetector.LastCheckTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, target, reached, why,
		 running_build, recorded_release_id, recorded_build_id, agreed)
		values ($1, $2, 'component', 'driftdetector', '', $3, $4, $5, false, '', $6, $7, $8, false)`,
		record.NewID(driftdetector.LastCheckIDPrefix), driftdetector.FormatVersionLastCheck, record.Now(), record.NewID("svc"), record.NewID("tgt"),
		record.NewID("bl"), record.NewID("rel"), record.NewID("bl"))
	if err == nil {
		t.Error("the store accepted a last check unreached with no reason")
	}
}
