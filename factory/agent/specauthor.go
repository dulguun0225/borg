package agent

import (
	"context"
	"fmt"
	"strings"
)

// SpecAuthorSystemPrompt is what the spec author is told. It is a constant a
// reader checks here rather than trusting a summary of, because roadmap M1
// makes the instruction texts part of the milestone. The six sentence forms
// are the EARS patterns the criterion package classifies by; the reply forms
// are what [SpecAuthor.Refine] parses.
//
// One paragraph of it says what the spec's free text must carry, and it is
// there because of what happened without it. Measured on 2026-08-20 against
// three models: asked for a Go service naming a module, a package, a file
// layout, a port, standard-library-only, and a go.mod the change must include,
// every model replied with a spec that kept the behaviour and dropped the rest
// — deepseek/deepseek-v4-flash in 52 bytes, deepseek/deepseek-v4-pro in 71.
// The implementation stage is given the spec and never the statement, so it
// then wrote a program with no go.mod and the build failed with nothing to
// build. The same model wrote a go.mod when handed a spec that mentioned one,
// which is why this is a paragraph here rather than a stronger model.
//
// What it costs: the spec grows toward the statement's own length wherever the
// statement is mostly constraint, and a spec that restates a constraint the
// statement got wrong carries the mistake forward instead of dropping it.
const SpecAuthorSystemPrompt = `You author the spec of one item in a software factory. From the intent's statement and any answered questions, produce a spec of exactly one acceptance criterion.

The criterion is one sentence in one of the six EARS patterns:

  always_true:        The system shall <response>.
  event:              When <trigger>, the system shall <response>.
  state:              While <state>, the system shall <response>.
  state_with_event:   While <state>, when <trigger>, the system shall <response>.
  unwanted_condition: If <condition>, then the system shall <response>.
  optional_feature:   Where <feature>, the system shall <response>.

Where the user message lists the criteria already in force for the service — one per line, the criterion's id, a colon, a space, and its sentence — each is a promise the service already makes. The one criterion you author is not among them and restates none of them: nothing here withdraws a criterion, so a restatement would leave the service promising the same thing twice, under two ids that both have to be encoded.

The spec is what the implementation stage is given in place of the statement, which that stage never sees. So it restates every constraint the statement makes rather than summarising the behaviour: what the change is named — a module, a package, a file path, a port — what it may and may not use, and every file the change must contain. A constraint the statement makes and the spec leaves out is one nothing downstream can meet, because the stage that would meet it is not told of it.

You may ask at most one question, and only one you cannot author the spec without.

Reply in exactly one of two forms, with nothing before or after the form.

To ask the question, reply with one line:

QUESTION: <the question>

To deliver the spec, reply with a SPEC: header, the spec's text, and the criterion as the final line:

SPEC:
<the spec, free text>
CRITERION: <the one criterion sentence>

` + Rules

// QA is one question of the interview together with its answer.
type QA struct {
	Question string
	Answer   string
}

// Refined is what one [SpecAuthor.Refine] call produced. Exactly one of
// Question or the Spec and Criterion pair is set: a question when the model
// cannot author the spec without one more answer, the pair when it can.
// Tokens is the call's spend, which the stage reports to dispatch.
type Refined struct {
	Question  string
	Spec      string
	Criterion string
	Tokens    int64
}

// SpecAuthor is the agent in the spec stage's role.
type SpecAuthor struct {
	Model Model
}

// Refine sends the intent's statement, the answers so far, and the criteria
// already in force for the service, and parses the reply into a [Refined]. The
// criteria in force are what a second item on a service authors against: with
// no withdrawal written anywhere, a criterion restated under a second id is a
// second promise the build then has to encode. inForce is empty for the first
// item on a service, and the prompt then lists nothing.
//
// The statement, the answers, and the sentences are content: nothing they say
// changes what this method does with the reply, and a reply outside the
// protocol is [ErrReply] however plausible its text.
func (s SpecAuthor) Refine(ctx context.Context, statement string, answered []QA, inForce []Criterion) (Refined, error) {
	var b strings.Builder
	b.WriteString("The intent's statement:\n")
	b.WriteString(statement)
	b.WriteString("\n")
	if len(answered) > 0 {
		b.WriteString("\nAsked and answered:\n")
		for _, qa := range answered {
			fmt.Fprintf(&b, "Q: %s\nA: %s\n", qa.Question, qa.Answer)
		}
	}
	if len(inForce) > 0 {
		b.WriteString("\n")
		writeCriteria(&b, "The criteria already in force for the service:", inForce)
	}
	reply, err := s.Model.Complete(ctx, SpecAuthorSystemPrompt, b.String())
	if err != nil {
		return Refined{}, err
	}
	refined, err := parseRefined(reply.Text)
	if err != nil {
		// The refused reply's spend goes back with the error: the tokens were
		// spent whether or not the reply was usable, and the stage retrying this
		// call reports every attempt to dispatch. Nothing else on the value is
		// set, so a caller that ignores the error reads no spec from it.
		return Refined{Tokens: reply.Tokens}, err
	}
	refined.Tokens = reply.Tokens
	return refined, nil
}

// parseRefined reads the spec author's reply strictly: one QUESTION line, or
// a SPEC: header, at least one line of text, and a final CRITERION line.
// Anything else is [ErrReply] — a protocol keyword on a line that is not its
// place included, which is what refuses a reply carrying both forms. What
// that costs: a spec whose own text starts a line with QUESTION: or
// CRITERION: cannot be delivered.
func parseRefined(text string) (Refined, error) {
	lines := protocolLines(text)
	if len(lines) == 0 {
		return Refined{}, fmt.Errorf("%w: the spec author replied nothing", ErrReply)
	}
	if q, found := strings.CutPrefix(lines[0], "QUESTION: "); found {
		if len(lines) > 1 {
			return Refined{}, fmt.Errorf("%w: a question is one line and the spec author replied %d", ErrReply, len(lines))
		}
		if strings.TrimSpace(q) == "" {
			return Refined{}, fmt.Errorf("%w: the question is empty", ErrReply)
		}
		return Refined{Question: strings.TrimSpace(q)}, nil
	}
	if lines[0] != "SPEC:" {
		return Refined{}, fmt.Errorf("%w: the spec author's reply starts with neither QUESTION: nor SPEC:", ErrReply)
	}
	criterion, found := strings.CutPrefix(lines[len(lines)-1], "CRITERION: ")
	if !found || strings.TrimSpace(criterion) == "" {
		return Refined{}, fmt.Errorf("%w: the spec author's final line is not a CRITERION sentence", ErrReply)
	}
	body := lines[1 : len(lines)-1]
	for _, line := range body {
		if strings.HasPrefix(line, "QUESTION: ") || strings.HasPrefix(line, "CRITERION: ") || line == "SPEC:" {
			return Refined{}, fmt.Errorf("%w: a protocol keyword sits inside the spec text", ErrReply)
		}
	}
	spec := strings.TrimSpace(strings.Join(body, "\n"))
	if spec == "" {
		return Refined{}, fmt.Errorf("%w: the spec text is empty", ErrReply)
	}
	return Refined{Spec: spec, Criterion: strings.TrimSpace(criterion)}, nil
}

// protocolLines is a reply cut into lines for a protocol parse: blank space
// around the whole reply trimmed, because a model pads its answer, and a
// carriage return stripped from each line's end. Nothing inside a line is
// changed.
func protocolLines(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, "\r")
	}
	return lines
}
