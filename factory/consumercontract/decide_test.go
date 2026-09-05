// Deciding one predicate, which is arithmetic over a form and over observed
// documents and reaches no database.
package consumercontract_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

func predicate(element string, kind gatepolicy.PredicateKind, argument string) consumercontract.Predicate {
	return consumercontract.Predicate{
		ItemID: "it_1", ServiceID: "svc_1", ArtifactID: "art_1", Address: "health",
		ProducerService: "producer", ProducerServiceID: "svc_2", Interface: "health",
		Element: element, Kind: kind, Argument: argument,
	}
}

var published = contract.Form{Name: "health", Kind: contract.KindInterface, Elements: []contract.Element{
	{Name: "Health.Status", Kind: contract.ElementField, Position: contract.PositionOutput,
		Type: "string", Populated: true},
	{Name: "Health.SendTimeMillis", Kind: contract.ElementField, Position: contract.PositionOutput,
		Type: "int64"},
	{Name: "Fetch", Kind: contract.ElementOperation, Position: contract.PositionOutput, Type: "func(Ask)"},
	{Name: "Ask.Reason", Kind: contract.ElementField, Position: contract.PositionInput,
		Type: "string", Domain: []string{"slow", "error"}},
	{Name: "Ask.Deep", Kind: contract.ElementField, Position: contract.PositionInput, Type: "bool",
		Required: true},
	{Name: "Ask.Since", Kind: contract.ElementField, Position: contract.PositionInput, Type: "int64",
		Range: &contract.Range{Low: 0, High: 100}},
}}

// TestAgainstFormDecidesSevenKindsAndLeavesTwo: what a form says is what it
// publishes and what it accepts, so both sides are decidable against one. What a
// producer actually returns is not in its form, which is the two that are left.
func TestAgainstFormDecidesSevenKindsAndLeavesTwo(t *testing.T) {
	for name, of := range map[string]struct {
		p       consumercontract.Predicate
		decided bool
		held    bool
	}{
		"a read of an element the form has": {predicate("Health.Status", gatepolicy.PredicateRead, ""), true, true},
		"a read of an element it does not":  {predicate("Health.Gone", gatepolicy.PredicateRead, ""), true, false},
		"populated where the form says so":  {predicate("Health.Status", gatepolicy.PredicatePopulated, ""), true, true},
		"populated where the form says not": {predicate("Health.SendTimeMillis", gatepolicy.PredicatePopulated, ""), true, false},
		"a unit the name carries":           {predicate("Health.SendTimeMillis", gatepolicy.PredicateUnit, "millis"), true, true},
		"a unit the name does not":          {predicate("Health.SendTimeMillis", gatepolicy.PredicateUnit, "seconds"), true, false},
		"an operation the form offers":      {predicate("Fetch", gatepolicy.PredicateCalled, ""), true, true},
		"an operation it does not":          {predicate("Gone", gatepolicy.PredicateCalled, ""), true, false},
		"an element sent that it accepts":   {predicate("Ask.Reason", gatepolicy.PredicateSent, consumercontract.Sent), true, true},
		"an element left out that it requires": {predicate("Ask.Deep", gatepolicy.PredicateSent,
			consumercontract.LeftOut), true, false},
		"a value sent inside what it accepts": {predicate("Ask.Reason", gatepolicy.PredicateSentDomain, "slow"), true, true},
		"a value sent outside it":             {predicate("Ask.Reason", gatepolicy.PredicateSentDomain, "slow|gone"), true, false},
		"a range sent inside what it accepts": {predicate("Ask.Since", gatepolicy.PredicateSentRange, "0..10"), true, true},
		"a range sent outside it":             {predicate("Ask.Since", gatepolicy.PredicateSentRange, "0..900"), true, false},
		"a domain, which a form cannot answer": {predicate("Health.Status", gatepolicy.PredicateDomain, "ok|error"),
			false, false},
		"a range, which a form cannot answer": {predicate("Health.SendTimeMillis", gatepolicy.PredicateRange, "0..10"),
			false, false},
	} {
		t.Run(name, func(t *testing.T) {
			result := of.p.AgainstForm(published)
			if result.Decided != of.decided || result.Held != of.held {
				t.Fatalf("%s decided %v held %v (%s), want decided %v held %v",
					name, result.Decided, result.Held, result.Why, of.decided, of.held)
			}
			if !of.held && result.Why == "" {
				t.Fatal("an outcome that is not a pass with no words is a rejection nobody can read")
			}
		})
	}
}

