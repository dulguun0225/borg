// The fake model preserves the shipped role interfaces; its generated service is the M11 demonstration.
package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/principal"
)

// The statements the tests give the run, and the spec and criterion the fake spec
// author authors for each. Keyed by the statement so that two candidates authored
// in one run author two different promises, which makes them two candidates rather than one change twice.
const (
	theStatement            = "The demo service needs a health check."
	theSecondStatement      = "The demo service needs a version endpoint."
	theThirdStatement       = "The demo service needs a readiness endpoint."
	theFourthStatement      = "The demo service needs an uptime endpoint."
	theQuestion             = "What does a healthy response say?"
	theAnswer               = "ok"
	criterionSentence       = "When asked for its health, the system shall respond ok."
	secondCriterionSentence = "When asked for its version, the system shall respond two."
	thirdCriterionSentence  = "When asked for its readiness, the system shall respond ready."
	fourthCriterionSentence = "When asked for its uptime, the system shall respond forever."
	// theScreenStatement is the one statement whose spec declares a screen's
	// state machine, for the tests over the transition check and the drivers.
	theScreenStatement      = "The demo service needs a login screen."
	screenCriterionSentence = "When asked for its login status, the system shall respond ready."
)

// theSpecs is what the fake spec author writes for each statement. Both sentences
var theSpecs = map[string]struct{ spec, criterion string }{
	theStatement:       {"The demo service answers a health check with ok.", criterionSentence},
	theSecondStatement: {"The demo service answers a version request with two.", secondCriterionSentence},
	theThirdStatement:  {"The demo service answers a readiness check with ready.", thirdCriterionSentence},
	theFourthStatement: {"The demo service answers an uptime request with forever.", fourthCriterionSentence},
	theScreenStatement: {"The demo service shows a login screen with a start and an active state.", screenCriterionSentence},
}

// screenDeclaration is the spec protocol's two-state, one-event machine,
// appended to [theScreenStatement]'s spec and checked by screenstatemachine.
const screenDeclaration = "SCREEN start: start, active\nTRANSITION start begin: active\nTERMINAL: active"

// rolePromptCriterion picks the criteria out of a role's user prompt: one line per
// criterion, its id then its sentence.
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
	// failEvery controls synthetic failures: zero succeeds, one always fails, and
	// larger values fail matching request numbers; slow changes duration only.
	failEvery int
	// slow makes the named operation slow without changing its outcomes. It is
	// the M11 demonstration's deliberately bad release.
	slow bool
	// apart is words a report carrying them is put in a group of its own for,
	// whatever it was grouped with before, and empty where nothing is. It is
	// how a test makes the role change its mind between two passes, which is
	// what a group it got wrong being split is: the role reads the same reports
	// again and answers differently.
	apart string
	// allTogether puts every report the message lists in one group, which is
	// the other direction the role may change its mind in and what leaves the
	// intents it emptied holding nothing.
	allTogether bool
	// decline is words a report carrying them is left out of the reply
	// entirely — named in no group, not even one of its own — which is how a
	// test makes the role's reply do what its own prompt forbids: leave a
	// report ungrouped. Empty where nothing is declined.
	decline string
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
		// An intent the grouper raised, whose statement is the summary the pass
		// wrote over the reports it was raised from. The interviewer reads the
		// statement, which is all it ever has.
		if strings.Contains(user, groupedStatement) {
			return agent.Reply{
				Text:  "READING:\nREQUIREMENT: When the form is saved, the system shall answer inside a second.",
				Units: map[string]int64{agent.UnitsOutput: 8},
			}, nil
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
				text := "SPEC:\n" + authored.spec + "\nCRITERION" + answers(user) + ": " + authored.criterion
				if statement == theScreenStatement {
					text += "\n" + screenDeclaration
				}
				return agent.Reply{Text: text, Units: map[string]int64{agent.UnitsOutput: 23}}, nil
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
	case agent.ShippedGrouperPrompt:
		return m.groupsOf(user)
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
		text, err := implementerReply(named, m.failEvery, m.slow)
		if err != nil {
			return agent.Reply{}, err
		}
		return agent.Reply{Text: text, Units: map[string]int64{agent.UnitsOutput: 37}}, nil
	}
	return agent.Reply{}, fmt.Errorf("fake model: the system prompt is neither role's")
}

// groupedStatement is what the pass that groups reports opens every statement
// it writes with, which is how this fake tells an intent the grouper raised
// from one somebody typed. The two spellings are one search apart: the other is
// statementOf in ../../grouper/grouping.go.
const groupedStatement = "end-user report(s) grouped as one problem"

