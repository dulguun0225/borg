// The end-to-end test: roadmap M1's demonstration, driven through the same
// run function the run subcommand calls, with a fake model and scripted
// input. Each test gets a PostgreSQL schema of its own with the whole factory
// schema applied through postgres.Apply. None of these tests skips when the
// database is unreachable: the milestone is demonstrated by them running, so
// an unreachable database fails the run.
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
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// The two interviews and the two specs the fake model plays out: the first
// item's on a service with nothing in force, the second item's on the same
// service, where the first criterion already is. Both sentences classify as
// the event pattern, and both are the pattern's response form, which is what
// theResponse reads the encoding's expected value out of.
const (
	theStatement            = "The demo service needs a health check."
	theQuestion             = "What does a healthy response say?"
	theAnswer               = "ok"
	criterionSentence       = "When asked for its health, the system shall respond ok."
	theSecondStatement      = "The demo service needs a version endpoint."
	secondCriterionSentence = "When asked for its version, the system shall respond two."
)

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
// wiring defect and an error. The first spec-author call asks the one
// question, the next delivers a spec — the second criterion where the prompt
// says one is already in force, so a second item on the service does not
// restate the first's promise — and the implementer call returns whole files
// with one encoding per criterion the prompt names.
type fakeModel struct {
	specCalls int
}

func (m *fakeModel) Complete(_ context.Context, system, user string) (agent.Reply, error) {
	switch system {
	case agent.SpecAuthorSystemPrompt:
		m.specCalls++
		if m.specCalls == 1 {
			return agent.Reply{Text: "QUESTION: " + theQuestion, Tokens: 11}, nil
		}
		spec, sentence := "The demo service answers a health check with ok.", criterionSentence
		if briefCriterion.MatchString(user) {
			spec, sentence = "The demo service answers a version request with two.", secondCriterionSentence
		}
		return agent.Reply{Text: "SPEC:\n" + spec + "\nCRITERION: " + sentence, Tokens: 23}, nil
	case agent.ImplementerSystemPrompt:
		named := briefCriterion.FindAllStringSubmatch(user, -1)
		if len(named) == 0 {
			return agent.Reply{}, fmt.Errorf("fake model: the implementer's prompt names no criterion")
		}
		text, err := implementerReply(named)
		if err != nil {
			return agent.Reply{}, err
		}
		return agent.Reply{Text: text, Tokens: 37}, nil
	}
	return agent.Reply{}, fmt.Errorf("fake model: the system prompt is neither role's")
}

// implementerReply is the implementer's whole reply for the criteria the brief
// named, in the order it named them: a module file, one function per criterion
// in main.go, and one encoding per criterion — a test naming that criterion's
// id in its body, whose expected value is read out of the criterion's sentence
// and not out of the code. Every criterion in force is encoded because the
// check over the build rejects one that is not, so a second item's reply
// carries the first item's encoding again. The main function sleeps in a loop
// so the deployed process stays alive for ReadRunning.
func implementerReply(named [][]string) (string, error) {
	main := []string{"package main", "", `import "time"`, ""}
	var encodings []string
	for i, match := range named {
		id, sentence := match[1], match[2]
		response := theResponse.FindStringSubmatch(sentence)
		if response == nil {
			return "", fmt.Errorf("fake model: the sentence of %s is not the response form this fake encodes: %q", id, sentence)
		}
		function := fmt.Sprintf("respond%d", i+1)
		main = append(main, fmt.Sprintf("func %s() string { return %q }", function, response[1]))
		encodings = append(encodings,
			"=== FILE "+function+"_test.go ===",
			"package main",
			"",
			`import "testing"`,
			"",
			fmt.Sprintf("func TestRespond%d(t *testing.T) {", i+1),
			"\t// "+id,
			fmt.Sprintf("\tif %s() != %q {", function, response[1]),
			fmt.Sprintf("\t\tt.Fatalf(%q, %s())", function+"() = %q, the criterion requires "+response[1], function),
			"\t}",
			"}",
			"=== END ===",
		)
	}
	main = append(main, "", "func main() {", "\tfor {", "\t\ttime.Sleep(time.Hour)", "\t}", "}")

	lines := []string{"=== FILE go.mod ===", "module demo", "", "go 1.24", "=== END ===", "=== FILE main.go ==="}
	lines = append(lines, main...)
	lines = append(lines, "=== END ===")
	lines = append(lines, encodings...)
	return strings.Join(lines, "\n"), nil
}

