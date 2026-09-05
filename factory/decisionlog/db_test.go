package decisionlog_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

func TestSchemaAppliesTwice(t *testing.T) {
	ctx, pool, _, _ := newLog(t)
	for _, statement := range append(append([]string{}, lease.DDL...), decisionlog.DDL...) {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying the schema a second time: %v", err)
		}
	}
}

// TestTheTenShapesChainUnbroken is the demonstration: all ten shapes
// appended — the decision as an opening, an acknowledgement and a closing;
// the wait as an opening and a closing; a read event for every read — and
// the chain read back whole.
func TestVerifyOfAnEmptyLogSucceeds(t *testing.T) {
	ctx, pool, _, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)
	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("an empty log does not verify: %v", err)
	}
}

func TestTheTenShapesChainUnbroken(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"gate":"merge_to_master"}`,
		FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	page, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: notifierActor, Payload: `{"page":"checkout error rate","reached":"owner"}`, FormatVersion: "page_event/1",
	})
	if err != nil {
		t.Fatalf("AppendPageEvent: %v", err)
	}
	waitOpen, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"waiting_on":"an unreachable credential"}`, FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("AppendWaitOpen: %v", err)
	}
	waitClose, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"condition":"gone"}`, FormatVersion: "wait/1", Closes: waitOpen.ID,
	})
	if err != nil {
		t.Fatalf("AppendWaitClose: %v", err)
	}
	rework, err := log.AppendReworkRequest(ctx, decisionlog.Entry{
		Actor: gate, Payload: `{"names":"Spec","defect":"the spec says two things"}`, FormatVersion: "rework_request/1",
	})
	if err != nil {
		t.Fatalf("AppendReworkRequest: %v", err)
	}
	rejection, err := log.AppendQueueRejection(ctx, decisionlog.Entry{
		Actor:   record.Actor{Kind: record.KindComponent, Key: "mergequeue"},
		Payload: `{"reading":"no longer passes"}`, FormatVersion: "queue_rejection/1",
	})
	if err != nil {
		t.Fatalf("AppendQueueRejection: %v", err)
	}
	policyVersion, err := log.AppendPolicyVersion(ctx, decisionlog.Entry{
		Actor: record.Actor{Kind: record.KindComponent, Key: "factory"}, Payload: `{"changed":"threshold"}`,
		FormatVersion: "policy_version/1",
	})
	if err != nil {
		t.Fatalf("AppendPolicyVersion: %v", err)
	}
	scoreVersion, err := log.AppendScoreVersion(ctx, decisionlog.Entry{
		Actor: record.Actor{Kind: record.KindComponent, Key: "score"}, Payload: `{"moved":"factor"}`,
		FormatVersion: "score_version/1",
	})
	if err != nil {
		t.Fatalf("AppendScoreVersion: %v", err)
	}
	install, err := log.AppendInstallEvent(ctx, decisionlog.Entry{
		Actor: record.Actor{Kind: record.KindComponent, Key: "first-start"}, Payload: `{"event":"upgrade","version":"1.2.3"}`,
		FormatVersion: "install_event/1",
	})
	if err != nil {
		t.Fatalf("AppendInstallEvent: %v", err)
	}
	ack, err := log.AppendDecisionAcknowledgement(ctx, decisionlog.Entry{
		Actor: owner, Payload: "", FormatVersion: "decision/1", Closes: opening.ID,
	})
	if err != nil {
		t.Fatalf("AppendDecisionAcknowledgement: %v", err)
	}
	closing, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: `{"note":"looks fine"}`, FormatVersion: "decision/1",
		Closes: opening.ID, Verdict: "approve", OpenedInWorkAt: record.Now(),
	})
	if err != nil {
		t.Fatalf("AppendDecisionClose: %v", err)
	}

	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	rows, err := reader.Read(ctx, owner)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}

	wantIDs := []string{
		opening.ID, page.ID, waitOpen.ID, waitClose.ID, rework.ID, rejection.ID,
		policyVersion.ID, scoreVersion.ID, install.ID, ack.ID, closing.ID,
	}
	// Plus the two read events this test's own reads just appended: one
	// from Verify above, one from Read appending its own before it answers.
	if len(rows) != len(wantIDs)+2 {
		t.Fatalf("Read returned %d rows, want %d", len(rows), len(wantIDs)+2)
	}
	for n, id := range wantIDs {
		if rows[n].ID != id {
			t.Errorf("row %d is %s, want %s", n+1, rows[n].ID, id)
		}
	}
	for n := range 2 {
		if got := rows[len(wantIDs)+n].Shape; got != decisionlog.ShapeReadEvent {
			t.Errorf("row %d is a %s, want a %s", len(wantIDs)+n+1, got, decisionlog.ShapeReadEvent)
		}
	}

	prevHash := ""
	for n, row := range rows {
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

	if closing.Verdict != "approve" {
		t.Errorf("the closing carries verdict %q, want %q", closing.Verdict, "approve")
	}
	if closing.Closes != opening.ID {
		t.Errorf("the closing closes %q, want the opening %q", closing.Closes, opening.ID)
	}
	if ack.Part != decisionlog.PartAcknowledgement || ack.Closes != opening.ID {
		t.Errorf("the acknowledgement is %+v, want part %q closing %q", ack, decisionlog.PartAcknowledgement, opening.ID)
	}
	if opening.PolicyVersion != "policy-1" || opening.ScoreVersion != "score-1" {
		t.Errorf("the opening does not name the versions it was decided under: %+v", opening)
	}
	if closing.PolicyVersion != "" || closing.ScoreVersion != "" {
		t.Errorf("the closing names a version, and only an opening or a truncation does")
	}
}

// TestATamperedRowIsNamed is Verify naming a middle row edited by raw SQL,
// and how it broke.
func TestATamperedRowIsNamed(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)
	appended := appendThreeOpenings(ctx, t, log, reader)
	middle := appended[1]

	tag, err := pool.Exec(ctx, `update decision_log set payload = $1 where seq = $2`, "tampered", middle.Seq)
	if err != nil {
		t.Fatalf("tampering with row %d: %v", middle.Seq, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("the tampering changed %d rows, want 1", tag.RowsAffected())
	}

	broken := brokenBy(t, reader.Verify(ctx, owner))
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
// followed the removed one names a predecessor that is no longer there.
func TestARemovedRowIsNamed(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)
	appended := appendThreeOpenings(ctx, t, log, reader)

	if _, err := pool.Exec(ctx, `delete from decision_log where seq = $1`, appended[1].Seq); err != nil {
		t.Fatalf("removing row %d: %v", appended[1].Seq, err)
	}

	broken := brokenBy(t, reader.Verify(ctx, owner))
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

// appendThreeOpenings appends three decision openings and checks the chain
// is whole before returning them, through a [decisionlog.Reader]'s Verify —
// which appends its own read event first, so a caller wanting an exact row
// count reads the log again afterwards rather than relying on this count.
func appendThreeOpenings(ctx context.Context, t *testing.T, log *decisionlog.Writer, reader *decisionlog.Reader) []decisionlog.Row {
	t.Helper()
	var appended []decisionlog.Row
	for _, payload := range []string{"first", "second", "third"} {
		row, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
			Actor: gate, Payload: payload, FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
		})
		if err != nil {
			t.Fatalf("AppendDecisionOpen(%q): %v", payload, err)
		}
		appended = append(appended, row)
	}
	if err := reader.Verify(ctx, owner); err != nil {
		t.Fatalf("the chain is broken before anything tampered with it: %v", err)
	}
	return appended
}
