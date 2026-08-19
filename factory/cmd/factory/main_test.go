// The end-to-end test: roadmap M1's demonstration, M2's, and M3's, driven
// through the same run function the run subcommand calls, with a fake model and
// scripted input. M2's is runs on one service decided differently — a human at
// every row, then nobody anywhere, then a pin putting one back. M3's is two
// candidates in one run, each on an environment of its own, merged in the queue's
// order. Each test gets a PostgreSQL schema of its own with the whole factory
// schema applied through postgres.Apply. None of these tests skips when the
// database is unreachable: the milestone is demonstrated by them running, so an
// unreachable database fails the run.
package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// theModel is the model id the run is configured with, which is the author every
// version it writes names and the identity the authorship prior is kept on.
const theModel = "fake-model-1"

// theArea is the area the run names. Without one the score can read neither
// context factor and a human decides every gate, so the milestone's own
// demonstration needs one.
const theArea = "payments"

// theCeiling is how many candidate environments the tests give the substrate room
// for. It is high enough that no test meets it by accident, and the one test about
// the ceiling lowers it to one.
const theCeiling = 8

// attemptBound is the bound in force in these tests: nothing here authors one,
// so it is what the score supplies. The tests that spend it read it from there
// rather than holding a number of their own, so authoring a different supplied
// value moves the tests with it.
var attemptBound = func() int {
	supplied, ok := score.Supplied(gatepolicy.AttemptBound)
	if !ok {
		panic("the score supplies no attempt bound")
	}
	return int(supplied)
}()

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

// briefCriterion picks the criteria out of a role's user prompt: one line per
// criterion, its id then its sentence, which is the shape both prompts render
// and the id shape the criterion package's encoding check matches.
var briefCriterion = regexp.MustCompile(`(?m)^(cr_[0-9a-f]{32}): (.*)$`)

// theResponse reads the response out of a criterion sentence in the event
// pattern, which is where the fake implementer takes an encoding's expected
// value from — the sentence, never the code it checks.
var theResponse = regexp.MustCompile(`shall respond (.+)\.$`)

// fakeModel answers by role, told apart by the system prompt — the same
// constant each real role sends, so a prompt this switch does not know is a
// wiring defect and an error. The first spec-author call asks the one question,
// and every call after it delivers the spec [theSpecs] holds for the statement in
// the prompt; the implementer call returns whole files with one encoding per
// criterion the prompt names.
type fakeModel struct {
	specCalls int
	// failEvery is how often the program this fake writes emits a failure rather
	// than an ok: nothing at zero, every unit at one, every other unit at two. It is
	// what a deliberately bad deploy is — an implementation that passes every
	// criterion in force and fails a share of the work it does, which is the shape of
	// defect the criteria cannot see and the watch window exists for.
	failEvery int
}

func (m *fakeModel) Complete(_ context.Context, system, user string) (agent.Reply, error) {
	switch system {
	case agent.SpecAuthorSystemPrompt:
		m.specCalls++
		if m.specCalls == 1 {
			return agent.Reply{Text: "QUESTION: " + theQuestion, Tokens: 11}, nil
		}
		for statement, authored := range theSpecs {
			if strings.Contains(user, statement) {
				return agent.Reply{
					Text:   "SPEC:\n" + authored.spec + "\nCRITERION: " + authored.criterion,
					Tokens: 23,
				}, nil
			}
		}
		// A revert, whose intent the comparison wrote at a rollback. Nothing on the item
		// says it is one — this reads the statement, which is all a spec author ever has.
		//
		// It introduces a criterion because the spec author's protocol requires exactly
		// one per spec version, which is a simplification M1 made and not something the
		// design asks for: a revert restores a behaviour the service already promises, so
		// the honest version introduces none. What the simplification costs is one
		// criterion per revert that nobody asked for.
		if strings.Contains(user, "Revert release") {
			return agent.Reply{
				Text: "SPEC:\nRestore the behaviour the condemned release changed, leaving every criterion in force as it is.\n" +
					"CRITERION: When asked what it was restored from, the system shall respond harm.",
				Tokens: 19,
			}, nil
		}
		return agent.Reply{}, fmt.Errorf("fake model: the spec author's prompt names no statement this fake authors for")
	case agent.ImplementerSystemPrompt:
		named := briefCriterion.FindAllStringSubmatch(user, -1)
		if len(named) == 0 {
			return agent.Reply{}, fmt.Errorf("fake model: the implementer's prompt names no criterion")
		}
		text, err := implementerReply(named, m.failEvery)
		if err != nil {
			return agent.Reply{}, err
		}
		return agent.Reply{Text: text, Tokens: 37}, nil
	}
	return agent.Reply{}, fmt.Errorf("fake model: the system prompt is neither role's")
}

// implementerReply is the implementer's whole reply for the criteria the brief
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
			fmt.Sprintf("func Test_%s(t *testing.T) {", id),
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
func interviewed(failEvery int) *fakeModel { return &fakeModel{specCalls: 1, failEvery: failEvery} }

// mainGo is the program every one of these fakes writes, and it is the one place
// this test does what the implementer's standing instruction asks: the program runs
// as a long-lived process, exercises its own behaviour over and over, and appends one
// line per exercise to the file the environment names. Without that the comparison
// reads nothing, every window ends at its cap, and the whole of this milestone is
// untestable — which is the instruction earning its place rather than decorating the
// prompt.
//
// failEvery is what makes a deploy deliberately bad: nothing at zero, every other
// unit at two. The failure is in no criterion's path, so a build with it passes every
// criterion in force and is condemned by its window instead — which is the shape of
// defect the criteria cannot see.
//
// The file's content depends on failEvery and on nothing else, so two good candidates
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
		"\t\t\t\t_, _ = f.WriteString(" + emit + " + \"\\n\")",
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
// one the brief names. Two candidates of one service then change one file
// differently, so the second one's re-verification against the master the first
// created is a merge that conflicts — which is a candidate failing on its own
// merits and the merge queue rejecting it.
type conflictingModel struct{ inner agent.Model }

func (m *conflictingModel) Complete(ctx context.Context, system, user string) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, system, user)
	if err != nil || system != agent.ImplementerSystemPrompt {
		return reply, err
	}
	named := briefCriterion.FindAllStringSubmatch(user, -1)
	introduced := named[len(named)-1][1]
	reply.Text += "\n=== FILE shared.go ===\npackage main\n\n// shared, last written for " + introduced + "\n=== END ==="
	return reply, nil
}

// newPath gives a test a schema of its own with the whole schema applied,
// temp directories for the repository and production's target, a secrets file
// holding the deploy credential, a target per environment whose started processes
// are stopped in cleanup — through the seam — and the deps the path runs over.
// input is what the scripted human types.
func newPath(t *testing.T, input string) (context.Context, deps, *bytes.Buffer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "factory_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	secrets := filepath.Join(t.TempDir(), "secrets")
	if err := os.WriteFile(secrets, []byte("deploy.local=unused\n"), 0o600); err != nil {
		t.Fatalf("writing the secrets file: %v", err)
	}
	resolver, err := secretref.Load(secrets)
	if err != nil {
		t.Fatalf("loading the secrets file: %v", err)
	}
	credential := secretref.MustNew("deploy.local")
	if _, err := resolver.Resolve(credential); err != nil {
		t.Fatalf("resolving the deploy credential: %v", err)
	}

	// One target per environment, the way the run composes them: production's is
	// the directory named here and each candidate environment's is one of its own.
	// Every target the run made is stopped in cleanup, a candidate environment torn
	// down mid-run having stopped its own already.
	targets := newTargetSet(func(dir string) targetseam.Target { return localtarget.New(dir) })
	t.Cleanup(func() {
		for dir, target := range targets.made {
			if err := target.Stop(context.Background(), "demo", credential); err != nil {
				t.Errorf("stopping the demo service on %s: %v", dir, err)
			}
		}
	})

	out := &bytes.Buffer{}
	d := deps{
		pool:             pool,
		model:            &fakeModel{},
		modelName:        theModel,
		targets:          targets,
		dir:              t.TempDir(),
		credential:       credential,
		in:               strings.NewReader(input),
		out:              out,
		human:            "owner",
		service:          "demo",
		area:             theArea,
		repo:             filepath.Join(t.TempDir(), "demo"),
		candidateCeiling: theCeiling,
		watchFor:         theWatchFor,
		watchEvery:       theWatchEvery,
	}
	installWindow(t, ctx, d, 1)
	return ctx, d, out
}

