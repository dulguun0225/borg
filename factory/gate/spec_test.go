// Tests of the Spec row's own mechanical rejection — both directions over the
// requirement a criterion names, and the row a check belongs to — and of who
// the row waits on when the version under decision withdraws a protection.
package gate_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/score"
)

// TestAWithdrawnProtectionRoutesTheRowToTheHumanItsProvenanceNames: withdrawing
// a criterion whose provenance names an authority is a resolved factor at this
// row, and the decision goes to that human rather than to the duty's holders by
// default.
func TestAWithdrawnProtectionRoutesTheRowToTheHumanItsProvenanceNames(t *testing.T) {
	assessment := assessed(0.1)
	assessment.Resolved = []score.Resolution{{
		Factor: "context.protection_withdrawn", Cause: score.CauseProtectionWithdrawn,
		Why:      "the version under decision withdraws a criterion whose provenance names an authority",
		RoutedTo: "person:the-confirmer",
	}}
	s, p := &fakeScore{assessment: assessment}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)
	// Somebody else holds the row's duty, so a row that read the duty alone
	// would wait on them and not on the human the provenance names.
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyConfirmTheCriteria)

	opened, err := g.Fire(ctx, specBy(t, ctx, pool, token, author, "it_0000000000000000000000000000000a"))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatalf("the row auto-passed, and a resolved factor puts a human there whatever the number")
	}
	if opened.WaitsOn.Human != "person:the-confirmer" {
		t.Errorf("the row waits on %+v, want the human the provenance names", opened.WaitsOn)
	}

	// A firing whose vector resolved nothing waits on the duty the design names
	// for the row, which is what every other Spec firing already does.
	plain := &fakeScore{assessment: assessed(0.1)}
	ctx, pool, token, g = newGate(t, plain, p)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyConfirmTheCriteria)
	opened, err = g.Fire(ctx, specBy(t, ctx, pool, token, author, "it_0000000000000000000000000000000a"))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.WaitsOn.Human != "" {
		t.Errorf("a firing that resolved nothing waits on %+v, want the row's own duty", opened.WaitsOn)
	}
}

// TestSpecRejectsInBothDirectionsOverTheRequirementNamed: a requirement
// assigned to the item that no criterion in force for it names, and a criterion
// naming a requirement assigned elsewhere.
func TestSpecRejectsInBothDirectionsOverTheRequirementNamed(t *testing.T) {
	for _, one := range []struct {
		name     string
		assigned []string
		named    []string
		want     string
	}{
		{"every requirement answered", []string{"rq_a", "rq_b"}, []string{"rq_b", "rq_a"}, ""},
		{"an item with no requirement", nil, nil, ""},
		{"a criterion naming none", []string{"rq_a"}, []string{"rq_a", ""}, ""},
		{"a requirement no criterion names", []string{"rq_a", "rq_b"}, []string{"rq_a"},
			gate.AutoRejectedByRequirementUnanswered},
		{"a criterion naming a requirement of another item", []string{"rq_a"}, []string{"rq_a", "rq_elsewhere"},
			gate.AutoRejectedByCriterionElsewhere},
		{"a spec that answers nothing it was assigned", []string{"rq_a"}, []string{"rq_elsewhere"},
			gate.AutoRejectedByRequirementUnanswered},
	} {
		check, found, rejects := gate.SpecRejection(one.assigned, one.named)
		if one.want == "" {
			if rejects {
				t.Errorf("%s: rejected by %q — %s, want no rejection", one.name, check, found)
			}
			continue
		}
		if !rejects || check != one.want {
			t.Errorf("%s: rejected by %q (%v), want %q", one.name, check, rejects, one.want)
		}
		if rejects && found == "" {
			t.Errorf("%s: the rejection says nothing about what it found", one.name)
		}
	}
}

// TestASpecCheckRejectsAtTheSpecRowAndNowhereElse: [gate.ChecksAt] is what a
// row rejects on, so the Spec row takes its own two checks and the merge row's
// are refused there.
func TestASpecCheckRejectsAtTheSpecRowAndNowhereElse(t *testing.T) {
	// A number under the threshold, so nothing but the check decides the row.
	s, p := &fakeScore{assessment: assessed(0.1)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, specBy(t, ctx, pool, token, author, "it_0000000000000000000000000000000a"))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.AutoReject(ctx, opened, gate.AutoRejectedByContractDiff, "a contract diff"); !errors.Is(err, gate.ErrCheckUnknown) {
		t.Errorf("the merge row's check at the Spec row = %v, want ErrCheckUnknown", err)
	}

	closing, err := g.AutoReject(ctx, opened, gate.AutoRejectedByRequirementUnanswered,
		"requirement rq_a is assigned to this item and no criterion in force for it names it")
	if err != nil {
		t.Fatalf("AutoReject at the Spec row: %v", err)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if payload.Verdict != string(gate.VerdictReject) ||
		payload.AutoRejectedBy != gate.AutoRejectedByRequirementUnanswered {
		t.Errorf("the close event reads %+v, want a reject naming the requirement check", payload)
	}
	if payload.ReturnsTo != gate.ReturnsToSpec {
		t.Errorf("the mechanical reject returns the item to %q, want the spec stage", payload.ReturnsTo)
	}

	// The merge row still rejects on its own checks, and the Spec row's are
	// refused there.
	merged, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("firing the merge row: %v", err)
	}
	if _, err := g.AutoReject(ctx, merged, gate.AutoRejectedByCriterionElsewhere, "a criterion"); !errors.Is(err, gate.ErrCheckUnknown) {
		t.Errorf("the Spec row's check at the merge row = %v, want ErrCheckUnknown", err)
	}
}