// newPath gives a test a schema of its own with the whole schema applied,
// temp directories for the repository and the targets, a secrets file holding
// the deploy credential, a local target whose started process is stopped in
// cleanup — through the seam — and the deps the path runs over. input is what
// the scripted human types.
func newPath(t *testing.T, input string) (context.Context, deps, *bytes.Buffer) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1_factory_" + hex.EncodeToString(suffix[:])

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

	targets := t.TempDir()
	target := localtarget.New(targets)
	t.Cleanup(func() {
		if err := target.Stop(context.Background(), "demo", credential); err != nil {
			t.Errorf("stopping the demo service: %v", err)
		}
	})

	out := &bytes.Buffer{}
	return ctx, deps{
		pool:       pool,
		model:      &fakeModel{},
		target:     target,
		targets:    targets,
		credential: credential,
		in:         strings.NewReader(input),
		out:        out,
		human:      "owner",
		service:    "demo",
		repo:       filepath.Join(t.TempDir(), "demo"),
	}, out
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

// TestOneChangeShips is the demonstration: one change followed end to end,
// approved at the one gate, released as number 1, deployed straight, running
// on the target, and walkable from the deploy back to the intent with the
// decision readable in a clean chain.
func TestOneChangeShips(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\napprove\n")

	res, err := run(ctx, d, theStatement)
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if res.rejected {
		t.Fatal("the run reports rejected, and the scripted verdict was approve")
	}

	// The intent: one round, its question answered, refined.
	in, err := intent.Get(ctx, d.pool, res.intentID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if in.Rounds != 1 {
		t.Errorf("intent rounds = %d, one round was asked", in.Rounds)
	}
	if in.State != intent.StateRefined {
		t.Errorf("intent state = %s, the interview marked it refined", in.State)
	}
	questions, err := intent.Questions(ctx, d.pool, res.intentID)
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
	it, err := item.Get(ctx, d.pool, res.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageMerged {
		t.Errorf("item stage = %s, the path ends at merged", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, res.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("the item has %d stage rows, spec and implementation reported one each", len(stages))
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

	// The release is number 1.
	rel, err := release.Get(ctx, d.pool, res.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("release number = %d, a service's first release is 1", rel.Number)
	}

	// The deploy completed and Current names it — what is running, not what
	// is newest.
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, "production")
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ID != res.deployID {
		t.Errorf("the current deploy is %q found=%t, the path deployed %s", current.ID, found, res.deployID)
	}
	if current.Status != deploy.StatusComplete {
		t.Errorf("deploy status = %s, the straight deploy completes", current.Status)
	}

	// The target runs the release.
	running, err := d.target.ReadRunning(ctx, "demo", d.credential)
	if err != nil {
		t.Fatalf("reading what the target runs: %v", err)
	}
	if running.Release != res.releaseID {
		t.Errorf("the target runs %q, the deploy put %s there", running.Release, res.releaseID)
	}

	// Master exists in the repository at the candidate commit.
	master, err := git(d.repo, "rev-parse", "master")
	if err != nil {
		t.Fatalf("reading master: %v", err)
	}
	if master != res.commit {
		t.Errorf("master is at %s, the fast-forward targeted %s", master, res.commit)
	}

	// The walk alone — the walk subcommand's code — reaches the intent's
	// statement from the deploy id, and reports the chain clean.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, res.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", res.deployID, theStatement, walked.String())
	}
	if !strings.Contains(walked.String(), "the chain is clean") {
		t.Errorf("the walk does not report the chain clean:\n%s", walked.String())
	}

	// The log: exactly one opening and one closing row, the closing naming
	// the opening and carrying verdict approve with the human as actor, the
	// opening's payload naming the implementation artifact.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the log holds %d rows, one decision is two", len(rows))
	}
	opening, closing := rows[0], rows[1]
	if opening.Shape != decisionlog.ShapeDecision || opening.Part != decisionlog.PartOpening {
		t.Errorf("the first row is shape %s part %s, the gate's firing appends an opening decision row", opening.Shape, opening.Part)
	}
	if closing.Part != decisionlog.PartClosing || closing.Closes != opening.ID {
		t.Errorf("the second row is part %s closing %q, the verdict closes %s", closing.Part, closing.Closes, opening.ID)
	}
	if closing.Actor != (record.Actor{Kind: record.KindHuman, Name: "owner"}) {
		t.Errorf("the closing row's actor is %+v, the human owner decided", closing.Actor)
	}
	var openingPayload gate.OpeningPayload
	if err := json.Unmarshal([]byte(opening.Payload), &openingPayload); err != nil {
		t.Fatalf("reading the opening payload: %v", err)
	}
	if openingPayload.ArtifactID != res.implArtifactID {
		t.Errorf("the opening names artifact %s, the decision was over implementation %s",
			openingPayload.ArtifactID, res.implArtifactID)
	}
	var closingPayload gate.ClosingPayload
	if err := json.Unmarshal([]byte(closing.Payload), &closingPayload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if closingPayload.Verdict != string(gate.VerdictApprove) {
		t.Errorf("the closing carries verdict %q, the human approved", closingPayload.Verdict)
	}
	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// TestTheWalkSkipsAPayloadItCannotRead appends an opening row whose payload is
// not the gate's shape, before the run, so the walk meets it first. A payload
// is unconstrained bytes by decisionlog's contract, so a row the walk cannot
// read is skipped and the search goes on — one such row does not take down
// every walk.
func TestTheWalkSkipsAPayloadItCannotRead(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\napprove\n")

	_, err := decisionlog.NewWriter(d.pool).AppendDecisionOpening(ctx, decisionlog.Entry{
		Actor:         record.Actor{Kind: record.KindComponent, Name: "gate.some_other_gate"},
		Payload:       "a payload this walk has no shape for",
		PolicyVersion: "policy-unauthored-m1",
		ScoreVersion:  "score-stub-m1",
	})
	if err != nil {
		t.Fatalf("appending the unreadable opening row: %v", err)
	}

	res, err := run(ctx, d, theStatement)
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}

	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, res.deployID); err != nil {
		t.Fatalf("the walk stopped on a row it cannot read: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", res.deployID, theStatement, walked.String())
	}
}

// TestAnEmptyAnswerIsAskedAgain scripts a blank line at the interview. The
// answer is write-once and the interview is one round, so the blank line is
// asked again rather than sent — sending it would stamp the question answered
// with nothing in it.
func TestAnEmptyAnswerIsAskedAgain(t *testing.T) {
	ctx, d, out := newPath(t, "\n"+theAnswer+"\napprove\n")

	res, err := run(ctx, d, theStatement)
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	if !strings.Contains(out.String(), "type one") {
		t.Errorf("the blank line was not asked again:\n%s", out)
	}

	questions, err := intent.Questions(ctx, d.pool, res.intentID)
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
	ctx, d, out := newPath(t, theAnswer+"\napprove\n")

	before, err := service.NewWriter(d.pool).Create(ctx,
		record.Actor{Kind: record.KindComponent, Name: "cut"}, d.service, d.repo)
	if err != nil {
		t.Fatalf("writing the service before the run: %v", err)
	}

	res, err := run(ctx, d, theStatement)
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

// TestARejectStopsThePath scripts a reject at the gate: the path stops before
// any release exists, the item stays at implementation, master is never
// created, and the closing row carries the feedback.
func TestARejectStopsThePath(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\nreject not what I asked for\n")

	res, err := run(ctx, d, theStatement)
	if err != nil {
		t.Fatalf("the path stopped with an error, and a reject is not one: %v\noutput so far:\n%s", err, out)
	}
	if !res.rejected {
		t.Fatal("the verdict was reject and the run does not say so")
	}
	if res.releaseID != "" || res.deployID != "" {
		t.Errorf("the run names release %q and deploy %q, a reject ships nothing", res.releaseID, res.deployID)
	}

	// No release exists.
	var releases int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+release.Table).Scan(&releases); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != 0 {
		t.Errorf("%d releases exist, a reject mints none", releases)
	}

	// The item stays at implementation.
	it, err := item.Get(ctx, d.pool, res.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("item stage = %s, a rejected item stays at implementation", it.Stage)
	}

	// Master was never created.
	if _, err := git(d.repo, "rev-parse", "--verify", "master"); err == nil {
		t.Error("master exists, and the fast-forward runs only after an approve")
	}

	// The closing row carries the feedback.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("the log holds %d rows, one decision is two", len(rows))
	}
	var closingPayload gate.ClosingPayload
	if err := json.Unmarshal([]byte(rows[1].Payload), &closingPayload); err != nil {
		t.Fatalf("reading the closing payload: %v", err)
	}
	if closingPayload.Verdict != string(gate.VerdictReject) {
		t.Errorf("the closing carries verdict %q, the human rejected", closingPayload.Verdict)
	}
	if closingPayload.Feedback != "not what I asked for" {
		t.Errorf("the closing carries feedback %q, the human typed %q", closingPayload.Feedback, "not what I asked for")
	}
}

