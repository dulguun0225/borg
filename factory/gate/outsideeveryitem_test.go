// The rows that belong to no item: a role prompt or a skill, the three
// withdrawals, and the shortening of decision-log retention. What they share is
// that they name no item, read no threshold, and wait on a human always; what
// separates them is what each one's subject is.
package gate_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/gate"
)

// TestARolePromptRowIsPendingPerVersion: one gate on one subject has at most one
// pending row, and the subject of the row that decides a version of what an
// agent is told is that version — so two versions may each be under decision at
// once, and a second firing over one version already pending is refused.
func TestARolePromptRowIsPendingPerVersion(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	store := artifact.NewStore(pool, token)
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: "fake-model-1"}
	forSpecAuthor, err := store.SubmitFleet(ctx, author, by, artifact.KindRolePrompt,
		"spec_author", "", "what the spec author is told", "")
	if err != nil {
		t.Fatalf("submitting the spec author's role prompt: %v", err)
	}
	forImplementer, err := store.SubmitFleet(ctx, author, by, artifact.KindRolePrompt,
		"implementer", "", "what the implementer is told", "")
	if err != nil {
		t.Fatalf("submitting the implementer's role prompt: %v", err)
	}

	first, err := g.Fire(ctx, gate.Firing{Row: gate.RolePromptOrSkill, ArtifactID: forSpecAuthor.ID})
	if err != nil {
		t.Fatalf("firing the row over the first version: %v", err)
	}
	if _, err := g.Fire(ctx, gate.Firing{Row: gate.RolePromptOrSkill, ArtifactID: forImplementer.ID}); err != nil {
		t.Fatalf("firing the row over a second version while the first is pending: %v", err)
	}
	if _, err := g.Fire(ctx, gate.Firing{
		Row: gate.RolePromptOrSkill, ArtifactID: forSpecAuthor.ID,
	}); !errors.Is(err, gate.ErrRowPending) {
		t.Errorf("firing the row a second time over one version = %v, want ErrRowPending", err)
	}
	if first.ArtifactID != forSpecAuthor.ID {
		t.Errorf("the open event names version %q, want the one under decision", first.ArtifactID)
	}
}

// TestARowThatDecidesARecordIsPendingPerRecord: one gate on one subject has at
// most one pending row, and the subject of a row that decides a record is that
// record — so a withdrawal of one safeguard does not stop a second safeguard's
// being decided, and a second firing over the one already pending is refused.
func TestARowThatDecidesARecordIsPendingPerRecord(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, _, g := newGate(t, s, p)

	first := "sfg_0000000000000000000000000000000a"
	second := "sfg_0000000000000000000000000000000b"
	opened, err := g.Fire(ctx, gate.Firing{Row: gate.SafeguardWithdrawal, RecordID: first})
	if err != nil {
		t.Fatalf("firing the row over the first safeguard: %v", err)
	}
	if opened.Subject.RecordID != first {
		t.Errorf("the open event names record %q, want the safeguard under decision", opened.Subject.RecordID)
	}
	if _, err := g.Fire(ctx, gate.Firing{Row: gate.SafeguardWithdrawal, RecordID: second}); err != nil {
		t.Fatalf("firing the row over a second safeguard while the first is pending: %v", err)
	}
	if _, err := g.Fire(ctx, gate.Firing{
		Row: gate.SafeguardWithdrawal, RecordID: first,
	}); !errors.Is(err, gate.ErrRowPending) {
		t.Errorf("firing the row a second time over one safeguard = %v, want ErrRowPending", err)
	}

	// The row names the record it decides and no other row does: a firing at a
	// row on an item's path that named one is refused before anything is
	// appended.
	if _, err := g.Fire(ctx, gate.Firing{
		Row: gate.HaltWithdrawal,
	}); !errors.Is(err, gate.ErrFiringIncomplete) {
		t.Errorf("a halt's withdrawal naming no halt = %v, want ErrFiringIncomplete", err)
	}
	named := mergeFiring
	named.RecordID = first
	if _, err := g.Fire(ctx, named); !errors.Is(err, gate.ErrFiringIncomplete) {
		t.Errorf("a merge row naming a record = %v, want ErrFiringIncomplete", err)
	}
}