// The watch window as these tests author it, and how long a run watches for.
//
// The supplied values are deliberately unreachable here and that is the design
// working rather than the tests fighting it: a size of two in a hundred needs traffic
// no test generates, and a cap of a day is exactly how long the second release of a
// service would wait behind a first that can never close clean. So the tests author a
// coarse size and a short cap, which is what an owner running a quiet service would
// do, and the run watches for longer than the cap so every window it opens closes
// before it returns.
const (
	theWindowSize       = 0.1
	theWindowConfidence = 0.95
	theWindowCap        = 1.0
	theWatchFor         = 4 * time.Second
	theWatchEvery       = 50 * time.Millisecond
)

// installWindow creates the service record and authors the watch window's four on
// it, before any run has opened a window.
//
// The service has to exist first, because those four are fields of its record — and
// it has to be authored before the first window opens, because a window copies the
// size, the confidence, and the cap onto itself at the open and an owner authoring
// afterwards does not move a window already open. Creating the service here is what
// [TestTheCutReachesAnExistingService] proves the cut is happy with: a service the
// work changes may exist already, and the cut writes a service's identity once.
func installWindow(t *testing.T, ctx context.Context, d deps, k float64) {
	t.Helper()
	owner := record.Actor{Kind: record.KindHuman, Name: d.human}
	if _, err := policy.NewFactory(d.pool).Install(ctx, owner, []string{d.dir}, d.credential); err != nil {
		t.Fatalf("installing the factory: %v", err)
	}
	svc, found, err := service.ByName(ctx, d.pool, d.service)
	if err != nil {
		t.Fatalf("reading the service: %v", err)
	}
	if !found {
		svc, err = service.NewWriter(d.pool).Create(ctx, cutActor, d.service, d.repo)
		if err != nil {
			t.Fatalf("writing the service: %v", err)
		}
	}
	factory := policy.NewFactory(d.pool)
	for _, authoring := range []struct {
		what  string
		write func() (policy.Version, error)
	}{
		{"the size", func() (policy.Version, error) {
			return factory.AuthorWindowSize(ctx, owner, svc.ID, theWindowSize)
		}},
		{"the confidence", func() (policy.Version, error) {
			return factory.AuthorWindowConfidence(ctx, owner, svc.ID, theWindowConfidence)
		}},
		{"the cap", func() (policy.Version, error) {
			return factory.AuthorWindowCap(ctx, owner, svc.ID, theWindowCap)
		}},
		{"K", func() (policy.Version, error) { return factory.AuthorK(ctx, owner, svc.ID, k) }},
	} {
		if _, err := authoring.write(); err != nil {
			t.Fatalf("authoring %s of the watch window: %v", authoring.what, err)
		}
	}
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// only is the one candidate of a single-intent run, which is what most of these
// tests assert over.
func only(t *testing.T, s shipped) *candidate {
	t.Helper()
	if len(s.candidates) != 1 {
		t.Fatalf("the run has %d candidates, want one", len(s.candidates))
	}
	return s.candidates[0]
}

// approvals is a scripted human approving every row that puts one there. A row
// that auto-passes consumes nothing, so a script with more approvals than rows is
// harmless and a script with fewer is what fails.
const approvals = "approve\napprove\napprove\n"

// TestOneChangeShips is the demonstration: one change followed end to end,
// approved at every row that put a human there, released as number 1, deployed
// straight, running on the target, and walkable from the deploy back to the intent
// with the decisions readable in a clean chain.
func TestOneChangeShips(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if c.rejected {
		t.Fatal("the run reports rejected, and every scripted verdict was approve")
	}

	// The intent: one round, its question answered, refined.
	in, err := intent.Get(ctx, d.pool, c.intentID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if in.Rounds != 1 {
		t.Errorf("intent rounds = %d, one round was asked", in.Rounds)
	}
	if in.State != intent.StateRefined {
		t.Errorf("intent state = %s, the interview marked it refined", in.State)
	}
	questions, err := intent.Questions(ctx, d.pool, c.intentID)
	if err != nil {
		t.Fatalf("reading the questions: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("the intent has %d questions, one was asked", len(questions))
	}
	if !questions[0].Answered() || questions[0].Answer != theAnswer {
		t.Errorf("the question's answer = %q answered=%t, the human answered %q",
			questions[0].Answer, questions[0].Answered(), theAnswer)
	}

	// The item: merged, one attempt with spend on each authored stage.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageMerged {
		t.Errorf("item stage = %s, the path ends at merged", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("the item has %d stage rows, spec and implementation reported one each: %+v", len(stages), stages)
	}
	reported := map[item.Stage]bool{}
	for _, st := range stages {
		reported[st.Stage] = true
		if st.Attempts != 1 {
			t.Errorf("stage %s attempts = %d, each stage ran once", st.Stage, st.Attempts)
		}
		if st.SpendTokens <= 0 {
			t.Errorf("stage %s spend = %d, each stage spent tokens", st.Stage, st.SpendTokens)
		}
	}
	if !reported[item.StageSpec] || !reported[item.StageImplementation] {
		t.Errorf("the reported stages are %v, spec and implementation were expected", stages)
	}

	// The release is number 1, and it names the build the re-verification made
	// rather than the one the implementation stage did — which for a candidate with
	// no master to merge is the same build.
	rel, err := release.Get(ctx, d.pool, c.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("release number = %d, a service's first release is 1", rel.Number)
	}
	if rel.BuildID != c.reverifiedBuildID {
		t.Errorf("the release names build %s, the re-verification produced %s", rel.BuildID, c.reverifiedBuildID)
	}

	// The deploy completed and Current names it — what is running, not what
	// is newest.
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ID != c.deployID {
		t.Errorf("the current deploy is %q found=%t, the path deployed %s", current.ID, found, c.deployID)
	}
	if current.Status != deploy.StatusComplete {
		t.Errorf("deploy status = %s, the straight deploy completes", current.Status)
	}
	if current.BuildID != rel.BuildID {
		t.Errorf("the deploy names build %s and the release names %s", current.BuildID, rel.BuildID)
	}

	// The target runs the build. What crosses the seam is the build and never the
	// release: a target runs a binary rather than a name.
	running, err := d.targets.at(d.dir).ReadRunning(ctx, "demo", d.credential)
	if err != nil {
		t.Fatalf("reading what the target runs: %v", err)
	}
	if running.Build != rel.BuildID {
		t.Errorf("the target runs %q, the deploy put build %s there", running.Build, rel.BuildID)
	}

	// Master exists in the repository at the commit the queue fast-forwarded to.
	master, err := git(d.repo, "rev-parse", "master")
	if err != nil {
		t.Fatalf("reading master: %v", err)
	}
	if master != c.reverifiedCommit {
		t.Errorf("master is at %s, the fast-forward targeted %s", master, c.reverifiedCommit)
	}

	// The walk alone — the walk subcommand's code — reaches the intent's
	// statement from the deploy id, and reports the chain clean.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, c.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", c.deployID, theStatement, walked.String())
	}
	if !strings.Contains(walked.String(), "the chain is clean") {
		t.Errorf("the walk does not report the chain clean:\n%s", walked.String())
	}

	// The log: three decisions, six rows — the candidate deploy row, the merge
	// row, and the production deploy row, each opened by its gate component.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("the log holds %d rows, three decisions are six:\n%s", len(rows), out)
	}
	for n, want := range []struct {
		part  decisionlog.Part
		actor string
	}{
		{decisionlog.PartOpening, "gate.deploy_to_candidate_environment"},
		{decisionlog.PartClosing, ""},
		{decisionlog.PartOpening, "gate.merge_to_master"},
		{decisionlog.PartClosing, ""},
		{decisionlog.PartOpening, "gate.deploy_to_production"},
		{decisionlog.PartClosing, ""},
	} {
		row := rows[n]
		if row.Shape != decisionlog.ShapeDecision || row.Part != want.part {
			t.Errorf("row %d is shape %s part %s, want a %s decision row", n+1, row.Shape, row.Part, want.part)
		}
		if want.actor != "" && row.Actor.Name != want.actor {
			t.Errorf("row %d's actor is %q, want %q", n+1, row.Actor.Name, want.actor)
		}
	}
	if rows[1].Closes != rows[0].ID || rows[3].Closes != rows[2].ID || rows[5].Closes != rows[4].ID {
		t.Error("a closing row does not close the opening row before it")
	}

	// Every decision names the policy version and the score version it was
	// decided under, and both are records rather than names.
	scoreVersion, found, err := score.Newest(ctx, d.pool)
	if err != nil || !found {
		t.Fatalf("reading the score version: %v", err)
	}
	policyVersion, err := policy.InForce(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the policy version: %v", err)
	}
	for _, opening := range []decisionlog.Row{rows[0], rows[2], rows[4]} {
		if opening.ScoreVersion != scoreVersion.ID {
			t.Errorf("an opening names score version %q, want the record %q", opening.ScoreVersion, scoreVersion.ID)
		}
		if opening.PolicyVersion != policyVersion.ID {
			t.Errorf("an opening names policy version %q, want the record %q", opening.PolicyVersion, policyVersion.ID)
		}
	}
	if _, err := score.Get(ctx, d.pool, rows[0].ScoreVersion); err != nil {
		t.Errorf("the score version a decision names does not read back: %v", err)
	}
	if _, err := policy.Get(ctx, d.pool, rows[0].PolicyVersion); err != nil {
		t.Errorf("the policy version a decision names does not read back: %v", err)
	}

	// The merge row's opening names the implementation version under decision and
	// the whole vector; neither deploy row's names an artifact, there being none
	// under decision at a deploy.
	mergeOpening := openingPayload(t, rows[2])
	if mergeOpening.ArtifactID != c.implArtifactID {
		t.Errorf("the merge opening names artifact %s, the decision was over implementation %s",
			mergeOpening.ArtifactID, c.implArtifactID)
	}
	if len(mergeOpening.Vector) == 0 || mergeOpening.Number <= 0 {
		t.Errorf("the merge opening carries %d factors and number %v", len(mergeOpening.Vector), mergeOpening.Number)
	}
	if len(mergeOpening.Unavailable) != 0 {
		t.Errorf("the merge opening names %v as unavailable, and every factor is computable here", mergeOpening.Unavailable)
	}
	if len(mergeOpening.Criteria) != 1 || mergeOpening.Criteria[0].Outcome != criterion.OutcomePassed {
		t.Errorf("the merge opening carries criteria %+v, want the one criterion passed", mergeOpening.Criteria)
	}
	for _, deployRow := range []decisionlog.Row{rows[0], rows[4]} {
		if payload := openingPayload(t, deployRow); payload.ArtifactID != "" {
			t.Errorf("a deploy opening names artifact %q, and nothing is under decision at a deploy", payload.ArtifactID)
		}
	}
	// The candidate deploy row's opening carries no outcome: the run that decides
	// the criteria is what that deploy is for.
	if payload := openingPayload(t, rows[0]); len(payload.Criteria) != 0 {
		t.Errorf("the candidate deploy opening carries criteria %+v, and none is decided yet", payload.Criteria)
	}

	// The first item on a fresh factory is decided by a human at every row, which
	// is the calibration the milestone states: no earlier release to return to, an
	// author nobody has approved, and an area with no history.
	for name, firing := range map[string]fired{
		"candidate deploy": c.candidateGate,
		"merge":            c.mergeGate,
		"production":       c.deployGate,
	} {
		if !firing.humanDecided {
			t.Errorf("the %s row auto-passed the first item of a fresh factory (number %v against threshold %v)",
				name, firing.number, firing.threshold)
		}
	}

	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// openingPayload and closingPayload unmarshal what a decision row says, which
// every assertion over a firing reads through.
func openingPayload(t *testing.T, row decisionlog.Row) gate.OpeningPayload {
	t.Helper()
	var payload gate.OpeningPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("reading the opening payload of %s: %v", row.ID, err)
	}
	return payload
}

func closingPayload(t *testing.T, row decisionlog.Row) gate.ClosingPayload {
	t.Helper()
	var payload gate.ClosingPayload
	if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
		t.Fatalf("reading the closing payload of %s: %v", row.ID, err)
	}
	return payload
}

