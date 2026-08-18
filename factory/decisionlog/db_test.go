// The database tests of this package are in decisionlog_test rather than in
// decisionlog, because they open the pool through package postgres and
// postgres imports decisionlog to apply its DDL. An external test package is
// a separate package to the compiler, so the edge is a test edge and not the
// cycle the compiler would refuse. deps.txt records it as
// "test decisionlog -> postgres secretref".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package decisionlog_test

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

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newLog gives a test a schema of its own, the log's DDL applied inside it,
// and a writer over it. The schema is dropped when the test ends, so a rerun
// on a database a previous run left dirty starts clean.
func newLog(t *testing.T) (context.Context, *pgxpool.Pool, *decisionlog.Writer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m0_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool, decisionlog.NewWriter(pool)
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

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}
var gate = record.Actor{Kind: record.KindComponent, Name: "gate.merge"}

func TestApplyRunsTwice(t *testing.T) {
	ctx, pool, _ := newLog(t)
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema a second time: %v", err)
	}
}

// TestTheThreeShapesChainUnbroken is the first half of the demonstration: all
// three shapes appended — the decision as its two rows — and the chain read
// back whole.
func TestTheThreeShapesChainUnbroken(t *testing.T) {
	ctx, pool, log := newLog(t)

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("an empty log does not verify: %v", err)
	}

	opening, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         gate,
		Payload:       `{"gate":"merge","waits_on":"owner"}`,
		PolicyVersion: "policy-1",
		ScoreVersion:  "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpening: %v", err)
	}
	page, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor:   record.Actor{Kind: record.KindComponent, Name: "operations.pager"},
		Payload: `{"page":"checkout error rate","reached":"owner"}`,
	})
	if err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}
	wait, err := log.AppendWait(ctx, decisionlog.Entry{
		Actor:   gate,
		Payload: `{"gate":"deploy","waiting_on":"an unreachable credential"}`,
	})
	if err != nil {
		t.Fatalf("AppendWait: %v", err)
	}
	closing, err := log.AppendDecisionClosing(ctx, decisionlog.Entry{
		Actor:   owner,
		Payload: `{"verdict":"approve"}`,
		Closes:  opening.ID,
	})
	if err != nil {
		t.Fatalf("AppendDecisionClosing: %v", err)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify: %v", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("Read returned %d rows, want 4", len(rows))
	}

	wantShapes := []decisionlog.Shape{decisionlog.ShapeDecision, decisionlog.ShapePageEvent,
		decisionlog.ShapeWait, decisionlog.ShapeDecision}
	wantParts := []decisionlog.Part{decisionlog.PartOpening, "", "", decisionlog.PartClosing}
	prevHash := ""
	for n, row := range rows {
		if row.Shape != wantShapes[n] {
			t.Errorf("row %d is a %s, want a %s", n+1, row.Shape, wantShapes[n])
		}
		if row.Part != wantParts[n] {
			t.Errorf("row %d is part %q, want %q", n+1, row.Part, wantParts[n])
		}
		if row.PrevHash != prevHash {
			t.Errorf("row %d names predecessor %q, want %q", n+1, row.PrevHash, prevHash)
		}
		if row.Hash != row.ChainHash() {
			t.Errorf("row %d stores a hash its fields do not produce", n+1)
		}
		if n > 0 && row.Seq <= rows[n-1].Seq {
			t.Errorf("row %d has seq %d, which does not follow %d", n+1, row.Seq, rows[n-1].Seq)
		}
		if err := row.Actor.Validate(); err != nil {
			t.Errorf("row %d carries no usable actor: %v", n+1, err)
		}
		if _, err := time.Parse(record.TimeLayout, row.At); err != nil {
			t.Errorf("row %d has timestamp %q: %v", n+1, row.At, err)
		}
		prevHash = row.Hash
	}

	if rows[0].ID != opening.ID || rows[1].ID != page.ID || rows[2].ID != wait.ID || rows[3].ID != closing.ID {
		t.Errorf("the rows read back are not the four appended")
	}
	if rows[0].PolicyVersion != "policy-1" || rows[0].ScoreVersion != "score-1" {
		t.Errorf("the opening does not name the versions it was decided under: %+v", rows[0])
	}
	for _, n := range []int{1, 2, 3} {
		if rows[n].PolicyVersion != "" || rows[n].ScoreVersion != "" {
			t.Errorf("row %d names a version, and only an opening does", n+1)
		}
	}
	if rows[3].Closes != opening.ID {
		t.Errorf("the closing closes %q, want the opening %q", rows[3].Closes, opening.ID)
	}
	if rows[0].Closes != "" || rows[1].Closes != "" || rows[2].Closes != "" {
		t.Errorf("a row that is not a closing closes something")
	}
}

