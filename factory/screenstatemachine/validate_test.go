package screenstatemachine_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

func draft() screenstatemachine.Draft {
	return screenstatemachine.Draft{
		Initial: "empty",
		States:  []string{"empty", "loading", "loaded", "failed"},
		Events:  []string{"load", "succeed", "fail", "retry"},
		Transitions: []screenstatemachine.Transition{
			{From: "empty", Event: "load", To: "loading"},
			{From: "loading", Event: "succeed", To: "loaded"},
			{From: "loading", Event: "fail", To: "failed"},
			{From: "failed", Event: "retry", To: "loading"},
		},
		Terminal: []string{"loaded"},
	}
}

func TestValidateAcceptsAWellFormedMachine(t *testing.T) {
	if err := screenstatemachine.Validate(draft()); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

func TestValidateRefusesNoInitial(t *testing.T) {
	d := draft()
	d.Initial = ""
	if err := screenstatemachine.Validate(d); !errors.Is(err, screenstatemachine.ErrInitialEmpty) {
		t.Errorf("Validate = %v, want %v", err, screenstatemachine.ErrInitialEmpty)
	}
}

func TestValidateRefusesAnInitialStateNotDeclared(t *testing.T) {
	d := draft()
	d.Initial = "nowhere"
	if err := screenstatemachine.Validate(d); !errors.Is(err, screenstatemachine.ErrInitialNotDeclared) {
		t.Errorf("Validate = %v, want %v", err, screenstatemachine.ErrInitialNotDeclared)
	}
}

func TestValidateRefusesTwoTransitionsOnOneEventFromOneState(t *testing.T) {
	d := draft()
	d.Transitions = append(d.Transitions, screenstatemachine.Transition{From: "loading", Event: "succeed", To: "failed"})
	err := screenstatemachine.Validate(d)
	var dup *screenstatemachine.DuplicateTransitionError
	if !errors.As(err, &dup) {
		t.Fatalf("Validate = %v, want a *DuplicateTransitionError", err)
	}
	if dup.State != "loading" || dup.Event != "succeed" {
		t.Errorf("DuplicateTransitionError = %+v, want state loading, event succeed", dup)
	}
}

func TestValidateRefusesAnUnreachableState(t *testing.T) {
	d := draft()
	d.States = append(d.States, "orphan")
	err := screenstatemachine.Validate(d)
	var unreachable *screenstatemachine.UnreachableStateError
	if !errors.As(err, &unreachable) {
		t.Fatalf("Validate = %v, want a *UnreachableStateError", err)
	}
	if unreachable.State != "orphan" {
		t.Errorf("UnreachableStateError = %+v, want state orphan", unreachable)
	}
}

func TestValidateRefusesANonTerminalStateWithNoEvent(t *testing.T) {
	d := draft()
	// "failed" has an event (retry); dropping that transition leaves it with
	// none, and it is not terminal.
	d.Transitions = d.Transitions[:len(d.Transitions)-1]
	err := screenstatemachine.Validate(d)
	var noEvent *screenstatemachine.NoEventFromStateError
	if !errors.As(err, &noEvent) {
		t.Fatalf("Validate = %v, want a *NoEventFromStateError", err)
	}
	if noEvent.State != "failed" {
		t.Errorf("NoEventFromStateError = %+v, want state failed", noEvent)
	}
}

// TestValidateAcceptsATransitionThatLeavesTheScreen is the closure rule
// applied to a transition naming another screen: it counts as the state's
// event and it is not a reachability edge inside this machine.
func TestValidateAcceptsATransitionThatLeavesTheScreen(t *testing.T) {
	d := screenstatemachine.Draft{
		Initial: "shown",
		States:  []string{"shown"},
		Events:  []string{"continue"},
		Transitions: []screenstatemachine.Transition{
			{From: "shown", Event: "continue", Screen: "ssm_next"},
		},
	}
	if err := screenstatemachine.Validate(d); err != nil {
		t.Errorf("Validate = %v, want nil", err)
	}
}

func TestCheckTransitionTargetsRejectsAScreenNotInForce(t *testing.T) {
	machines := []screenstatemachine.Machine{{
		ID: "ssm_a",
		Transitions: []screenstatemachine.Transition{
			{From: "shown", Event: "continue", Screen: "ssm_gone"},
		},
	}}
	err := screenstatemachine.CheckTransitionTargets(machines, map[string]bool{"ssm_next": true})
	var notFound *screenstatemachine.ScreenNotFoundError
	if !errors.As(err, &notFound) {
		t.Fatalf("CheckTransitionTargets = %v, want a *ScreenNotFoundError", err)
	}
	if notFound.Screen != "ssm_gone" {
		t.Errorf("ScreenNotFoundError = %+v, want screen ssm_gone", notFound)
	}
}

func TestCheckTransitionTargetsAcceptsAScreenInForce(t *testing.T) {
	machines := []screenstatemachine.Machine{{
		ID: "ssm_a",
		Transitions: []screenstatemachine.Transition{
			{From: "shown", Event: "continue", Screen: "ssm_next"},
		},
	}}
	if err := screenstatemachine.CheckTransitionTargets(machines, map[string]bool{"ssm_next": true}); err != nil {
		t.Errorf("CheckTransitionTargets = %v, want nil", err)
	}
}
