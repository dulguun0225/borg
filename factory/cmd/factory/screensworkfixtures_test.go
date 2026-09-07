// One human's acts at Work, each made through the call a screen makes and
// each read back from the view that human would be looking at: the pass the
// factory runs between two verdicts, the approve, the acknowledgement, the
// edit in place, and the take-over of an item the factory gave up on.
package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// pass is one pass of the path, which is what serve runs on its own interval
// between the verdicts a human gives at Work. It answers whether the pass
// moved anything.
func (s *screenServer) pass(t *testing.T, ctx context.Context, out *bytes.Buffer) bool {
	t.Helper()
	moved, escalated := s.passAllowingEscalation(t, ctx, out)
	if escalated {
		t.Fatalf("the factory gave up on an item where this pass was to make progress\noutput so far:\n%s", out)
	}
	return moved
}

// passAllowingEscalation is [screenServer.pass] where the factory giving up is
// what the caller is waiting for: the pass reports the item escalated, its open
// rows are abandoned, and duty 12 takes it over at Work.
func (s *screenServer) passAllowingEscalation(t *testing.T, ctx context.Context, out *bytes.Buffer) (moved, escalated bool) {
	t.Helper()
	a, err := s.p.advance(ctx)
	if errors.Is(err, dispatch.ErrOutOfAttempts) {
		return a.moved, true
	}
	if err != nil {
		t.Fatalf("the pass: %v\noutput so far:\n%s", err, out)
	}
	return a.moved, false
}

// roundWaitingOn is the round of the interview one intent is waiting on.
//
// It is read from the records and not from a view: the badge counts an open
// round and no address carries the question or its id, so the id a human types
// at Work is one they were given somewhere this product does not serve.
func roundWaitingOn(t *testing.T, ctx context.Context, p *path, intentID string) intent.Question {
	t.Helper()
	waiting, found, err := unansweredRound(ctx, p.d.pool, intentID)
	if err != nil {
		t.Fatalf("reading the round intent %s waits on: %v", intentID, err)
	}
	if !found {
		t.Fatalf("intent %s waits on no round of the interview", intentID)
	}
	return waiting
}

// boardWaitingOnAHuman is the Work board narrowed to what waits on a human,
// which is the home view's own filter.
func boardWaitingOnAHuman(t *testing.T, s *screenServer) screens.Work {
	t.Helper()
	var board screens.Work
	s.get(t, "/api/work?waiting_on_a_human=true", &board)
	return board
}

// rowOn is one item's row of a board, and whether the board holds one.
func rowOn(board screens.Work, itemID string) (screens.WorkRow, bool) {
	for _, row := range board.Rows {
		if row.ItemID == itemID {
			return row, true
		}
	}
	return screens.WorkRow{}, false
}

// theOneItemWaiting is the item the board shows waiting, which is the one this
// episode's intent was decomposed into.
func theOneItemWaiting(t *testing.T, s *screenServer) string {
	t.Helper()
	board := boardWaitingOnAHuman(t, s)
	if len(board.Rows) != 1 {
		t.Fatalf("the board holds %d row(s) waiting on a human, want the one item: %+v", len(board.Rows), board.Rows)
	}
	if board.Rows[0].Waiting == "" {
		t.Errorf("the board's one row says it waits on nothing: %+v", board.Rows[0])
	}
	return board.Rows[0].ItemID
}

// summaryOn is one row of an item's timeline, by the open event it was
// rendered from.
func summaryOn(t *testing.T, s *screenServer, itemID, openEventID string) screens.DecisionSummary {
	t.Helper()
	var view screens.Item
	s.get(t, "/api/item/"+itemID, &view)
	for _, one := range view.Decisions {
		if one.OpenEventID == openEventID {
			return one
		}
	}
	t.Fatalf("the timeline of item %s holds no row %s: %+v", itemID, openEventID, view.Decisions)
	return screens.DecisionSummary{}
}

