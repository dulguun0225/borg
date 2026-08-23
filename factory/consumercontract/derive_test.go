// The derivation and the deciding, which are the two parts of this package that
// reach no database: one reads a checkout and the other is arithmetic over a form
// and over observed documents. The record is tested in db_test.go.
package consumercontract_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// allowed is the list of allowed predicate kinds in force in these tests: the
// five kinds the factory owns, which is the unauthored value and what an owner
// extends.
var allowed = gatepolicy.AllowedPredicateKindNames()

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

const mirror = `package main

// Health is what this service reads of the producer's health interface.
type Health struct {
	Status         string ` + "`borg:\"populated,domain=ok|error\"`" + `
	SendTimeMillis int64  ` + "`borg:\"unit=millis\"`" + `
	Detail         string
	Unread         string ` + "`borg:\"populated\"`" + `
}
`

const reads = `package main

import "fmt"

func report(h Health) {
	fmt.Println(h.Status, h.SendTimeMillis, h.Detail)
}

func main() { report(Health{}) }
`

func TestDeriveDeclaresTheFieldsTheCodeReads(t *testing.T) {
	dir := checkout(t, map[string]string{
		consumercontract.FileName("producer", "health"): mirror,
		"main.go": reads,
	})
	drafts, err := consumercontract.Derive(dir, allowed)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	got := map[string]string{}
	for _, d := range drafts {
		if d.ProducerService != "producer" || d.Interface != "health" {
			t.Fatalf("a draft names %s.%s, want producer.health", d.ProducerService, d.Interface)
		}
		got[d.Element+"/"+string(d.Kind)] = d.Argument
	}
	for _, want := range []string{
		"Status/read", "Status/populated", "Status/domain",
		"SendTimeMillis/read", "SendTimeMillis/unit",
		"Detail/read",
	} {
		if _, found := got[want]; !found {
			t.Fatalf("%s was not derived; derived %v", want, got)
		}
	}
	if got["Status/domain"] != "ok|error" {
		t.Fatalf("the domain of Status is %q", got["Status/domain"])
	}
	if got["SendTimeMillis/unit"] != "millis" {
		t.Fatalf("the unit of SendTimeMillis is %q", got["SendTimeMillis/unit"])
	}
	// The field nothing reads declares nothing, however many tags it carries.
	// That is what makes a consumer which stops reading an element stop declaring
	// it with nobody remembering to.
	for key := range got {
		if len(key) > 6 && key[:6] == "Unread" {
			t.Fatalf("%s was derived, and nothing in the consumer reads that field", key)
		}
	}
}

func TestDeriveIsSilentAboutAReadItCannotSee(t *testing.T) {
	// The read is through a map and not a selector, which is one of the two blind
	// cases: an unprotected assumption, and what a safeguard's predicate is for.
	dir := checkout(t, map[string]string{
		consumercontract.FileName("producer", "health"): mirror,
		"main.go": `package main

func main() {
	by := map[string]string{"Status": "ok"}
	_ = by["Status"]
}
`,
	})
	drafts, err := consumercontract.Derive(dir, allowed)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	if len(drafts) != 0 {
		t.Fatalf("derived %v from a checkout whose only read is a map key", drafts)
	}
}

func TestDeriveRefusesAKindOutsideTheCatalog(t *testing.T) {
	dir := checkout(t, map[string]string{
		consumercontract.FileName("producer", "health"): mirror,
		"main.go": reads,
	})
	// A list an owner narrowed to the read predicate alone is not a state gate
	// policy can reach — a safeguard may only extend it — but it is what this
	// check is about: a consumer picks from the list and cannot invent a kind.
	_, err := consumercontract.Derive(dir, []string{string(gatepolicy.PredicateRead)})
	if !errors.Is(err, consumercontract.ErrNotAnAllowedPredicateKind) {
		t.Fatalf("deriving against a list of one kind returned %v, want ErrNotAnAllowedPredicateKind", err)
	}
}

func TestDeriveRefusesAMirrorItCannotRead(t *testing.T) {
	dir := checkout(t, map[string]string{
		consumercontract.FileName("producer", "health"): "package main\n\ntype A struct{}\ntype B struct{}\n",
	})
	if _, err := consumercontract.Derive(dir, allowed); !errors.Is(err, consumercontract.ErrDerivation) {
		t.Fatalf("a mirror with two exported struct types returned %v, want ErrDerivation", err)
	}
}

// Deciding one predicate.

func predicate(element string, kind gatepolicy.PredicateKind, argument string) consumercontract.Predicate {
	return consumercontract.Predicate{
		ItemID: "it_1", ServiceID: "svc_1", ArtifactID: "art_1",
		ProducerService: "producer", ProducerServiceID: "svc_2", Interface: "health",
		Element: element, Kind: kind, Argument: argument,
	}
}

