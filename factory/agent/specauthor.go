package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
)

// ShippedSpecAuthorPrompt is the role prompt the product ships for the spec
// author. It is not what a run reads: the factory enters it through the
// artifact store at its first start, and dispatch hands the role the version
// in force. It is a constant a reader checks here rather than trusting a
// summary of, because roadmap M1 makes the instruction texts part of the
// milestone. The six sentence forms are the EARS patterns the criterion
// package classifies by; the reply forms are what [SpecAuthor.Refine] parses.
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
const ShippedSpecAuthorPrompt = `You author the spec of one item in a software factory. From the intent's statement and any answered questions, produce a spec and the acceptance criteria it introduces.

An item names exactly one service, and the user message names which. One request can produce several items in several services — a change that crosses a published interface is at least three of them — so the spec you author is the part of the request that belongs to the service named and never the whole of it. The criteria already in force are that service's own.

Each criterion is one sentence in one of the six EARS patterns:

  always_true:        The system shall <response>.
  event:              When <trigger>, the system shall <response>.
  state:              While <state>, the system shall <response>.
  state_with_event:   While <state>, when <trigger>, the system shall <response>.
  unwanted_condition: If <condition>, then the system shall <response>.
  optional_feature:   Where <feature>, the system shall <response>.

Every criterion names the requirement it answers. The user message lists this item's requirements one per line, the requirement's id, a colon, a space, and its statement; write that id on the criterion's line, after the word CRITERION and before the colon. Every requirement listed is named by at least one criterion, and a criterion answering no listed requirement is not authored. Where the user message lists no requirement at all, the criteria are what the requirements will be written from, so write the criterion line with no id, as CRITERION followed straight by the colon.

Where the user message lists the criteria already in force for the service — one per line, the criterion's id, a colon, a space, and its sentence — each is a promise the service already makes. A criterion you author is not among them and restates none of them. Where the change replaces a promise the service already makes, withdraw the old criterion by its id in the same spec: a restatement without a withdrawal leaves the service promising the same thing twice, under two ids that both have to be encoded.

Where the user message says the item has a user interface, the spec also declares that screen's state machine: its states, the initial one, the events, and one transition per state and event. A machine is well formed — no two transitions on one event from one state, every declared state reachable from the initial one, and every state either terminal or answering an event — and the implementation is rejected where it admits a transition the machine does not declare.

The spec is what the implementation stage is given in place of the statement, which that stage never sees. So it restates every constraint the statement makes rather than summarising the behaviour: what the change is named — a module, a package, a file path, a port — what it may and may not use, and every file the change must contain. A constraint the statement makes and the spec leaves out is one nothing downstream can meet, because the stage that would meet it is not told of it.

Where the user message carries a reject or a rework request, it names what was found wrong with the version it decided over. Author against what was found wrong.

You may ask at most one question, and only one you cannot author the spec without.

Reply in exactly one of two forms, with nothing before or after the form.

To ask the question, reply with one line:

QUESTION: <the question>

To deliver the spec, reply with a SPEC: header, the spec's text, and then the criteria, any withdrawals, and any screen state machine, one per line, in that order:

SPEC:
<the spec, free text>
CRITERION <requirement id>: <the criterion sentence>
WITHDRAW: <the id of a criterion in force this spec replaces>
SCREEN <initial state>: <state>, <state>, ...
TRANSITION <from state> <event>: <to state>
TERMINAL: <state>, <state>, ...

` + Rules

// Question is one question of the interview together with its answer, which is
// what the two records the store keeps are called.
type Question struct {
	Question string
	Answer   string
}

// Requirement is one requirement of the intent as a role is told it: the id a
// criterion names and the statement the requirement makes. It is what makes
// traceability's third link authored rather than reconstructed — a criterion
// names the requirement it answers, and this is where the ids come from.
type Requirement struct {
	ID        string
	Statement string
}

// Returned is what sent the item back to this stage: the reason a gate's
// reject or an author's rework request carried, and the text of the version it
// was decided over. It is material like everything else a stage hands an
// agent, and it is empty on a first attempt.
type Returned struct {
	Reason string
	// Version is the artifact version's own text — the spec, the plan, the
	// tasks — as the decision read it.
	Version string
}

// Empty reports whether nothing sent the item back here.
func (r Returned) Empty() bool { return r.Reason == "" && r.Version == "" }

