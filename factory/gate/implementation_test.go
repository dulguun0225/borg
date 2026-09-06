// The Implementation row's own rejections over the screens, and the one screen
// outcome that is not a rejection: a screen nobody could derive.
package gate_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// theMachine is one screen in force: two states, one event, and the one
// transition it declares. The machine is closed, so every other transition from
// that state on that event is forbidden.
var theMachine = screenstatemachine.Machine{
	ID: "ssm_0000000000000000000000000000000a", Screen: "ssm_0000000000000000000000000000000a",
	Initial: "empty", States: []string{"empty", "loaded"}, Events: []string{"load"},
	Transitions: []screenstatemachine.Transition{{From: "empty", Event: "load", To: "loaded"}},
	Terminal:    []string{"loaded"},
}

// driven is the driver derivation of a build that drives every state the machine
// declares, which is what leaves the drivers rejecting nothing.
func driven() screenstatemachine.DriverDerivation {
	return screenstatemachine.DriverDerivation{
		Toolchain: screenstatemachine.Toolchain,
		Drivers: []screenstatemachine.Driver{
			{Screen: theMachine.Screen, State: "empty"},
			{Screen: theMachine.Screen, State: "loaded"},
		},
	}
}

// TestAForbiddenTransitionRejectsAtImplementation: an implementation that admits
// a transition the machine forbids is rejected here mechanically, whatever the
// score returns, which is the whole reason for authoring a screen as a machine.
func TestAForbiddenTransitionRejectsAtImplementation(t *testing.T) {
	if !slices.Contains(gate.ChecksAt(gate.Implementation), gate.AutoRejectedByForbiddenTransition) {
		t.Fatal("the Implementation row rejects on no forbidden transition")
	}

	admitted := screenstatemachine.Derivation{
		Screens: []screenstatemachine.ScreenDerivation{{
			Screen: theMachine.Screen,
			Transitions: []screenstatemachine.DerivedTransition{
				{From: "empty", Event: "load", To: "failed"},
			},
		}},
	}
	check, found, rejects := gate.ScreenRejection(admitted, driven(), []screenstatemachine.Machine{theMachine})
	if !rejects || check != gate.AutoRejectedByForbiddenTransition {
		t.Fatalf("the rejection is %q (%v), want the forbidden transition", check, rejects)
	}
	if !strings.Contains(found, "failed") {
		t.Errorf("what it found reads %q, want the transition the implementation admits", found)
	}

	// The transition the machine declares rejects nothing.
	declared := screenstatemachine.Derivation{
		Screens: []screenstatemachine.ScreenDerivation{{
			Screen: theMachine.Screen,
			Transitions: []screenstatemachine.DerivedTransition{
				{From: "empty", Event: "load", To: "loaded"},
			},
		}},
	}
	if _, _, rejects := gate.ScreenRejection(declared, driven(), []screenstatemachine.Machine{theMachine}); rejects {
		t.Errorf("an implementation admitting what the machine declares rejects")
	}
}

// TestTheDriversRejectInBothDirectionsAtImplementation: a state in force that
// nothing drives, and a driver naming a state no machine in force declares.
func TestTheDriversRejectInBothDirectionsAtImplementation(t *testing.T) {
	declared := screenstatemachine.Derivation{
		Screens: []screenstatemachine.ScreenDerivation{{Screen: theMachine.Screen}},
	}
	inForce := []screenstatemachine.Machine{theMachine}

	undriven := screenstatemachine.DriverDerivation{
		Toolchain: screenstatemachine.Toolchain,
		Drivers:   []screenstatemachine.Driver{{Screen: theMachine.Screen, State: "empty"}},
	}
	check, found, rejects := gate.ScreenRejection(declared, undriven, inForce)
	if !rejects || check != gate.AutoRejectedByADriver {
		t.Fatalf("a state nothing drives is %q (%v), want the drivers' rejection", check, rejects)
	}
	if !strings.Contains(found, "loaded") {
		t.Errorf("what it found reads %q, want the state nothing drives", found)
	}

	undeclared := driven()
	undeclared.Drivers = append(undeclared.Drivers,
		screenstatemachine.Driver{Screen: theMachine.Screen, State: "failed"})
	if check, _, rejects := gate.ScreenRejection(declared, undeclared, inForce); !rejects ||
		check != gate.AutoRejectedByADriver {
		t.Errorf("a driver naming a state no machine declares is %q (%v), want the drivers' rejection",
			check, rejects)
	}
}

// TestAScreenThatCouldNotBeDerivedRejectsNothingAndResolvesInstead: the check
// rejects only a transition it can show the implementation admits, so a screen
// it could not read rejects nothing — and the firing carries it to the score,
// which resolves the factor rather than passing the build.
func TestAScreenThatCouldNotBeDerivedRejectsNothingAndResolvesInstead(t *testing.T) {
	notDerived := screenstatemachine.Derivation{
		Screens: []screenstatemachine.ScreenDerivation{{
			Screen: theMachine.Screen,
			Cause:  screenstatemachine.CauseConstructNotFollowed,
			// A construct the extractor met and could not follow, which is what
			// a partial extraction reads as none for.
			Constructs: []string{"screen.ssm.go:9 — a dispatch through a table"},
			// What it did read is not acted on: a partial extraction reads as
			// none rather than as clean.
			Transitions: []screenstatemachine.DerivedTransition{
				{From: "empty", Event: "load", To: "failed"},
			},
		}},
	}
	if check, _, rejects := gate.ScreenRejection(notDerived, driven(),
		[]screenstatemachine.Machine{theMachine}); rejects {
		t.Errorf("a screen nothing could derive rejected with %q, and the check rejects only what it can show", check)
	}

	// A build no extractor covers is could not derive for every screen, and the
	// drivers of that same build read the same way: neither rejects.
	noExtractor := screenstatemachine.DriverDerivation{CouldNotDerive: "no extractor covers this build"}
	if _, _, rejects := gate.ScreenRejection(notDerived, noExtractor,
		[]screenstatemachine.Machine{theMachine}); rejects {
		t.Errorf("a build whose drivers no extractor could read rejected")
	}

	s, p := &fakeScore{assessment: assessed(0.1)}, &fakePolicy{applied: applied(0.9)}
	ctx, pool, token, g := newGate(t, s, p)
	built, err := artifact.NewStore(pool, token).SubmitImplementation(ctx, owner,
		artifact.By{Authorship: artifact.AuthorshipHuman, Author: "the implementer"},
		mergeFiring.ItemID, "what was built", "")
	if err != nil {
		t.Fatalf("submitting the implementation version: %v", err)
	}
	f := mergeFiring
	f.Row = gate.Implementation
	f.ArtifactID = built.ID
	f.Screens = notDerived
	if _, err := g.Fire(ctx, f); err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if len(s.asked.ScreensNotDerived) != 1 ||
		!strings.Contains(s.asked.ScreensNotDerived[0], theMachine.Screen) {
		t.Errorf("the score was asked about %v, want the screen the check could not derive",
			s.asked.ScreensNotDerived)
	}

	// A row that decides no build derived no screen, and a firing there that
	// named one is refused before anything is appended.
	outside := gate.Firing{Row: gate.HaltWithdrawal, RecordID: "hlt_0000000000000000000000000000000a"}
	outside.Screens = notDerived
	if _, err := g.Fire(ctx, outside); err == nil {
		t.Errorf("a row that decides no build accepted a screen derivation")
	}
}
