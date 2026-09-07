// The shortening of decision-log retention as this interface decides it — a
// value written pending, a row routed away from whoever wrote it, and the
// authors whose priors the cut would restart — and the retention pass that
// enforces the value afterwards.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestAShorteningIsDecidedAtARowRoutedAwayFromWhoeverWroteIt: shortening
// decision-log retention removes a protection, so it is decided and not merely
// written. The value is written pending as a record of its own naming its
// author, the row that decides it is routed away from that author, and nothing
// is in force until the row closes.
func TestAShorteningIsDecidedAtARowRoutedAwayFromWhoeverWroteIt(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	keeper := shortenAsKeeper(t, ctx, d, s)
	settings, err := factorysettings.Get(ctx, d.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.DecisionLogRetentionSeconds.Present {
		t.Fatal("the shorter value is in force with nothing having decided it")
	}

	written := pendingShortening(t, ctx, d.pool)
	if written.Seconds != 1 || written.Approved {
		t.Fatalf("the pending shortening is %+v, want one second and unapproved", written)
	}
	if written.Actor.Key != keeper.Key {
		t.Fatalf("the shortening names %q as its author, want the human who wrote the value, %q",
			written.Actor.Key, keeper.Key)
	}

	s.mustCall(t, "decideRecordRow", screens.DecideRecordRowArgs{
		RowKind: gate.DecisionLogRetentionShortening.String(), RecordID: written.ID,
		Verdict: string(gate.VerdictApprove), OpenedInWorkAt: theOpenedInWorkAt,
	})
	settings, err = factorysettings.Get(ctx, d.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !settings.DecisionLogRetentionSeconds.Present || settings.DecisionLogRetentionSeconds.Number != 1 {
		t.Errorf("the retention in force is %+v after the row closed, want the one second it decided",
			settings.DecisionLogRetentionSeconds)
	}

	// The row named the record it decides and barred the human who wrote the
	// value, which is what "routed to a human other than the one who authored
	// the shorter value" comes to where the row names no duty.
	opening := shorteningRow(t, ctx, d.pool, d.token)
	if opening.RecordID != settings.ID {
		t.Errorf("the open event names record %q, want the factory-wide settings record", opening.RecordID)
	}
	if opening.WaitsOn.NotHuman != written.Actor.Key || written.Actor.Key == "" {
		t.Errorf("the row bars %q, want the human who wrote the value, %q",
			opening.WaitsOn.NotHuman, written.Actor.Key)
	}
	if !opening.HumanDecides {
		t.Error("the row auto-passed, and a human is at it always")
	}
	// The one field only a screen fills: when the actor opened the row in Work.
	if closing := shorteningClose(t, ctx, d.pool, d.token); closing.OpenedInWorkAt != theOpenedInWorkAt {
		t.Errorf("the close event says the row was opened in Work at %q, want %q",
			closing.OpenedInWorkAt, theOpenedInWorkAt)
	}
}

// theOpenedInWorkAt is what a screen reports as the moment the actor opened the
// row, which is the one field on a close event no other caller has ever filled.
const theOpenedInWorkAt = "2026-09-07T00:00:00.000000000Z"

// shortenAsKeeper authors a decision-log retention of one second as a human
// other than the owner, so the row that decides it is routed away from
// somebody the test can then decide it as. It answers with that human.
//
// The duty is declared first because an acting call is refused on a People row
// holding no duty, no obligation and no lent credential.
func shortenAsKeeper(t *testing.T, ctx context.Context, d deps, s *screenServer) record.Actor {
	t.Helper()
	keeper := owner(t, ctx, d.pool, d.token, "keeper")
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: keeper.Key, Duty: 1})
	was := s.principal
	s.principal = keeper.Key
	s.mustCall(t, "authorParameter", screens.AuthorParameterArgs{
		Parameter: "decision_log_retention", Value: "1",
	})
	s.principal = was
	return keeper
}