// TestACandidateGetsAnEnvironmentOfItsOwn is M3's first claim: the gate that
// decides the candidate's deploy creates an environment named for the item, the
// build goes on it and the deploy record names that build and no release, the
// criteria are decided there, and the environment is torn down at the merge with
// its record kept.
func TestACandidateGetsAnEnvironmentOfItsOwn(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if env.ID != c.environmentID {
		t.Errorf("the item's environment is %s, the run composed %s", env.ID, c.environmentID)
	}
	if env.Kind != environment.KindCandidate {
		t.Errorf("the environment's kind is %s, want a candidate's", env.Kind)
	}
	if env.Name != environment.NameForItem(c.itemID) {
		t.Errorf("the environment is named %q, want %q", env.Name, environment.NameForItem(c.itemID))
	}
	if env.ItemID != c.itemID {
		t.Errorf("the environment names item %s, want %s", env.ItemID, c.itemID)
	}
	if !slices.Equal(env.Targets, []string{c.environmentDir}) {
		t.Errorf("the environment's targets are %v, want the directory of its own %q", env.Targets, c.environmentDir)
	}
	if len(env.ComposedFrom) != 0 {
		t.Errorf("the environment was composed from %+v, and the cut declared no dependency", env.ComposedFrom)
	}
	if env.Live() {
		t.Error("the environment is still live, and the item merged")
	}
	if !c.tornDown {
		t.Error("the run does not report the environment torn down")
	}
	// The record is kept rather than deleted, because the deploy records naming it
	// would otherwise point at nothing.
	if _, err := environment.Get(ctx, d.pool, env.ID); err != nil {
		t.Errorf("the torn-down environment's record does not read back: %v", err)
	}

	// The candidate deploy record names the build and no release: the number is
	// minted one gate below it.
	candidateDeploy, err := deploy.Get(ctx, d.pool, c.candidateDeployID)
	if err != nil {
		t.Fatalf("reading the candidate deploy: %v", err)
	}
	if candidateDeploy.EnvironmentID != env.ID {
		t.Errorf("the candidate deploy names environment %s, want %s", candidateDeploy.EnvironmentID, env.ID)
	}
	if candidateDeploy.ReleaseID != "" {
		t.Errorf("the candidate deploy names release %q, and no number exists at that row", candidateDeploy.ReleaseID)
	}
	if candidateDeploy.BuildID == "" {
		t.Error("the candidate deploy names no build, and the build is what it put there")
	}
	// Nothing is current on a candidate environment: Current reads the records
	// that name a release.
	if _, running, err := deploy.Current(ctx, d.pool, res.serviceID, env.ID); err != nil || running {
		t.Errorf("Current on a candidate environment = running %v, %v", running, err)
	}

	// The criteria were decided against the build, on that environment, by the
	// deploy agent.
	results, err := criterion.ResultsForBuild(ctx, d.pool, candidateDeploy.BuildID)
	if err != nil {
		t.Fatalf("reading what was decided over the build: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("%d criteria were decided over the build, one was in force: %+v", len(results), results)
	}
	if results[0].Outcome != criterion.OutcomePassed {
		t.Errorf("the criterion is %s over the build, want passed", results[0].Outcome)
	}
	if results[0].Actor.Name != "deploy" {
		t.Errorf("the result was written by %q, want the deploy agent", results[0].Actor.Name)
	}
	if !strings.Contains(out.String(), "ran twice on the candidate environment") {
		t.Errorf("the run does not report the encodings running twice:\n%s", out)
	}
}

// TestTwoCandidatesProceedAtOnce is M3's demonstration: two intents in one run,
// two items cut on the same master, two candidate environments live at once with
// different targets and different deploy records, and the queue merging them in
// its order — the second re-verifying against the master the first created, which
// is a build the implementation stage never made.
func TestTwoCandidatesProceedAtOnce(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	// K at two, so both releases may hold a window open and both may deploy. At the
	// K of one the score supplies, the second candidate's own environment and its merge
	// are unaffected and its production deploy waits behind the first release's window —
	// which is [TestKHoldsTheNextProductionDeploy], and is the serial factory the
	// design says a K of one is.
	installWindow(t, ctx, d, 2)

	// One change first, so master exists and the two candidates below are both
	// based on it. Two candidates cut before any release have no common commit, and
	// what that costs is stated where the queue merges them.
	first, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("")
	res, err := run(ctx, d, []string{theSecondStatement, theThirdStatement})
	if err != nil {
		t.Fatalf("the two-candidate run stopped: %v\noutput so far:\n%s", err, out)
	}
	if len(res.candidates) != 2 {
		t.Fatalf("the run has %d candidates, two intents are two", len(res.candidates))
	}
	a, b := res.candidates[0], res.candidates[1]

	// Two environments, neither reading the other's: different records, different
	// items, different directories, different deploy records.
	if a.environmentID == b.environmentID || a.environmentDir == b.environmentDir {
		t.Fatalf("the two candidates share environment %s at %s", a.environmentID, a.environmentDir)
	}
	if a.candidateDeployID == b.candidateDeployID {
		t.Error("the two candidate deploys are one record")
	}
	for _, c := range res.candidates {
		env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
		if err != nil || !found {
			t.Fatalf("ForItem(%s) = found %v, %v", c.itemID, found, err)
		}
		if env.ItemID != c.itemID {
			t.Errorf("environment %s names item %s, want %s", env.ID, env.ItemID, c.itemID)
		}
		if !c.merged {
			t.Errorf("item %s did not merge:\n%s", c.itemID, out)
		}
		if !c.tornDown {
			t.Errorf("item %s merged and its environment was not torn down", c.itemID)
		}
	}

	// Each candidate's criteria attach to its own build, so what one run produced
	// is not read as the other's.
	for _, c := range res.candidates {
		results, err := criterion.ResultsForBuild(ctx, d.pool, c.reverifiedBuildID)
		if err != nil {
			t.Fatalf("reading what was decided over build %s: %v", c.reverifiedBuildID, err)
		}
		if len(results) == 0 {
			t.Errorf("nothing was decided over build %s", c.reverifiedBuildID)
		}
		for _, result := range results {
			if result.BuildID != c.reverifiedBuildID {
				t.Errorf("a result of build %s names %s", c.reverifiedBuildID, result.BuildID)
			}
		}
	}

	// The queue merged them in its order: numbers 2 and 3 after the first run's 1.
	numbers := map[int64]string{}
	for _, c := range res.candidates {
		rel, err := release.Get(ctx, d.pool, c.releaseID)
		if err != nil {
			t.Fatalf("reading release %s: %v", c.releaseID, err)
		}
		numbers[rel.Number] = c.itemID
	}
	if numbers[2] != a.itemID || numbers[3] != b.itemID {
		t.Errorf("the numbers went %+v, want 2 to the first-approved item %s and 3 to %s",
			numbers, a.itemID, b.itemID)
	}

	// The second candidate's release names a build the implementation stage never
	// made: it is the re-verification's, made from the candidate branch with the
	// master the first one created merged into it.
	if b.reverifiedBuildID == b.buildID {
		t.Error("the second candidate's re-verification reused its own build, and master had moved under it")
	}
	if _, err := git(d.repo, "merge-base", "--is-ancestor", first.candidates[0].reverifiedCommit,
		b.reverifiedCommit); err != nil {
		t.Errorf("the first release's commit is not an ancestor of the second candidate's re-verified commit: %v", err)
	}

	// Master is at the last commit the queue fast-forwarded to.
	master, err := git(d.repo, "rev-parse", "master")
	if err != nil {
		t.Fatalf("reading master: %v", err)
	}
	if master != b.reverifiedCommit {
		t.Errorf("master is at %s, the last fast-forward targeted %s", master, b.reverifiedCommit)
	}

	// Both deployed, and production runs the last one.
	for _, c := range res.candidates {
		if c.deployID == "" {
			t.Errorf("item %s minted release %s and deployed nothing", c.itemID, c.releaseID)
		}
	}
	running, err := d.targets.at(d.dir).ReadRunning(ctx, "demo", d.credential)
	if err != nil {
		t.Fatalf("reading what production runs: %v", err)
	}
	if running.Build != b.reverifiedBuildID {
		t.Errorf("production runs %q, the last deploy put build %s there", running.Build, b.reverifiedBuildID)
	}

	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after two candidates at once: %v", err)
	}
}

