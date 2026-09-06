// What closes a firing: a human's verdict, the factory's own pass, and its own
// reject, each row's own actions, and the sample's one asymmetry.
package gate_test

import (
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAnAutoPassIsClosedByTheGateComponent is the milestone's own demonstration
// at the level of one row: the number is under the threshold, no safeguard adds
// a human, and the factory gives the verdict itself.
func TestAnAutoPassIsClosedByTheGateComponent(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.1)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.HumanDecides || len(opened.Marks) != 0 {
		t.Fatalf("a number of 0.1 against a threshold of 0.3 put a human at the row: %v", opened.Marks)
	}
	closing, err := g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	if closing.Actor.Kind != record.KindComponent || closing.Actor.Key != "gate.merge_to_master" {
		t.Errorf("the closing's actor is %s %q, want the gate component", closing.Actor.Kind, closing.Actor.Key)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictApprove) || payload.WhyItAutoPassed != score.AutoPassThreshold {
		t.Errorf("the closing says %+v, want an approve auto-passed by the threshold", payload)
	}

	// The open event of an auto-pass waits on nobody, which is what tells a
	// reader of the log that nothing was ever pending here.
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	var opening gate.OpeningPayload
	if err := json.Unmarshal([]byte(rowByID(t, rows, opened.Row.ID).Payload), &opening); err != nil {
		t.Fatalf("unmarshalling the opening payload: %v", err)
	}
	if opening.WaitsOn.Duty != 0 || opening.WaitsOn.Human != "" || len(opening.WaitsOn.Holders) != 0 {
		t.Errorf("an auto-passed firing waits on %+v, want nothing", opening.WaitsOn)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatalf("Verify after an auto-pass: %v", err)
	}
}

// TestASafeguardAddsAHumanWhateverTheNumberReads: a safeguard can only add. The
// number is well under the threshold and a human decides anyway, the mark says
// so, and the factory may not close the decision itself.
func TestASafeguardAddsAHumanWhateverTheNumberReads(t *testing.T) {
	safeguarded := applied(0.3)
	safeguarded.HumanBySafeguard = true
	safeguarded.Safeguards = []string{"sfg_00000000000000000000000000000001"}
	s, p := &fakeScore{assessment: assessed(0.05)}, &fakePolicy{applied: safeguarded}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides || !slices.Contains(opened.Marks, gate.MarkSafeguard) {
		t.Fatalf("the safeguard put no human at the row: human %v marks %v", opened.HumanDecides, opened.Marks)
	}
	if _, err := g.AutoPass(ctx, opened); !errors.Is(err, gate.ErrHumanDecides) {
		t.Fatalf("AutoPass over a firing a safeguard reached = %v, want ErrHumanDecides", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictApprove}); err != nil {
		t.Fatalf("Decide: %v", err)
	}
}

// TestBothReasonsAreToldApart: a number over the threshold and a safeguard at
// once each leave their own mark, because withdrawing the safeguard would not
// remove the human the number put there.
func TestBothReasonsAreToldApart(t *testing.T) {
	safeguarded := applied(0.3)
	safeguarded.HumanBySafeguard = true
	s, p := &fakeScore{assessment: assessed(0.9)}, &fakePolicy{applied: safeguarded}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !slices.Contains(opened.Marks, gate.MarkTheNumber) || !slices.Contains(opened.Marks, gate.MarkSafeguard) {
		t.Errorf("the firing's marks are %v, want both the number and the safeguard", opened.Marks)
	}
}

// TestEachRowOffersItsOwnActions: reject is available up to the merge to master
// and nowhere after it, and hold is offered by the deploy row alone.
func TestEachRowOffersItsOwnActions(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	merged, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire at the merge row: %v", err)
	}
	if _, err := g.Decide(ctx, merged, gate.Given{Actor: owner, Verdict: gate.VerdictHold}); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("holding at the merge row = %v, want ErrVerdictUnknown", err)
	}

	deployed, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire at the deploy row: %v", err)
	}
	if _, err := g.Decide(ctx, deployed, gate.Given{Actor: owner, Verdict: gate.VerdictReject, Reason: "no"}); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("rejecting at the deploy row = %v, want ErrVerdictUnknown", err)
	}
	if _, err := g.Decide(ctx, deployed, gate.Given{Actor: owner, Verdict: gate.Verdict("edit")}); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("an action neither row has = %v, want ErrVerdictUnknown", err)
	}
}

