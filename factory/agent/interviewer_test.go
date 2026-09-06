package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestInterviewParsesTheReading: the reading is a set of statements and not a
// paragraph, one behaviour each, which is what intake writes as the intent's
// requirements when the requester confirms it.
func TestInterviewParsesTheReading(t *testing.T) {
	model := &fakeModel{
		text: "READING:\nREQUIREMENT: When asked for its health, the system shall respond ok.\n" +
			"REQUIREMENT: If the check fails, then the system shall say which check failed.\n",
		units: map[string]int64{UnitsInput: 7, UnitsOutput: 2},
	}
	read, err := Interviewer{Model: model, Prompt: ShippedInterviewerPrompt}.Interview(
		context.Background(), as(), Interviewing{
			Statement: "The demo service needs a health check.",
			Answered:  []Question{{Question: "What does a healthy response say?", Answer: "ok"}},
		})
	if err != nil {
		t.Fatalf("Interview: %v", err)
	}
	if read.Question != "" {
		t.Errorf("Question = %q, want none where the reading was stated", read.Question)
	}
	if len(read.Requirements) != 2 || read.Requirements[0] != "When asked for its health, the system shall respond ok." {
		t.Errorf("Requirements = %v, want one per REQUIREMENT line", read.Requirements)
	}
	if read.Units[UnitsOutput] != 2 {
		t.Errorf("Units = %v, want the reply's units per kind", read.Units)
	}
	if model.system != ShippedInterviewerPrompt {
		t.Error("the prompt sent is not the one the role was handed")
	}
	for _, want := range []string{"The demo service needs a health check.", "What does a healthy response say?", "ok"} {
		if !strings.Contains(model.user, want) {
			t.Errorf("the user message does not carry %q:\n%s", want, model.user)
		}
	}
}

// TestInterviewParsesTheQuestion: the other of the two forms, which is the
// interview asking one more round.
func TestInterviewParsesTheQuestion(t *testing.T) {
	model := &fakeModel{text: "QUESTION: What does a healthy response say?\n"}
	read, err := Interviewer{Model: model, Prompt: ShippedInterviewerPrompt}.Interview(
		context.Background(), as(), Interviewing{Statement: "The demo service needs a health check."})
	if err != nil {
		t.Fatalf("Interview: %v", err)
	}
	if read.Question != "What does a healthy response say?" || len(read.Requirements) != 0 {
		t.Errorf("the reply parsed to %+v, want the question alone", read)
	}
}

// TestAReplyOutsideTheTwoFormsIsRefused: a reply is parsed and never
// interpreted, so one in neither form, one in both, and an empty reading are
// each refused whatever they say.
func TestAReplyOutsideTheTwoFormsIsRefused(t *testing.T) {
	for _, reply := range []string{
		"The reading is that the service needs a health check.",
		"QUESTION: what?\nREADING:\nREQUIREMENT: The system shall answer.",
		"READING:\nThe system shall answer.",
		"READING:\n",
		"QUESTION:",
		"",
	} {
		model := &fakeModel{text: reply}
		_, err := Interviewer{Model: model, Prompt: ShippedInterviewerPrompt}.Interview(
			context.Background(), as(), Interviewing{Statement: "s"})
		if !errors.Is(err, ErrReply) {
			t.Errorf("Interview on %q = %v, want ErrReply", reply, err)
		}
	}
}

// TestInterviewCarriesWhatSentTheIntentBack: a rework request naming the intent
// reopens the interview, and the round that follows is told what was found
// wrong with the reading it decided over.
func TestInterviewCarriesWhatSentTheIntentBack(t *testing.T) {
	model := &fakeModel{text: "READING:\nREQUIREMENT: The system shall answer.\n"}
	if _, err := (Interviewer{Model: model, Prompt: ShippedInterviewerPrompt}).Interview(
		context.Background(), as(), Interviewing{
			Statement: "s",
			Returned:  Returned{Reason: "the reading missed the refund case", Version: "the earlier reading"},
		}); err != nil {
		t.Fatalf("Interview: %v", err)
	}
	for _, want := range []string{"the reading missed the refund case", "the earlier reading"} {
		if !strings.Contains(model.user, want) {
			t.Errorf("the user message does not carry %q:\n%s", want, model.user)
		}
	}
}

func TestInterviewRefusesARoleWithNoPromptInForce(t *testing.T) {
	model := &fakeModel{text: "READING:\nREQUIREMENT: The system shall answer.\n"}
	_, err := Interviewer{Model: model}.Interview(context.Background(), as(), Interviewing{Statement: "s"})
	if !errors.Is(err, ErrNoPrompt) {
		t.Fatalf("Interview = %v, want ErrNoPrompt", err)
	}
	if model.system != "" {
		t.Error("a role with no version in force called the model")
	}
}
