// The derivation and the diff, which are the two parts of this package that are
// arithmetic over text rather than rows: neither reaches a database, so both are
// tested here and the records are tested in db_test.go.
package contract_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
)

// write puts one file in a fresh directory and returns the directory.
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

const health = `package main

// Health is what this service publishes.
type Health struct {
	Status         string ` + "`borg:\"populated\"`" + `
	SendTimeMillis int64  ` + "`borg:\"populated\"`" + `
	Detail         string ` + "`borg:\"deprecated\"`" + `
	unexported     string
}
`

func TestDeriveReadsTheFormOutOfTheSource(t *testing.T) {
	dir := checkout(t, map[string]string{
		"contract.health.go": health,
		"main.go":            "package main\n\nfunc main() {}\n",
	})
	forms, err := contract.Derive(dir)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	if len(forms) != 1 {
		t.Fatalf("derived %d forms, want the one contract file's", len(forms))
	}
	form := forms[0]
	if form.Name != "health" || form.Kind != contract.KindInterface {
		t.Fatalf("the form is %q of kind %q, want health of kind interface", form.Name, form.Kind)
	}
	var names []string
	for _, e := range form.Elements {
		names = append(names, e.Name)
	}
	if want := []string{"Detail", "SendTimeMillis", "Status"}; !slices.Equal(names, want) {
		t.Fatalf("the elements are %v, want %v — sorted, and the unexported field left out", names, want)
	}
	status, _ := form.Element("Status")
	if status.Type != "string" || !status.Populated || status.Deprecated {
		t.Fatalf("Status is %+v, want a populated unmarked string", status)
	}
	sendTime, _ := form.Element("SendTimeMillis")
	if sendTime.Type != "int64" {
		t.Fatalf("SendTimeMillis is a %q, want the source's own int64", sendTime.Type)
	}
	detail, _ := form.Element("Detail")
	if detail.Populated || !detail.Deprecated {
		t.Fatalf("Detail is %+v, want optional and marked deprecated", detail)
	}
	if marked := form.Marked(); !slices.Equal(marked, []string{"Detail"}) {
		t.Fatalf("the marked elements are %v, want Detail alone", marked)
	}
}

func TestDeriveReadsTheKindOffTheFileName(t *testing.T) {
	dir := checkout(t, map[string]string{
		"store.ledger.go": "package main\n\ntype Ledger struct {\n\tID string `borg:\"populated\"`\n}\n",
	})
	forms, err := contract.Derive(dir)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	if len(forms) != 1 || forms[0].Kind != contract.KindStore || forms[0].Name != "ledger" {
		t.Fatalf("derived %+v, want one store named ledger", forms)
	}
	if !forms[0].Kind.Forward() {
		t.Fatal("a store's promise does not run forward, and the whole rollback rule rests on it")
	}
}

func TestDeriveIgnoresEveryFileThatIsNotOne(t *testing.T) {
	dir := checkout(t, map[string]string{
		"main.go":                 "package main\n\nfunc main() {}\n",
		"contract.health_test.go": "package main\n",
		"contract..go":            "package main\n",
		"notes.md":                "not go at all",
	})
	forms, err := contract.Derive(dir)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	if len(forms) != 0 {
		t.Fatalf("derived %+v, want none — no file here is a contract file", forms)
	}
}

func TestDeriveRefusesAContractFileItCannotRead(t *testing.T) {
	for name, content := range map[string]string{
		"no exported struct": "package main\n\ntype health struct{ A string }\n",
		"two of them":        "package main\n\ntype A struct{ X string }\ntype B struct{ Y string }\n",
		"does not parse":     "package main\n\ntype A struct{",
		"an embedded field":  "package main\n\ntype A struct{ B }\ntype B struct{}\n",
	} {
		t.Run(name, func(t *testing.T) {
			dir := checkout(t, map[string]string{"contract.health.go": content})
			_, err := contract.Derive(dir)
			if !errors.Is(err, contract.ErrDerivation) {
				t.Fatalf("deriving %s returned %v, want ErrDerivation — a build that names a contract file meant to publish one",
					name, err)
			}
		})
	}
}

// A form and its versions.

func TestDiffBreaksOnThreeThingsForAnInterface(t *testing.T) {
	before := form(contract.KindInterface,
		element("Status", "string", true, false),
		element("Detail", "string", false, false),
		element("Count", "int64", false, false),
	)
	for name, after := range map[string]contract.Form{
		"an element removed": form(contract.KindInterface,
			element("Status", "string", true, false),
			element("Count", "int64", false, false)),
		"a type changed": form(contract.KindInterface,
			element("Status", "string", true, false),
			element("Detail", "string", false, false),
			element("Count", "int32", false, false)),
		"a populated element weakened": form(contract.KindInterface,
			element("Status", "string", false, false),
			element("Detail", "string", false, false),
			element("Count", "int64", false, false)),
	} {
		t.Run(name, func(t *testing.T) {
			change := contract.Diff(before, after)
			if !change.Moved() {
				t.Fatal("the form moved and the diff says it did not")
			}
			if len(change.Breaking) == 0 {
				t.Fatalf("%s is not breaking, and every consumer reading that element does: %s",
					name, change.Describe())
			}
		})
	}
}

