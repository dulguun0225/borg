// Helpers shared by the contract tests: the fake spec author and
// implementer, the file builders their episodes derive from, and the
// paths the episodes run.
package main

import (
	"bytes"
	"context"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
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
var removeStatement = contractcheck.RemovalStatement(theService, theHealthInterface, "Health.Detail")

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
// there is nothing for a consumer contract to be decided against.
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
// type, whatever it is called, with the `borg` tag on the fields that carry one
// and a `json` tag on every one of them.
//
// The json tag is the element's own name, which is the message's name and the
// field's: a message contributes itself and one element per field, named
// Message.Field, so a document keyed by the field alone would be a document
// naming nothing any predicate asserts. The tag is what keeps the exchange the
// process writes and the form the derivation reads spelled the same way.
func structFile(fields []field) string {
	var b strings.Builder
	b.WriteString("package main\n\n// Health is what this file declares.\ntype Health struct {\n")
	for _, f := range fields {
		json := fmt.Sprintf("Health.%s", f.name)
		if f.tag == "" {
			fmt.Fprintf(&b, "\t%s %s `json:%q`\n", f.name, f.kind, json)
			continue
		}
		fmt.Fprintf(&b, "\t%s %s `borg:%q json:%q`\n", f.name, f.kind, f.tag, json)
	}
	b.WriteString("}\n")
	return b.String()
}

// mirrorAddress is the address a consumer reaches one producer's interface at.
// A call site does not say which producer it reaches — the address arrives
// through the service's configuration — so the address is the name the mirror
// file and the configuration file agree on, and it is derived here rather than
// chosen so that the two cannot drift apart.
func mirrorAddress(producer, interfaceName string) string {
	return producer + "-" + interfaceName
}

// mirrorFiles is what a consumer's build carries: the mirror of the interface it
// reads, the entry that says which producer that mirror's address reaches, and
// the code that reads it. What the factory takes as declared is the mirror's
// fields the code actually selects, so all three are needed — a field in one
// without a read in the other declares nothing, and a mirror whose address the
// configuration file does not hold could not be derived at all.
func mirrorFiles(producer, interfaceName string, fields ...field) []agent.File {
	var reads strings.Builder
	reads.WriteString("package main\n\n// read is what this service does with the interface it consumes.\nfunc read(h Health) string {\n\treturn \"\"")
	for _, f := range fields {
		fmt.Fprintf(&reads, " + fmt.Sprint(h.%s)", f.name)
	}
	reads.WriteString("\n}\n")
	address := mirrorAddress(producer, interfaceName)
	return []agent.File{
		{Path: consumercontract.FileName(address), Content: structFile(fields)},
		{Path: consumercontract.ConfigurationFile,
			Content: fmt.Sprintf("%s %s %s\n", address, producer, interfaceName)},
		{Path: "reader.go", Content: "package main\n\nimport \"fmt\"\n" +
			strings.TrimPrefix(reads.String(), "package main\n")},
	}
}

// contractMainGo is the program every one of these fakes writes: the long-lived process that
// exercises itself, appends one line per unit of work to the file BORG_SIGNAL names —
// the time the unit finished, a tab, and the outcome, which is the second emission
// version's shape — and, where it publishes an interface, one JSON document per unit
// to the file BORG_EXCHANGE names.
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
		"\t\temit(signal, time.Now().UTC().Format(time.RFC3339Nano)+\"\\tok\\n\")",
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
// these episodes are about what follows decomposition.
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
			text, err := contractReply(s, rolePromptCriterion.FindAllStringSubmatch(user, -1))
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
// program, the shape's own files, and one encoding per criterion the role prompt names —
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
				"package main\n\nimport \"testing\"\n\nfunc Test_%s_candidate_environment(t *testing.T) {\n\tif %s() != %q {\n\t\tt.Fatal(%q)\n\t}\n}\n",
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

// pair is the first episode: one intent decomposed into two items, one per service, the
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
