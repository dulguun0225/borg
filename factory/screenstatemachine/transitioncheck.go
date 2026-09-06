package screenstatemachine

import (
	"errors"
	"fmt"
	"slices"
)

// The transition check. Whether a program admits a transition is a question
// over its reachable states and is not decidable in general, so this
// approximates in one direction and states it: it rejects only a transition it
// can show the implementation admits, and never rejects on what it could not
// follow. What it could not follow is enumerated per screen and takes one
// outcome, could not derive for that screen, which resolves the way an
// unavailable factor does.

// Cause is why a screen's transitions could not be derived. The three call for
// different responses — the first is lifted by an extractor shipping, the
// second by an item on the build or on the extractor, the third by building the
// screen from constructs the extractor follows — so the record says which.
type Cause string

const (
	// CauseNoExtractor is that no extractor covers the build's toolchain.
	CauseNoExtractor Cause = "no_extractor"
	// CauseExtractionFailed is that an extractor ran and failed on the screen,
	// and the record carries what it reported.
	CauseExtractionFailed Cause = "extraction_failed"
	// CauseConstructNotFollowed is that the screen holds a construct the
	// extractor met and could not follow. A partial extraction reads as none
	// rather than as clean, because the transition most likely to be forbidden
	// is the one made by routing around the declared constructs.
	CauseConstructNotFollowed Cause = "construct_not_followed"
)

// Causes is every cause a could-not-derive screen may name.
var Causes = []Cause{CauseNoExtractor, CauseExtractionFailed, CauseConstructNotFollowed}

// Extractor is which extractor read a build's screens: its name and version,
// the toolchain it covers, and the factory version that shipped it. Which
// toolchains have one is a fact of the factory's version, the arrangement a
// consumer contract's derivation already has.
type Extractor struct {
	Name           string
	Version        string
	Toolchain      string
	FactoryVersion string
}

// DerivedTransition is one transition the extractor can show the implementation
// admits: the state it leaves from, the event it answers, and where it goes —
// which is a state of the same machine or the id of another screen, as the
// implementation names it.
type DerivedTransition struct {
	From  string
	Event string
	To    string
}

// ScreenDerivation is what the extractor made of one screen: the transitions it
// can show the implementation admits, or the cause it could not derive them and
// the constructs that defeated it. It is a record and not an empty list,
// because "the implementation admits nothing" and "nothing was visible" call
// for opposite responses.
type ScreenDerivation struct {
	// Screen is the screen's identity, which is the id of the machine that
	// introduced it.
	Screen      string
	Transitions []DerivedTransition
	// Cause is empty on a screen that derived, and names why on one that could
	// not.
	Cause Cause
	// Constructs is what the extractor met and could not follow, on
	// [CauseConstructNotFollowed] and nowhere else — a handler registered at run
	// time, a dispatch through a table, a state derived from data a server
	// returns, a transition a third-party component performs.
	Constructs []string
	// Reported is what the extractor reported, on [CauseExtractionFailed] and
	// nowhere else.
	Reported string
}

// CouldNotDerive reports whether the screen's transitions could not be derived.
func (s ScreenDerivation) CouldNotDerive() bool { return s.Cause != "" }

// Describe is the screen's outcome in the words a reader at a gate sees. The
// vector names the screen and the constructs that defeated the analysis, and
// this is that sentence.
func (s ScreenDerivation) Describe() string {
	switch s.Cause {
	case CauseNoExtractor:
		return "could not derive for " + s.Screen + ": no extractor covers this build"
	case CauseExtractionFailed:
		return "could not derive for " + s.Screen + ": the extraction failed: " + s.Reported
	case CauseConstructNotFollowed:
		return fmt.Sprintf("could not derive for %s: %d construct(s) the analysis could not follow: %v",
			s.Screen, len(s.Constructs), s.Constructs)
	}
	return fmt.Sprintf("derived %d transition(s) for %s", len(s.Transitions), s.Screen)
}

// Derivation is what one run of the transition check produced over a build:
// which extractor ran, and one [ScreenDerivation] per screen in force.
type Derivation struct {
	Extractor Extractor
	Screens   []ScreenDerivation
}

// Unavailable is every screen this run could not derive. It is what the score
// reads: a screen nobody could read makes the machine's enforcement unknowable
// rather than clean, and a factor the score cannot compute is resolved and
// never valued — a human at this gate whatever the formula returns. The caller
// that reads it into a vector is the score, and doc.go names it as one this
// package does not hold.
func (d Derivation) Unavailable() []ScreenDerivation {
	var could []ScreenDerivation
	for _, s := range d.Screens {
		if s.CouldNotDerive() {
			could = append(could, s)
		}
	}
	return could
}

// ForbiddenTransitionError is a transition the implementation admits that the
// machine forbids: from a state the machine declares, on an event it declares,
// to a destination the machine does not declare from there on that event. The
// machine is closed, so every undeclared transition is forbidden.
type ForbiddenTransitionError struct {
	Screen, From, Event, To string
	// Declared is where the machine does send that state on that event, and is
	// empty where the machine declares no transition there at all.
	Declared string
}

func (e *ForbiddenTransitionError) Error() string {
	if e.Declared == "" {
		return fmt.Sprintf("screenstatemachine: the implementation of %s moves from %s on %s to %s, and the machine declares no transition there",
			e.Screen, e.From, e.Event, e.To)
	}
	return fmt.Sprintf("screenstatemachine: the implementation of %s moves from %s on %s to %s, and the machine declares %s",
		e.Screen, e.From, e.Event, e.To, e.Declared)
}

// CheckTransitions is the rejection the Implementation gate makes from a
// [Derivation], one error per admitted forbidden transition joined, so a caller
// acting on one at a time would rebuild once per transition.
//
// It rejects nothing for a screen that could not be derived: that outcome is
// read by [Derivation.Unavailable] and resolves a human at the gate instead. A
// derived transition whose from-state or whose event the machine does not
// declare is not rejected either, the direction being fixed at transitions the
// machine could have declared and did not.
func CheckTransitions(derived Derivation, inForce []Machine) error {
	declared := make(map[string]Machine, len(inForce))
	for _, m := range inForce {
		declared[m.Screen] = m
	}

	var defects []error
	for _, screen := range derived.Screens {
		m, held := declared[screen.Screen]
		if screen.CouldNotDerive() || !held {
			continue
		}
		for _, t := range screen.Transitions {
			if !slices.Contains(m.States, t.From) || !slices.Contains(m.Events, t.Event) {
				continue
			}
			destination, found := destinationOf(m, t.From, t.Event)
			if found && destination == t.To {
				continue
			}
			defects = append(defects, &ForbiddenTransitionError{
				Screen: screen.Screen, From: t.From, Event: t.Event, To: t.To, Declared: destination,
			})
		}
	}
	return errors.Join(defects...)
}

// destinationOf is where the machine sends a state on an event: the state it
// moves to, or the id of the screen it leaves to, and false where the machine
// declares no transition there.
func destinationOf(m Machine, from, event string) (string, bool) {
	for _, t := range m.Transitions {
		if t.From != from || t.Event != event {
			continue
		}
		if t.Screen != "" {
			return t.Screen, true
		}
		return t.To, true
	}
	return "", false
}