// DraftCriterion is one criterion the spec author authored: the sentence, and
// the id of the requirement it answers.
type DraftCriterion struct {
	Sentence      string
	RequirementID string
}

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

// ScreenTransition is one transition of a [ScreenMachine].
type ScreenTransition struct {
	From  string
	Event string
	To    string
}

// Refined is what one [SpecAuthor.Refine] call produced. Exactly one of
// Question or the Spec and Criteria pair is set: a question when the model
// cannot author the spec without one more answer, the pair when it can. Units
// is what the call spent, per kind the provider counts apart, which the
// component that dispatched the role records.
type Refined struct {
	Question string
	Spec     string
	// Criteria is what the version introduces, and Withdrawn the ids of the
	// criteria in force it takes down in the same version.
	Criteria  []DraftCriterion
	Withdrawn []string
	// Screen is the state machine the version declares where the item has a
	// user interface, and is nil where it declares none.
	Screen *ScreenMachine
	Units  map[string]int64
}

// SpecAuthor is the agent in the spec stage's role.
type SpecAuthor struct {
	Model Model
	// Prompt is the role prompt version in force, which dispatch read off the
	// artifact store's chain for this role and handed over. A role with no
	// prompt is a dispatch that should not have happened, and [SpecAuthor.Refine]
	// refuses it rather than falling back on the shipped words.
	Prompt string
	// Effort is the effort the fleet entry names, handed over by the same
	// component and sent with the call. An empty one asks the provider for
	// none.
	Effort string
}

// Refining is what one [SpecAuthor.Refine] call is given: the intent's
// statement, the service the item changes, the answers so far, the
// requirements this item answers, the criteria already in force for that
// service, whether the item has a user interface, and what sent the item back
// here.
//
// The service is named because an item names one and an intent may produce
// several: a change that crosses a published interface is three items over two
// services, and a spec author told only the statement would author the whole
// request into each of them.
type Refining struct {
	Statement    string
	Service      string
	Answered     []Question
	Requirements []Requirement
	InForce      []Criterion
	// UserInterface is whether the item has a screen, which is what makes the
	// state machine part of the version rather than an extra.
	UserInterface bool
	Returned      Returned
}

// ErrNoPrompt is returned by a role whose role prompt version is empty. A role
// with no version in force is a hold dispatch writes and a run it does not
// make, so a call that got this far is a defect in the caller.
var ErrNoPrompt = fmt.Errorf("agent: the role has no role prompt version in force")

// Refine sends the role prompt and parses the reply into a [Refined].
//
// The statement, the answers, and the sentences are content: nothing they say
// changes what this method does with the reply, and a reply outside the
// protocol is [ErrReply] however plausible its text.
func (s SpecAuthor) Refine(ctx context.Context, as principal.Principal, of Refining) (Refined, error) {
	if s.Prompt == "" {
		return Refined{}, ErrNoPrompt
	}
	var b strings.Builder
	b.WriteString("The intent's statement:\n")
	b.WriteString(of.Statement)
	b.WriteString("\n")
	if of.Service != "" {
		fmt.Fprintf(&b, "\nThe service this item changes: %s\n", of.Service)
	}
	if len(of.Answered) > 0 {
		b.WriteString("\nAsked and answered:\n")
		for _, q := range of.Answered {
			fmt.Fprintf(&b, "Q: %s\nA: %s\n", q.Question, q.Answer)
		}
	}
	if len(of.Requirements) > 0 {
		b.WriteString("\nThe requirements this item answers:\n")
		for _, r := range of.Requirements {
			fmt.Fprintf(&b, "%s: %s\n", r.ID, r.Statement)
		}
	}
	if len(of.InForce) > 0 {
		b.WriteString("\n")
		writeCriteria(&b, "The criteria already in force for the service:", of.InForce)
	}
	if of.UserInterface {
		b.WriteString("\nThis item has a user interface, so the spec declares its screen's state machine.\n")
	}
	writeReturned(&b, of.Returned)
	reply, err := s.Model.Complete(ctx, as, Call{System: s.Prompt, User: b.String(), Effort: s.Effort})
	if err != nil {
		return Refined{}, err
	}
	refined, err := parseRefined(reply.Text)
	if err != nil {
		// The refused reply's spend goes back with the error: the units were
		// spent whether or not the reply was usable, and the component
		// retrying this call records every attempt. Nothing else on the value
		// is set, so a caller that ignores the error reads no spec from it.
		return Refined{Units: reply.Units}, err
	}
	refined.Units = reply.Units
	return refined, nil
}

