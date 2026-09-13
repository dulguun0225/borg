package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
)

// TestAnAbandonmentsActorIsAComponent is C0853: the component that ended the
// decision is the actor, the acknowledgement path already policing kind the
// same way for a human.
func TestAnAbandonmentsActorIsAComponent(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Closes: opening.ID, Reason: "dropped",
	}); !errors.Is(err, decisionlog.ErrAbandonmentNotComponent) {
		t.Errorf("an abandonment by a human: %v, want ErrAbandonmentNotComponent", err)
	}

	bad := aRow()
	bad.FormatVersion, bad.Shape, bad.Part = "decision/1", decisionlog.ShapeDecision, decisionlog.PartAbandonment
	bad.Closes, bad.Reason, bad.Actor = opening.ID, "dropped", owner
	if got, want := refusedBy(t, insertAround(ctx, pool, bad)), "abandonment_actor_component"; got != want {
		t.Errorf("an abandonment by a human around the method was refused by %q, want %q", got, want)
	}

	if _, err := log.AppendDecisionAbandonment(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", Closes: opening.ID, Reason: "dropped",
	}); err != nil {
		t.Fatalf("an abandonment by a component: %v", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestAWaitOpensAsTheComponentThatMetIt is C0975: the component that met the
// condition is the caller and the actor.
func TestAWaitOpensAsTheComponentThatMetIt(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	if _, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "wait/1",
	}); !errors.Is(err, decisionlog.ErrWaitOpenNotComponent) {
		t.Errorf("a wait opened by a human: %v, want ErrWaitOpenNotComponent", err)
	}

	bad := aRow()
	bad.Actor = owner
	if got, want := refusedBy(t, insertAround(ctx, pool, bad)), "wait_open_actor_component"; got != want {
		t.Errorf("a wait opened by a human around the method was refused by %q, want %q", got, want)
	}

	opening, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "wait/1",
	})
	if err != nil {
		t.Fatalf("a wait opened by a component: %v", err)
	}
	// The closing is not policed the same way: whichever component next
	// reaches the work may not be the one that met the condition.
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
		Actor: notifierActor, Payload: "x", FormatVersion: "wait/1", Closes: opening.ID,
	}); err != nil {
		t.Fatalf("a wait closed by a different component: %v", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// An agent that cannot reach its own model credential is the specific
// exception to the component actor on a wait; the caller names its dispatch.
func TestCredentialWaitCarriesTheAgentThatCouldNotReach(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	as := principal.OfAgent("model", "dispatch", "scope")
	payload := `{"kind":"credential_unreachable","credential_name":"model.test"}`
	for name, entry := range map[string]decisionlog.Entry{
		"ordinary wait":      {Actor: as.Actor, Principal: as, Payload: "x"},
		"no credential name": {Actor: as.Actor, Principal: as, Payload: `{"kind":"credential_unreachable"}`},
		"no caller":          {Actor: as.Actor, Payload: payload},
		"another caller":     {Actor: as.Actor, Principal: principal.OfAgent("other", "dispatch", "scope"), Payload: payload},
		"human":              {Actor: owner, Principal: ownerReading, Payload: payload},
	} {
		entry.FormatVersion = "wait/1"
		if _, err := log.AppendWaitOpen(ctx, entry); !errors.Is(err, decisionlog.ErrWaitOpenNotComponent) {
			t.Errorf("%s: %v", name, err)
		}
		bad := aRow()
		bad.FormatVersion, bad.Actor, bad.Payload = "wait/2", entry.Actor, entry.Payload
		bad.CallerKind, bad.CallerKey, bad.CallerKeyBasis = entry.Principal.Actor.Kind, entry.Principal.Actor.Key, entry.Principal.Actor.Basis
		bad.CallerDispatchID, bad.CallerScope = entry.Principal.DispatchID, entry.Principal.Scope
		if got := refusedBy(t, insertAround(ctx, pool, bad)); got != "wait_open_actor_component" {
			t.Errorf("%s around writer: %s", name, got)
		}
	}
	opening, err := log.AppendWaitOpen(ctx, decisionlog.Entry{Actor: as.Actor, Principal: as, Payload: payload, FormatVersion: "wait/1"})
	if err != nil {
		t.Fatal(err)
	}
	if opening.Actor != as.Actor || opening.CallerKey != as.Actor.Key || opening.CallerDispatchID != as.DispatchID {
		t.Fatalf("lost credential caller: %+v", opening)
	}
	if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{Actor: gate, Payload: "resumed", FormatVersion: "wait/1", Closes: opening.ID}); err != nil {
		t.Fatal(err)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatal(err)
	}
}
