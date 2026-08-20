// Roadmap M5's demonstration, driven through the same run function the run
// subcommand calls: two services, a contract derived from a producer's build, a
// declaration derived from a consumer's build, a breaking change stopped at the merge
// row naming the consumer it would break, and the three items that get the same
// change through.
//
// The fake model here is a second one beside main_test.go's, and deliberately so:
// what these episodes need of the implementer is a contract file and a mirror, and
// keying those off the spec keeps M1's fake unchanged.
//
// None of these tests skips when the database is unreachable: the milestone is
// demonstrated by them running.
package main

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/declaration"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/pin"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// theHealthInterface is the name the producer's build gives what it publishes, and
// theLedgerStore the name it gives its own store. Both are the file name's, which is
// what says the kind as well.
const (
	theHealthInterface = "health"
	theLedgerStore     = "ledger"
)

// The statements of the episodes. The removal's is the detector's own, computed
// rather than written out, because a run given a statement works the intent already
// waiting with it — which is how a removal reaches the pipeline at all.
const (
	pairStatement    = "Publish a health interface on demo and read it from reader."
	breakStatement   = "Drop the Detail element from demo's health interface."
	addStatement     = "Add DetailText to demo's health interface and mark Detail deprecated."
	migrateStatement = "Move reader onto demo's DetailText element."
	storeStatement   = "Give demo a ledger store with an id."
	storeBreak       = "Add an always-populated amount to demo's ledger store."
	storeMigrate     = "Add an optional amount to demo's ledger store."
)

// removeStatement is what the detector asks for, derived from the contract and the
// element and from nothing else.
var removeStatement = contractcheck.RemovalStatement(theService, theHealthInterface, "Detail")

// approvals enough for any of these episodes. A row that auto-passes reads nothing
// and a surplus is never read, so the count is not worth predicting.
var manyApprovals = strings.Repeat("approve\n", 40)

// field is one element of a contract file or of a mirror, as the source states it.
type field struct {
	name string
	kind string
	// tag is the `borg` tag's words, comma separated, and empty for a field with
	// none — which is optional and unmarked on a producer, and a bare read on a
	// consumer.
	tag string
}

// shape is what one item of one episode authors: the spec the spec author delivers,
// the criterion it introduces, the files the implementer writes beside main.go and
// the encodings, and the value the program marshals into its exchange file.
//
// The exchange expression is empty for a service that publishes no interface, which
// is what a consumer is: a program that publishes nothing writes no document, and
// there is nothing for a declaration to be decided against.
type shape struct {
	spec      string
	criterion string
	files     []agent.File
	exchange  string
}

