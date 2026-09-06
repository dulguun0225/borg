// The derivation, which is arithmetic over text rather than rows: it reaches no
// database. The diff is diff_test.go and the records are db_test.go.
package contract_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
)

// checkout puts files in a fresh directory and returns the directory.
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

// Health is what this service returns.
type Health struct {
	Status         string ` + "`borg:\"populated,domain=ok|error\"`" + `
	SendTimeMillis int64  ` + "`borg:\"populated\"`" + `
	Detail         string ` + "`borg:\"deprecated\"`" + `
	unexported     string
}

// Ask is what a caller sends.
type Ask struct {
	Deep  bool  ` + "`borg:\"required\"`" + `
	Since int64 ` + "`borg:\"range=0..100\"`" + `
}

// Fetch is the operation.
func Fetch(id string, ask Ask) Health { return Health{} }
`

func TestDeriveReadsEveryKindOfElementOutOfTheSource(t *testing.T) {
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
	want := []string{
		"Ask", "Ask.Deep", "Ask.Since", "Fetch", "Fetch.ask", "Fetch.id",
		"Health", "Health.Detail", "Health.SendTimeMillis", "Health.Status",
	}
	if !slices.Equal(names, want) {
		t.Fatalf("the elements are\n%v\nwant\n%v — sorted, the unexported field left out", names, want)
	}

	for _, e := range []struct {
		name     string
		kind     contract.ElementKind
		position contract.Position
	}{
		{"Fetch", contract.ElementOperation, contract.PositionOutput},
		{"Fetch.id", contract.ElementArgument, contract.PositionInput},
		{"Ask", contract.ElementMessage, contract.PositionInput},
		{"Ask.Deep", contract.ElementField, contract.PositionInput},
		{"Health", contract.ElementMessage, contract.PositionOutput},
		{"Health.Status", contract.ElementField, contract.PositionOutput},
	} {
		got, found := form.Element(e.name)
		if !found {
			t.Fatalf("the form has no element %s", e.name)
		}
		if got.Kind != e.kind || got.Position != e.position {
			t.Errorf("%s is a %q in position %q, want a %q in position %q",
				e.name, got.Kind, got.Position, e.kind, e.position)
		}
	}

	status, _ := form.Element("Health.Status")
	if status.Type != "string" || !status.Populated || status.Marked {
		t.Errorf("Health.Status is %+v, want a populated unmarked string", status)
	}
	if !slices.Equal(status.Domain, []string{"ok", "error"}) {
		t.Errorf("Health.Status accepts %v, want the tag's two names", status.Domain)
	}
	since, _ := form.Element("Ask.Since")
	if since.Range == nil || *since.Range != (contract.Range{Low: 0, High: 100}) {
		t.Errorf("Ask.Since accepts %v, want 0..100", since.Range)
	}
	deep, _ := form.Element("Ask.Deep")
	if !deep.Required {
		t.Error("Ask.Deep is not required, and the tag says it is")
	}
	if id, _ := form.Element("Fetch.id"); !id.Required || id.Type != "string" {
		t.Errorf("Fetch.id is %+v, want a required string — a caller has no way not to pass one", id)
	}
	if fetch, _ := form.Element("Fetch"); fetch.Type == "" {
		t.Error("the operation carries no type, so a signature redeclared would not read as a retype")
	}
	if marked := form.Marked(); !slices.Equal(marked, []string{"Health.Detail"}) {
		t.Fatalf("the marked elements are %v, want Health.Detail alone", marked)
	}
}

func TestAnOperationCarriesTheMarkInItsDocComment(t *testing.T) {
	dir := checkout(t, map[string]string{
		"contract.health.go": "package main\n\n//borg:deprecated\nfunc Old() {}\n\nfunc New() {}\n",
	})
	forms, err := contract.Derive(dir)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	if marked := forms[0].Marked(); !slices.Equal(marked, []string{"Old"}) {
		t.Fatalf("the marked elements are %v, want Old — Go has no tag on a func", marked)
	}
}

func TestDeriveReadsTheKindOffTheFileName(t *testing.T) {
	dir := checkout(t, map[string]string{
		"store.ledger.go": "package main\n\ntype Ledger struct {\n\tID string `borg:\"populated,notnull,unique\"`\n}\n",
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
	id, found := forms[0].Element("Ledger.ID")
	if !found {
		t.Fatal("the store's form has no element Ledger.ID")
	}
	if id.Position != contract.PositionStore {
		t.Errorf("Ledger.ID is in position %q, and a store is read and written by one service", id.Position)
	}
	if !id.NotNull || !id.Unique {
		t.Errorf("Ledger.ID is %+v, want the two constraints the tag states", id)
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
		"nothing exported":      "package main\n\ntype health struct{ A string }\n",
		"does not parse":        "package main\n\ntype A struct{",
		"an embedded field":     "package main\n\ntype A struct{ B }\ntype B struct{}\n",
		"an unnamed argument":   "package main\n\nfunc Fetch(string) {}\n",
		"a domain naming none":  "package main\n\ntype A struct{ X string `borg:\"domain=\"` }\n",
		"a range that is not a": "package main\n\ntype A struct{ X int `borg:\"range=high\"` }\n",
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

func TestTwoMessagesAreTwoSetsOfElements(t *testing.T) {
	dir := checkout(t, map[string]string{
		"contract.health.go": "package main\n\ntype A struct{ X string }\ntype B struct{ X string }\n",
	})
	forms, err := contract.Derive(dir)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	var names []string
	for _, e := range forms[0].Elements {
		names = append(names, e.Name)
	}
	if want := []string{"A", "A.X", "B", "B.X"}; !slices.Equal(names, want) {
		t.Fatalf("the elements are %v, want %v — a field is named by the message it is in", names, want)
	}
}
