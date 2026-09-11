package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
)

// TestAWriteCarriesThePrincipalBesideTheActor is C0110/C0111: the call that
// reaches a seam carries the principal, carried from the entrance, recorded
// beside the actor on the row. A caller that has none leaves it empty.
func TestAWriteCarriesThePrincipalBesideTheActor(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	agent := principal.OfAgent("anthropic/claude-opus-4.8", "dsp_00112233445566778899aabbccddeeff", "the item's own repository")
	withPrincipal, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: notifierActor, Payload: "x", FormatVersion: "page_event/1", Principal: agent,
	})
	if err != nil {
		t.Fatalf("AppendPageEvent with a principal: %v", err)
	}
	if withPrincipal.CallerKind != agent.Actor.Kind || withPrincipal.CallerKey != agent.Actor.Key ||
		withPrincipal.CallerKeyBasis != agent.Actor.Basis {
		t.Errorf("the row's caller is %s %s %s, want the agent's actor",
			withPrincipal.CallerKind, withPrincipal.CallerKey, withPrincipal.CallerKeyBasis)
	}
	if withPrincipal.CallerDispatchID != agent.DispatchID || withPrincipal.CallerScope != agent.Scope {
		t.Errorf("the row names dispatch %q under scope %q, want %q and %q",
			withPrincipal.CallerDispatchID, withPrincipal.CallerScope, agent.DispatchID, agent.Scope)
	}

	noPrincipal, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: notifierActor, Payload: "x", FormatVersion: "page_event/1",
	})
	if err != nil {
		t.Fatalf("AppendPageEvent with no principal: %v", err)
	}
	if noPrincipal.CallerKind != "" || noPrincipal.CallerKey != "" || noPrincipal.CallerKeyBasis != "" ||
		noPrincipal.CallerDispatchID != "" || noPrincipal.CallerScope != "" {
		t.Errorf("a call with no principal stored %+v, want every caller field empty", noPrincipal)
	}

	incomplete := principal.OfAgent("anthropic/claude-opus-4.8", "", "")
	if _, err := log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: notifierActor, Payload: "x", FormatVersion: "page_event/1", Principal: incomplete,
	}); err == nil {
		t.Error("a principal naming an agent with no dispatch and no scope was accepted")
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}

// TestOpenedInWorkAtNamesWorkAsCaller is C0893: the close event's own time when
// the actor opened the row in Work is written with Work as the caller, so it
// is the screen's report and not the human's.
func TestOpenedInWorkAtNamesWorkAsCaller(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	opening, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Verdict: "approve",
		Closes: opening.ID, OpenedInWorkAt: "2026-08-17T00:00:00.000000000Z",
	}); !errors.Is(err, decisionlog.ErrOpenedInWorkAtCaller) {
		t.Errorf("an opened-in-Work time with no caller: %v, want ErrOpenedInWorkAtCaller", err)
	}

	second, err := log.AppendDecisionOpen(ctx, decisionlog.Entry{
		Actor: gate, Payload: "x", FormatVersion: "decision/1", PolicyVersion: "policy-1", ScoreVersion: "score-1",
	})
	if err != nil {
		t.Fatalf("AppendDecisionOpen: %v", err)
	}
	if _, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Verdict: "approve",
		Closes: second.ID, OpenedInWorkAt: "2026-08-17T00:00:00.000000000Z", Principal: ownerReading,
	}); !errors.Is(err, decisionlog.ErrOpenedInWorkAtCaller) {
		t.Errorf("an opened-in-Work time called as the human: %v, want ErrOpenedInWorkAtCaller", err)
	}

	closing, err := log.AppendDecisionClose(ctx, decisionlog.Entry{
		Actor: owner, Payload: "x", FormatVersion: "decision/1", Verdict: "approve",
		Closes: second.ID, OpenedInWorkAt: "2026-08-17T00:00:00.000000000Z", Principal: principal.OfComponent("work"),
	})
	if err != nil {
		t.Fatalf("an opened-in-Work time called as Work: %v", err)
	}
	if closing.CallerKind != "component" || closing.CallerKey != "work" {
		t.Errorf("the closing's caller is %s %q, want the Work screen", closing.CallerKind, closing.CallerKey)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("a refused row reached the log: %v", err)
	}
}
