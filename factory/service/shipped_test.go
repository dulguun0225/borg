// shipped_test.go is the shipped defaults of the four fields the design
// fixes rather than gives a number for: an unauthored one reads the default,
// an authored one reads what was authored.
package service_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

// TestShippedDefaultsAppliedWhereUnauthored: each of the four fields the
// design fixes rather than gives a number for reads the shipped default where
// an owner authored none, and the authored value where they authored one.
func TestShippedDefaultsAppliedWhereUnauthored(t *testing.T) {
	unauthored := gatepolicy.Authored{}
	authored := gatepolicy.Authored{Number: 7, Present: true}

	cases := []struct {
		name    string
		inForce func(gatepolicy.Authored) float64
		shipped float64
	}{
		{"mutant cap", service.MutantCapInForce, service.ShippedMutantCap},
		{"failure-record key cap", service.FailureRecordKeyCapInForce, service.ShippedFailureRecordKeyCap},
		{"unreliable bound", service.UnreliableBoundInForce, service.ShippedUnreliableBound},
		{"incident-item bound", service.IncidentItemBoundSecondsInForce, service.ShippedIncidentItemBoundSeconds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.inForce(unauthored); got != c.shipped {
				t.Errorf("%s unauthored = %v, want the shipped default %v", c.name, got, c.shipped)
			}
			if got := c.inForce(authored); got != authored.Number {
				t.Errorf("%s authored = %v, want the authored value %v", c.name, got, authored.Number)
			}
		})
	}
}