// writeReturned puts a reject or a rework request into a role's user message,
// with its reason and the version it was decided over. One function, so the
// four roles cannot describe the same material differently.
func writeReturned(b *strings.Builder, r Returned) {
	if r.Empty() {
		return
	}
	fmt.Fprintf(b, "\nThis item was sent back to this stage. What was found wrong: %s\n", r.Reason)
	if r.Version != "" {
		fmt.Fprintf(b, "The version that was decided over:\n%s\n", r.Version)
	}
}

// parseRefined reads the spec author's reply strictly: one QUESTION line, or
// a SPEC: header, at least one line of text, and then the criteria, the
// withdrawals and the machine, each on a line of its own. Anything else is
// [ErrReply] — a protocol keyword on a line that is not its place included,
// which is what refuses a reply carrying both forms. What that costs: a spec
// whose own text starts a line with one of the keywords cannot be delivered.
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

	// The spec's free text runs from the header to the first declaration line,
	// and every line after it is a declaration.
	first := len(lines)
	for n := 1; n < len(lines); n++ {
		if declares(lines[n]) {
			first = n
			break
		}
	}
	spec := strings.TrimSpace(strings.Join(lines[1:first], "\n"))
	if spec == "" {
		return Refined{}, fmt.Errorf("%w: the spec text is empty", ErrReply)
	}
	for _, line := range lines[1:first] {
		if strings.HasPrefix(line, "QUESTION: ") || line == "SPEC:" {
			return Refined{}, fmt.Errorf("%w: a protocol keyword sits inside the spec text", ErrReply)
		}
	}
	refined := Refined{Spec: spec}
	for _, line := range lines[first:] {
		if err := parseDeclaration(&refined, line); err != nil {
			return Refined{}, err
		}
	}
	if len(refined.Criteria) == 0 {
		return Refined{}, fmt.Errorf("%w: the spec author's reply names no criterion", ErrReply)
	}
	return refined, nil
}

// declares reports whether a line opens one of the spec author's declarations.
func declares(line string) bool {
	for _, keyword := range []string{"CRITERION ", "CRITERION: ", "WITHDRAW: ", "SCREEN ", "TRANSITION ", "TERMINAL: "} {
		if strings.HasPrefix(line, keyword) {
			return true
		}
	}
	return false
}

// parseDeclaration reads one declaration line onto the refined value.
func parseDeclaration(refined *Refined, line string) error {
	switch {
	case strings.HasPrefix(line, "CRITERION"):
		// Two forms: the id between the word and the colon where the item has
		// requirements to answer, and no id at all where the user message
		// listed none — the first call of an interview, whose criteria are
		// what the requirements are then written from.
		rest := strings.TrimPrefix(line, "CRITERION")
		requirement, sentence, found := strings.Cut(rest, ": ")
		if !found || strings.TrimSpace(sentence) == "" {
			return fmt.Errorf("%w: a criterion line is CRITERION <requirement id>: <sentence>, and this is %q", ErrReply, line)
		}
		refined.Criteria = append(refined.Criteria, DraftCriterion{
			Sentence: strings.TrimSpace(sentence), RequirementID: strings.TrimSpace(requirement),
		})
		return nil
	case strings.HasPrefix(line, "WITHDRAW: "):
		id := strings.TrimSpace(strings.TrimPrefix(line, "WITHDRAW: "))
		if id == "" {
			return fmt.Errorf("%w: a withdrawal names the criterion it takes down", ErrReply)
		}
		refined.Withdrawn = append(refined.Withdrawn, id)
		return nil
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
		refined.Screen.Transitions = append(refined.Screen.Transitions, ScreenTransition{
			From: from, Event: strings.TrimSpace(event), To: strings.TrimSpace(to),
		})
		if !contains(refined.Screen.Events, strings.TrimSpace(event)) {
			refined.Screen.Events = append(refined.Screen.Events, strings.TrimSpace(event))
		}
		return nil
	case strings.HasPrefix(line, "TERMINAL: "):
		if refined.Screen == nil {
			return fmt.Errorf("%w: a terminal state arrived before the screen it belongs to", ErrReply)
		}
		refined.Screen.Terminal = commaList(strings.TrimPrefix(line, "TERMINAL: "))
		return nil
	default:
		return fmt.Errorf("%w: the spec author's line %q is none of its declarations", ErrReply, line)
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
