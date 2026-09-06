package screenstatemachine_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// checkout writes a checkout with the files given, keyed by path, and returns
// the directory.
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", full, err)
		}
	}
	return dir
}

const oneScreen = "ssm_00000000000000000000000000000001"

func oneMachine() screenstatemachine.Machine {
	return screenstatemachine.Machine{
		ID: oneScreen, Screen: oneScreen,
		Initial: "empty",
		States:  []string{"empty", "loading", "loaded"},
		Events:  []string{"load", "succeed"},
		Transitions: []screenstatemachine.Transition{
			{From: "empty", Event: "load", To: "loading"},
			{From: "loading", Event: "succeed", To: "loaded"},
		},
	}
}

// TestDeriveTransitionsReadsWhatTheImplementationAdmits: the extractor reads the
// screen's own transition function and reports the transitions it can show the
// implementation admits, which is what the check is decided against.
func TestDeriveTransitionsReadsWhatTheImplementationAdmits(t *testing.T) {
	dir := checkout(t, map[string]string{
		"go.mod": "module example.com/screen\n\ngo 1.24\n",
		screenstatemachine.FileName(oneScreen): `package main

func Transition(from, event string) string {
	switch from {
	case "empty":
		switch event {
		case "load":
			return "loading"
		}
	case "loading":
		switch event {
		case "succeed":
			return "loaded"
		}
	}
	return from
}
`,
	})
	derived := screenstatemachine.DeriveTransitions(dir, []screenstatemachine.Machine{oneMachine()},
		screenstatemachine.GoExtractor("test"))
	if len(derived.Screens) != 1 {
		t.Fatalf("Screens = %+v, want one per machine in force", derived.Screens)
	}
	screen := derived.Screens[0]
	if screen.CouldNotDerive() {
		t.Fatalf("the screen could not be derived: %s", screen.Describe())
	}
	want := []screenstatemachine.DerivedTransition{
		{From: "empty", Event: "load", To: "loading"},
		{From: "loading", Event: "succeed", To: "loaded"},
	}
	if len(screen.Transitions) != len(want) {
		t.Fatalf("Transitions = %+v, want %+v", screen.Transitions, want)
	}
	for n, one := range want {
		if screen.Transitions[n] != one {
			t.Errorf("Transitions[%d] = %+v, want %+v", n, screen.Transitions[n], one)
		}
	}
	if err := screenstatemachine.CheckTransitions(derived, []screenstatemachine.Machine{oneMachine()}); err != nil {
		t.Errorf("CheckTransitions over an implementation that admits what the machine declares = %v", err)
	}
}

// TestDeriveTransitionsRejectsAnAdmittedForbiddenTransition: an implementation
// that moves somewhere the machine does not declare is rejected, which is the
// whole reason for authoring the screen as a state machine.
func TestDeriveTransitionsRejectsAnAdmittedForbiddenTransition(t *testing.T) {
	dir := checkout(t, map[string]string{
		"go.mod": "module example.com/screen\n\ngo 1.24\n",
		screenstatemachine.FileName(oneScreen): `package main

func Transition(from, event string) string {
	switch from {
	case "empty":
		switch event {
		case "load":
			return "loaded"
		}
	}
	return from
}
`,
	})
	derived := screenstatemachine.DeriveTransitions(dir, []screenstatemachine.Machine{oneMachine()},
		screenstatemachine.GoExtractor("test"))
	err := screenstatemachine.CheckTransitions(derived, []screenstatemachine.Machine{oneMachine()})
	if err == nil {
		t.Fatal("CheckTransitions accepted an implementation that skips loading")
	}
}