// TestAHoldCloses: a hold is the verdict of that firing's decision, with the
// human as the actor, and it says nothing about a stage to return to — the
// change is still good and the event is queued.
func TestAHoldCloses(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	closing, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictHold, Reason: "the dependency is not live"})
	if err != nil {
		t.Fatalf("Decide(hold): %v", err)
	}
	if closing.Actor != owner {
		t.Errorf("the hold's actor is %+v, want the human who set it", closing.Actor)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictHold) {
		t.Errorf("the closing says verdict %q, want a hold", payload.Verdict)
	}
	if payload.ReturnsTo != "" {
		t.Errorf("the hold says the item returns to %q, and a hold sends nothing back", payload.ReturnsTo)
	}
	if payload.WhyItAutoPassed != "" {
		t.Errorf("the hold says it was auto-passed by %q", payload.WhyItAutoPassed)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatalf("Verify after a hold: %v", err)
	}
}

// TestARejectNamesTheStageItReturnsTo: the merge row's reject sends the item to
// the nearest authoring stage above it, there being no stage of its own and none
// between.
func TestARejectNamesTheStageItReturnsTo(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	feedback := "the encoding of cr_0000000000000000000000000000000b asserts the code, not the criterion"
	closing, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictReject, Reason: feedback})
	if err != nil {
		t.Fatalf("Decide(reject, feedback): %v", err)
	}

	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictReject) || payload.Reason != feedback {
		t.Errorf("the closing says %+v", payload)
	}
	if payload.ReturnsTo != gate.ReturnsToImplementation {
		t.Errorf("the reject returns the item to %q, want %q", payload.ReturnsTo, gate.ReturnsToImplementation)
	}
}

// TestARejectWithoutReasonIsRefused: a reject and a hold each carry a reason, so
// one carrying none is refused and no close event is appended.
func TestARejectWithoutReasonIsRefused(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictReject}); !errors.Is(err, gate.ErrReasonMissing) {
		t.Fatalf("Decide(reject, no reason) = %v, want ErrReasonMissing", err)
	}

	// Fire's own check that nothing is already pending reads the log first,
	// which appends a read event ahead of the opening; this Read appends one
	// more, so the log holds three rows and not the one this firing appended.
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("the log holds %d rows after the refused reject, want 3", len(rows))
	}
}

// TestAutoRejectIsTheFactorysOwnAndIsAllowedOverAHuman: the factory may not approve
// over a human and it rejects before one is asked, which is the one asymmetry between
// the two calls.
func TestAutoRejectIsTheFactorysOwnAndIsAllowedOverAHuman(t *testing.T) {
	// A number over the threshold, so the firing puts a human at the row.
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatal("the firing put no human at the row, and this test is about rejecting over one")
	}
	if _, err := g.AutoPass(ctx, opened); !errors.Is(err, gate.ErrHumanDecides) {
		t.Fatalf("AutoPass over a human = %v, want ErrHumanDecides", err)
	}

	closing, err := g.AutoReject(ctx, opened, gate.AutoRejectedByContractDiff,
		"health.Detail is removed and the reader still declares it")
	if err != nil {
		t.Fatalf("AutoReject: %v", err)
	}
	if closing.Actor.Kind != record.KindComponent || closing.Actor.Key != "gate.merge_to_master" {
		t.Errorf("the close event was written as %s %s, want the gate component", closing.Actor.Kind, closing.Actor.Key)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictReject) || payload.AutoRejectedBy != gate.AutoRejectedByContractDiff {
		t.Fatalf("the close event reads %+v", payload)
	}
	if payload.Reason == "" || payload.ReturnsTo != gate.ReturnsToImplementation {
		t.Errorf("a mechanical reject carries a reason %q and returns to %q", payload.Reason, payload.ReturnsTo)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatalf("the chain does not verify after a mechanical reject: %v", err)
	}

	// A rejection that names no check is refused: it is only readable against the
	// check it came from.
	second, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("the second Fire: %v", err)
	}
	if _, err := g.AutoReject(ctx, second, "", "something"); !errors.Is(err, gate.ErrCheckMissing) {
		t.Errorf("a mechanical reject naming no check = %v, want ErrCheckMissing", err)
	}
	if _, err := g.AutoReject(ctx, second, gate.AutoRejectedByConsumerContract, ""); !errors.Is(err, gate.ErrCheckMissing) {
		t.Errorf("a mechanical reject saying nothing = %v, want ErrCheckMissing", err)
	}
	// And the production deploy row does not reject at all: by then the merge has
	// happened and the number is assigned.
	deploy, err := g.Fire(ctx, deployFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("firing the deploy row: %v", err)
	}
	if _, err := g.AutoReject(ctx, deploy, gate.AutoRejectedByConsumerContract, "anything"); !errors.Is(err, gate.ErrVerdictUnknown) {
		t.Errorf("a mechanical reject at the production deploy row = %v, want ErrVerdictUnknown", err)
	}
}