var published = contract.Form{Name: "health", Kind: contract.KindInterface, Elements: []contract.Element{
	{Name: "Status", Type: "string", Populated: true},
	{Name: "SendTimeMillis", Type: "int64"},
}}

func TestAgainstFormDecidesThreeKindsAndLeavesTwo(t *testing.T) {
	for name, of := range map[string]struct {
		p       consumercontract.Predicate
		decided bool
		held    bool
	}{
		"a read of an element the form has":    {predicate("Status", gatepolicy.PredicateRead, ""), true, true},
		"a read of an element it does not":     {predicate("Gone", gatepolicy.PredicateRead, ""), true, false},
		"populated where the form says so":     {predicate("Status", gatepolicy.PredicatePopulated, ""), true, true},
		"populated where the form says not":    {predicate("SendTimeMillis", gatepolicy.PredicatePopulated, ""), true, false},
		"a unit the name carries":              {predicate("SendTimeMillis", gatepolicy.PredicateUnit, "millis"), true, true},
		"a unit the name does not":             {predicate("SendTimeMillis", gatepolicy.PredicateUnit, "seconds"), true, false},
		"a domain, which a form cannot answer": {predicate("Status", gatepolicy.PredicateDomain, "ok|error"), false, false},
		"a range, which a form cannot answer":  {predicate("SendTimeMillis", gatepolicy.PredicateRange, "0..10"), false, false},
	} {
		t.Run(name, func(t *testing.T) {
			result := of.p.AgainstForm(published)
			if result.Decided != of.decided || result.Held != of.held {
				t.Fatalf("%s decided %v held %v (%s), want decided %v held %v",
					name, result.Decided, result.Held, result.Why, of.decided, of.held)
			}
			if of.decided && !of.held && result.Why == "" {
				t.Fatal("a failure with no words is a rejection nobody can read")
			}
		})
	}
}

func TestAgainstExchangeDecidesEveryKind(t *testing.T) {
	good := []consumercontract.Document{
		{"Status": "ok", "SendTimeMillis": float64(3)},
		{"Status": "error", "SendTimeMillis": float64(7)},
	}
	for name, of := range map[string]struct {
		p         consumercontract.Predicate
		documents []consumercontract.Document
		held      bool
	}{
		"a read that is carried":      {predicate("Status", gatepolicy.PredicateRead, ""), good, true},
		"a read that is not":          {predicate("Gone", gatepolicy.PredicateRead, ""), good, false},
		"populated in every exchange": {predicate("Status", gatepolicy.PredicatePopulated, ""), good, true},
		"empty in one":                {predicate("Status", gatepolicy.PredicatePopulated, ""), []consumercontract.Document{{"Status": "ok"}, {"Status": ""}}, false},
		"inside its domain":           {predicate("Status", gatepolicy.PredicateDomain, "ok|error"), good, true},
		"outside its domain":          {predicate("Status", gatepolicy.PredicateDomain, "ok"), good, false},
		"inside its range":            {predicate("SendTimeMillis", gatepolicy.PredicateRange, "0..10"), good, true},
		"outside its range":           {predicate("SendTimeMillis", gatepolicy.PredicateRange, "0..5"), good, false},
		"a unit the name carries":     {predicate("SendTimeMillis", gatepolicy.PredicateUnit, "millis"), good, true},
		"no exchange at all":          {predicate("Status", gatepolicy.PredicateRead, ""), nil, false},
	} {
		t.Run(name, func(t *testing.T) {
			result := of.p.AgainstExchange(of.documents)
			if !result.Decided {
				t.Fatal("an observed exchange decides every kind, and this one decided nothing")
			}
			if result.Held != of.held {
				t.Fatalf("%s held %v (%s), want %v", name, result.Held, result.Why, of.held)
			}
		})
	}
}

func TestAPredicateRefusesAnArgumentOfTheWrongShape(t *testing.T) {
	for name, p := range map[string]consumercontract.Predicate{
		"a read carrying one":     predicate("Status", gatepolicy.PredicateRead, "millis"),
		"a unit carrying none":    predicate("Status", gatepolicy.PredicateUnit, ""),
		"a range that is one end": predicate("Status", gatepolicy.PredicateRange, "3"),
		"a range the wrong way":   predicate("Status", gatepolicy.PredicateRange, "9..1"),
		"a domain of nothing":     predicate("Status", gatepolicy.PredicateDomain, "||"),
	} {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); err == nil {
				t.Fatalf("%s validated, and nothing could decide it", name)
			}
		})
	}
}