// TestAWithdrawalRowDoesNotRouteToTheHumanWhoWroteIt: the actor on a withdrawal
// is never the human its row waits on, so a close by them is refused where
// another decider exists.
func TestAWithdrawalRowDoesNotRouteToTheHumanWhoWroteIt(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	// The safeguard's own routing names a duty two humans hold, and the human who
	// wrote the withdrawal is one of them.
	declares(t, ctx, pool, token, owner, author.Key, gate.DutyConfirmTheCriteria)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyConfirmTheCriteria)

	opened, err := g.Fire(ctx, gate.Firing{
		Row:      gate.SafeguardWithdrawal,
		RecordID: "sfg_0000000000000000000000000000000a",
		RoutedTo: gate.RoutedTo{
			Duty: gate.DutyConfirmTheCriteria, NotHuman: author.Key,
		},
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides || !slices.Contains(opened.Marks, gate.MarkWithdrawalRow) {
		t.Fatalf("the row decides %v with marks %v, want a human always", opened.HumanDecides, opened.Marks)
	}
	if opened.WaitsOn.NotHuman != author.Key {
		t.Errorf("the open event bars %q, want the human who wrote the withdrawal", opened.WaitsOn.NotHuman)
	}

	if _, err := g.Decide(ctx, opened, gate.Given{
		Actor: author, Verdict: gate.VerdictApprove,
	}); !errors.Is(err, gate.ErrClosedByTheActor) {
		t.Fatalf("the writer closing their own withdrawal = %v, want ErrClosedByTheActor", err)
	}
	closed, err := g.Decide(ctx, opened, gate.Given{Actor: second, Verdict: gate.VerdictApprove})
	if err != nil {
		t.Fatalf("the other holder closing it: %v", err)
	}
	if closed.SelfApproval {
		t.Errorf("a close by the other holder carries the self-approval field")
	}
}

// TestAWithdrawalRowStillFiresWhereTheTwoAreOnePerson: an install with one owner
// cannot separate who wrote a withdrawal from who decides it, so the row still
// fires, the decision is still written, and the close says what it is.
func TestAWithdrawalRowStillFiresWhereTheTwoAreOnePerson(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, _, g := newGate(t, s, p)

	// Nobody holds a duty and the safeguard names no human, so the row widens to
	// the owner — who is also the human who wrote the withdrawal.
	opened, err := g.Fire(ctx, gate.Firing{
		Row:      gate.HaltWithdrawal,
		RecordID: "hlt_0000000000000000000000000000000a",
		RoutedTo: gate.RoutedTo{NotHuman: owner.Key},
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.WaitsOn.TheOwner() {
		t.Fatalf("a halt's withdrawal waits on %+v, want the owner", opened.WaitsOn)
	}

	closed, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove})
	if err != nil {
		t.Fatalf("the one owner closing the row: %v", err)
	}
	if !closed.SelfApproval {
		t.Errorf("the close does not say the writer decided it: %+v", closed)
	}
}

// TestALegalHoldsWithdrawalIsARowOfItsOwn: a legal hold ends only at a gate row
// of its own, held by a human always and routed away from the human who wrote
// the withdrawal.
func TestALegalHoldsWithdrawalIsARowOfItsOwn(t *testing.T) {
	if !slices.Contains(gate.Kinds, gate.KindLegalHoldWithdrawal) {
		t.Fatal("a legal hold's withdrawal is not among the rows")
	}
	if gate.LegalHoldWithdrawal.DecidesAnItem() || gate.LegalHoldWithdrawal.ReadsAThreshold() {
		t.Errorf("the row decides an item or reads a threshold, and it does neither")
	}
	actions, err := gate.Actions(gate.LegalHoldWithdrawal)
	if err != nil {
		t.Fatalf("Actions: %v", err)
	}
	if len(actions) != 3 || slices.Contains(actions, gate.VerdictHold) {
		t.Errorf("the row offers %v, want approve, reject and refer — a hold would name a state it is already in", actions)
	}

	s, p := &fakeScore{assessment: assessed(0)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyConfirmTheCriteria)

	opened, err := g.Fire(ctx, gate.Firing{
		Row:      gate.LegalHoldWithdrawal,
		RecordID: "lgh_0000000000000000000000000000000a",
		RoutedTo: gate.RoutedTo{Duty: gate.DutyConfirmTheCriteria, NotHuman: author.Key},
	})
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides || opened.Assessment.Vector != nil {
		t.Errorf("the row decides %v with a vector of %v, want a human always and no factor set",
			opened.HumanDecides, opened.Assessment.Vector)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{
		Actor: author, Verdict: gate.VerdictApprove,
	}); !errors.Is(err, gate.ErrClosedByTheActor) {
		t.Errorf("the writer closing their own withdrawal = %v, want ErrClosedByTheActor", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: second, Verdict: gate.VerdictApprove}); err != nil {
		t.Errorf("the holder the row routes to closing it: %v", err)
	}
}