// TestTheQueueRejectsACandidateThatFailedItsOwnReverification is the queue's
// rejection: two candidates change one file differently, so the second one's
// re-verification against the master the first created is a merge that conflicts.
// The item goes back to Implementation with an attempt counted there, no release
// is minted for it, its environment stays its own, and the log holds a wait row
// naming the queue as its actor.
func TestTheQueueRejectsACandidateThatFailedItsOwnReverification(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &conflictingModel{inner: &fakeModel{}}

	if _, err := run(ctx, d, []string{theStatement}); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("")
	res, err := run(ctx, d, []string{theSecondStatement, theThirdStatement})
	if err != nil {
		t.Fatalf("the run stopped, and a queue rejection is not an error: %v\noutput so far:\n%s", err, out)
	}
	a, b := res.candidates[0], res.candidates[1]

	if !a.merged {
		t.Fatalf("the first candidate in the queue did not merge:\n%s", out)
	}
	if b.merged || !b.queueRejected {
		t.Fatalf("the second candidate merged=%v rejected=%v, want the queue rejecting it:\n%s",
			b.merged, b.queueRejected, out)
	}
	if b.releaseID != "" {
		t.Errorf("the rejected candidate minted release %s", b.releaseID)
	}
	if !strings.Contains(b.queueWhy, "merging master") {
		t.Errorf("the queue rejected it because %q, want the merge that conflicted", b.queueWhy)
	}

	// The item is back at Implementation with an attempt counted there — the
	// rework booked against the thing that was wrong.
	it, err := item.Get(ctx, d.pool, b.itemID)
	if err != nil {
		t.Fatalf("reading the rejected item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, b.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	var attempts int
	for _, st := range stages {
		if st.Stage == item.StageImplementation {
			attempts = st.Attempts
		}
	}
	if attempts != 2 {
		t.Errorf("the implementation stage records %d attempts, want the authoring one and the queue's rejection", attempts)
	}

	// Its environment is still its own: nothing waits on the environment a
	// rejected candidate used, and it stays the item's until it merges or is
	// dropped.
	env, found, err := environment.ForItem(ctx, d.pool, b.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if !env.Live() {
		t.Error("the rejected candidate's environment was torn down, and it stays the item's")
	}

	// The rejection is a wait row the log wrote with the queue as caller and
	// actor: no gate fired, the merge gate's own having closed as an approval.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	var waits int
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeWait {
			continue
		}
		waits++
		if row.ID != b.queueWaitRow {
			t.Errorf("the log's wait row is %s, the run reported %s", row.ID, b.queueWaitRow)
		}
		if row.Actor != mergequeue.Actor {
			t.Errorf("the wait row's actor is %+v, want the queue %+v", row.Actor, mergequeue.Actor)
		}
		var payload mergequeue.RejectionPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the rejection payload: %v", err)
		}
		if payload.Kind != mergequeue.RejectionKind || payload.ItemID != b.itemID {
			t.Errorf("the rejection payload is %+v, want kind %q for item %s",
				payload, mergequeue.RejectionKind, b.itemID)
		}
		if payload.ReturnsTo != gate.ReturnsTo || !payload.CountsAnAttempt {
			t.Errorf("the rejection returns the item to %q and counts an attempt %v",
				payload.ReturnsTo, payload.CountsAnAttempt)
		}
	}
	if waits != 1 {
		t.Errorf("the log holds %d wait rows, one candidate was rejected", waits)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after a queue rejection: %v", err)
	}
}

// TestThePriorityReordersTheQueue is the settable order: an owner writes a
// priority through dispatch and the queue takes that candidate first. What it
// changes is when a candidate re-verifies and never what it has to pass — so the
// one that goes second is the one whose merge conflicts, which is the opposite of
// what happens with the priorities left where the cut wrote them.
func TestThePriorityReordersTheQueue(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &conflictingModel{inner: &fakeModel{}}

	if _, err := run(ctx, d, []string{theStatement}); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	// The priority is written between the merge gate and the queue, which is what
	// a surface would do: the run does both in one call, so this test drives the
	// steps rather than run itself.
	d.in = strings.NewReader("")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	var candidates []*candidate
	for _, statement := range []string{theSecondStatement, theThirdStatement} {
		c, err := p.author(ctx, statement, statement)
		if err != nil {
			t.Fatalf("authoring %q: %v\noutput so far:\n%s", statement, err, out)
		}
		candidates = append(candidates, c)
		p.byItem[c.itemID] = c
	}
	for _, c := range candidates {
		if err := p.candidateEnvironment(ctx, c); err != nil {
			t.Fatalf("the candidate environment of %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
		if err := p.mergeGate(ctx, c); err != nil {
			t.Fatalf("the merge gate of %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
	}

	// The second-approved candidate is pushed to the front.
	owner := record.Actor{Kind: record.KindHuman, Name: d.human}
	if _, err := item.NewDispatch(d.pool).SetPriority(ctx, owner, candidates[1].itemID, 5); err != nil {
		t.Fatalf("setting the priority: %v", err)
	}
	members, err := p.queue.Members(ctx, p.svc.ID)
	if err != nil {
		t.Fatalf("reading the queue's members: %v", err)
	}
	if len(members) != 2 || members[0].ID != candidates[1].itemID {
		t.Fatalf("the queue's order is %+v, want the pushed candidate %s first", members, candidates[1].itemID)
	}

	if _, err := p.runQueue(ctx); err != nil {
		t.Fatalf("the queue stopped: %v\noutput so far:\n%s", err, out)
	}
	if !candidates[1].merged {
		t.Errorf("the candidate an owner pushed to the front did not merge:\n%s", out)
	}
	if candidates[0].merged || !candidates[0].queueRejected {
		t.Errorf("the candidate behind it merged=%v rejected=%v, and its merge is the one that conflicts",
			candidates[0].merged, candidates[0].queueRejected)
	}
}

// TestTheSubstrateWithNoRoomWaits is the other hold at the candidate deploy row,
// and the one that writes: the substrate has no room for another environment, that
// condition is not a record, and no parameter of an owner's limits it — so it goes
// into the log as a wait with the deploy agent as caller and actor.
func TestTheSubstrateWithNoRoomWaits(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.candidateCeiling = 1

	res, err := run(ctx, d, []string{theStatement, theSecondStatement})
	if err != nil {
		t.Fatalf("the run stopped, and a wait is not an error: %v\noutput so far:\n%s", err, out)
	}
	a, b := res.candidates[0], res.candidates[1]

	if a.environmentID == "" {
		t.Fatalf("the first candidate got no environment with room for one:\n%s", out)
	}
	if b.environmentID != "" {
		t.Errorf("the second candidate got environment %s with the ceiling at one", b.environmentID)
	}
	if b.factoryHold != gate.HoldNoRoomForAnotherEnvironment {
		t.Errorf("the second candidate's hold is %q, want the substrate's", b.factoryHold)
	}
	if b.candidateGate.opening != "" {
		t.Error("the candidate deploy row fired for the held candidate, and a factory hold is not a verdict")
	}
	if b.releaseID != "" || b.deployID != "" {
		t.Errorf("the held candidate minted release %q and deployed %q", b.releaseID, b.deployID)
	}

	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	var waits int
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeWait {
			continue
		}
		waits++
		if row.ID != b.holdWaitRow {
			t.Errorf("the wait row is %s, the run reported %s", row.ID, b.holdWaitRow)
		}
		if row.Actor.Name != "deploy" {
			t.Errorf("the wait row's actor is %q, want the deploy agent that met the condition", row.Actor.Name)
		}
		var payload substrateWait
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the wait payload: %v", err)
		}
		if payload.Kind != SubstrateWaitKind || payload.ItemID != b.itemID {
			t.Errorf("the wait payload is %+v, want kind %q for item %s", payload, SubstrateWaitKind, b.itemID)
		}
		if payload.Ceiling != 1 || payload.Live != 1 {
			t.Errorf("the wait payload says %d live against a ceiling of %d, want 1 and 1", payload.Live, payload.Ceiling)
		}
	}
	if waits != 1 {
		t.Errorf("the log holds %d wait rows, one candidate met the ceiling", waits)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after a wait: %v", err)
	}
}

// TestADeclaredDependencyThatIsNotLiveHolds is the factory's own hold at both
// deploy rows, and the one that writes nothing: a declared dependency that is not
// its service's current release. No run of this interface declares one — the cut
// yields one item per intent — so the condition is driven directly against an item
// that waits on one that has not shipped.
func TestADeclaredDependencyThatIsNotLiveHolds(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	shippedItem := only(t, res).itemID

	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}

	// An item waiting on the one that shipped: its dependency is live, so nothing
	// holds.
	live, err := item.NewCut(d.pool).Create(ctx, cutActor, item.New{
		IntentID: "in_dependent", ServiceID: res.serviceID, Branch: "item/dependent-live",
		WaitsOn: []string{shippedItem},
	})
	if err != nil {
		t.Fatalf("cutting the dependent item: %v", err)
	}
	held, err := p.dependencyHold(ctx, live)
	if err != nil {
		t.Fatalf("dependencyHold: %v", err)
	}
	if held != "" {
		t.Errorf("the dependency is the service's current release and the hold says %q", held)
	}

	// An item waiting on one that has not shipped: the hold fires, and it names
	// the condition rather than a verdict.
	unshipped, err := item.NewCut(d.pool).Create(ctx, cutActor, item.New{
		IntentID: "in_dependent2", ServiceID: res.serviceID, Branch: "item/dependent-waiting",
	})
	if err != nil {
		t.Fatalf("cutting the item nothing shipped: %v", err)
	}
	waiting, err := item.NewCut(d.pool).Create(ctx, cutActor, item.New{
		IntentID: "in_dependent3", ServiceID: res.serviceID, Branch: "item/dependent-held",
		WaitsOn: []string{unshipped.ID},
	})
	if err != nil {
		t.Fatalf("cutting the held item: %v", err)
	}
	held, err = p.dependencyHold(ctx, waiting)
	if err != nil {
		t.Fatalf("dependencyHold: %v", err)
	}
	if !strings.Contains(held, gate.HoldDependencyNotLive) {
		t.Errorf("the hold says %q, want %q", held, gate.HoldDependencyNotLive)
	}

	// Nothing was written for it: a hold over a record that already exists is
	// recomputed at every firing, and a record for it would be a decision where
	// nothing is decided.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeWait {
			t.Errorf("the log holds a wait row %s, and the dependency hold writes nothing", row.ID)
		}
	}
}

