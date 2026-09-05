// Tests of the contracts query: what a change breaks, read off the
// records in front of an owner rather than estimated.
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/service"
)

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
		"last known-good release 1",
		"read on demo.health.Health.Status",
	} {
		if !strings.Contains(printed.String(), want) {
			t.Errorf("the graph does not say %q:\n%s", want, printed)
		}
	}
}
