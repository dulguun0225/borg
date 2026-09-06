// The shortening of decision-log retention as this interface decides it — a
// value written pending, a row routed away from whoever wrote it, and the
// authors whose priors the cut would restart — and the retention pass that
// enforces the value afterwards.
package main

import (
	"context"
	"encoding/json"
	"errors"
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
)

// TestAShorteningIsDecidedAtARowRoutedAwayFromWhoeverWroteIt: shortening
// decision-log retention removes a protection, so it is decided and not merely
// written. The value is written pending as a record of its own naming its
// author, the row that decides it is routed away from that author, and nothing
// is in force until the row closes.
func TestAShorteningIsDecidedAtARowRoutedAwayFromWhoeverWroteIt(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	if err := authorCommand([]string{
		"-parameter", "decision_log_retention", "-value", "1", "-human", "keeper",
	}); err != nil {
		t.Fatalf("author decision_log_retention: %v", err)
	}
	settings, err := factorysettings.Get(ctx, pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if settings.DecisionLogRetentionSeconds.Present {
		t.Fatal("the shorter value is in force with nothing having decided it")
	}

	written := pendingShortening(t, ctx, pool)
	if written.Seconds != 1 || written.Approved {
		t.Fatalf("the pending shortening is %+v, want one second and unapproved", written)
	}

	if err := approveCommand([]string{"-retention-shortening", written.ID, "-human", "owner"}); err != nil {
		t.Fatalf("approve -retention-shortening: %v", err)
	}
	settings, err = factorysettings.Get(ctx, pool)
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
	opening := shorteningRow(t, ctx, pool)
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
}

// TestTheRetentionPassNamesTheValuesAuthorAndTheVersionsInForce: the truncation
// row names who authored the retention value, the value it enforced, the
// boundary it cut to, and the two versions in force at the cut — and the
// boundary is checked against the value, so the row's claim is one the cut
// obeyed.
func TestTheRetentionPassNamesTheValuesAuthorAndTheVersionsInForce(t *testing.T) {
	ctx, pool := newOwner(t)
	install(t, ctx, pool)

	if err := authorCommand([]string{
		"-parameter", "decision_log_retention", "-value", "1", "-human", "keeper",
	}); err != nil {
		t.Fatalf("author decision_log_retention: %v", err)
	}
	written := pendingShortening(t, ctx, pool)
	if err := approveCommand([]string{"-retention-shortening", written.ID, "-human", "owner"}); err != nil {
		t.Fatalf("approve -retention-shortening: %v", err)
	}

	rows := logRows(t, ctx, pool)
	fresh := rows[len(rows)-1].ID
	// A boundary written a moment ago is inside the value in force, and the cut
	// that named it would remove rows that value keeps.
	if err := truncateCommand([]string{"-boundary", fresh}); !errors.Is(err,
		decisionlog.ErrBoundaryInsideTheRetention) {
		t.Fatalf("cutting to a row inside the retention = %v, want ErrBoundaryInsideTheRetention", err)
	}

	time.Sleep(1100 * time.Millisecond)
	if err := truncateCommand([]string{"-boundary", fresh, "-human", "owner"}); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	var truncation decisionlog.Row
	for _, row := range logRows(t, ctx, pool) {
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

// pendingShortening is the shortening the author subcommand wrote, read out of
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
func shorteningRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool) gate.OpeningPayload {
	t.Helper()
	for _, row := range logRows(t, ctx, pool) {
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

// logRows is every row of the log, read the way [policyVersions] reads the
// versions: the lease is taken again, each subcommand having taken one of its
// own for the life of the command and released it when it ended.
func logRows(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []decisionlog.Row {
	t.Helper()
	token, err := lease.Acquire(ctx, pool, defaultInstance(), -time.Second)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}
	rows, err := decisionlog.NewReader(pool, token).
		Read(ctx, asPrincipal(owner(t, ctx, pool, token, "owner")))
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return rows
}