func TestAgainstExchangeDecidesWhatARunCanShow(t *testing.T) {
	good := []consumercontract.Document{
		{"Health.Status": "ok", "Health.SendTimeMillis": float64(3)},
		{"Health.Status": "error", "Health.SendTimeMillis": float64(7)},
	}
	for name, of := range map[string]struct {
		p         consumercontract.Predicate
		documents []consumercontract.Document
		held      bool
	}{
		"a read that is carried":      {predicate("Health.Status", gatepolicy.PredicateRead, ""), good, true},
		"a read that is not":          {predicate("Health.Gone", gatepolicy.PredicateRead, ""), good, false},
		"populated in every exchange": {predicate("Health.Status", gatepolicy.PredicatePopulated, ""), good, true},
		"empty in one": {predicate("Health.Status", gatepolicy.PredicatePopulated, ""),
			[]consumercontract.Document{{"Health.Status": "ok"}, {"Health.Status": ""}}, false},
		"inside its domain":  {predicate("Health.Status", gatepolicy.PredicateDomain, "ok|error"), good, true},
		"outside its domain": {predicate("Health.Status", gatepolicy.PredicateDomain, "ok"), good, false},
		"inside its range":   {predicate("Health.SendTimeMillis", gatepolicy.PredicateRange, "0..10"), good, true},
		"outside its range":  {predicate("Health.SendTimeMillis", gatepolicy.PredicateRange, "0..5"), good, false},
		"a unit the name carries": {predicate("Health.SendTimeMillis", gatepolicy.PredicateUnit, "millis"),
			good, true},
		"an element written into the store": {predicate("Health.Status", gatepolicy.PredicateSent,
			consumercontract.Sent), good, true},
	} {
		t.Run(name, func(t *testing.T) {
			result := of.p.AgainstExchange(of.documents)
			if !result.Decided {
				t.Fatalf("%s decided nothing (%s), and the run carried the element", name, result.Why)
			}
			if result.Held != of.held {
				t.Fatalf("%s held %v (%s), want %v", name, result.Held, result.Why, of.held)
			}
		})
	}
}

// TestAPredicateDecidedAgainstNothingIsUndecided: a predicate decided against
// nothing passes every check the factory has and assures nothing, and the consumer
// whose path a run never exercised is exactly the consumer the check exists for.
// Undecided is read at the Merge to master gate the way a failure is.
func TestAPredicateDecidedAgainstNothingIsUndecided(t *testing.T) {
	for name, of := range map[string]struct {
		p         consumercontract.Predicate
		documents []consumercontract.Document
	}{
		"no exchange at all": {predicate("Health.Status", gatepolicy.PredicateRead, ""), nil},
		"a domain whose element no exchange carried": {
			predicate("Health.Detail", gatepolicy.PredicateDomain, "ok|error"),
			[]consumercontract.Document{{"Health.Status": "ok"}}},
		"a range whose element no exchange carried": {
			predicate("Health.Latency", gatepolicy.PredicateRange, "0..10"),
			[]consumercontract.Document{{"Health.Status": "ok"}}},
		"whether an operation is called": {predicate("Fetch", gatepolicy.PredicateCalled, ""),
			[]consumercontract.Document{{"Health.Status": "ok"}}},
	} {
		t.Run(name, func(t *testing.T) {
			result := of.p.AgainstExchange(of.documents)
			if result.Decided || result.Held {
				t.Fatalf("%s decided %v held %v, want undecided", name, result.Decided, result.Held)
			}
			if result.Why == "" {
				t.Fatal("an undecided predicate with no words is a stop nobody can read")
			}
		})
	}
}

func TestAPredicateRefusesAnArgumentOfTheWrongShape(t *testing.T) {
	for name, p := range map[string]consumercontract.Predicate{
		"a read carrying one":       predicate("Health.Status", gatepolicy.PredicateRead, "millis"),
		"a unit carrying none":      predicate("Health.Status", gatepolicy.PredicateUnit, ""),
		"a range that is one end":   predicate("Health.Status", gatepolicy.PredicateRange, "3"),
		"a range the wrong way":     predicate("Health.Status", gatepolicy.PredicateRange, "9..1"),
		"a domain of nothing":       predicate("Health.Status", gatepolicy.PredicateDomain, "||"),
		"a sent that is neither":    predicate("Ask.Reason", gatepolicy.PredicateSent, "maybe"),
		"a predicate with no entry": {ItemID: "it_1", ServiceID: "svc_1", ArtifactID: "art_1", ProducerService: "p", Interface: "health", Element: "Health.Status", Kind: gatepolicy.PredicateRead},
	} {
		t.Run(name, func(t *testing.T) {
			if err := p.Validate(); err == nil {
				t.Fatalf("%s validated, and nothing could decide it", name)
			}
		})
	}
}