// TestTheVerdictsGateWritesAreTheOnesTheScoreReads holds two spellings together.
// The score reads a close event's verdict when it counts outcomes and cannot import
// this package, importing it the other way, so it declares the two words itself —
// and two packages naming one word are two able to disagree.
func TestTheVerdictsGateWritesAreTheOnesTheScoreReads(t *testing.T) {
	if string(gate.VerdictApprove) != score.VerdictApproved {
		t.Errorf("the gate writes %q and the score reads %q", gate.VerdictApprove, score.VerdictApproved)
	}
	if string(gate.VerdictReject) != score.VerdictRejected {
		t.Errorf("the gate writes %q and the score reads %q", gate.VerdictReject, score.VerdictRejected)
	}
}

// TestTheSampleRemovesTheNumbersHumanAndNoOtherIsTheOneAsymmetryHere: the gate
// asks the score's sample with the safeguard's answer and after the independent
// driftdetector's, so a held-out item passes the gate the number would have gated and
// neither of the other two.
func TestTheSampleRemovesTheNumbersHumanAndNoOtherIsTheOneAsymmetryHere(t *testing.T) {
	// Over the threshold and held out: no human, and the close event says the
	// sample passed it.
	s := &fakeScore{assessment: assessed(0.6), selection: score.Selection{HeldOut: true, Why: score.SelectedHere}}
	ctx, _, _, g := newGate(t, s, &fakePolicy{applied: applied(0.3)})
	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.HumanDecides {
		t.Errorf("a held-out firing put a human at the row: %v", opened.Marks)
	}
	if !opened.HeldOut || opened.WhyHeldOut != score.SelectedHere {
		t.Errorf("the firing reads held out %v because %q", opened.HeldOut, opened.WhyHeldOut)
	}
	if s.askedHoldOut != [2]bool{true, false} || s.askedItemID != mergeFiring.ItemID {
		t.Errorf("the gate asked the sample about %q with %v, want the item with the number over the threshold and no safeguard",
			s.askedItemID, s.askedHoldOut)
	}
	closing, err := g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.WhyItAutoPassed != score.AutoPassSample {
		t.Errorf("the close event says %q, want the sample", payload.WhyItAutoPassed)
	}

	// A safeguard adds a human whatever the sample answers, and the gate hands the
	// sample the safeguard's answer so that it cannot select at all.
	safeguarded := &fakeScore{assessment: assessed(0.1), selection: score.Selection{HeldOut: true}}
	safeguardedApplied := applied(0.3)
	safeguardedApplied.HumanBySafeguard = true
	ctx, _, _, g = newGate(t, safeguarded, &fakePolicy{applied: safeguardedApplied})
	opened, err = g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire over a row a safeguard reached: %v", err)
	}
	if !opened.HumanDecides {
		t.Error("a row a safeguard reached auto-passed")
	}
	if safeguarded.askedHoldOut != [2]bool{false, true} {
		t.Errorf("the gate asked the sample with %v, want the safeguard's answer", safeguarded.askedHoldOut)
	}

	// Under the threshold and held out: still held out — the selection is the
	// item's — and the close event says the threshold, because the score would have
	// passed this one anyway and it is evidence about no gate.
	under := &fakeScore{assessment: assessed(0.1), selection: score.Selection{HeldOut: true, Why: score.SelectedEarlier}}
	ctx, _, _, g = newGate(t, under, &fakePolicy{applied: applied(0.3)})
	opened, err = g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire under the threshold: %v", err)
	}
	if !opened.HeldOut {
		t.Error("an item selected earlier is not held out here")
	}
	closing, err = g.AutoPass(ctx, opened)
	if err != nil {
		t.Fatalf("AutoPass: %v", err)
	}
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.WhyItAutoPassed != score.AutoPassThreshold {
		t.Errorf("the close event says %q, want the threshold", payload.WhyItAutoPassed)
	}
}