// theShapes is every episode's shape, keyed by the statement and the service the
// item changes. One intent's two items differ exactly here: the spec author is told
// which service it is authoring for, and an item names one service.
var theShapes = map[string]shape{
	pairStatement + "/" + theService: {
		spec:      "demo publishes the health interface with Status and Detail.",
		criterion: "When asked for its health, the system shall respond ok.",
		files: []agent.File{contractFile(theHealthInterface,
			field{"Status", "string", "populated"}, field{"Detail", "string", ""})},
		exchange: `Health{Status: "ok", Detail: "fine"}`,
	},
	pairStatement + "/" + theSecondService: {
		spec:      "reader reads Status and Detail of demo's health interface.",
		criterion: "When asked what it read, the system shall respond read.",
		files: mirrorFiles(theService, theHealthInterface,
			field{"Status", "string", "populated,domain=ok|error"}, field{"Detail", "string", ""}),
	},
	breakStatement + "/" + theService: {
		spec:      "demo publishes the health interface with Status alone.",
		criterion: "When asked for its trimmed health, the system shall respond trimmed.",
		files:     []agent.File{contractFile(theHealthInterface, field{"Status", "string", "populated"})},
		exchange:  `Health{Status: "ok"}`,
	},
	addStatement + "/" + theService: {
		spec:      "demo publishes the health interface with Status, Detail marked deprecated, and DetailText.",
		criterion: "When asked for its widened health, the system shall respond widened.",
		files: []agent.File{contractFile(theHealthInterface,
			field{"Status", "string", "populated"},
			field{"Detail", "string", "deprecated"},
			field{"DetailText", "string", ""})},
		exchange: `Health{Status: "ok", Detail: "fine", DetailText: "fine"}`,
	},
	migrateStatement + "/" + theSecondService: {
		spec:      "reader reads Status and DetailText of demo's health interface.",
		criterion: "When asked what it migrated onto, the system shall respond migrated.",
		files: mirrorFiles(theService, theHealthInterface,
			field{"Status", "string", "populated,domain=ok|error"}, field{"DetailText", "string", ""}),
	},
	removeStatement + "/" + theService: {
		spec:      "demo publishes the health interface with Status and DetailText.",
		criterion: "When asked for its cleaned health, the system shall respond cleaned.",
		files: []agent.File{contractFile(theHealthInterface,
			field{"Status", "string", "populated"}, field{"DetailText", "string", ""})},
		exchange: `Health{Status: "ok", DetailText: "fine"}`,
	},
	storeStatement + "/" + theService: {
		spec:      "demo has a ledger store with an always-populated ID.",
		criterion: "When asked for its ledger, the system shall respond ledger.",
		files:     []agent.File{storeFile(theLedgerStore, field{"ID", "string", "populated"})},
	},
	storeBreak + "/" + theService: {
		spec:      "demo has a ledger store with an always-populated ID and an always-populated Amount.",
		criterion: "When asked for its widened ledger, the system shall respond widened.",
		files: []agent.File{storeFile(theLedgerStore,
			field{"ID", "string", "populated"}, field{"Amount", "int64", "populated"})},
	},
	storeMigrate + "/" + theService: {
		spec:      "demo has a ledger store with an always-populated ID and an optional Amount.",
		criterion: "When asked for its optional ledger, the system shall respond optional.",
		files: []agent.File{storeFile(theLedgerStore,
			field{"ID", "string", "populated"}, field{"Amount", "int64", ""})},
	},
}

// contractFile is the one file a published interface is derived from: one exported
// struct type, in a file whose name says the interface and the kind.
func contractFile(name string, fields ...field) agent.File {
	return agent.File{Path: contract.FileName(contract.KindInterface, name), Content: structFile(fields)}
}

// storeFile is the same for a store, which is the same convention and the other
// prefix — and the whole of how the kind is decided.
func storeFile(name string, fields ...field) agent.File {
	return agent.File{Path: contract.FileName(contract.KindStore, name), Content: structFile(fields)}
}

// structFile is the source of a contract file or a mirror: one exported struct
// type, whatever it is called, with the `borg` tag on the fields that carry one.
func structFile(fields []field) string {
	var b strings.Builder
	b.WriteString("package main\n\n// Health is what this file declares.\ntype Health struct {\n")
	for _, f := range fields {
		if f.tag == "" {
			fmt.Fprintf(&b, "\t%s %s\n", f.name, f.kind)
			continue
		}
		fmt.Fprintf(&b, "\t%s %s `borg:%q`\n", f.name, f.kind, f.tag)
	}
	b.WriteString("}\n")
	return b.String()
}

// mirrorFiles is what a consumer's build carries: the mirror of the interface it
// reads, and the code that reads it. What the factory takes as declared is the
// mirror's fields the code actually selects, so both files are needed and a field in
// one without a read in the other declares nothing.
func mirrorFiles(producer, interfaceName string, fields ...field) []agent.File {
	var reads strings.Builder
	reads.WriteString("package main\n\n// read is what this service does with the interface it consumes.\nfunc read(h Health) string {\n\treturn \"\"")
	for _, f := range fields {
		fmt.Fprintf(&reads, " + fmt.Sprint(h.%s)", f.name)
	}
	reads.WriteString("\n}\n")
	return []agent.File{
		{Path: declaration.FileName(producer, interfaceName), Content: structFile(fields)},
		{Path: "reader.go", Content: "package main\n\nimport \"fmt\"\n" +
			strings.TrimPrefix(reads.String(), "package main\n")},
	}
}

