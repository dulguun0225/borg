// The fake model the tests run against: the statements and criteria it
// answers with by role, the program it has the implementer write, and the
// wrappers that make one candidate's merge conflict or its implementer's reply refuse.
package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/principal"
)

// The statements the tests give the run, and the spec and the criterion the fake
// spec author authors for each. Keyed by the statement so that two candidates
// authored in one run author two different promises, which is what makes them two
// candidates rather than one change twice.
const (
	theStatement       = "The demo service needs a health check."
	theSecondStatement = "The demo service needs a version endpoint."
	theThirdStatement  = "The demo service needs a readiness endpoint."
	theFourthStatement = "The demo service needs an uptime endpoint."
	theQuestion        = "What does a healthy response say?"
	theAnswer          = "ok"

	criterionSentence       = "When asked for its health, the system shall respond ok."
	secondCriterionSentence = "When asked for its version, the system shall respond two."
	thirdCriterionSentence  = "When asked for its readiness, the system shall respond ready."
	fourthCriterionSentence = "When asked for its uptime, the system shall respond forever."
)

// theSpecs is what the fake spec author writes for each statement. Both sentences
// of every pair classify as the event pattern in the pattern's response form,
// which is what theResponse reads the encoding's expected value out of.
var theSpecs = map[string]struct{ spec, criterion string }{
	theStatement:       {"The demo service answers a health check with ok.", criterionSentence},
	theSecondStatement: {"The demo service answers a version request with two.", secondCriterionSentence},
	theThirdStatement:  {"The demo service answers a readiness check with ready.", thirdCriterionSentence},
	theFourthStatement: {"The demo service answers an uptime request with forever.", fourthCriterionSentence},
}

// rolePromptCriterion picks the criteria out of a role's user prompt: one line per
// criterion, its id then its sentence, which is the shape both prompts render
// and the id shape the criterion package's encoding check matches.
var rolePromptCriterion = regexp.MustCompile(`(?m)^(cr_[0-9a-f]{32}): (.*)$`)

// rolePromptRequirement picks the requirements this item answers out of the spec
// author's user prompt, rendered in the same shape: the id, a colon, and the
// statement. Every criterion names the requirement it answers, so this fake
// writes the first id it is given on the criterion line — the spec author's own
// protocol, and what the Spec row's rejection is read against.
var rolePromptRequirement = regexp.MustCompile(`(?m)^(rq_[0-9a-f]{32}): (.*)$`)

// theResponse reads the response out of a criterion sentence in the event
// pattern, which is where the fake implementer takes an encoding's expected
// value from — the sentence, never the code it checks.
var theResponse = regexp.MustCompile(`shall respond (.+)\.$`)

// fakeModel answers by role, told apart by the system prompt — the same
// constant each real role sends, so a prompt this switch does not know is a
// wiring defect and an error. The first interviewer call asks the one question
// and every call after it states the reading; every spec-author call delivers
// the spec [theSpecs] holds for the statement in the prompt; the implementer
// call returns whole files with one encoding per criterion the prompt names.
type fakeModel struct {
	// interviewCalls is how many times the role put on the intent has been
	// called, the first of which asks the interview's one question.
	interviewCalls int
	// failEvery is how often the program this fake writes emits a failure rather
	// than an ok: nothing at zero, every unit at one, every other unit at two. It is
	// what a deliberately bad deploy is — an implementation that passes every
	// criterion in force and fails a share of the work it does, which is the shape of
	// defect the criteria cannot see and the analysis window exists for.
	failEvery int
}