// reportLine picks the reports out of the grouper's user message: one line per
// report, its id, its kind, and the words, which is the shape agent.Grouper
// renders them in.
var reportLine = regexp.MustCompile(`(?m)^(rep_[0-9a-f]{32}) (bug|complaint): (.*)$`)

// groupsOf is the grouper's reply for the reports the user message lists: one
// group per first word of a report's words. Deciding whether two free-text
// reports are one problem is what the real model is for, so this fake groups on
// something a test can write, and every test that drives the grouper says which
// reports are one problem by starting them with the same word.
//
// [fakeModel.apart] and [fakeModel.allTogether] are how a test makes the role
// change its mind between two passes: the same reports read again and answered
// differently, which is what a group the role got wrong being split — or two
// groups it should never have kept apart being merged — comes back as.
//
// Every report the message lists is in a group, which is what the role's prompt
// requires: a report that goes with no other is a group of its own — except one
// carrying [fakeModel.decline], which this fake leaves out of every group to
// stand in for a reply that does what the prompt forbids.
func (m *fakeModel) groupsOf(user string) (agent.Reply, error) {
	listed := reportLine.FindAllStringSubmatch(user, -1)
	if len(listed) == 0 {
		return agent.Reply{}, fmt.Errorf("fake model: the grouper's prompt lists no report")
	}
	var order []string
	var alone []string
	together := map[string][]string{}
	for _, one := range listed {
		words := strings.Fields(one[3])
		if len(words) == 0 {
			return agent.Reply{}, fmt.Errorf("fake model: report %s carries no words", one[1])
		}
		if m.decline != "" && strings.Contains(one[3], m.decline) {
			continue
		}
		if m.apart != "" && strings.Contains(one[3], m.apart) {
			alone = append(alone, one[1])
			continue
		}
		key := words[0]
		if m.allTogether {
			key = "every report"
		}
		if _, seen := together[key]; !seen {
			order = append(order, key)
		}
		together[key] = append(together[key], one[1])
	}
	text := "GROUPS:"
	for _, word := range order {
		text += "\n" + strings.Join(together[word], " ")
	}
	for _, id := range alone {
		text += "\n" + id
	}
	return agent.Reply{Text: text, Units: map[string]int64{agent.UnitsOutput: 5}}, nil
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
// answers ReadRunning, and implementation source files plus one encoding per criterion. Every
// criterion in force is encoded because the check over the build rejects one that
// is not, so a candidate's reply carries the encodings of the criteria already in
// force again.
//
// Each set of files is named by the criterion's id and its content is derived
// from that criterion's sentence, never from the code it checks. Naming them by
// the id rather than by position is what lets two candidates of one service merge:
// each adds the files of the criterion it introduced and rewrites the files of the
// criteria already in force with the same bytes, so no two sides of the merge
// change one file differently.
func implementerReply(named [][]string, failEvery int, slow bool) (string, error) {
	files := append([]string{
		"=== FILE go.mod ===", "module demo", "", "go 1.24", "=== END ===",
	}, mainGo(failEvery, slow)...)
	files = append(files, "=== FILE store.checkout.go ===", "package main", "", "type CheckoutStore struct { HazardousCount int64 }", "var checkoutStore = CheckoutStore{HazardousCount: 1}", "func hazardousCount() int64 { return checkoutStore.HazardousCount }", "=== END ===")
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
			"=== FILE instrumentation_"+id+".go ===",
			"package main",
			"",
			"func instrumentation_"+id+"() {}",
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

// mainGo builds the long-lived source used by the command-level fakes. The
// source writes two emission/3 records per exercise and one hazardous-operation
// count per interval because localtarget accepts, stamps, and appends them to
// the per-build signal file.
func mainGo(failEvery int, slow bool) []string {
	emit := `"success"`
	if failEvery == 1 {
		emit = `"failure"`
	} else if failEvery > 1 {
		emit = fmt.Sprintf("map[bool]string{true: \"failure\", false: \"success\"}[n%%%d == 0]", failEvery)
	}
	duration := "0"
	exerciseSleep := ""
	if slow {
		duration = "200*time.Millisecond"
		exerciseSleep = "\t time.Sleep(duration)"
	}
	return []string{
		"=== FILE main.go ===",
		"package main",
		"",
		"import (\n\t\"encoding/json\"\n\t\"os\"\n\t\"strconv\"\n\t\"strings\"\n\t\"time\"\n)",
		"",
		"func main() {",
		"\ttarget := os.Getenv(\"BORG_TARGET\")",
		"\tbuild := os.Getenv(\"BORG_BUILD\")",
		"\tdeploy := os.Getenv(\"BORG_DEPLOY\")",
		"\ttraffic := os.Getenv(\"BORG_TRAFFIC\")",
		"\tlastHazard := int64(-1)",
		"\tfor n := 1; ; n++ {",
		"\t\tif target != \"\" && serves(traffic, build) {",
		"\t\t\tat := time.Now().UTC()",
		"\t\t\tinterval := at.UnixNano() / int64(50*time.Millisecond)",
		"\t\t\tif interval != lastHazard { writeRecord(emission{Version: \"emission/3\", Kind: \"hazardous_operation\", Time: at, Service: \"demo\", Build: build, Deploy: deploy, Target: target, Operation: \"checkout\", HazardousCount: hazardousCount()}); lastHazard = interval }",
		"\t\t\tarrival := emission{Version: \"emission/3\", Kind: \"arrival\", Time: at, Service: \"demo\", Build: build, Deploy: deploy, Target: target, Operation: \"checkout\", Deadline: 500*time.Millisecond}",
		"\t\t\tcompletion := arrival",
		"\t\t\tcompletion.Kind = \"completion\"",
		"\t\t\tcompletion.Deadline = 0",
		"\t\t\tcompletion.Time = at.Add(" + duration + ")",
		"\t\t\tcompletion.Outcome = " + emit + "",
		"\t\t\tcompletion.Duration = " + duration + "",
		"\t\t\tif completion.Outcome == \"failure\" { completion.FailureClass = \"synthetic\"; completion.CodeLocation = \"checkout\" }",
		"\t\t\texercise(arrival, completion, " + duration + ")",
		"\t\t}",
		"\t\ttime.Sleep(time.Millisecond)",
		"\t}",
		"}",
		"",
		"func exercise(arrival, completion emission, duration time.Duration) {",
		"\twriteRecord(arrival)",
		exerciseSleep,
		"\twriteRecord(completion)",
		"}",
		"",
		"type emission struct {",
		"\tVersion string `json:\"version\"`",
		"\tKind string `json:\"kind\"`",
		"\tTime time.Time `json:\"time\"`",
		"\tService string `json:\"service\"`",
		"\tBuild string `json:\"build\"`",
		"\tDeploy string `json:\"deploy\"`",
		"\tTarget string `json:\"target\"`",
		"\tOperation string `json:\"operation\"`",
		"\tOutcome string `json:\"outcome,omitempty\"`",
		"\tDuration time.Duration `json:\"duration,omitempty\"`",
		"\tDeadline time.Duration `json:\"deadline,omitempty\"`",
		"\tFailureClass string `json:\"failure_class,omitempty\"`",
		"\tCodeLocation string `json:\"code_location,omitempty\"`",
		"\tHazardousCount int64 `json:\"hazardous_count,omitempty\"`",
		"}",
		"",
		"func writeRecord(one emission) {",
		"\tdata, err := json.Marshal(one)",
		"\tif err == nil { _, _ = os.Stdout.Write(append(data, 10)) }",
		"}",
		"",
		"func serves(path, build string) bool {",
		"\tif path == \"\" { return true }",
		"\tcontent, err := os.ReadFile(path)",
		"\tif err != nil { return true }",
		"\tfor _, line := range strings.Split(string(content), \"\\n\") {",
		"\t\tfields := strings.Fields(line)",
		"\t\tif len(fields) != 2 || fields[0] != build { continue }",
		"\t\tshare, err := strconv.ParseFloat(fields[1], 64)",
		"\t\treturn err == nil && share > 0",
		"\t}",
		"\treturn false",
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

// criterionOnceFailingModel wraps a model and corrupts the one implementer
// reply that introduces the criterion whose sentence is sentence, so that
// criterion's own encoded test fails both times [path.decideCriteria] runs the
// encodings on the candidate environment — the shape a real defect the
// criteria are meant to catch takes. Every call after that one passes through
// to the wrapped model untouched, so a rebuild against what the row found
// wrong encodes the criterion correctly. It also keeps every implementer
// call's user prompt, in order, so a test can read what a later attempt was
// told was found wrong.
type criterionOnceFailingModel struct {
	inner     agent.Model
	sentence  string
	corrupted bool

	implementerUsers []string
}

func (m *criterionOnceFailingModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, as, call)
	if err != nil || call.System != agent.ShippedImplementerPrompt {
		return reply, err
	}
	m.implementerUsers = append(m.implementerUsers, call.User)
	if m.corrupted {
		return reply, nil
	}
	named := rolePromptCriterion.FindAllStringSubmatch(call.User, -1)
	if len(named) == 0 {
		return reply, nil
	}
	introduced := named[len(named)-1]
	if introduced[2] != m.sentence {
		return reply, nil
	}
	m.corrupted = true
	pattern := regexp.MustCompile(`(func respond_` + introduced[1] + `\(\) string \{ return )"[^"]*"( \})`)
	reply.Text = pattern.ReplaceAllString(reply.Text, `${1}"wrong"${2}`)
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