// twoRunsOnOneService walks the path twice on one service, approving both, and
// returns what each run shipped. The second run reads from a reader of its own
// because run wraps the reader in a bufio.Scanner, which reads further than the
// lines it hands back, so a second scanner over the same reader finds it
// drained — the run subcommand is one process per run and never meets that. The
// fake spec author asks its one question on its first call only, so the second
// run's one scripted line is the verdict.
func twoRunsOnOneService(t *testing.T, firstVerdict string) (context.Context, deps, shipped, shipped) {
	t.Helper()
	ctx, d, out := newPath(t, theAnswer+"\n"+firstVerdict+"\n")

	first, err := run(ctx, d, theStatement)
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("approve\n")
	second, err := run(ctx, d, theSecondStatement)
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	return ctx, d, first, second
}

// TestASecondChangeShips is the second change on one service: a second intent,
// a second item, a second criterion authored beside the one already in force,
// and a build encoding both — which is what the encoding check demands, a
// criterion in force with no encoding naming it being refused. It ships as
// release number 2, and the walk from its deploy reaches its own intent.
func TestASecondChangeShips(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, "approve")

	if second.serviceID != first.serviceID {
		t.Fatalf("the second run cut on service %s, the first on %s, and both name the same service",
			second.serviceID, first.serviceID)
	}
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

	// Two criteria in force, the second one authored rather than the first
	// restated.
	inForce, err := criterion.InForce(ctx, d.pool, second.serviceID)
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