// approveAtWork is one row read where a human reads it and then approved,
// carrying when the actor opened the row in Work — the one field no terminal
// could fill. What it asserts is the pair of readings a human takes around one
// verdict: the row pending at its own address and on the board, and the same
// row closed at both once the call has answered.
func approveAtWork(t *testing.T, s *screenServer, opened gate.Opened, itemID string) {
	t.Helper()
	openEventID := opened.Row.ID
	var before screens.Decision
	s.get(t, "/api/decision/"+openEventID, &before)
	if before.ItemID != itemID || before.GateRow != opened.Gate.String() {
		t.Errorf("the row at %s reads item %q and row %q, want %q and its own", opened.Gate, before.ItemID, before.GateRow, itemID)
	}
	if before.Closed != nil || before.Abandoned != nil {
		t.Fatalf("the row at %s is not pending when it is about to be decided: %+v", opened.Gate, before)
	}
	if before.OpenedInWorkAt != "" {
		t.Errorf("the row at %s carries when it was opened in Work before any verdict, and that arrives with the close", opened.Gate)
	}
	if len(before.Vector) == 0 {
		t.Errorf("the row at %s carries no vector, and the vector is what a human decides on", opened.Gate)
	}
	if row, on := rowOn(boardWaitingOnAHuman(t, s), itemID); !on {
		t.Errorf("the board shows no row for item %s while it waits at %s", itemID, opened.Gate)
	} else if !strings.Contains(row.Waiting, opened.Gate.String()) {
		t.Errorf("the board says the item waits on %q, want the %s row it is pending at", row.Waiting, opened.Gate)
	}

	openedInWork := record.FormatTime(time.Now().Add(-90 * time.Second))
	s.mustCall(t, "decide", screens.DecideArgs{
		OpenEventID: openEventID, Verdict: string(gate.VerdictApprove), OpenedInWorkAt: openedInWork,
	})

	var after screens.Decision
	s.get(t, "/api/decision/"+openEventID, &after)
	if after.Closed == nil || after.Closed.Verdict != string(gate.VerdictApprove) {
		t.Fatalf("the row at %s reads %+v after the verdict, want it closed as approve", opened.Gate, after.Closed)
	}
	// The name and not the key: every human-facing field of a view carries the
	// name the People mapping gives the key, and only a field named for the key
	// carries one.
	if after.Closed.Actor != s.p.d.human {
		t.Errorf("the close at %s names actor %q, want the human at the screen", opened.Gate, after.Closed.Actor)
	}
	if after.OpenedInWorkAt != openedInWork {
		t.Errorf("the row at %s reads opened_in_work_at %q, want the %q the screen sent", opened.Gate, after.OpenedInWorkAt, openedInWork)
	}
	summary := summaryOn(t, s, itemID, openEventID)
	if summary.Verdict != string(gate.VerdictApprove) || summary.OpenedInWorkAt != openedInWork {
		t.Errorf("the timeline reads %s as verdict %q opened in Work at %q, want approve and %q",
			opened.Gate, summary.Verdict, summary.OpenedInWorkAt, openedInWork)
	}
	if summary.Score == nil {
		t.Errorf("the timeline carries no number beside the verdict at %s, and the number arrives with the close", opened.Gate)
	}
}

// acknowledgeAtWork is a holder saying they have a row. It decides nothing: the
// row stays pending and in front of every other holder, which is what the
// readings after it are for.
func acknowledgeAtWork(t *testing.T, s *screenServer, opened gate.Opened, itemID string) {
	t.Helper()
	openEventID := opened.Row.ID
	s.mustCall(t, "acknowledge", screens.AcknowledgeArgs{OpenEventID: openEventID})

	var view screens.Decision
	s.get(t, "/api/decision/"+openEventID, &view)
	if view.Closed != nil || view.Abandoned != nil {
		t.Fatalf("the row at %s is no longer pending after an acknowledgement, which decides nothing: %+v", opened.Gate, view)
	}
	if len(view.Acknowledgements) != 1 || view.Acknowledgements[0].HumanKey != s.p.human.Key {
		t.Errorf("the row reads acknowledgements %+v, want the one the human at the screen made", view.Acknowledgements)
	}
	if _, on := rowOn(boardWaitingOnAHuman(t, s), itemID); !on {
		t.Error("the board dropped the item after one holder acknowledged its row, and the row stays in front of the rest")
	}
	if len(summaryOn(t, s, itemID, openEventID).Acknowledgements) != 1 {
		t.Error("the item's timeline does not show the acknowledgement")
	}
}