// TestATamperedRowIsNamed is the second half: a middle row edited by raw SQL,
// and Verify naming that row and how it broke.
func TestATamperedRowIsNamed(t *testing.T) {
	ctx, pool, log := newLog(t)
	appended := appendThree(ctx, t, pool, log)
	middle := appended[1]

	tag, err := pool.Exec(ctx, `update decision_log set payload = $1 where seq = $2`, "tampered", middle.Seq)
	if err != nil {
		t.Fatalf("tampering with row %d: %v", middle.Seq, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("the tampering changed %d rows, want 1", tag.RowsAffected())
	}

	broken := brokenBy(t, decisionlog.Verify(ctx, pool))
	if broken.Row.Seq != middle.Seq {
		t.Errorf("Verify names row %d, the tampered row is %d", broken.Row.Seq, middle.Seq)
	}
	if broken.Row.ID != middle.ID {
		t.Errorf("Verify names %s, the tampered row is %s", broken.Row.ID, middle.ID)
	}
	if broken.Break != decisionlog.BreakFields {
		t.Errorf("Verify reports %v, want %v", broken.Break, decisionlog.BreakFields)
	}
	if broken.Want != broken.Row.ChainHash() {
		t.Errorf("Verify wants %s, the row's fields hash to %s", broken.Want, broken.Row.ChainHash())
	}
}

// TestARemovedRowIsNamed is the other way a chain breaks: the row that
// followed the removed one names a predecessor that is no longer there. It
// removes a row from inside the chain, which is the case Verify catches;
// TestATruncatedTailIsNotCaught is the case it does not.
func TestARemovedRowIsNamed(t *testing.T) {
	ctx, pool, log := newLog(t)
	appended := appendThree(ctx, t, pool, log)

	if _, err := pool.Exec(ctx, `delete from decision_log where seq = $1`, appended[1].Seq); err != nil {
		t.Fatalf("removing row %d: %v", appended[1].Seq, err)
	}

	broken := brokenBy(t, decisionlog.Verify(ctx, pool))
	if broken.Row.Seq != appended[2].Seq {
		t.Errorf("Verify names row %d, want the row after the removed one, %d", broken.Row.Seq, appended[2].Seq)
	}
	if broken.Break != decisionlog.BreakPredecessor {
		t.Errorf("Verify reports %v, want %v", broken.Break, decisionlog.BreakPredecessor)
	}
	if broken.Want != appended[0].Hash {
		t.Errorf("Verify wants %s, the row now before it hashes to %s", broken.Want, appended[0].Hash)
	}
}

// TestATruncatedTailIsNotCaught records what the chain does not prove, and
// fails if that ever stops being the truth.
//
// Nothing anchors the head, so Verify walks forward from the empty
// predecessor hash and stops at whatever row is last. Removing the last row
// leaves an unbroken chain of the rows that remain, and removing it also frees
// its prev_hash value for the unique constraint, so an ordinary append writes
// a replacement over it that verifies clean. The history read back is not the
// history that happened, and Verify returns nil for both.
//
// This is seam 2's deferral and not a defect in this package —
// ../../end-goal/deferred.md#security-comes-last says where the head is
// anchored can wait. Anchoring it is what closes this, and when someone does
// that, this test is what tells them the limit they are removing was known:
// it will start failing, and the assertions below become the assertions of
// TestARemovedRowIsNamed.
func TestATruncatedTailIsNotCaught(t *testing.T) {
	ctx, pool, log := newLog(t)
	appended := appendThree(ctx, t, pool, log)
	tail := appended[len(appended)-1]

	if _, err := pool.Exec(ctx, `delete from decision_log where seq = $1`, tail.Seq); err != nil {
		t.Fatalf("removing the last row: %v", err)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify = %v; the head is anchored now, so this test has outlived the limit it records", err)
	}

	// The freed prev_hash is why a truncation is not merely undetected: the
	// log goes on accepting appends as though the removed row never was.
	replacement, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor: gate, Payload: "written over the removed tail", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("appending over the removed tail: %v", err)
	}
	if replacement.PrevHash != appended[len(appended)-2].Hash {
		t.Fatalf("the replacement names predecessor %s, want the row before the removed one", replacement.PrevHash)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify = %v; the head is anchored now, so this test has outlived the limit it records", err)
	}

	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != len(appended) {
		t.Fatalf("the log holds %d rows, want %d", len(rows), len(appended))
	}
	if rows[len(rows)-1].Payload == tail.Payload {
		t.Fatalf("the removed row is still there; this test is not exercising what it says")
	}
}

// brokenBy is the [*decisionlog.BrokenError] Verify returned, or a failure
// where it returned nil or anything else.
func brokenBy(t *testing.T, err error) *decisionlog.BrokenError {
	t.Helper()
	if err == nil {
		t.Fatal("Verify returned nil, want the chain reported broken")
	}
	var broken *decisionlog.BrokenError
	if !errors.As(err, &broken) {
		t.Fatalf("Verify returned %v, want a *decisionlog.BrokenError", err)
	}
	return broken
}

func appendThree(ctx context.Context, t *testing.T, pool *pgxpool.Pool, log *decisionlog.Writer) []decisionlog.Row {
	t.Helper()
	var appended []decisionlog.Row
	for _, payload := range []string{"first", "second", "third"} {
		row, err := log.AppendDecisionOpening(ctx, decisionlog.Entry{
			Actor: gate, Payload: payload, PolicyVersion: "policy-1", ScoreVersion: "score-1",
		})
		if err != nil {
			t.Fatalf("AppendDecisionOpening(%q): %v", payload, err)
		}
		appended = append(appended, row)
	}
	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("the chain is broken before anything tampered with it: %v", err)
	}
	return appended
}