// TestAConstructTheExtractorCannotFollowIsCouldNotDerive: a partial extraction
// reads as none rather than as clean, because the transition most likely to be
// forbidden is the one made by routing around the declared constructs.
func TestAConstructTheExtractorCannotFollowIsCouldNotDerive(t *testing.T) {
	for name, body := range map[string]string{
		"a state returned by a call": `
	switch from {
	case "empty":
		switch event {
		case "load":
			return next()
		}
	}
	return from`,
		"a dispatch through a table": `
	switch from {
	case "empty":
		return table[event]
	}
	return from`,
		"a default case": `
	switch from {
	default:
		switch event {
		case "load":
			return "loaded"
		}
	}
	return from`,
		"a destination for every state at once": `
	return "loaded"`,
	} {
		dir := checkout(t, map[string]string{
			"go.mod": "module example.com/screen\n\ngo 1.24\n",
			screenstatemachine.FileName(oneScreen): "package main\n\nvar table map[string]string\n\nfunc next() string { return \"\" }\n\n" +
				"func Transition(from, event string) string {" + body + "\n}\n",
		})
		derived := screenstatemachine.DeriveTransitions(dir, []screenstatemachine.Machine{oneMachine()},
			screenstatemachine.GoExtractor("test"))
		screen := derived.Screens[0]
		if !screen.CouldNotDerive() {
			t.Errorf("%s derived %+v, want could not derive", name, screen.Transitions)
			continue
		}
		if screen.Cause != screenstatemachine.CauseConstructNotFollowed {
			t.Errorf("%s = cause %q, want %q", name, screen.Cause, screenstatemachine.CauseConstructNotFollowed)
		}
		if len(screen.Constructs) == 0 {
			t.Errorf("%s names no construct that defeated the analysis", name)
		}
	}
}

// TestABuildNoExtractorCoversAndAScreenWithNoFileAreCouldNotDerive: the three
// routes to the one outcome — no extractor, an extraction that fails outright,
// and a construct that could not be followed — are told apart by the cause, so
// a reader knows what would lift it.
func TestABuildNoExtractorCoversAndAScreenWithNoFileAreCouldNotDerive(t *testing.T) {
	bare := checkout(t, map[string]string{"main.rs": "fn main() {}\n"})
	derived := screenstatemachine.DeriveTransitions(bare, []screenstatemachine.Machine{oneMachine()},
		screenstatemachine.GoExtractor("test"))
	if derived.Screens[0].Cause != screenstatemachine.CauseNoExtractor {
		t.Errorf("a build with no go.mod = cause %q, want %q",
			derived.Screens[0].Cause, screenstatemachine.CauseNoExtractor)
	}

	empty := checkout(t, map[string]string{"go.mod": "module example.com/screen\n\ngo 1.24\n"})
	derived = screenstatemachine.DeriveTransitions(empty, []screenstatemachine.Machine{oneMachine()},
		screenstatemachine.GoExtractor("test"))
	if derived.Screens[0].Cause != screenstatemachine.CauseExtractionFailed {
		t.Errorf("a checkout with no screen file = cause %q, want %q",
			derived.Screens[0].Cause, screenstatemachine.CauseExtractionFailed)
	}
	if derived.Screens[0].Reported == "" {
		t.Error("an extraction that failed reports nothing")
	}
	if len(derived.Unavailable()) != 1 {
		t.Errorf("Unavailable = %+v, want the one screen", derived.Unavailable())
	}
}

// TestATransitionMayLeaveTheScreenInTheImplementation: a destination that is a
// screen id rather than a state is what the machine's own leaving transition is
// checked against.
func TestATransitionMayLeaveTheScreenInTheImplementation(t *testing.T) {
	m := oneMachine()
	m.Events = append(m.Events, "done")
	m.Transitions = append(m.Transitions, screenstatemachine.Transition{
		From: "loaded", Event: "done", Screen: "ssm_00000000000000000000000000000002",
	})
	dir := checkout(t, map[string]string{
		"go.mod": "module example.com/screen\n\ngo 1.24\n",
		screenstatemachine.FileName(oneScreen): `package main

func Transition(from, event string) string {
	switch from {
	case "loaded":
		switch event {
		case "done":
			return "ssm_00000000000000000000000000000002"
		}
	}
	return from
}
`,
	})
	derived := screenstatemachine.DeriveTransitions(dir, []screenstatemachine.Machine{m},
		screenstatemachine.GoExtractor("test"))
	if err := screenstatemachine.CheckTransitions(derived, []screenstatemachine.Machine{m}); err != nil {
		t.Errorf("CheckTransitions over a transition that leaves the screen = %v", err)
	}
}