// TestTheWalkSkipsAPayloadItCannotRead appends an opening row whose payload is
// not the gate's shape, before the run, so the walk meets it first. A payload
// is unconstrained bytes by decisionlog's contract, so a row the walk cannot
// read is skipped and the search goes on — one such row does not take down
// every walk.
func TestTheWalkSkipsAPayloadItCannotRead(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	_, err := decisionlog.NewWriter(d.pool).AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Name: "gate.some_other_gate"},
		Payload:       "a payload this walk has no shape for",
		PolicyVersion: "policy-unauthored-m1",
		ScoreVersion:  "score-stub-m1",
	})
	if err != nil {
		t.Fatalf("appending the unreadable opening row: %v", err)
	}

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, c.deployID); err != nil {
		t.Fatalf("the walk stopped on a row it cannot read: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", c.deployID, theStatement, walked.String())
	}
}

// TestAnEmptyAnswerIsAskedAgain scripts a blank line at the interview. The
// answer is write-once and the interview is one round, so the blank line is
// asked again rather than sent — sending it would stamp the question answered
// with nothing in it.
func TestAnEmptyAnswerIsAskedAgain(t *testing.T) {
	ctx, d, out := newPath(t, "\n"+theAnswer+"\n"+approvals)

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "type one") {
		t.Errorf("the blank line was not asked again:\n%s", out)
	}

	questions, err := intent.Questions(ctx, d.pool, only(t, res).intentID)
	if err != nil {
		t.Fatalf("reading the questions: %v", err)
	}
	if len(questions) != 1 || questions[0].Answer != theAnswer {
		t.Errorf("the questions are %+v, want the one question answered %q", questions, theAnswer)
	}
}

