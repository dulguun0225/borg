// What a written element declares beside what a read one does, and which mirror
// a read or a write pairs with where a field name is shared: by the type of the
// value it is bound to, not by the field name alone. Reaches no database.
package consumercontract_test

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// populatedInputMirror is a mirror whose one input field the convention marks
// always populated, which a store's consumer's write side never derived.
const populatedInputMirror = `package main

type Ask struct {
	Reason string ` + "`borg:\"populated\"`" + `
}

func Fetch(ask Ask) string { return "" }
`

// TestAWrittenElementDeclaresWhetherItIsWrittenPopulated: a store's consumer
// contract is derived from writes as well as reads — the elements the code
// writes, and whether each is written populated, from the build like everything
// else, which does not need the element to be read too.
func TestAWrittenElementDeclaresWhetherItIsWrittenPopulated(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\n",
		consumercontract.FileName("health"): populatedInputMirror,
		"main.go": `package main

func report() {
	ask := Ask{Reason: "slow"}
	_ = Fetch(ask)
}

func main() { report() }
`,
	})
	d, got := derived(t, dir)
	if d.CouldNotDerive() {
		t.Fatalf("the derivation could not run: %s", d.Describe())
	}
	if _, found := got["Ask.Reason/populated"]; !found {
		t.Fatalf("Ask.Reason is written populated and that was not derived; derived %v", got)
	}
}

// TestAStoreElementBothReadAndWrittenAssertsPopulatedOnce: a store element read
// and written asserts populated on both sides of the same fact, and the record
// holds it once — the store's unique constraint on one version's assertions
// would otherwise refuse the second.
func TestAStoreElementBothReadAndWrittenAssertsPopulatedOnce(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "ledger self ledger store\n",
		consumercontract.FileName("ledger"): "package main\n\ntype Ledger struct {\n\tID string `borg:\"populated\"`\n}\n",
		"main.go": `package main

import "fmt"

func main() {
	row := Ledger{ID: "a"}
	fmt.Println(row.ID)
}
`,
	})
	d, _ := derived(t, dir)
	if d.CouldNotDerive() {
		t.Fatalf("the derivation could not run: %s", d.Describe())
	}
	count := 0
	for _, draft := range d.Drafts {
		if draft.Element == "Ledger.ID" && draft.Kind == gatepolicy.PredicatePopulated {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("Ledger.ID/populated was derived %d time(s), want exactly once", count)
	}
}

// orderMirror is a second producer's mirror declaring a field name — Status —
// that the health mirror also declares on an unrelated type.
const orderMirror = `package main

type Order struct {
	Status string
}
`

// TestAFieldNameSharedAcrossMirrorsIsNotConflated: the extractor pairs a read or
// a write with the mirror it is made through, by the type of the value it is
// bound to, and a field name two mirrors share is two elements and not one.
func TestAFieldNameSharedAcrossMirrorsIsNotConflated(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\norders producer2 orders\n",
		consumercontract.FileName("health"): mirror,
		consumercontract.FileName("orders"): orderMirror,
		"main.go": `package main

func report() {
	o := Order{Status: "shipped"}
	_ = o.Status
}

func main() { report() }
`,
	})
	d, got := derived(t, dir)
	if d.CouldNotDerive() {
		t.Fatalf("the derivation could not run: %s", d.Describe())
	}
	if _, found := got["Order.Status/read"]; !found {
		t.Fatalf("Order.Status was not derived as read; derived %v", got)
	}
	for key := range got {
		if strings.HasPrefix(key, "Health.") {
			t.Fatalf("%s was derived, and nothing in the consumer touches the health mirror at all: %v", key, got)
		}
	}
}

// TestAFieldReadThroughAFunctionParameterPairsWithItsOwnMirror: the pairing
// takes the receiver's type — here, a function parameter's — and not the field
// name alone.
func TestAFieldReadThroughAFunctionParameterPairsWithItsOwnMirror(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\norders producer2 orders\n",
		consumercontract.FileName("health"): mirror,
		consumercontract.FileName("orders"): orderMirror,
		"main.go": `package main

func report(o Order) {
	_ = o.Status
}

func main() {}
`,
	})
	d, got := derived(t, dir)
	if d.CouldNotDerive() {
		t.Fatalf("the derivation could not run: %s", d.Describe())
	}
	if _, found := got["Order.Status/read"]; !found {
		t.Fatalf("Order.Status was not derived as read through a function parameter; derived %v", got)
	}
	if _, found := got["Health.Status/read"]; found {
		t.Fatalf("Health.Status was derived, and nothing in the consumer touches the health mirror: %v", got)
	}
}
