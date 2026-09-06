// The two refusals the gate supplies to the log's writer, both read as
// per-person keys: a close by the human who wrote the version under decision,
// and a refer with nobody left to refer to.
package gate_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// author and second are two humans by per-person key, which is what the People
// declaration holds, what an artifact version's actor is, and what a close
// event's actor is.
var (
	author = record.Actor{Kind: record.KindHuman, Key: "person:author", Basis: record.BasisClaimed}
	second = record.Actor{Kind: record.KindHuman, Key: "person:second", Basis: record.BasisClaimed}
)

// declares writes that key holds duty, appending no policy version — a People
// writer composed with no policy factory, which is what a test of the gate
// needs from the declaration.
func declares(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	actor record.Actor, key string, duty people.Duty) {
	t.Helper()
	if _, err := people.NewWriter(pool, token, (*policy.Factory)(nil)).
		Declare(ctx, actor, key, people.OfDuty(duty)); err != nil {
		t.Fatalf("declaring that %s holds duty %d: %v", key, duty, err)
	}
}

// specBy writes a spec version whose actor is the given human, and answers with
// the Spec row's firing over it. The Spec row is where the design applies the
// refusal, and duty 6 is the duty it waits on.
func specBy(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	who record.Actor, itemID string) gate.Firing {
	t.Helper()
	version, _, _, err := artifact.NewStore(pool, token).SubmitSpec(ctx, who,
		artifact.By{Authorship: artifact.AuthorshipHuman, Author: "the spec's author"},
		itemID, mergeFiring.ServiceID, "what the item is for", nil, nil, nil, "")
	if err != nil {
		t.Fatalf("submitting the spec version: %v", err)
	}
	f := mergeFiring
	f.Row = gate.Spec
	f.ItemID = itemID
	f.BuildID = ""
	f.ArtifactID = version.ID
	f.Criteria, f.CriteriaInForce = nil, 0
	return f
}

// TestASelfApprovalIsRefusedWhereASecondHolderExists: the row re-fires to that
// holder rather than closing to its own author, and the comparison is between
// the close event's actor key and the key the version was written under.
func TestASelfApprovalIsRefusedWhereASecondHolderExists(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	declares(t, ctx, pool, token, owner, author.Key, gate.DutyConfirmTheCriteria)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyConfirmTheCriteria)

	opened, err := g.Fire(ctx, specBy(t, ctx, pool, token, author, "it_0000000000000000000000000000000a"))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if len(opened.WaitsOn.Holders) != 2 {
		t.Fatalf("the row waits on %v, want both holders by key", opened.WaitsOn.Holders)
	}

	if _, err := g.Decide(ctx, opened, gate.Given{
		Actor: author, Verdict: gate.VerdictApprove,
	}); !errors.Is(err, gate.ErrSelfApproval) {
		t.Fatalf("the author closing their own version = %v, want ErrSelfApproval", err)
	}

	closing, err := g.Decide(ctx, opened, gate.Given{Actor: second, Verdict: gate.VerdictApprove})
	if err != nil {
		t.Fatalf("the second holder closing it: %v", err)
	}
	if closing.SelfApproval {
		t.Errorf("a close by the second holder carries the self-approval field")
	}
}

// TestASelfApprovalIsCarriedWhereNoSecondHolderExists: an install with one
// owner is allowed, and its trail says what it is — the row fires to the editor
// and the close event carries the field, on the row and in the payload.
func TestASelfApprovalIsCarriedWhereNoSecondHolderExists(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	declares(t, ctx, pool, token, owner, author.Key, gate.DutyConfirmTheCriteria)

	opened, err := g.Fire(ctx, specBy(t, ctx, pool, token, author, "it_0000000000000000000000000000000b"))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	closing, err := g.Decide(ctx, opened, gate.Given{Actor: author, Verdict: gate.VerdictApprove})
	if err != nil {
		t.Fatalf("the sole holder closing their own version: %v", err)
	}
	if !closing.SelfApproval {
		t.Errorf("the close event does not carry the self-approval field: %+v", closing)
	}
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &payload); err != nil {
		t.Fatalf("unmarshalling the closing payload: %v", err)
	}
	if !payload.SelfApproval {
		t.Errorf("the closing payload does not carry the self-approval field: %+v", payload)
	}
}

// TestAReferWidensToTheOwnerAndIsThenRefused: a referred row re-fires to a
// holder who has not referred it, widens to the owner where none is left, and a
// refer at the widened row is refused — the row cannot widen twice.
func TestAReferWidensToTheOwnerAndIsThenRefused(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	declares(t, ctx, pool, token, owner, author.Key, gate.DutyUAT)
	declares(t, ctx, pool, token, owner, second.Key, gate.DutyUAT)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}

	first, err := g.Refer(ctx, opened, author, "I cannot judge this myself", mergeFiring)
	if err != nil {
		t.Fatalf("the first holder referring: %v", err)
	}
	if len(first.Reopened.WaitsOn.Holders) != 1 || first.Reopened.WaitsOn.Holders[0] != second.Key {
		t.Fatalf("the re-fired row waits on %v, want the holder who has not referred it",
			first.Reopened.WaitsOn.Holders)
	}

	last, err := g.Refer(ctx, first.Reopened, second, "nor can I", mergeFiring)
	if err != nil {
		t.Fatalf("the last holder referring: %v", err)
	}
	if !last.Reopened.WaitsOn.TheOwner() {
		t.Fatalf("the row after the last holder referred waits on %+v, want the owner",
			last.Reopened.WaitsOn)
	}

	if _, err := g.Refer(ctx, last.Reopened, owner, "and neither can I", mergeFiring); !errors.Is(err,
		gate.ErrNobodyLeftToReferTo) {
		t.Errorf("a refer at the widened row = %v, want ErrNobodyLeftToReferTo", err)
	}

	// What the human has left is a reject whose reason says what they could not
	// read, and that close is not refused.
	if _, err := g.Decide(ctx, last.Reopened, gate.Given{
		Actor: owner, Verdict: gate.VerdictReject, Reason: "the diff is larger than anyone here can read",
	}); err != nil {
		t.Errorf("rejecting at the widened row: %v", err)
	}
}

// TestAnAcknowledgementIsRefusedOnceTheDecisionHasEnded: an acknowledgement sits
// between the opening and the row that ends it, so one after the close would
// report a shared duty's time on a decision nobody was deciding.
func TestAnAcknowledgementIsRefusedOnceTheDecisionHasEnded(t *testing.T) {
	s, p := &fakeScore{assessment: assessed(0.6)}, &fakePolicy{applied: applied(0.3)}
	ctx, pool, token, g := newGate(t, s, p)

	declares(t, ctx, pool, token, owner, author.Key, gate.DutyUAT)

	opened, err := g.Fire(ctx, mergeFiring)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Acknowledge(ctx, opened, author); err != nil {
		t.Fatalf("Acknowledge while the row is pending: %v", err)
	}
	if _, err := g.Decide(ctx, opened, gate.Given{Actor: author, Verdict: gate.VerdictApprove}); err != nil {
		t.Fatalf("Decide: %v", err)
	}

	if _, err := g.Acknowledge(ctx, opened, second); !errors.Is(err, decisionlog.ErrAlreadyEnded) {
		t.Errorf("Acknowledge after the close = %v, want ErrAlreadyEnded", err)
	}
}