// contractMainGo is the program every one of these fakes writes: the long-lived process that
// exercises itself, appends one line per unit of work to the file BORG_SIGNAL names,
// and — where it publishes an interface — one JSON document per unit to the file
// BORG_EXCHANGE names.
//
// The document is marshalled from the contract's own type, so its keys are the
// element names the derivation read out of the same source. Two spellings of one name
// is exactly what that avoids.
func contractMainGo(exchange string) []agent.File {
	body := []string{
		"package main",
		"",
		"import (",
		"\t\"os\"",
		"\t\"time\"",
		")",
		"",
		"func main() {",
		"\tsignal := os.Getenv(\"BORG_SIGNAL\")",
		"\tfor {",
		"\t\temit(signal, \"ok\\n\")",
		"\t\twriteExchange()",
		"\t\ttime.Sleep(time.Millisecond)",
		"\t}",
		"}",
		"",
		"func emit(path, line string) {",
		"\tif path == \"\" {",
		"\t\treturn",
		"\t}",
		"\tf, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)",
		"\tif err == nil {",
		"\t\t_, _ = f.WriteString(line)",
		"\t\t_ = f.Close()",
		"\t}",
		"}",
	}
	exchangeFile := []string{"package main", "", "func writeExchange() {}"}
	if exchange != "" {
		exchangeFile = []string{
			"package main",
			"",
			"import (",
			"\t\"encoding/json\"",
			"\t\"os\"",
			")",
			"",
			"// writeExchange appends one document per unit of work, marshalled from the",
			"// contract's own type so the keys are the element names.",
			"func writeExchange() {",
			"\tline, err := json.Marshal(" + exchange + ")",
			"\tif err != nil {",
			"\t\treturn",
			"\t}",
			"\temit(os.Getenv(\"BORG_EXCHANGE\"), string(line)+\"\\n\")",
			"}",
		}
	}
	return []agent.File{
		{Path: "go.mod", Content: "module demo\n\ngo 1.24\n"},
		{Path: "main.go", Content: strings.Join(body, "\n") + "\n"},
		{Path: "exchange.go", Content: strings.Join(exchangeFile, "\n") + "\n"},
	}
}

// theServiceOfPrompt reads which service the spec author is authoring for, which the
// prompt names because an item names one service and an intent may produce several.
var theServiceOfPrompt = regexp.MustCompile(`(?m)^The service this item changes: (.+)$`)

// contractModel is the fake model of these episodes: a spec author whose spec is
// keyed by the statement and the service, and an implementer whose files are keyed by
// the spec it was given. It asks no question — the interview is M1's demonstration and
// these episodes are about what follows the cut.
type contractModel struct{}

func (m *contractModel) Complete(_ context.Context, system, user string) (agent.Reply, error) {
	switch system {
	case agent.SpecAuthorSystemPrompt:
		named := theServiceOfPrompt.FindStringSubmatch(user)
		if named == nil {
			return agent.Reply{}, fmt.Errorf("fake model: the spec author's prompt names no service")
		}
		for key, s := range theShapes {
			statement, svc, _ := strings.Cut(key, "/"+named[1])
			if svc != "" || !strings.Contains(user, statement) {
				continue
			}
			return agent.Reply{Text: "SPEC:\n" + s.spec + "\nCRITERION: " + s.criterion, Tokens: 21}, nil
		}
		return agent.Reply{}, fmt.Errorf("fake model: no shape for service %s in this prompt", named[1])
	case agent.ImplementerSystemPrompt:
		for _, s := range theShapes {
			if !strings.Contains(user, "\n"+s.spec+"\n") {
				continue
			}
			text, err := contractReply(s, briefCriterion.FindAllStringSubmatch(user, -1))
			if err != nil {
				return agent.Reply{}, err
			}
			return agent.Reply{Text: text, Tokens: 41}, nil
		}
		return agent.Reply{}, fmt.Errorf("fake model: the implementer's prompt carries no spec this fake implements")
	}
	return agent.Reply{}, fmt.Errorf("fake model: the system prompt is neither role's")
}

