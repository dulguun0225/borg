package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestRefineParsesAScreenStateMachine: where the item has a user interface the
// spec version declares the screen's machine, and the states, events,
// transitions and terminal states come back on the value.
func TestRefineParsesAScreenStateMachine(t *testing.T) {
	model := &fakeModel{
		text: "SPEC:\nThe screen lists health.\n" +
			"CRITERION rq_a: The system shall answer.\n" +
			"SCREEN loading: loading, ready, failed\n" +
			"TRANSITION loading loaded: ready\n" +
			"TRANSITION loading errored: failed\n" +
			"TERMINAL: ready, failed\n",
	}
	refined, err := SpecAuthor{Model: model, Prompt: ShippedSpecAuthorPrompt}.Refine(context.Background(), as(), Refining{
		Statement: "a health screen", UserInterface: true,
	})
	if err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if refined.Screen == nil {
		t.Fatal("Screen is nil, want the machine the reply declared")
	}
	if refined.Screen.Initial != "loading" || len(refined.Screen.States) != 3 {
		t.Errorf("Screen = %+v, want the initial state and the three declared states", refined.Screen)
	}
	if len(refined.Screen.Transitions) != 2 || refined.Screen.Transitions[0].To != "ready" {
		t.Errorf("Transitions = %+v, want the two the reply declared", refined.Screen.Transitions)
	}
	if len(refined.Screen.Events) != 2 {
		t.Errorf("Events = %v, want one per event the transitions name", refined.Screen.Events)
	}
	if len(refined.Screen.Terminal) != 2 {
		t.Errorf("Terminal = %v, want the two terminal states", refined.Screen.Terminal)
	}
	if !strings.Contains(model.user, "user interface") {
		t.Errorf("the user message does not say the item has a user interface: %q", model.user)
	}
}

// TestATransitionMayLeaveTheScreen: a transition's destination may be another
// screen named by id rather than a state of this machine, which is what joins
// screens into the sequence a person crosses. A transition leaves the screen or
// it stays, never both.
func TestATransitionMayLeaveTheScreen(t *testing.T) {
	model := &fakeModel{
		text: "SPEC:\nThe screen lists health.\n" +
			"CRITERION rq_a: The system shall answer.\n" +
			"SCREEN loading: loading, ready\n" +
			"TRANSITION loading loaded: ready\n" +
			"TRANSITION ready done: SCREEN ssm_0123456789abcdef0123456789abcdef\n" +
			"TERMINAL: ready\n",
	}
	refined, err := SpecAuthor{Model: model, Prompt: ShippedSpecAuthorPrompt}.Refine(context.Background(), as(), Refining{
		Statement: "a health screen", UserInterface: true,
	})
	if err != nil {
		t.Fatalf("Refine: %v", err)
	}
	if len(refined.Screen.Transitions) != 2 {
		t.Fatalf("Transitions = %+v, want two", refined.Screen.Transitions)
	}
	staying, leaving := refined.Screen.Transitions[0], refined.Screen.Transitions[1]
	if staying.To != "ready" || staying.Screen != "" {
		t.Errorf("the staying transition is %+v, want it to name a state and no screen", staying)
	}
	if leaving.Screen != "ssm_0123456789abcdef0123456789abcdef" || leaving.To != "" {
		t.Errorf("the leaving transition is %+v, want it to name a screen and no state", leaving)
	}
	if leaving.Destination() != leaving.Screen {
		t.Errorf("Destination = %q, want the screen it leaves to", leaving.Destination())
	}
}

// TestATransitionLeavingToNothingIsRefused: SCREEN with no id names no screen,
// and a destination that names nothing is a reply outside the protocol.
func TestATransitionLeavingToNothingIsRefused(t *testing.T) {
	model := &fakeModel{
		text: "SPEC:\nThe screen lists health.\n" +
			"CRITERION rq_a: The system shall answer.\n" +
			"SCREEN loading: loading\n" +
			"TRANSITION loading done: SCREEN \n",
	}
	_, err := SpecAuthor{Model: model, Prompt: ShippedSpecAuthorPrompt}.Refine(context.Background(), as(), Refining{
		Statement: "a health screen", UserInterface: true,
	})
	if !errors.Is(err, ErrReply) {
		t.Errorf("Refine = %v, want ErrReply", err)
	}
}

// TestTheSpecAuthorPromptNamesTheLeavingForm: the prompt and the parse are
// changed together, so the form a transition leaving the screen takes is
// written in the shipped words.
func TestTheSpecAuthorPromptNamesTheLeavingForm(t *testing.T) {
	if !strings.Contains(ShippedSpecAuthorPrompt, "TRANSITION <from state> <event>: SCREEN <the id of another screen>") {
		t.Error("ShippedSpecAuthorPrompt does not carry the form of a transition that leaves the screen")
	}
}