// TestTheCutReachesAnExistingService writes the service record before the run
// and asserts the path reaches it rather than creating a second one. The
// service's name is unique in the store, so a cut that created every run
// would be refused by that constraint from the second item on that service
// onwards — a later change, or this one run again after a reject.
func TestTheCutReachesAnExistingService(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	// The service record is already there — newPath writes it, the watch window's
	// four being fields of it and having to be authored before the first window opens.
	// What this test is about is what the cut does with one it finds.
	before, found, err := service.ByName(ctx, d.pool, d.service)
	if err != nil || !found {
		t.Fatalf("reading the service before the run: found %v, %v", found, err)
	}

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if res.serviceID != before.ID {
		t.Errorf("the cut used service %s, the record it should have reached is %s", res.serviceID, before.ID)
	}

	var services int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+service.Table+` where name = $1`, d.service).Scan(&services); err != nil {
		t.Fatalf("counting the services named %q: %v", d.service, err)
	}
	if services != 1 {
		t.Errorf("%d services are named %q, the cut writes a service's identity once", services, d.service)
	}
}

// TestARejectStopsThePath scripts a reject at the merge row: the path stops
// before any release exists, the item goes back to implementation with an attempt
// counted there, master is never created, and the closing row carries the
// feedback.
func TestARejectStopsThePath(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\napprove\nreject not what I asked for\n")

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped with an error, and a reject is not one: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if !c.rejected {
		t.Fatal("the verdict was reject and the run does not say so")
	}
	if c.releaseID != "" || c.deployID != "" {
		t.Errorf("the run names release %q and deploy %q, a reject ships nothing", c.releaseID, c.deployID)
	}

	// No release exists.
	var releases int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+release.Table).Scan(&releases); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != 0 {
		t.Errorf("%d releases exist, a reject mints none", releases)
	}

	// The item is at implementation with two attempts there: the authoring one,
	// and the one the reject booked against the stage it was sent to.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("item stage = %s, a rejected item goes back to implementation", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		want := 1
		if st.Stage == item.StageImplementation {
			want = 2
		}
		if st.Attempts != want {
			t.Errorf("stage %s attempts = %d, want %d", st.Stage, st.Attempts, want)
		}
	}

	// Master was never created.
	if _, err := git(d.repo, "rev-parse", "--verify", "master"); err == nil {
		t.Error("master exists, and the fast-forward runs only after the queue passes a candidate")
	}

	// The environment stays the item's: nothing waits on the environment a
	// rejected candidate used.
	env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if !env.Live() {
		t.Error("the rejected item's environment was torn down, and it stays the item's until it merges or is dropped")
	}

	// The closing row carries the feedback.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("the log holds %d rows, and a reject at the merge row is two decisions: the production row never fires", len(rows))
	}
	payload := closingPayload(t, rows[3])
	if payload.Verdict != string(gate.VerdictReject) {
		t.Errorf("the closing carries verdict %q, the human rejected", payload.Verdict)
	}
	if payload.Feedback != "not what I asked for" {
		t.Errorf("the closing carries feedback %q, the human typed %q", payload.Feedback, "not what I asked for")
	}
	if payload.ReturnsTo != gate.ReturnsTo {
		t.Errorf("the closing returns the item to %q, want %q", payload.ReturnsTo, gate.ReturnsTo)
	}
}

// twoRunsOnOneService walks the path twice on one service and returns what each
// run shipped. The second run reads from a reader of its own because run wraps
// the reader in a bufio.Scanner, which reads further than the lines it hands
// back, so a second scanner over the same reader finds it drained — the run
// subcommand is one process per run and never meets that. The fake spec author
// asks its one question on its first call only.
//
// secondInput is what the second run's human types, and it is empty wherever the
// second run is meant to auto-pass at every row: a reader with nothing in it is
// how a test says nobody was asked anything.
func twoRunsOnOneService(t *testing.T, firstVerdicts, secondInput string) (context.Context, deps, *candidate, *candidate) {
	t.Helper()
	ctx, d, out := newPath(t, theAnswer+"\n"+firstVerdicts)

	first, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(secondInput)
	second, err := run(ctx, d, []string{theSecondStatement})
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	return ctx, d, only(t, first), only(t, second)
}

// TestASecondChangeShips is the second change on one service: a second intent, a
// second item, a second criterion authored beside the one already in force, and a
// build encoding both — which is what the encoding check demands of a build whose
// item set holds the first item, it having merged. It ships as release number 2,
// and the walk from its deploy reaches its own intent.
func TestASecondChangeShips(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, approvals, "")

	if second.itemID == first.itemID {
		t.Errorf("both runs report item %s, a second change is a second item", second.itemID)
	}

	// Release number 2, the number being minted per service.
	rel, err := release.Get(ctx, d.pool, second.releaseID)
	if err != nil {
		t.Fatalf("reading the second release: %v", err)
	}
	if rel.Number != 2 {
		t.Errorf("the second release's number = %d, the second release of a service is 2", rel.Number)
	}

	// Two criteria in force for the second item's build, the second one authored
	// rather than the first restated.
	inForce, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{first.itemID, second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria in force: %v", err)
	}
	if len(inForce) != 2 {
		t.Fatalf("%d criteria are in force, two items introduced one each: %+v", len(inForce), inForce)
	}
	if inForce[0].Sentence != criterionSentence || inForce[1].Sentence != secondCriterionSentence {
		t.Errorf("the sentences in force are %q and %q, want the first item's then the second's",
			inForce[0].Sentence, inForce[1].Sentence)
	}

	// The second build encodes both, which is the check the second run had to
	// pass and is asserted here over the tree that build was made from.
	if err := criterion.CheckEncodings(d.repo, inForce); err != nil {
		t.Errorf("the second build does not satisfy the encoding check: %v", err)
	}
	encoded, err := criterion.Encodings(d.repo)
	if err != nil {
		t.Fatalf("reading the encodings: %v", err)
	}
	if len(encoded) != 2 {
		t.Errorf("the build names %d criteria in its tests, both criteria in force are encoded: %v", len(encoded), encoded)
	}

	// The walk from the second deploy reaches the second intent and no other.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, second.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theSecondStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", second.deployID, theSecondStatement, walked.String())
	}
	if strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s reaches the first intent's statement:\n%s", second.deployID, walked.String())
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after two changes: %v", err)
	}
}

// TestTheSecondChangeShipsWithNoHumanAtAnyGate is M2's demonstration: the second
// item on the service reads under the threshold at every row, so the factory gives
// every verdict itself and nobody is asked anything — the second run's scripted
// input is empty, so a run that stopped to ask would fail on a reader with nothing
// in it.
//
// What made the difference is the first run: a human approved its implementation,
// which narrowed the prior on the model that wrote it and the history of the area
// it was in, and its release gave the service something to return to. The factory
// earns the autonomy rather than starting with it.
func TestTheSecondChangeShipsWithNoHumanAtAnyGate(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, approvals, "")

	for name, firing := range map[string]fired{
		"candidate deploy": first.candidateGate,
		"merge":            first.mergeGate,
		"production":       first.deployGate,
	} {
		if !firing.humanDecided {
			t.Fatalf("the first item's %s row auto-passed, and on a fresh factory a human decides every one", name)
		}
	}
	for name, firing := range map[string]fired{
		"candidate deploy": second.candidateGate,
		"merge":            second.mergeGate,
		"production":       second.deployGate,
	} {
		if firing.humanDecided {
			t.Fatalf("the second item's %s row put a human there because %q", name, firing.whyHuman)
		}
	}
	if second.deployID == "" {
		t.Fatal("the second item did not deploy")
	}
	if second.mergeGate.number >= second.mergeGate.threshold {
		t.Errorf("the second item's merge number is %v against a threshold of %v",
			second.mergeGate.number, second.mergeGate.threshold)
	}
	if !(second.mergeGate.number < first.mergeGate.number) {
		t.Errorf("the second item reads %v and the first read %v, and the evidence the first left narrows the second",
			second.mergeGate.number, first.mergeGate.number)
	}

	// Every one of the second run's decisions was closed by the gate component and
	// says what auto-passed it, and every opening row of an auto-pass waits on
	// nobody — which is how a reader of the log tells a decision nobody was asked
	// to make from a pending one.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 12 {
		t.Fatalf("the log holds %d rows, two runs of three decisions are twelve", len(rows))
	}
	for _, row := range rows[6:] {
		if row.Part == decisionlog.PartOpening {
			if payload := openingPayload(t, row); payload.WaitsOn != "" {
				t.Errorf("an auto-passed firing waits on %q", payload.WaitsOn)
			}
			continue
		}
		if row.Actor.Kind != record.KindComponent {
			t.Errorf("the second run's closing was written by %+v, want the gate component", row.Actor)
		}
		payload := closingPayload(t, row)
		if payload.Verdict != string(gate.VerdictApprove) || payload.AutoPassedBy != gate.AutoPassedByThreshold {
			t.Errorf("the closing says %+v, want an approve auto-passed by the threshold", payload)
		}
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after two runs: %v", err)
	}
}