// contractReply is the implementer's whole reply for one shape: the module, the
// program, the shape's own files, and one encoding per criterion the brief names —
// every criterion in force, because the check over the build rejects one that is not
// encoded.
func contractReply(s shape, named [][]string) (string, error) {
	if len(named) == 0 {
		return "", fmt.Errorf("fake model: the implementer's prompt names no criterion")
	}
	files := append(contractMainGo(s.exchange), s.files...)
	for _, match := range named {
		id, sentence := match[1], match[2]
		response := theResponse.FindStringSubmatch(sentence)
		if response == nil {
			return "", fmt.Errorf("fake model: the sentence of %s is not the response form this fake encodes: %q", id, sentence)
		}
		function := "respond_" + id
		files = append(files,
			agent.File{Path: function + ".go", Content: fmt.Sprintf(
				"package main\n\nfunc %s() string { return %q }\n", function, response[1])},
			agent.File{Path: function + "_test.go", Content: fmt.Sprintf(
				"package main\n\nimport \"testing\"\n\nfunc Test_%s(t *testing.T) {\n\tif %s() != %q {\n\t\tt.Fatal(%q)\n\t}\n}\n",
				id, function, response[1], function+" does not answer what the criterion requires")},
		)
	}
	var b strings.Builder
	for _, f := range files {
		fmt.Fprintf(&b, "=== FILE %s ===\n%s\n=== END ===\n", f.Path, f.Content)
	}
	return b.String(), nil
}

// newContractPath is the install these episodes run on: two services, the contract
// fake, and a script long enough for any row that asks.
func newContractPath(t *testing.T) (context.Context, deps, *bytes.Buffer) {
	t.Helper()
	ctx, d, out := newPathOn(t, manyApprovals, theService, theSecondService)
	d.model = &contractModel{}
	return ctx, d, out
}

// pair is the first episode: one intent cut into two items, one per service, the
// consumer's waiting on the producer's. It is a helper because four of these tests
// start from it.
func pair(t *testing.T, ctx context.Context, d deps, out *bytes.Buffer) shipped {
	t.Helper()
	res, err := run(ctx, d, []asked{across(pairStatement, theService, theSecondService)})
	if err != nil {
		t.Fatalf("the pair run stopped: %v\noutput:\n%s", err, out)
	}
	return res
}

// runOne is one episode: one intent on one service.
func runOne(t *testing.T, ctx context.Context, d deps, out *bytes.Buffer, statement, on string) shipped {
	t.Helper()
	d.in = strings.NewReader(manyApprovals)
	res, err := run(ctx, d, []asked{across(statement, on)})
	if err != nil {
		t.Fatalf("the run of %q stopped: %v\noutput:\n%s", statement, err, out)
	}
	return res
}

