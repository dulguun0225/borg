// Tests of a store's forward promise: an always-populated column is
// refused because a rollback restores what a past release wrote.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gate"
)

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
