// [gate.Gate.Refer]: a reason is required the way a reject's and a hold's is,
// a refer where the row already waits on the owner — nobody holding its
// duty, which is every row's starting position with no People declaration
// written — has nobody left to refer to, and the one row decided over a set is
// re-fired over that set.
package gate_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/policy"
)

func TestReferRequiresAReasonAndRefusesWithNobodyLeft(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	opened, err := g.Fire(ctx, mergeRowFiring(t, ctx, pool, token))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	if _, err := g.Refer(ctx, opened, owner, "", mergeFiring); !errors.Is(err, gate.ErrReasonMissing) {
		t.Errorf("Refer with no reason = %v, want ErrReasonMissing", err)
	}

	// Nobody holds duty 7 (UAT), which the merge row waits on, so the row
	// already waits on the owner and a refer has nobody left to reach.
	if _, err := g.Refer(ctx, opened, owner, "I cannot judge this myself", mergeFiring); !errors.Is(err, gate.ErrNobodyLeftToReferTo) {
		t.Errorf("Refer with nobody left = %v, want ErrNobodyLeftToReferTo", err)
	}

	// Decide refuses a refer outright: it is given through Refer, which closes
	// the row and re-fires it in one call.
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: owner, Verdict: gate.VerdictRefer}); !errors.Is(err, gate.ErrReferGivenHere) {
		t.Errorf("Decide(refer) = %v, want ErrReferGivenHere", err)
	}
}

// TestAReferAtDecompositionReFiresTheSet: the one row decided over a set is
// re-fired over that set, read off the closed row's own open event.
// [gate.Gate.Fire] refuses a Decomposition firing outright, so a refer re-fired
// through it would leave the close chained and no row pending over the set.
//
// The design names no duty for that row, so a firing of it waits on the owner
// and a refer there is refused before anything is appended. The holders are put
// on the row here the way a record's own routing puts them on the rows outside
// every item; what this pins is what the re-fire is over.
func TestAReferAtDecompositionReFiresTheSet(t *testing.T) {
	s := &varyingScore{by: map[string]float64{"it_a": 0.7, "it_b": 0.7}}
	p := &fakePolicy{applied: applied(0.5)}
	ctx, pool, token, g := newGate(t, s, p)

	declares(t, ctx, pool, token, owner, author.Key, gate.DutyUAT)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyUAT)

	set := gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000a",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", AreaID: "ar_a", Requirements: 2},
			{ItemID: "it_b", ServiceID: "svc_b", AreaID: "ar_a", Requirements: 5, WaitsOn: []string{"it_a"}},
		},
	}
	opened, err := g.FireSet(ctx, set)
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}

	// The row as it is fired waits on the owner, so a refer at it reaches
	// nobody and chains nothing: the refusal is read before the close.
	if _, err := g.Refer(ctx, opened, owner, "I cannot judge this decomposition",
		gate.Firing{Row: gate.Decomposition}); !errors.Is(err, gate.ErrNobodyLeftToReferTo) {
		t.Fatalf("a refer at a row waiting on the owner = %v, want ErrNobodyLeftToReferTo", err)
	}

	held := opened
	held.WaitsOn = gate.Waits{Duty: gate.DutyUAT, Holders: []string{author.Key, second.Key}}
	referred, err := g.Refer(ctx, held, author, "I cannot judge this decomposition",
		gate.Firing{Row: gate.Decomposition})
	if err != nil {
		t.Fatalf("Refer at the Decomposition row: %v", err)
	}
	if referred.Close.Closes != opened.Row.ID {
		t.Errorf("the refer closes %q, want the row it was given at", referred.Close.Closes)
	}
	if referred.Reopened.Gate != gate.Decomposition {
		t.Fatalf("the row fired again is %s, want the row that was referred", referred.Reopened.Gate)
	}
	if referred.Reopened.Subject.IntentID != set.IntentID {
		t.Errorf("the row fired again decides intent %q", referred.Reopened.Subject.IntentID)
	}

	var payload gate.SetOpeningPayload
	if err := json.Unmarshal([]byte(referred.Reopened.Row.Payload), &payload); err != nil {
		t.Fatalf("reading the re-fired opening payload: %v", err)
	}
	if len(payload.Set) != 2 {
		t.Fatalf("the row fired again carries %d member(s), want the set the closed row decided", len(payload.Set))
	}
	if payload.Set[1].ItemID != "it_b" || payload.Set[1].ServiceID != "svc_b" ||
		payload.Set[1].Requirements != 5 || len(payload.Set[1].WaitsOn) != 1 {
		t.Errorf("the re-fired set is %+v, want the members the closed row named", payload.Set)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatalf("the chain does not verify after a refer at the Decomposition row: %v", err)
	}
}

// TestAReferAtDecompositionCarriesReferrersAndRefusesARepeat: the referrers
// carry forward from one Decomposition firing to the next the way they do at
// every other row, so a second refer re-fires to the holder who has not
// referred it and, once both have, the row widens to the owner and a further
// refer there is refused.
//
// A safeguard's own routing to a duty is what gives this row real holders at
// all — the design names no duty for it — the way
// [TestASafeguardedRowRoutesToTheHumanTheSafeguardNames] gives one a named
// human.
func TestAReferAtDecompositionCarriesReferrersAndRefusesARepeat(t *testing.T) {
	s := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.2}}
	p := &fakePolicy{applied: policy.Applied{
		PolicyVersion: testPolicyVersion, Threshold: 0.9, ThresholdFrom: policy.FromSupplied,
		HumanBySafeguard: true, Safeguards: []string{"sg_0000000000000000000000000000000c"},
	}}
	ctx, pool, token, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.SafeguardRouting = func(context.Context, []string) (gate.RoutedTo, error) {
			return gate.RoutedTo{Duty: gate.DutyUAT}, nil
		}
	})
	declares(t, ctx, pool, token, owner, author.Key, gate.DutyUAT)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyUAT)

	set := gate.SetFiring{
		IntentID:      "in_0000000000000000000000000000000c",
		EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", AreaID: "ar_a", Requirements: 2},
			{ItemID: "it_b", ServiceID: "svc_b", AreaID: "ar_a", Requirements: 5, WaitsOn: []string{"it_a"}},
		},
	}
	opened, err := g.FireSet(ctx, set)
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	if len(opened.WaitsOn.Holders) != 2 {
		t.Fatalf("the safeguarded row waits on %v, want both holders of duty 7", opened.WaitsOn.Holders)
	}

	first, err := g.Refer(ctx, opened, author, "I cannot judge this myself", gate.Firing{Row: gate.Decomposition})
	if err != nil {
		t.Fatalf("the first holder referring: %v", err)
	}
	if len(first.Reopened.WaitsOn.Holders) != 1 || first.Reopened.WaitsOn.Holders[0] != second.Key {
		t.Fatalf("the re-fired set waits on %v, want only the holder who has not referred it",
			first.Reopened.WaitsOn.Holders)
	}

	last, err := g.Refer(ctx, first.Reopened, second, "nor can I", gate.Firing{Row: gate.Decomposition})
	if err != nil {
		t.Fatalf("the second holder referring: %v", err)
	}
	if !last.Reopened.WaitsOn.TheOwner() {
		t.Fatalf("the row after both holders referred waits on %+v, want the owner", last.Reopened.WaitsOn)
	}

	if _, err := g.Refer(ctx, last.Reopened, owner, "and neither can I", gate.Firing{Row: gate.Decomposition}); !errors.Is(err, gate.ErrNobodyLeftToReferTo) {
		t.Errorf("a refer at the widened row = %v, want ErrNobodyLeftToReferTo", err)
	}
}