// TestOneIntentBecomesTwoItemsAndTheContractArrivesWithTheRelease is the first
// episode: Decomposition fires over a set for the first time, the producer's release
// publishes the contract at 1.0.0 inside the mint that gave it its number, the
// consumer's environment is composed from what the producer is running, and the
// consumer's release derives a declaration naming what its code reads.
func TestOneIntentBecomesTwoItemsAndTheContractArrivesWithTheRelease(t *testing.T) {
	ctx, d, out := newContractPath(t)
	res := pair(t, ctx, d, out)

	if len(res.cuts) != 1 || len(res.cuts[0].itemIDs) != 2 {
		t.Fatalf("the run cut %+v, want one intent and two items", res.cuts)
	}
	set := res.cuts[0]
	if !set.decided || !set.approved {
		t.Fatalf("the Decomposition row decided=%v approved=%v over a set of two", set.decided, set.approved)
	}
	if set.fired.opening == "" || set.fired.closing == "" {
		t.Fatalf("the Decomposition firing left %+v, want two rows", set.fired)
	}
	if !strings.Contains(out.String(), "the diff factors are unavailable here") {
		t.Errorf("the row does not say why its vector has holes in it:\n%s", out)
	}

	// One intent, two items, two services — and the intent is what joins them,
	// which is work that spans services needing no record type of its own.
	var producer, consumer *candidate
	for _, c := range res.candidates {
		if c.svc.Name == theService {
			producer = c
		}
		if c.svc.Name == theSecondService {
			consumer = c
		}
	}
	if producer == nil || consumer == nil {
		t.Fatalf("the run produced %d candidates and not one per service", len(res.candidates))
	}
	if producer.intentID != consumer.intentID {
		t.Fatalf("the two items name intents %s and %s, and one request produced both",
			producer.intentID, consumer.intentID)
	}
	if len(consumer.waitsOn) != 1 || consumer.waitsOn[0] != producer.itemID {
		t.Fatalf("the consumer's item waits on %v, want the producer's item %s", consumer.waitsOn, producer.itemID)
	}

	// The producer's release published the contract, and the queue wrote it inside
	// the mint that gave that release its number.
	if !producer.merged || producer.deployID == "" {
		t.Fatalf("the producer merged=%v deployed=%q", producer.merged, producer.deployID)
	}
	if len(producer.published) != 1 {
		t.Fatalf("the producer's release published %d contracts, want the one its build declares", len(producer.published))
	}
	published := producer.published[0]
	if !published.Created || !published.Moved || published.Version.Semver != contract.FirstVersion {
		t.Fatalf("the contract published as %+v, want a created contract at %s", published, contract.FirstVersion)
	}
	if published.Version.ReleaseID != producer.releaseID {
		t.Errorf("the version names release %s, and the release minted with it is %s",
			published.Version.ReleaseID, producer.releaseID)
	}
	if published.Version.ReleaseNumber != producer.releaseNumber {
		t.Errorf("the version carries number %d and the release is %d",
			published.Version.ReleaseNumber, producer.releaseNumber)
	}
	form, err := contract.FormOf(ctx, d.pool, published.Contract, published.Version.ID)
	if err != nil {
		t.Fatalf("FormOf: %v", err)
	}
	if len(form.Elements) != 2 {
		t.Fatalf("the form has %d elements: %+v", len(form.Elements), form.Elements)
	}
	status, _ := form.Element("Status")
	if !status.Populated {
		t.Error("Status is not always populated, and the source tags it populated")
	}

	// The consumer's environment was composed from the producer's current release,
	// which is the first composition any run in this repository has performed with
	// something in it.
	if len(consumer.composedFrom) != 1 || consumer.composedFrom[0].ReleaseID != producer.releaseID {
		t.Fatalf("the consumer's environment was composed from %+v, want the producer's release %s",
			consumer.composedFrom, producer.releaseID)
	}

	// And its release derived a declaration naming what its code reads.
	if consumer.declarationArtifactID == "" {
		t.Fatal("the consumer's build derived no declaration, and its code reads two of the producer's elements")
	}
	predicates, err := declaration.ForArtifact(ctx, d.pool, consumer.declarationArtifactID)
	if err != nil {
		t.Fatalf("ForArtifact: %v", err)
	}
	kinds := map[string]bool{}
	for _, p := range predicates {
		kinds[p.Element+"/"+string(p.Kind)] = true
		if p.ProducerServiceID != producer.svc.ID {
			t.Errorf("a predicate names producer %q, want the demo service's id", p.ProducerServiceID)
		}
	}
	for _, want := range []string{"Status/read", "Status/populated", "Status/domain", "Detail/read"} {
		if !kinds[want] {
			t.Errorf("%s was not derived; the declaration is %v", want, kinds)
		}
	}
	if kinds["Detail/populated"] {
		t.Error("Detail was declared populated, and the mirror tags it with nothing")
	}
}