// TestTheRetentionPassNamesTheValuesAuthorAndTheVersionsInForce: the truncation
// row names who authored the retention value, the value it enforced, the
// boundary it cut to, and the two versions in force at the cut — and the
// boundary is checked against the value, so the row's claim is one the cut
// obeyed.
func TestTheRetentionPassNamesTheValuesAuthorAndTheVersionsInForce(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	shortenAsKeeper(t, ctx, d, s)
	written := pendingShortening(t, ctx, d.pool)
	s.mustCall(t, "decideRecordRow", screens.DecideRecordRowArgs{
		RowKind: gate.DecisionLogRetentionShortening.String(), RecordID: written.ID,
		Verdict: string(gate.VerdictApprove),
	})

	rows := logRows(t, ctx, d.pool, d.token)
	fresh := rows[len(rows)-1].ID
	// Both cuts are made inside one handover of the lease: the pass is a
	// subcommand and one process holds the lease at a time, so the fixture
	// gives it up and takes it back with a token that fences the one before.
	throughASubcommand(t, ctx, &d, func() error {
		// A boundary written a moment ago is inside the value in force, and
		// the cut that named it would remove rows that value keeps.
		if err := truncateCommand([]string{"-boundary", fresh}); !errors.Is(err,
			decisionlog.ErrBoundaryInsideTheRetention) {
			return fmt.Errorf("cutting to a row inside the retention = %v, want ErrBoundaryInsideTheRetention", err)
		}
		time.Sleep(1100 * time.Millisecond)
		return truncateCommand([]string{"-boundary", fresh, "-human", "owner"})
	})

	var truncation decisionlog.Row
	for _, row := range logRows(t, ctx, d.pool, d.token) {
		if row.Shape == decisionlog.ShapeTruncation {
			truncation = row
		}
	}
	if truncation.ID == "" {
		t.Fatal("the log holds no truncation row, and that row is what says a cut happened and where")
	}
	if truncation.PolicyVersion == "" || truncation.ScoreVersion == "" {
		t.Errorf("the truncation names policy version %q and score version %q, want both in force at the cut",
			truncation.PolicyVersion, truncation.ScoreVersion)
	}
	if truncation.Actor.Key != written.Actor.Key {
		t.Errorf("the truncation names %q, want the human who authored the value, %q — the pass was run by owner",
			truncation.Actor.Key, written.Actor.Key)
	}
	var cut decisionlog.Cut
	if err := json.Unmarshal([]byte(truncation.Payload), &cut); err != nil {
		t.Fatalf("reading the cut: %v", err)
	}
	if cut.RetentionSeconds != 1 || cut.Boundary != fresh {
		t.Errorf("the cut enforced %d second(s) to %s, want the one second it was authored at, to %s",
			cut.RetentionSeconds, cut.Boundary, fresh)
	}
}

// TestTheRowNamesThePriorsTheCutWouldRestart: the row that decides a shortening
// names each author whose per-author prior stands drifted and whose held-out
// decisions the cut would remove, which is the prior the score restarts when
// those decisions go. Both halves are needed: an author whose prior does not
// stand drifted is not named, and neither is one whose decisions the cut leaves.
func TestTheRowNamesThePriorsTheCutWouldRestart(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)
	token := testToken(t, ctx, pool)

	// A version somebody authored, and a held-out decision over it in the log.
	// The firing that would write one is a run's, and what this reads is the
	// row it leaves, so the row is written here directly.
	store := artifact.NewStore(pool, token)
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: "fake-model-1"}
	version, err := store.SubmitFleet(ctx, decompositionActor, by, artifact.KindRolePrompt,
		"spec_author", "", "what the spec author is told", "")
	if err != nil {
		t.Fatalf("submitting the version: %v", err)
	}
	payload, err := json.Marshal(gate.OpeningPayload{
		OpenEvent: score.OpenEvent{
			Gate: gate.RolePromptOrSkill.String(), ArtifactID: version.ID, HeldOut: true,
		},
	})
	if err != nil {
		t.Fatalf("marshalling the opening: %v", err)
	}
	if _, err := decisionlog.NewWriter(pool, token).AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Key: "gate", Basis: record.BasisClaimed},
		Payload:       string(payload),
		FormatVersion: "decision/1",
		PolicyVersion: "pv_00000000000000000000000000000001",
		ScoreVersion:  "scv_0000000000000000000000000000001",
	}); err != nil {
		t.Fatalf("appending the held-out decision: %v", err)
	}

	asOwner, err := humanNamed(ctx, pool, token, "owner")
	if err != nil {
		t.Fatalf("humanNamed: %v", err)
	}
	// The cut a value of one second permits reaches back to a moment ago, so
	// the row above has to be older than that before it is one the cut removes.
	time.Sleep(1100 * time.Millisecond)

	drifted := score.Version{Drift: []score.Drift{{Author: "fake-model-1"}}}
	named, err := priorsRestartedBy(ctx, pool, token, drifted, asOwner, 1)
	if err != nil {
		t.Fatalf("priorsRestartedBy: %v", err)
	}
	if len(named) != 1 || named[0] != "fake-model-1" {
		t.Errorf("the row names %v, want the author whose prior stands drifted and whose decisions go", named)
	}

	// An author whose prior does not stand drifted is not named, however much
	// of their evidence the cut removes.
	steady, err := priorsRestartedBy(ctx, pool, token, score.Version{}, asOwner, 1)
	if err != nil {
		t.Fatalf("priorsRestartedBy: %v", err)
	}
	if len(steady) != 0 {
		t.Errorf("the row names %v under a version that found no prior drifted", steady)
	}

	// And neither is one whose decisions the cut leaves: a value reaching back
	// a day keeps the row above.
	kept, err := priorsRestartedBy(ctx, pool, token, drifted, asOwner, 24*3600)
	if err != nil {
		t.Fatalf("priorsRestartedBy: %v", err)
	}
	if len(kept) != 0 {
		t.Errorf("the row names %v for a cut that removes none of their decisions", kept)
	}
}

