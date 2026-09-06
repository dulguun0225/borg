package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// ShippedInterviewerPrompt is the role prompt the product ships for the
// interviewer, entered through the artifact store at the factory's first start
// and read in force at dispatch. The interviewer is put on an intent and not on
// an item: there is no item to author against until decomposition has run, and
// what this role produces is the reading a requester confirms.
//
// The six sentence forms are the EARS patterns package criterion classifies by,
// stated here because the reading is written in them: the completeness checks
// and the item-size target are denominated in one requirement, and a statement
// carrying two behaviours would make that unit whatever the interview chose.
const ShippedInterviewerPrompt = `You run the interview of one intent in a software factory. From the request's statement and any answered questions, either ask what you cannot proceed without or state what you have understood is wanted.

What ends the questioning is having enough to cut the request into items and author a spec per item, and never a human saying the request is clear. Ask only what you cannot get that far without.

What you state is the reading: the request in the requester's own terms, as a set of statements and never as a paragraph. One statement carries one behaviour, and each is one sentence in one of the six EARS patterns:

  always_true:                   The system shall <response>.
  event:                         When <trigger>, the system shall <response>.
  state:                         While <state>, the system shall <response>.
  state_with_an_event_inside_it: While <state>, when <trigger>, the system shall <response>.
  unwanted_condition:            If <condition>, then the system shall <response>.
  optional_feature:              Where <feature>, the system shall <response>.

The reading says what is wanted and never how it is built: no file, no module, no interface of your own invention. A constraint the statement makes is part of what is wanted and is stated as one of the requirements.

You decide nothing about how the work is cut up, nothing about which service changes, and no acceptance criterion: the requester confirms this reading, decomposition cuts it into items, and the spec stage authors the criteria against it.

Where the user message carries a reject or a rework request, it names what was found wrong with the reading it decided over. State the reading again against what was found wrong.

Reply in exactly one of two forms, with nothing before or after the form.

To ask, reply with one line:

QUESTION: <the question>

To state the reading, reply with a READING: header and one requirement per line:

READING:
REQUIREMENT: <one statement in one of the six patterns>

` + Rules

// Reading is what one [Interviewer.Interview] call produced. Exactly one of
// Question or Requirements is set: a question where the role cannot proceed
// without one more answer, the reading where it can. Units is what the call
// spent, per kind the provider counts apart, which the component that
// dispatched the role records.
type Reading struct {
	Question string
	// Requirements is the reading as statements, one behaviour each, which
	// intake writes as the intent's requirements when the requester confirms
	// them.
	Requirements []string
	Units        map[string]int64
}

// Interviewing is what one [Interviewer.Interview] call is given: the intent's
// statement, the rounds answered so far, and what sent the intent back here.
//
// It names no service and no item. The interview happens before decomposition,
// so there is neither to name, and the criteria in force are the spec stage's
// material and not this role's.
type Interviewing struct {
	Statement string
	Answered  []Question
	Returned  Returned
}

// Interviewer is the agent in the interview's role, one of the two put on an
// intent.
type Interviewer struct {
	Model Model
	// Prompt is the role prompt version in force, handed over by the component
	// that dispatched the role. An empty one is [ErrNoPrompt].
	Prompt string
	// Effort is the effort the fleet entry names, handed over by the same
	// component and sent with the call. An empty one asks the provider for
	// none.
	Effort string
}

// Interview sends the role prompt and parses the reply into a [Reading].
//
// The statement and the answers are content: nothing they say changes what this
// method does with the reply, and a reply outside the protocol is [ErrReply]
// however plausible its text.
func (i Interviewer) Interview(ctx context.Context, as principal.Principal, of Interviewing) (Reading, error) {
	if i.Prompt == "" {
		return Reading{}, ErrNoPrompt
	}
	var b strings.Builder
	b.WriteString("The request's statement:\n")
	b.WriteString(of.Statement)
	b.WriteString("\n")
	if len(of.Answered) > 0 {
		b.WriteString("\nAsked and answered:\n")
		for _, q := range of.Answered {
			fmt.Fprintf(&b, "Q: %s\nA: %s\n", q.Question, q.Answer)
		}
		b.WriteString("The interview's one round is spent: reply with a READING and not a QUESTION.\n")
	}
	writeReturned(&b, of.Returned)
	reply, err := i.Model.Complete(ctx, as, Call{System: i.Prompt, User: b.String(), Effort: i.Effort})
	if err != nil {
		return Reading{}, err
	}
	read, err := parseReading(reply.Text)
	// A second question is outside the protocol once the round is spent — the
	// interview is one round or none — and refusing it here is what lets the
	// stage try again inside its attempt limit rather than stop the run.
	if err == nil && read.Question != "" && len(of.Answered) > 0 {
		err = fmt.Errorf("%w: the interviewer asked a second question, and the interview is one round or none", ErrReply)
	}
	if err != nil {
		// The refused reply's spend goes back with the error: the units were
		// spent whether or not the reply was usable, and the component
		// retrying this call records every attempt.
		return Reading{Units: reply.Units}, err
	}
	read.Units = reply.Units
	return read, nil
}

// parseReading reads the two forms the role's prompt states and nothing else: a
// question, or a reading of one requirement per line. A reply in neither form,
// one carrying both, and a reading with no requirement in it are each
// [ErrReply].
func parseReading(text string) (Reading, error) {
	lines := protocolLines(text)
	if len(lines) == 0 {
		return Reading{}, fmt.Errorf("%w: the interviewer replied nothing", ErrReply)
	}
	if question, is := strings.CutPrefix(lines[0], "QUESTION:"); is {
		if len(lines) > 1 {
			return Reading{}, fmt.Errorf("%w: the interviewer asked and stated a reading in one reply", ErrReply)
		}
		asked := strings.TrimSpace(question)
		if asked == "" {
			return Reading{}, fmt.Errorf("%w: the interviewer's question is empty", ErrReply)
		}
		return Reading{Question: asked}, nil
	}
	if lines[0] != "READING:" {
		return Reading{}, fmt.Errorf("%w: the interviewer's reply starts with neither QUESTION: nor READING:", ErrReply)
	}
	var read Reading
	for _, line := range lines[1:] {
		statement, is := strings.CutPrefix(line, "REQUIREMENT:")
		if !is {
			return Reading{}, fmt.Errorf("%w: the interviewer's reading carries a line that is not a REQUIREMENT:", ErrReply)
		}
		if statement = strings.TrimSpace(statement); statement != "" {
			read.Requirements = append(read.Requirements, statement)
		}
	}
	if len(read.Requirements) == 0 {
		return Reading{}, fmt.Errorf("%w: the interviewer's reading states no requirement", ErrReply)
	}
	return read, nil
}
