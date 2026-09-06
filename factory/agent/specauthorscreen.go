package agent

import (
	"fmt"
	"strings"
)

// The screen state machine half of the spec author's reply, split out of
// specauthor.go so that neither file passes the length a file is held to. The
// role, the prompt and the rest of the protocol are there; the machine, its
// transitions and the three lines that carry them are here.

// ScreenMachine is the state machine a spec version declares for a screen: the
// states, the initial one, the transitions between them, and the states that
// are terminal. It is what [screenstatemachine.Draft] is composed from by the
// stage that submits the version.
type ScreenMachine struct {
	Initial     string
	States      []string
	Events      []string
	Transitions []ScreenTransition
	Terminal    []string
}

// ScreenTransition is one transition of a [ScreenMachine]. To is a state of the
// same machine, and is empty where Screen names another screen by id instead: a
// transition leaves the screen or it stays, never both.
type ScreenTransition struct {
	From   string
	Event  string
	To     string
	Screen string
}

// Destination is where the transition goes, in the words a role is told it: the
// state of the same machine, or the id of the screen it leaves to.
func (t ScreenTransition) Destination() string {
	if t.Screen != "" {
		return t.Screen
	}
	return t.To
}

// ScreenInForce is one screen the implementer is told about: the screen's id,
// the states its machine in force declares, and that machine's transitions. It
// is what the drivers and the screen's own transition function are authored
// against.
type ScreenInForce struct {
	ID          string
	States      []string
	Transitions []ScreenTransition
}

// parseScreenDeclaration reads the three lines that carry the machine — SCREEN,
// TRANSITION and TERMINAL — onto the refined value. Its caller has already
// decided the line is one of them.
func parseScreenDeclaration(refined *Refined, line string) error {
	switch {
	case strings.HasPrefix(line, "SCREEN "):
		rest := strings.TrimPrefix(line, "SCREEN ")
		initial, states, found := strings.Cut(rest, ": ")
		if !found || strings.TrimSpace(initial) == "" {
			return fmt.Errorf("%w: a screen line is SCREEN <initial state>: <states>, and this is %q", ErrReply, line)
		}
		if refined.Screen != nil {
			return fmt.Errorf("%w: the spec author declared a second screen, and an item's spec declares one", ErrReply)
		}
		refined.Screen = &ScreenMachine{Initial: strings.TrimSpace(initial), States: commaList(states)}
		return nil
	case strings.HasPrefix(line, "TRANSITION "):
		rest := strings.TrimPrefix(line, "TRANSITION ")
		head, to, found := strings.Cut(rest, ": ")
		from, event, split := strings.Cut(strings.TrimSpace(head), " ")
		if !found || !split || strings.TrimSpace(to) == "" || from == "" || strings.TrimSpace(event) == "" {
			return fmt.Errorf("%w: a transition line is TRANSITION <from> <event>: <to>, and this is %q", ErrReply, line)
		}
		if refined.Screen == nil {
			return fmt.Errorf("%w: a transition arrived before the screen it belongs to", ErrReply)
		}
		// A destination written as SCREEN <id> leaves the screen: it names
		// another screen and not a state of this machine, and a transition
		// leaves the screen or it stays, never both.
		transition := ScreenTransition{From: from, Event: strings.TrimSpace(event)}
		if word, screen, _ := strings.Cut(strings.TrimSpace(to), " "); word == "SCREEN" {
			if strings.TrimSpace(screen) == "" {
				return fmt.Errorf("%w: a transition leaving the screen names the screen by id, and this is %q", ErrReply, line)
			}
			transition.Screen = strings.TrimSpace(screen)
		} else {
			transition.To = strings.TrimSpace(to)
		}
		refined.Screen.Transitions = append(refined.Screen.Transitions, transition)
		if !contains(refined.Screen.Events, transition.Event) {
			refined.Screen.Events = append(refined.Screen.Events, transition.Event)
		}
		return nil
	default:
		if refined.Screen == nil {
			return fmt.Errorf("%w: a terminal state arrived before the screen it belongs to", ErrReply)
		}
		refined.Screen.Terminal = commaList(strings.TrimPrefix(line, "TERMINAL: "))
		return nil
	}
}

// commaList is one comma-separated list of names, each trimmed, empty entries
// dropped.
func commaList(text string) []string {
	var out []string
	for _, part := range strings.Split(text, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

// contains reports whether the list already holds the value.
func contains(list []string, value string) bool {
	for _, one := range list {
		if one == value {
			return true
		}
	}
	return false
}