func (m *fakeModel) Complete(_ context.Context, _ principal.Principal, call agent.Call) (agent.Reply, error) {
	system, user := call.System, call.User
	switch system {
	case agent.ShippedInterviewerPrompt:
		m.interviewCalls++
		if m.interviewCalls == 1 {
			return agent.Reply{Text: "QUESTION: " + theQuestion, Units: map[string]int64{agent.UnitsOutput: 11}}, nil
		}
		// The reading is one statement, and it is the sentence the spec author
		// will answer with a criterion: this fake states the reading in the
		// form a criterion takes, so a run's requirement and its criterion say
		// the same thing and the Spec row's check over what answers what has
		// something to read.
		for statement, authored := range theSpecs {
			if strings.Contains(user, statement) {
				return agent.Reply{
					Text:  "READING:\nREQUIREMENT: " + authored.criterion,
					Units: map[string]int64{agent.UnitsOutput: 7},
				}, nil
			}
		}
		// A revert, whose intent the health monitor wrote at a rollback. The
		// interviewer reads the statement, which is all it ever has.
		if strings.Contains(user, "failed its analysis window and was rolled back") {
			return agent.Reply{
				Text:  "READING:\nREQUIREMENT: When asked what it was restored from, the system shall respond harm.",
				Units: map[string]int64{agent.UnitsOutput: 9},
			}, nil
		}
		return agent.Reply{}, fmt.Errorf("fake model: the interviewer's prompt names no statement this fake reads")
	case agent.ShippedSpecAuthorPrompt:
		for statement, authored := range theSpecs {
			if strings.Contains(user, statement) {
				return agent.Reply{
					Text:  "SPEC:\n" + authored.spec + "\nCRITERION" + answers(user) + ": " + authored.criterion,
					Units: map[string]int64{agent.UnitsOutput: 23},
				}, nil
			}
		}
		// A revert, whose intent the health monitor wrote at a rollback. Nothing on the item
		// says it is one — this reads the statement, which is all a spec author ever has.
		//
		// It introduces a criterion because the spec author's protocol requires exactly
		// one per spec version, which is a simplification M1 made and not something the
		// design asks for: a revert restores a behaviour the service already promises, so
		// the honest version introduces none. What the simplification costs is one
		// criterion per revert that nobody asked for.
		if strings.Contains(user, "failed its analysis window and was rolled back") {
			return agent.Reply{
				Text: "SPEC:\nRestore the behaviour the failed release changed, leaving every criterion in force as it is.\n" +
					"CRITERION" + answers(user) + ": When asked what it was restored from, the system shall respond harm.",
				Units: map[string]int64{agent.UnitsOutput: 19},
			}, nil
		}
		return agent.Reply{}, fmt.Errorf("fake model: the spec author's prompt names no statement this fake authors for")
	case agent.ShippedPlannerPrompt:
		// The plan is prose and the gate over it takes Edit in place, so what
		// this fake writes is one paragraph naming what the implementer will
		// do — enough for the version to exist and be decided, which is what
		// the row demonstrates.
		return agent.Reply{
			Text:  "PLAN:\nWrite one file per criterion in force and one encoding beside each, and a main that emits the quantity the factory watches.",
			Units: map[string]int64{agent.UnitsOutput: 13},
		}, nil
	case agent.ShippedTaskAuthorPrompt:
		return agent.Reply{
			Text:  "TASKS:\nWrite the module file.\nWrite one file and one encoding per criterion in force.\nWrite the main that emits the quantity.",
			Units: map[string]int64{agent.UnitsOutput: 17},
		}, nil
	case agent.ShippedImplementerPrompt:
		named := rolePromptCriterion.FindAllStringSubmatch(user, -1)
		if len(named) == 0 {
			return agent.Reply{}, fmt.Errorf("fake model: the implementer's prompt names no criterion")
		}
		text, err := implementerReply(named, m.failEvery)
		if err != nil {
			return agent.Reply{}, err
		}
		return agent.Reply{Text: text, Units: map[string]int64{agent.UnitsOutput: 37}}, nil
	}
	return agent.Reply{}, fmt.Errorf("fake model: the system prompt is neither role's")
}

// answers is what goes between the word CRITERION and the colon: the id of the
// first requirement the user message lists, and nothing where it lists none —
// which is the interview's own first call, whose criteria are what the
// requirements are then written from.
func answers(user string) string {
	named := rolePromptRequirement.FindStringSubmatch(user)
	if named == nil {
		return ""
	}
	return " " + named[1]
}

// implementerReply is the implementer's whole reply for the criteria the role prompt
// named: a module file, a main function that stays alive so the deployed process
// answers ReadRunning, and one source file plus one encoding per criterion. Every
// criterion in force is encoded because the check over the build rejects one that
// is not, so a candidate's reply carries the encodings of the criteria already in
// force again.
//
// Each pair of files is named by the criterion's id and its content is derived
// from that criterion's sentence, never from the code it checks. Naming them by
// the id rather than by position is what lets two candidates of one service merge:
// each adds the files of the criterion it introduced and rewrites the files of the
// criteria already in force with the same bytes, so no two sides of the merge
// change one file differently.
func implementerReply(named [][]string, failEvery int) (string, error) {
	files := append([]string{
		"=== FILE go.mod ===", "module demo", "", "go 1.24", "=== END ===",
	}, mainGo(failEvery)...)
	for _, match := range named {
		id, sentence := match[1], match[2]
		response := theResponse.FindStringSubmatch(sentence)
		if response == nil {
			return "", fmt.Errorf("fake model: the sentence of %s is not the response form this fake encodes: %q", id, sentence)
		}
		function := "respond_" + id
		files = append(files,
			"=== FILE "+function+".go ===",
			"package main",
			"",
			fmt.Sprintf("func %s() string { return %q }", function, response[1]),
			"=== END ===",
			"=== FILE "+function+"_test.go ===",
			"package main",
			"",
			`import "testing"`,
			"",
			fmt.Sprintf("func Test_%s_candidate_environment(t *testing.T) {", id),
			fmt.Sprintf("\tif %s() != %q {", function, response[1]),
			fmt.Sprintf("\t\tt.Fatalf(%q, %s())", function+"() = %q, the criterion requires "+response[1], function),
			"\t}",
			"}",
			"=== END ===",
		)
	}
	return strings.Join(files, "\n"), nil
}