// TestAPinPutsAHumanBackAtAGateAndTheHoldStopsTheDeploy is the other half of
// M2's demonstration. An owner pins the production deploy row, so the row the
// score would have passed puts a human there; the human holds; and the run stops
// with the release minted, nothing deployed, no attempt counted, and the item
// where it was.
func TestAPinPutsAHumanBackAtAGateAndTheHoldStopsTheDeploy(t *testing.T) {
	ctx, d, _, _ := twoRunsOnOneService(t, approvals, "")

	placed, version, err := policy.NewFactory(d.pool).Pin(ctx,
		record.Actor{Kind: record.KindHuman, Name: d.human}, gatepolicy.RiskThreshold,
		pin.Subject{Kind: pin.SubjectGateRow, ID: string(gate.DeployToProduction)}, 0, nil)
	if err != nil {
		t.Fatalf("placing the pin: %v", err)
	}

	d.in = strings.NewReader("hold the window before this one is still open\n")
	res, err := run(ctx, d, []string{theThirdStatement})
	if err != nil {
		t.Fatalf("the third run stopped, and a hold is not an error: %v", err)
	}
	third := only(t, res)

	if !third.held {
		t.Fatal("the verdict was hold and the run does not say so")
	}
	if third.mergeGate.humanDecided {
		t.Errorf("the merge row put a human there because %q, and the pin names the deploy row alone", third.mergeGate.whyHuman)
	}
	if !third.deployGate.humanDecided || third.deployGate.whyHuman != gate.WhyPinned {
		t.Errorf("the deploy row says human %v because %q, want the pin",
			third.deployGate.humanDecided, third.deployGate.whyHuman)
	}
	if third.deployGate.number >= third.deployGate.threshold {
		t.Errorf("the deploy number is %v against a threshold of %v, and the pin is what put a human there rather than the number",
			third.deployGate.number, third.deployGate.threshold)
	}
	if !slices.Contains(third.deployGate.pins, placed.ID) {
		t.Errorf("the firing names pins %v, want the one placed", third.deployGate.pins)
	}
	if third.deployGate.policyVersion != version.ID {
		t.Errorf("the firing names policy version %q, want the one the pin appended %q",
			third.deployGate.policyVersion, version.ID)
	}

	// The release is minted and nothing is deployed: a hold is a stop and not an
	// undo, and the change is still good.
	if third.releaseID == "" {
		t.Error("the run minted no release, and the hold is after the merge")
	}
	if third.deployID != "" {
		t.Errorf("the run deployed %s, and a hold stops the event", third.deployID)
	}
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ReleaseID == third.releaseID {
		t.Errorf("what runs in production is %q of release %q, and the held release is not deployed",
			current.ID, current.ReleaseID)
	}

	// The item is merged and stays there, and no attempt was counted for the
	// hold: a hold is not a failed attempt.
	it, err := item.Get(ctx, d.pool, third.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageMerged {
		t.Errorf("the held item is at %s, want merged", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, third.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		if st.Attempts != 1 {
			t.Errorf("stage %s records %d attempts, and a hold counts none", st.Stage, st.Attempts)
		}
	}

	// The hold is the verdict of that firing's decision, with the human as its
	// actor.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	closing := rows[len(rows)-1]
	if closing.Actor.Kind != record.KindHuman {
		t.Errorf("the hold was written by %+v, want the human who set it", closing.Actor)
	}
	payload := closingPayload(t, closing)
	if payload.Verdict != string(gate.VerdictHold) {
		t.Errorf("the closing says verdict %q, want a hold", payload.Verdict)
	}
	if payload.ReturnsTo != "" {
		t.Errorf("the hold sends the item to %q, and a hold sends nothing back", payload.ReturnsTo)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify after a hold: %v", err)
	}

	// Withdrawing the pin leaves the row the score's again, which is what a pin
	// being a bound rather than a precedence means at this row: nothing else
	// moved.
	if _, err := policy.NewFactory(d.pool).WithdrawPin(ctx,
		record.Actor{Kind: record.KindHuman, Name: d.human}, placed.ID); err != nil {
		t.Fatalf("withdrawing the pin: %v", err)
	}
	applied, err := policy.NewReader(d.pool).AtGate(ctx, policy.Subjects{
		GateRow:       string(gate.DeployToProduction),
		EnvironmentID: res.environmentID,
		ServiceID:     res.serviceID,
		AreaID:        res.areaID,
	})
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if applied.HumanPinned {
		t.Error("the withdrawn pin still adds a human at the row")
	}
}

// TestTheSecondCandidateBranchIsBasedOnMaster is why the first item's encoding
// is in the second item's build. The cut has the implementation role commit the
// candidate branch with no base, and that is the case for every candidate cut
// before the first release: the first branch is one commit deep with no ancestor,
// and the second is based on master, so what the first item merged is in the tree
// the second one starts from.
func TestTheSecondCandidateBranchIsBasedOnMaster(t *testing.T) {
	_, d, first, second := twoRunsOnOneService(t, approvals, "")

	if _, err := git(d.repo, "merge-base", "--is-ancestor", "master", second.branch); err != nil {
		t.Errorf("master is not an ancestor of %s, and every candidate after the first release is based on master: %v",
			second.branch, err)
	}
	depth, err := git(d.repo, "rev-list", "--count", first.branch)
	if err != nil {
		t.Fatalf("counting the first branch's commits: %v", err)
	}
	if depth != "1" {
		t.Errorf("%s is %s commits deep, the first item's branch is committed with no base", first.branch, depth)
	}
}

// TestARejectThenASecondRunShips is the other way a service reaches a second
// item: the first was rejected, so master does not exist and the second branch is
// committed with no base too. The rejected item's criterion is not in force for
// the second item's build — a build is a set of items and the rejected one is not
// in it, which is what lets a candidate cut in parallel with another one build at
// all. What it ships is release number 1, the reject having minted none.
func TestARejectThenASecondRunShips(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, "approve\nreject not what I asked for\n", approvals)

	if !first.rejected {
		t.Fatal("the first run's scripted verdict was a reject and the run does not say so")
	}
	if second.rejected {
		t.Fatal("the second run reports rejected, and its scripted verdict was approve")
	}

	rel, err := release.Get(ctx, d.pool, second.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("the release's number = %d, the rejected item minted none so this is the service's first", rel.Number)
	}

	// One criterion in force for the second item's build: its own. The rejected
	// item's is a promise the service records and this build's tree could not keep.
	inForce, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria in force: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ItemID != second.itemID {
		t.Fatalf("%d criteria are in force for the second item's build: %+v", len(inForce), inForce)
	}
	if err := criterion.CheckEncodings(d.repo, inForce); err != nil {
		t.Errorf("the second build does not satisfy the encoding check: %v", err)
	}

	// Both criteria are records of the service all the same, nothing here
	// withdrawing one.
	both, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{first.itemID, second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria of both items: %v", err)
	}
	if len(both) != 2 {
		t.Errorf("%d criteria belong to the two items, each introduced one", len(both))
	}

	// The second branch had no base either: the reject minted no release, so
	// nothing had created master by the time it was cut.
	depth, err := git(d.repo, "rev-list", "--count", second.branch)
	if err != nil {
		t.Fatalf("counting the second branch's commits: %v", err)
	}
	if depth != "1" {
		t.Errorf("the second branch is %s commits deep, and with master absent it is committed with no base", depth)
	}
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

func (m *refusingModel) Complete(ctx context.Context, system, user string) (agent.Reply, error) {
	if system == agent.ImplementerSystemPrompt {
		m.callsMade++
		if m.refused < m.refusals {
			m.refused++
			return agent.Reply{Text: "Sure! Here are the files you asked for:\n\n=== FILE main.go ===\npackage main\n=== END ===", Tokens: 5}, nil
		}
	}
	return m.inner.Complete(ctx, system, user)
}

// TestARefusedReplyIsRetried is the bound doing its work: the implementer's
// first reply is prose the protocol refuses, the second is a change, and the
// take ships — with the item's implementation stage recording both attempts,
// because the count the bound is compared against is the item's own.
func TestARefusedReplyIsRetried(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &refusingModel{inner: &fakeModel{}, refusals: 1}

	res, err := run(ctx, d, []string{theStatement})
	if err != nil {
		t.Fatalf("the path stopped, and one refused reply is inside the bound: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if c.deployID == "" {
		t.Fatal("the run shipped nothing after a retry that succeeded")
	}
	if !strings.Contains(out.String(), "reply was refused") {
		t.Errorf("the run does not report the refused reply:\n%s", out)
	}

	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		want := 1
		if st.Stage == item.StageImplementation {
			want = 2
		}
		if st.Attempts != want {
			t.Errorf("stage %s attempts = %d, want %d", st.Stage, st.Attempts, want)
		}
		if st.SpendTokens <= 0 {
			t.Errorf("stage %s spend = %d, a refused attempt spent tokens too", st.Stage, st.SpendTokens)
		}
	}
}

// TestAStageOutOfAttemptsStops is the other end of the bound: every reply
// refused, so the factory stops retrying and says it is stuck. Nothing ships,
// and the item carries the whole count — which is what an escalation is read
// from once there is a surface to read it on.
func TestAStageOutOfAttemptsStops(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	model := &refusingModel{inner: &fakeModel{}, refusals: attemptBound + 5}
	d.model = model

	res, err := run(ctx, d, []string{theStatement})
	if err == nil {
		t.Fatalf("the path finished, and every implementer reply was refused:\n%s", out)
	}
	if !errors.Is(err, agent.ErrReply) {
		t.Errorf("the error is %v, want the refused reply wrapped in it", err)
	}
	if !strings.Contains(err.Error(), "stuck on this item") {
		t.Errorf("the error is %q, and a stage out of attempts is the factory saying it cannot do this one", err)
	}
	if model.callsMade != attemptBound {
		t.Errorf("the implementer was called %d times, the bound is %d", model.callsMade, attemptBound)
	}
	if len(res.candidates) != 0 {
		t.Errorf("the run reports %d candidates, a stage out of attempts finishes none", len(res.candidates))
	}

	// The item exists and carries the whole count, the stage having reported each
	// attempt as it was made.
	var attempts int
	if err := d.pool.QueryRow(ctx, `select attempts from `+item.StageTable+` where stage = $1`,
		string(item.StageImplementation)).Scan(&attempts); err != nil {
		t.Fatalf("reading the implementation stage's attempts: %v", err)
	}
	if attempts != attemptBound {
		t.Errorf("the implementation stage records %d attempts, the bound spent %d", attempts, attemptBound)
	}
}

// erroringModel answers the implementer with an error that is not a protocol
// failure, standing for the rate-limited account the design holds rather than
// retries.
type erroringModel struct {
	inner agent.Model
	calls int
}

var errNotTheProtocol = errors.New("the model API answered 429")

func (m *erroringModel) Complete(ctx context.Context, system, user string) (agent.Reply, error) {
	if system == agent.ImplementerSystemPrompt {
		m.calls++
		return agent.Reply{}, errNotTheProtocol
	}
	return m.inner.Complete(ctx, system, user)
}

// TestAnErrorThatIsNotAProtocolFailureIsNotRetried is what the bound is not
// for: an account that refuses the call is a hold in the design and not an
// attempt at the work, so the first failure stops the run with its own error
// and the remaining attempts are never spent.
func TestAnErrorThatIsNotAProtocolFailureIsNotRetried(t *testing.T) {
	ctx, d, _ := newPath(t, theAnswer+"\n"+approvals)
	model := &erroringModel{inner: &fakeModel{}}
	d.model = model

	_, err := run(ctx, d, []string{theStatement})
	if !errors.Is(err, errNotTheProtocol) {
		t.Fatalf("the path stopped with %v, want the model's own error", err)
	}
	if strings.Contains(err.Error(), "stuck on this item") {
		t.Errorf("the error is %q, and this failure is not the factory running out of attempts", err)
	}
	if model.calls != 1 {
		t.Errorf("the implementer was called %d times, an error the bound does not cover is not retried", model.calls)
	}
}

// TestARunThatStoppedLeavesAnItemTheNextQueueFinishes is the queue's membership
// being the service's and not the run's. A run that stopped after one merge gate
// approved leaves that item at the queued stage, and nothing in the crude interface
// clears one — so the next run has to finish it rather than failing on it, which is
// what a run of a service whose queue holds somebody else's item would otherwise do
// after it had already spent the model calls.
func TestARunThatStoppedLeavesAnItemTheNextQueueFinishes(t *testing.T) {
	// The input runs out after the first candidate's merge gate: the interview, two
	// candidate deploy rows, and one merge approval, and then nothing for the second
	// merge row to read.
	ctx, d, out := newPath(t, theAnswer+"\napprove\napprove\napprove\n")

	stopped, err := run(ctx, d, []string{theStatement, theSecondStatement})
	if err == nil {
		t.Fatalf("the run finished, and its input ended before the second merge row:\n%s", out)
	}
	if len(stopped.candidates) != 2 {
		t.Fatalf("the run authored %d candidates before it stopped, want two", len(stopped.candidates))
	}
	left := stopped.candidates[0]
	it, err := item.Get(ctx, d.pool, left.itemID)
	if err != nil {
		t.Fatalf("reading the item the run left: %v", err)
	}
	if it.Stage != item.StageQueued {
		t.Fatalf("the first item is at %s, and the run stopped after its merge gate approved", it.Stage)
	}

	// A later run on the same service: its queue holds the item left behind and its
	// own, and it finishes both. It is asked for verdicts because the stopped run
	// minted no release, so the service still has nothing to return to and its number
	// still reads over the threshold.
	d.in = strings.NewReader(approvals)
	next, err := run(ctx, d, []string{theFourthStatement})
	if err != nil {
		t.Fatalf("the next run stopped on an item the earlier one left queued: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "was left in the queue by an earlier run") {
		t.Errorf("the run does not report adopting the item left behind:\n%s", out)
	}

	byItem := map[string]*candidate{}
	for _, c := range next.candidates {
		byItem[c.itemID] = c
	}
	adopted := byItem[left.itemID]
	if adopted == nil {
		t.Fatalf("the run reports %d candidates and none of them is the adopted item %s", len(next.candidates), left.itemID)
	}
	if !adopted.merged || adopted.releaseID == "" {
		t.Errorf("the adopted item merged=%v release=%q", adopted.merged, adopted.releaseID)
	}
	if !adopted.tornDown {
		t.Error("the adopted item merged and its environment was not torn down")
	}
	if adopted.deployID == "" {
		t.Error("the adopted item was minted a release and deployed nothing")
	}
	for _, c := range next.candidates {
		if c.itemID != left.itemID && !c.merged {
			t.Errorf("the run's own item %s did not merge alongside the adopted one", c.itemID)
		}
	}

	// One release per item, and no item has two: the queue mints once per merge.
	var releases, items int
	if err := d.pool.QueryRow(ctx, `select count(*), count(distinct item_id) from `+release.Table).Scan(&releases, &items); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != items {
		t.Errorf("%d releases across %d items, and one item is one release", releases, items)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}