// editThePlanAtWork is duty 11: the implementation plan the factory could not
// produce properly, written together with it at the gate. The version the human
// typed supersedes the one under decision, the row it was open at is abandoned,
// and the row fires again over what was authored.
func editThePlanAtWork(t *testing.T, s *screenServer, opened gate.Opened, itemID string) {
	t.Helper()
	openEventID := opened.Row.ID
	before := newestVersionOfKind(t, s, itemID, artifact.KindImplementationPlan)
	s.mustCall(t, "editInPlace", screens.EditInPlaceArgs{
		OpenEventID: openEventID, VersionText: theEditedPlan,
	})

	var superseded screens.Decision
	s.get(t, "/api/decision/"+openEventID, &superseded)
	if superseded.Abandoned == nil {
		t.Fatalf("the row the edit superseded reads %+v, want it abandoned with no verdict coming", superseded)
	}
	if superseded.Closed != nil {
		t.Errorf("the row the edit superseded carries a verdict: %+v", superseded.Closed)
	}
	if after := newestVersionOfKind(t, s, itemID, artifact.KindImplementationPlan); after.ID == before.ID {
		t.Errorf("the timeline still shows version %s at the implementation plan, want the one the human typed", before.ID)
	}
	// The row fired again over the new version, which is what makes an edit in
	// place an authoring and not a verdict.
	again := false
	pending, err := s.p.gate.Pending(context.Background())
	if err != nil {
		t.Fatalf("reading the pending rows after the edit: %v", err)
	}
	for _, one := range pending {
		if one.Subject.ItemID == itemID && one.Gate.Kind == gate.KindImplementationPlan && one.Row.ID != openEventID {
			again = true
		}
	}
	if !again {
		t.Error("no implementation plan row is pending over the version the human authored")
	}
}

// newestVersionOfKind is the version of one kind an item's timeline shows. It
// is one and never a history: the timeline shows the newest version of each
// kind, so a version an edit in place superseded is not on it and what says the
// edit was taken is the identifier changing.
func newestVersionOfKind(t *testing.T, s *screenServer, itemID string, kind artifact.Kind) screens.ArtifactVersion {
	t.Helper()
	var view screens.Item
	s.get(t, "/api/item/"+itemID, &view)
	for _, one := range view.Versions {
		if one.Kind == string(kind) {
			return one
		}
	}
	t.Fatalf("the timeline of item %s shows no %s version: %+v", itemID, kind, view.Versions)
	return screens.ArtifactVersion{}
}

// takeOverAtWork is duty 12: the item the factory gave up on, shown on the
// board as escalated with a stage to return it to, and taken over there.
func takeOverAtWork(t *testing.T, s *screenServer, itemID string) {
	t.Helper()
	row, on := rowOn(boardWaitingOnAHuman(t, s), itemID)
	if !on {
		t.Fatalf("the board shows no row for item %s, and the factory has given up on it", itemID)
	}
	if row.Stage != string(item.StageEscalated) {
		t.Errorf("the board reads the item at stage %q, want it escalated", row.Stage)
	}
	if !strings.Contains(row.Waiting, "duty 12") {
		t.Errorf("the board says the item waits on %q, want the take-over duty 12 holds", row.Waiting)
	}
	var home screens.Home
	s.get(t, "/api/home", &home)
	if home.Badge.Escalations != 1 {
		t.Errorf("the badge counts %d escalation(s), want the one the factory gave up on: %+v", home.Badge.Escalations, home.Badge)
	}

	s.mustCall(t, "takeOver", screens.TakeOverArgs{
		ItemID: itemID, Stage: string(item.StageImplementation),
	})

	var board screens.Work
	s.get(t, "/api/work", &board)
	taken, on := rowOn(board, itemID)
	if !on || taken.Stage != string(item.StageImplementation) {
		t.Errorf("the item reads %+v after the take-over, want it back at the stage it was returned to", taken)
	}
	if _, still := rowOn(boardWaitingOnAHuman(t, s), itemID); still {
		t.Error("the item still waits on a human after it was taken over, and the take-over is what clears that row")
	}
	s.get(t, "/api/home", &home)
	if home.Badge.Escalations != 0 {
		t.Errorf("the badge counts %d escalation(s) after the take-over: %+v", home.Badge.Escalations, home.Badge)
	}
}