// TestABreakingChangeIsRejectedAtTheMergeRowNamingTheConsumer is the milestone's own
// episode: a candidate that passes every criterion in force and removes an element a
// consumer declares, rejected before a verdict is asked for, with the consumer on the
// row.
func TestABreakingChangeIsRejectedAtTheMergeRowNamingTheConsumer(t *testing.T) {
	ctx, d, out := newContractPath(t)
	pair(t, ctx, d, out)

	res := runOne(t, ctx, d, out, breakStatement, theService)
	c := only(t, res)
	if c.merged {
		t.Fatalf("a candidate that removes an element the consumer declares merged:\n%s", out)
	}
	if !c.autoRejected || c.autoRejectedBy != gate.AutoRejectedByContractDiff {
		t.Fatalf("the candidate was rejected by %q (auto %v), want the producer's own contract diff",
			c.autoRejectedBy, c.autoRejected)
	}
	// Every criterion in force passed: the break is in no criterion's path, which
	// is the shape of defect a criterion cannot see.
	for _, result := range c.criteria {
		if result.Outcome.Blocks() {
			t.Fatalf("criterion %s is %s, and this episode is about a change the criteria cannot see",
				result.CriterionID, result.Outcome)
		}
	}
	if c.checked == nil || c.checked.Passed() {
		t.Fatal("enforcement reports the candidate as passing")
	}
	if !strings.Contains(c.checked.Why(), "Detail") {
		t.Errorf("the rejection does not name the element: %s", c.checked.Why())
	}
	consumer, found, err := service.ByName(ctx, d.pool, theSecondService)
	if err != nil || !found {
		t.Fatalf("reading the consumer: found %v, %v", found, err)
	}
	if !strings.Contains(c.checked.Why(), consumer.ID) {
		t.Errorf("the rejection does not name the consumer it would break: %s", c.checked.Why())
	}

	// The item went back to Implementation with an attempt counted there, which is
	// what a reject at this row does.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	if !strings.Contains(out.String(), "before a verdict was asked for") {
		t.Errorf("the run does not say the check rejected before a verdict:\n%s", out)
	}
}

// migrated is the three items of the migration, run one after another: the producer
// adds the new element beside the old and marks the old, the consumer migrates onto
// the new one, and the list on the old element empties.
func migrated(t *testing.T, ctx context.Context, d deps, out *bytes.Buffer) {
	t.Helper()
	pair(t, ctx, d, out)

	added := only(t, runOne(t, ctx, d, out, addStatement, theService))
	if !added.merged {
		t.Fatalf("the producer's addition did not merge:\n%s", out)
	}
	if len(added.published) != 1 || added.published[0].Version.Semver != (contract.Semver{Major: 1, Minor: 1}) {
		t.Fatalf("the addition published %+v, want 1.1.0 — an addition and a mark break nothing",
			added.published)
	}

	migrated := only(t, runOne(t, ctx, d, out, migrateStatement, theSecondService))
	if !migrated.merged || migrated.deployID == "" {
		t.Fatalf("the consumer's migration merged=%v deployed=%q:\n%s", migrated.merged, migrated.deployID, out)
	}
}

// TestTheThreeItemsOfAMigrationGetTheBreakingChangeThrough: the addition, the
// consumer's migration, and the removal — each shipping alone, and the removal
// passing the same check that rejected the second episode because the list emptied
// with nobody removing anything.
func TestTheThreeItemsOfAMigrationGetTheBreakingChangeThrough(t *testing.T) {
	ctx, d, out := newContractPath(t)
	migrated(t, ctx, d, out)

	// The detector raised the removal when the list emptied, which is the third
	// item's intent and nobody had to remember it.
	waiting, found, err := intent.Unrefined(ctx, d.pool, removeStatement)
	if err != nil {
		t.Fatalf("reading the detector's intent: %v", err)
	}
	if !found {
		t.Fatalf("the detector raised no removal intent after the list emptied:\n%s", out)
	}
	if waiting.Source != intent.SourceDetector {
		t.Errorf("the removal intent came from %s, want the detector", waiting.Source)
	}

	removed := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if removed.intentID != waiting.ID {
		t.Fatalf("the removal run took in intent %s, and the detector's is %s — a run given a statement works the intent waiting with it",
			removed.intentID, waiting.ID)
	}
	if !removed.merged {
		t.Fatalf("the removal did not merge after the list emptied:\n%s", out)
	}
	if len(removed.published) != 1 || removed.published[0].Version.Semver != (contract.Semver{Major: 2}) {
		t.Fatalf("the removal published %+v, want 2.0.0 — a removal is the major", removed.published)
	}
	if len(removed.published[0].Change.Removed) != 1 || removed.published[0].Change.Removed[0] != "Detail" {
		t.Errorf("the removal's diff removed %v", removed.published[0].Change.Removed)
	}
}

