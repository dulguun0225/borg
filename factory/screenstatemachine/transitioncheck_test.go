package screenstatemachine_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// machineInForce is one machine as the check reads it, with the screen already
// set — the check reads machines the store returned, so the identity is there.
func machineInForce() screenstatemachine.Machine {
	return screenstatemachine.Machine{
		ID:      "ssm_00000000000000000000000000000001",
		Screen:  "ssm_00000000000000000000000000000001",
		Initial: "empty",
		States:  []string{"empty", "loading", "loaded"},
		Events:  []string{"load", "succeed", "done"},
		Transitions: []screenstatemachine.Transition{
			{From: "empty", Event: "load", To: "loading"},
			{From: "loading", Event: "succeed", To: "loaded"},
			{From: "loaded", Event: "done", Screen: "ssm_00000000000000000000000000000002"},
		},
	}
}

// TestCheckTransitionsRejectsOnlyWhatTheMachineForbids: the machine is closed,
// so a transition it does not declare from a declared state on a declared event
// is forbidden, and a destination other than the declared one is forbidden too.
// A transition the machine declares exactly is not.
func TestCheckTransitionsRejectsOnlyWhatTheMachineForbids(t *testing.T) {
	m := machineInForce()
	derived := screenstatemachine.Derivation{
		Extractor: screenstatemachine.GoExtractor("test"),
		Screens: []screenstatemachine.ScreenDerivation{{
			Screen: m.Screen,
			Transitions: []screenstatemachine.DerivedTransition{
				{From: "empty", Event: "load", To: "loading"},
				{From: "loaded", Event: "done", To: "ssm_00000000000000000000000000000002"},
				{From: "empty", Event: "succeed", To: "loaded"},
				{From: "loading", Event: "succeed", To: "empty"},
			},
		}},
	}
	err := screenstatemachine.CheckTransitions(derived, []screenstatemachine.Machine{m})
	if err == nil {
		t.Fatal("CheckTransitions accepted an implementation that admits two forbidden transitions")
	}
	joined, ok := err.(interface{ Unwrap() []error })
	if !ok {
		t.Fatalf("CheckTransitions = %v, want the defects joined", err)
	}
	if len(joined.Unwrap()) != 2 {
		t.Errorf("CheckTransitions returned %d defect(s), want 2: %v", len(joined.Unwrap()), err)
	}
	var forbidden *screenstatemachine.ForbiddenTransitionError
	if !errors.As(err, &forbidden) {
		t.Fatalf("CheckTransitions = %v, want a ForbiddenTransitionError", err)
	}
}

// TestCheckTransitionsNeverRejectsOnWhatItCouldNotFollow: a screen that could
// not be derived rejects nothing here — that outcome resolves a human at the
// gate instead, and the screens it names are what the score reads.
func TestCheckTransitionsNeverRejectsOnWhatItCouldNotFollow(t *testing.T) {
	m := machineInForce()
	derived := screenstatemachine.Derivation{
		Extractor: screenstatemachine.GoExtractor("test"),
		Screens: []screenstatemachine.ScreenDerivation{{
			Screen:     m.Screen,
			Cause:      screenstatemachine.CauseConstructNotFollowed,
			Constructs: []string{"screen.x.go:9 — a destination this extractor cannot read off the source"},
		}},
	}
	if err := screenstatemachine.CheckTransitions(derived, []screenstatemachine.Machine{m}); err != nil {
		t.Errorf("CheckTransitions over a screen nobody could read = %v, want nil", err)
	}
	unavailable := derived.Unavailable()
	if len(unavailable) != 1 || unavailable[0].Screen != m.Screen {
		t.Fatalf("Unavailable = %+v, want the one screen nobody could read", unavailable)
	}
	if !unavailable[0].CouldNotDerive() {
		t.Error("the screen does not report that it could not be derived")
	}
}

// TestCheckTransitionsPassesOverAStateOrEventTheMachineDoesNotDeclare: the
// direction is fixed at transitions the machine could have declared and did
// not, so a from-state or an event outside the machine is not this check's.
func TestCheckTransitionsPassesOverAStateOrEventTheMachineDoesNotDeclare(t *testing.T) {
	m := machineInForce()
	derived := screenstatemachine.Derivation{
		Screens: []screenstatemachine.ScreenDerivation{{
			Screen: m.Screen,
			Transitions: []screenstatemachine.DerivedTransition{
				{From: "printing", Event: "load", To: "loading"},
				{From: "empty", Event: "cancel", To: "loading"},
			},
		}},
	}
	if err := screenstatemachine.CheckTransitions(derived, []screenstatemachine.Machine{m}); err != nil {
		t.Errorf("CheckTransitions = %v, want nil", err)
	}
}

// TestUnavailableIsEmptyWhereEveryScreenDerived: a run that read every screen
// resolves nothing, which is what lets the gate decide on the score.
func TestUnavailableIsEmptyWhereEveryScreenDerived(t *testing.T) {
	derived := screenstatemachine.Derivation{
		Screens: []screenstatemachine.ScreenDerivation{{Screen: "ssm_a"}},
	}
	if got := derived.Unavailable(); len(got) != 0 {
		t.Errorf("Unavailable = %+v, want none", got)
	}
	if want := "derived 0 transition(s) for ssm_a"; derived.Screens[0].Describe() != want {
		t.Errorf("Describe = %q, want %q", derived.Screens[0].Describe(), want)
	}
}
