package consumercontract_test

import (
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"testing"
)

func TestExchangePresenceAndPopulationAreDifferentAssertions(t *testing.T) {
	for name, test := range map[string]struct {
		kind      gatepolicy.PredicateKind
		argument  string
		documents []consumercontract.Document
		held      bool
	}{
		"left out":                 {gatepolicy.PredicateSent, consumercontract.LeftOut, []consumercontract.Document{{"Other": 1}}, true},
		"left out in only one":     {gatepolicy.PredicateSent, consumercontract.LeftOut, []consumercontract.Document{{}, {"Field": nil}}, false},
		"sent null":                {gatepolicy.PredicateSent, consumercontract.Sent, []consumercontract.Document{{"Field": nil}}, true},
		"sent empty":               {gatepolicy.PredicateSent, consumercontract.Sent, []consumercontract.Document{{"Field": ""}}, true},
		"sent missing in one":      {gatepolicy.PredicateSent, consumercontract.Sent, []consumercontract.Document{{"Field": "good"}, {}}, false},
		"populated missing in one": {gatepolicy.PredicatePopulated, "", []consumercontract.Document{{"Field": "good"}, {}}, false},
		"populated null":           {gatepolicy.PredicatePopulated, "", []consumercontract.Document{{"Field": nil}}, false},
	} {
		t.Run(name, func(t *testing.T) {
			got := predicate("Field", test.kind, test.argument).AgainstExchange(test.documents)
			if !got.Decided || got.Held != test.held {
				t.Fatalf("got %+v", got)
			}
		})
	}
}