// TestAPinnedPredicateStopsTheRemovalUntilItIsWithdrawn: a pin never stops the item
// existing, only passing, and what a reader of that rejection needs is the pin and
// its author.
func TestAPinnedPredicateStopsTheRemovalUntilItIsWithdrawn(t *testing.T) {
	ctx, d, out := newContractPath(t)
	migrated(t, ctx, d, out)

	producer, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the producer: found %v, %v", found, err)
	}
	con, found, err := contract.ByName(ctx, d.pool, producer.ID, theHealthInterface)
	if err != nil || !found {
		t.Fatalf("reading the contract: found %v, %v", found, err)
	}
	owner := record.Actor{Kind: record.KindHuman, Name: d.human}
	placed, _, err := policy.NewFactory(d.pool).Pin(ctx, owner, gatepolicy.PinnedPredicate,
		pin.Subject{Kind: pin.SubjectContractElement, ID: contract.ElementSubject(con.ID, "Detail")},
		pin.Bound{Predicate: pin.Predicate{Kind: gatepolicy.PredicateRead}})
	if err != nil {
		t.Fatalf("pinning the predicate: %v", err)
	}

	blocked := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if blocked.merged {
		t.Fatalf("the removal merged with a pinned predicate naming the element:\n%s", out)
	}
	if blocked.autoRejectedBy != gate.AutoRejectedByPinnedPredicate {
		t.Fatalf("the removal was rejected by %q, want the pinned predicate", blocked.autoRejectedBy)
	}
	if !strings.Contains(blocked.checked.Why(), placed.ID) || !strings.Contains(blocked.checked.Why(), d.human) {
		t.Errorf("the rejection names neither the pin nor its author: %s", blocked.checked.Why())
	}
	// An attempt was counted at the stage the item went back to, which is what a
	// blocked removal costs: a full pass of the pipeline on work the factory raised
	// itself.
	stages, err := item.Stages(ctx, d.pool, blocked.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	attempts := 0
	for _, s := range stages {
		if s.Stage == item.StageImplementation {
			attempts = s.Attempts
		}
	}
	if attempts < 2 {
		t.Errorf("the implementation stage stands at %d attempts, and the rejection counts one there", attempts)
	}

	if _, err := policy.NewFactory(d.pool).WithdrawPin(ctx, owner, placed.ID); err != nil {
		t.Fatalf("withdrawing the pin: %v", err)
	}
	through := only(t, runOne(t, ctx, d, out, removeStatement, theService))
	if !through.merged {
		t.Fatalf("the removal is still refused after the pin was withdrawn:\n%s", out)
	}
}

// TestAStoresForwardPromiseRefusesAnAlwaysPopulatedColumn: the store is a contract
// too, its consumer is the service's own past, and that is the one break no list
// empties to allow.
func TestAStoresForwardPromiseRefusesAnAlwaysPopulatedColumn(t *testing.T) {
	ctx, d, out := newContractPath(t)

	first := only(t, runOne(t, ctx, d, out, storeStatement, theService))
	if !first.merged || len(first.published) != 1 {
		t.Fatalf("the store's first release published %+v:\n%s", first.published, out)
	}
	if first.published[0].Contract.Kind != contract.KindStore {
		t.Fatalf("the contract's kind is %q, and the file name says store", first.published[0].Contract.Kind)
	}
	if !first.published[0].Contract.Kind.Forward() {
		t.Fatal("a store's promise does not run forward, and the whole rollback rule rests on it")
	}

	broken := only(t, runOne(t, ctx, d, out, storeBreak, theService))
	if broken.merged {
		t.Fatalf("a store gained an always-populated column and merged:\n%s", out)
	}
	if broken.autoRejectedBy != gate.AutoRejectedByContractDiff {
		t.Fatalf("the addition was rejected by %q", broken.autoRejectedBy)
	}
	if !strings.Contains(broken.checked.Why(), "rollback restores") {
		t.Errorf("the rejection does not name the store's own consumer: %s", broken.checked.Why())
	}

	optional := only(t, runOne(t, ctx, d, out, storeMigrate, theService))
	if !optional.merged {
		t.Fatalf("the same column added optional is refused too:\n%s", out)
	}
	if len(optional.published) != 1 || optional.published[0].Version.Semver != (contract.Semver{Major: 1, Minor: 1}) {
		t.Fatalf("the optional addition published %+v, want 1.1.0", optional.published)
	}
}