// TestTheSecondCandidateBranchIsBasedOnMaster is why the first item's encoding
// is in the second item's build. The cut has the implementation role commit the
// candidate branch with no base, and that is the first release's case: the first
// branch is one commit deep with no ancestor, and the second is based on master,
// so what the first item merged is in the tree the second one starts from.
func TestTheSecondCandidateBranchIsBasedOnMaster(t *testing.T) {
	_, d, first, second := twoRunsOnOneService(t, "approve")

	firstBranch, secondBranch := "item/"+first.intentID, "item/"+second.intentID

	if _, err := git(d.repo, "merge-base", "--is-ancestor", "master", secondBranch); err != nil {
		t.Errorf("master is not an ancestor of %s, and every item after the first is based on master: %v",
			secondBranch, err)
	}
	depth, err := git(d.repo, "rev-list", "--count", firstBranch)
	if err != nil {
		t.Fatalf("counting the first branch's commits: %v", err)
	}
	if depth != "1" {
		t.Errorf("%s is %s commits deep, the first item's branch is committed with no base", firstBranch, depth)
	}
}

// TestARejectThenASecondRunShips is the other way a service reaches a second
// item: the first was rejected, so master does not exist and the second branch
// is committed with no base too — but the rejected item's criterion is in force
// all the same, nothing here withdrawing one, so the second build has to encode
// both. What it ships is release number 1, the reject having minted none.
func TestARejectThenASecondRunShips(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, "reject not what I asked for")

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

	inForce, err := criterion.InForce(ctx, d.pool, second.serviceID)
	if err != nil {
		t.Fatalf("reading the criteria in force: %v", err)
	}
	if len(inForce) != 2 {
		t.Fatalf("%d criteria are in force, a rejected item's criterion is in force too: %+v", len(inForce), inForce)
	}
	if err := criterion.CheckEncodings(d.repo, inForce); err != nil {
		t.Errorf("the second build does not satisfy the encoding check: %v", err)
	}

	// The second branch had no base either: the reject minted no release, so
	// nothing had created master by the time it was cut.
	depth, err := git(d.repo, "rev-list", "--count", "item/"+second.intentID)
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
	ctx, d, out := newPath(t, theAnswer+"\napprove\n")
	d.model = &refusingModel{inner: &fakeModel{}, refusals: 1}

	res, err := run(ctx, d, theStatement)
	if err != nil {
		t.Fatalf("the path stopped, and one refused reply is inside the bound: %v\noutput so far:\n%s", err, out)
	}
	if res.deployID == "" {
		t.Fatal("the run shipped nothing after a retry that succeeded")
	}
	if !strings.Contains(out.String(), "reply was refused") {
		t.Errorf("the run does not report the refused reply:\n%s", out)
	}

	stages, err := item.Stages(ctx, d.pool, res.itemID)
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
	ctx, d, out := newPath(t, theAnswer+"\napprove\n")
	model := &refusingModel{inner: &fakeModel{}, refusals: attemptBound + 5}
	d.model = model

	res, err := run(ctx, d, theStatement)
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
	if res.releaseID != "" || res.deployID != "" {
		t.Errorf("the run names release %q and deploy %q, a stage out of attempts ships nothing", res.releaseID, res.deployID)
	}

	stages, err := item.Stages(ctx, d.pool, res.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	var implementation int
	for _, st := range stages {
		if st.Stage == item.StageImplementation {
			implementation = st.Attempts
		}
	}
	if implementation != attemptBound {
		t.Errorf("the implementation stage records %d attempts, the bound spent %d", implementation, attemptBound)
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
	ctx, d, _ := newPath(t, theAnswer+"\napprove\n")
	model := &erroringModel{inner: &fakeModel{}}
	d.model = model

	_, err := run(ctx, d, theStatement)
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