// pendingShortening is the shortening the authoring call wrote, read out of
// its own table: package factorysettings has no read that lists them, there
// being no caller for one but this.
func pendingShortening(t *testing.T, ctx context.Context, pool *pgxpool.Pool) factorysettings.Shortening {
	t.Helper()
	var id string
	if err := pool.QueryRow(ctx, `select id from `+factorysettings.ShorteningTable+
		` where not approved order by at desc limit 1`).Scan(&id); err != nil {
		t.Fatalf("reading the shortening that was written: %v", err)
	}
	written, err := factorysettings.GetShortening(ctx, pool, id)
	if err != nil {
		t.Fatalf("GetShortening: %v", err)
	}
	return written
}

// shorteningRow is the open event of the row that decided the shortening.
func shorteningRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) gate.OpeningPayload {
	t.Helper()
	for _, row := range logRows(t, ctx, pool, token) {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		var opening gate.OpeningPayload
		if json.Unmarshal([]byte(row.Payload), &opening) != nil {
			continue
		}
		if opening.Gate == gate.DecisionLogRetentionShortening.String() {
			return opening
		}
	}
	t.Fatal("the log holds no opening at the row that decides a shortening")
	return gate.OpeningPayload{}
}

// shorteningClose is the close event of the row that decided the shortening,
// found by the opening it closes: the close event's payload names no row, so
// the opening is what says which row this is. When the actor opened the row in
// Work is a column of the log's own row and not a field of the payload, which
// is why this answers the row.
func shorteningClose(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) decisionlog.Row {
	t.Helper()
	rows := logRows(t, ctx, pool, token)
	openingID := ""
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		var opening gate.OpeningPayload
		if json.Unmarshal([]byte(row.Payload), &opening) != nil {
			continue
		}
		if opening.Gate == gate.DecisionLogRetentionShortening.String() {
			openingID = row.ID
		}
	}
	for _, row := range rows {
		if row.Part == decisionlog.PartClose && row.Closes == openingID && openingID != "" {
			return row
		}
	}
	t.Fatal("the log holds no close event at the row that decides a shortening")
	return decisionlog.Row{}
}

// logRows is every row of the log, read with the token the caller holds:
// every read appends a read event of its own, which is a write and is fenced,
// so a read made after a subcommand has taken and given back the lease reads
// with the token the fixture took after it.
func logRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) []decisionlog.Row {
	t.Helper()
	rows, err := decisionlog.NewReader(pool, token).
		Read(ctx, asPrincipal(owner(t, ctx, pool, token, "owner")))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return rows
}