// TestTheContractsQueryReadsTheWholeGraph: what a change breaks is a query rather
// than an estimate, and this is it read off the records in front of an owner.
func TestTheContractsQueryReadsTheWholeGraph(t *testing.T) {
	ctx, d, out := newContractPath(t)
	pair(t, ctx, d, out)

	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	services, err := service.All(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the services: %v", err)
	}
	printed := &bytes.Buffer{}
	p.d.out = printed
	if err := printContracts(ctx, p, services); err != nil {
		t.Fatalf("printContracts: %v", err)
	}
	for _, want := range []string{
		"contract health of demo (interface)",
		"it promises backward",
		"1.0.0 at release 1",
		"Status: string, always populated",
		"production runs release 1, which publishes 1.0.0",
		"restore floor release 1",
		"read on demo.health.Status",
	} {
		if !strings.Contains(printed.String(), want) {
			t.Errorf("the graph does not say %q:\n%s", want, printed)
		}
	}
}

// TestADecompositionRejectionSupersedesTheSetAndCountsARecut: the row can stop a bad
// cut and cannot repair one — the re-cut needs a cut that decides a decomposition
// rather than one told what to produce, and this interface is told.
func TestADecompositionRejectionSupersedesTheSetAndCountsARecut(t *testing.T) {
	ctx, d, out := newPathOn(t, "reject this should have been three items\n", theService, theSecondService)
	d.model = &contractModel{}

	res, err := run(ctx, d, []asked{across(pairStatement, theService, theSecondService)})
	if err != nil {
		t.Fatalf("the run stopped, and a rejected cut is the gate working: %v\noutput:\n%s", err, out)
	}
	if len(res.cuts) != 1 {
		t.Fatalf("the run cut %d sets", len(res.cuts))
	}
	set := res.cuts[0]
	if !set.decided || set.approved {
		t.Fatalf("the row decided=%v approved=%v, want a rejection", set.decided, set.approved)
	}
	if set.recuts != 1 {
		t.Fatalf("the intent stands at %d re-cuts, want the one this rejection counted", set.recuts)
	}

	// Every item of the set is superseded, and none of them reached a stage below
	// the cut: nothing was authored against a set the gate refused.
	for _, c := range res.candidates {
		if !c.superseded {
			t.Errorf("item %s survived the rejection", c.itemID)
		}
		if c.environmentID != "" || c.merged {
			t.Errorf("item %s reached an environment or a merge after the set was rejected", c.itemID)
		}
		it, err := item.Get(ctx, d.pool, c.itemID)
		if err != nil {
			t.Fatalf("reading item %s: %v", c.itemID, err)
		}
		if it.Stage != item.StageSuperseded {
			t.Errorf("item %s is at %s, want superseded", c.itemID, it.Stage)
		}
		if len(it.SupersededBy) != 0 {
			t.Errorf("item %s points at %v, and no re-cut replaced it", c.itemID, it.SupersededBy)
		}
	}
	// The count is on the intent and in a field of its own beside the interview's
	// rounds, because the two are different stretches of work.
	in, err := intent.Get(ctx, d.pool, res.candidates[0].intentID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if in.Recuts != 1 || in.Rounds != 0 {
		t.Errorf("the intent stands at %d re-cuts and %d rounds", in.Recuts, in.Rounds)
	}
	if !strings.Contains(out.String(), "the re-cut itself is not built") {
		t.Errorf("the run does not say what a rejected cut leaves:\n%s", out)
	}
}