func TestDiffBreaksOnNothingAnInterfaceMayDo(t *testing.T) {
	before := form(contract.KindInterface, element("Status", "string", true, false))
	after := form(contract.KindInterface,
		element("Status", "string", true, true),
		element("Detail", "string", false, false),
		element("Count", "int64", true, false),
	)
	change := contract.Diff(before, after)
	if len(change.Breaking) != 0 {
		t.Fatalf("adding elements and marking one breaks %v, and neither breaks a consumer", change.Breaking)
	}
	if !slices.Equal(change.Added, []string{"Count", "Detail"}) {
		t.Fatalf("added %v, want Count and Detail", change.Added)
	}
	if !slices.Equal(change.Marked, []string{"Status"}) {
		t.Fatalf("marked %v, want Status", change.Marked)
	}
	if next := contract.FirstVersion.Next(len(change.Breaking) > 0); next != (contract.Semver{Major: 1, Minor: 1}) {
		t.Fatalf("the version moves to %s, want 1.1.0 — a mark and an addition are a minor", next)
	}
}

func TestDiffBreaksAStoreOnAPopulatedAddition(t *testing.T) {
	before := form(contract.KindStore, element("ID", "string", true, false))
	after := form(contract.KindStore,
		element("ID", "string", true, false),
		element("Amount", "int64", true, false),
	)
	change := contract.Diff(before, after)
	if !slices.Equal(change.Breaking, []string{"Amount"}) {
		t.Fatalf("breaking is %v, want Amount — a store promises forward, and the build being restored does not write it",
			change.Breaking)
	}
	// The same addition on an interface breaks nothing, which is the whole
	// difference between the two promises.
	asInterface := contract.Diff(form(contract.KindInterface, element("ID", "string", true, false)),
		form(contract.KindInterface,
			element("ID", "string", true, false),
			element("Amount", "int64", true, false)))
	if len(asInterface.Breaking) != 0 {
		t.Fatalf("the same addition breaks %v on a published interface, and a consumer reads what it reads",
			asInterface.Breaking)
	}
}

func TestTheFirstFormBreaksNothing(t *testing.T) {
	after := form(contract.KindStore,
		element("ID", "string", true, false),
		element("Amount", "int64", true, false),
	)
	change := contract.Diff(contract.Form{}, after)
	if len(change.Breaking) != 0 {
		t.Fatalf("a contract's first form breaks %v, and there is no earlier build to break", change.Breaking)
	}
	if !change.Moved() {
		t.Fatal("a first form did not move, and the release that publishes it mints a version")
	}
}

func TestAVersionMovesOnlyWhereTheFormDoes(t *testing.T) {
	one := form(contract.KindInterface, element("Status", "string", true, false))
	// The same elements in the other order are the same form: the order is not
	// part of the identity, so two builds that declare them differently publish
	// one form.
	same := contract.Form{Name: "health", Kind: contract.KindInterface, Elements: []contract.Element{
		element("Status", "string", true, false),
	}}
	if contract.Diff(one, same).Moved() {
		t.Fatal("an unchanged form moved, so every release would mint a version")
	}
	if got := contract.Diff(one, same).Describe(); got != "the form is unchanged" {
		t.Fatalf("an unchanged form describes itself as %q", got)
	}
}

func TestSemverRoundTripsAndThePatchNeverMoves(t *testing.T) {
	v := contract.FirstVersion
	for range 3 {
		v = v.Next(false)
	}
	v = v.Next(true)
	if v != (contract.Semver{Major: 2}) {
		t.Fatalf("three minors and a major from 1.0.0 is %s, want 2.0.0", v)
	}
	parsed, err := contract.ParseSemver(v.String())
	if err != nil || parsed != v {
		t.Fatalf("parsing %s gave %s, %v", v, parsed, err)
	}
	if v.Patch != 0 {
		t.Fatal("the patch moved, and nothing in a form is a patch-level change")
	}
}

func TestDDLListsEveryKind(t *testing.T) {
	joined := ""
	for _, statement := range contract.DDL {
		joined += statement
	}
	for _, kind := range contract.Kinds {
		if !strings.Contains(joined, "'"+string(kind)+"'") {
			t.Fatalf("the schema does not list kind %q, so the store would admit a contract this package refuses", kind)
		}
	}
}

func TestElementSubjectIsStableAcrossVersions(t *testing.T) {
	if got := contract.ElementSubject("con_abc", "Status"); got != "con_abc.Status" {
		t.Fatalf("the subject of an element is %q", got)
	}
}