// interviewed is a fake whose one interview round is already behind it, which is
// what a test swapping the model in mid-way needs: the interview is one round or none
// per intent, and a fresh fake would ask its question again to a reader with nothing in
// it.
func interviewed(failEvery int) *fakeModel {
	return &fakeModel{interviewCalls: 1, failEvery: failEvery}
}

// mainGo is the program every one of these fakes writes, and it is the one place
// this test does what the implementer's standing instruction asks: the program runs
// as a long-lived process, exercises its own behaviour over and over, and appends one
// line per exercise to the file the environment names. Without that the health monitor
// reads nothing, every window ends at its cap, and the whole of this milestone is
// untestable — which is the instruction earning its place rather than decorating the
// prompt.
//
// failEvery is what makes a deploy deliberately bad: nothing at zero, every other
// unit at two. The failure is in no criterion's path, so a build with it passes every
// criterion in force and is failed by its window instead — which is the shape of
// defect the criteria cannot see.
//
// Each line is the time the exercise finished, a tab, and the outcome, which is
// the second emission version's shape: the time is what the factory assigns a
// unit of work to an interval by, and without it a service can be read against
// another build and never against its own past.
//
// The source depends on failEvery and on nothing else, so two good candidates
// of one service write identical bytes and their merge does not conflict, and a run
// that writes the good version over the bad one is a real revert.
func mainGo(failEvery int) []string {
	emit := `"ok"`
	if failEvery == 1 {
		emit = `"error"`
	} else if failEvery > 1 {
		emit = fmt.Sprintf("map[bool]string{true: \"error\", false: \"ok\"}[n%%%d == 0]", failEvery)
	}
	return []string{
		"=== FILE main.go ===",
		"package main",
		"",
		`import (`,
		`	"os"`,
		`	"time"`,
		`)`,
		"",
		"func main() {",
		"\tsignal := os.Getenv(\"BORG_SIGNAL\")",
		"\tfor n := 1; ; n++ {",
		"\t\tif signal != \"\" {",
		"\t\t\tf, err := os.OpenFile(signal, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)",
		"\t\t\tif err == nil {",
		"\t\t\t\t_, _ = f.WriteString(time.Now().UTC().Format(time.RFC3339Nano) + \"\\t\" + " + emit + " + \"\\n\")",
		"\t\t\t\t_ = f.Close()",
		"\t\t\t}",
		"\t\t}",
		"\t\ttime.Sleep(time.Millisecond)",
		"\t}",
		"}",
		"=== END ===",
	}
}

// conflictingModel wraps a model and has the implementer write one more file
// whose content is the id of the criterion this item introduced, which is the last
// one the role prompt names. Two candidates of one service then change one file
// differently, so the second one's re-verification against the master the first
// created is a merge that conflicts — which is a candidate failing on its own
// merits and the merge queue rejecting it.
type conflictingModel struct{ inner agent.Model }

func (m *conflictingModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, as, call)
	if err != nil || call.System != agent.ShippedImplementerPrompt {
		return reply, err
	}
	named := rolePromptCriterion.FindAllStringSubmatch(call.User, -1)
	introduced := named[len(named)-1][1]
	reply.Text += "\n=== FILE shared.go ===\npackage main\n\n// shared, last written for " + introduced + "\n=== END ==="
	return reply, nil
}

// refusingModel wraps a model and answers the implementer's first refusals
// times with prose outside the block protocol — what a real model did twice in
// a row on 2026-08-18 — then lets the wrapped model answer. The spec author's
// calls pass through untouched, so a test aims the refusals at one stage.
type refusingModel struct {
	inner     agent.Model
	refusals  int
	refused   int
	callsMade int
}

func (m *refusingModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	if call.System == agent.ShippedImplementerPrompt {
		m.callsMade++
		if m.refused < m.refusals {
			m.refused++
			return agent.Reply{Text: "Sure! Here are the files you asked for:\n\n=== FILE main.go ===\npackage main\n=== END ===", Units: map[string]int64{agent.UnitsOutput: 5}}, nil
		}
	}
	return m.inner.Complete(ctx, as, call)
}
