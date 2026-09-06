// Tests of a store's forward promise: an always-populated column is
// refused because a rollback restores what a past release wrote.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/principal"
)

// retriedWithNoFix wraps a model and appends a distinguishing, otherwise inert
// file to every implementer reply, numbered by call — what makes a rebuild
// that fixes nothing still produce a new commit to build, the way a real
// model's prose would vary between attempts even where its defect does not.
// Nothing it adds touches the store's declared shape, so the forward promise
// the wrapped model's build breaks stays broken on every attempt, and
// [path.mergeUntilQueued] keeps rejecting it until the stage's own attempt
// limit is spent.
type retriedWithNoFix struct {
	inner agent.Model
	calls int
}

func (m *retriedWithNoFix) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, as, call)
	if err != nil || call.System != agent.ShippedImplementerPrompt {
		return reply, err
	}
	m.calls++
	reply.Text += fmt.Sprintf("\n=== FILE attempt_%d.go ===\npackage main\n\n// attempt %d, the same defect every time\n=== END ===\n",
		m.calls, m.calls)
	return reply, nil
}

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

	// A build with an always-populated column is not something a rebuild fixes:
	// [path.mergeUntilQueued] sends it back to Implementation and it comes back
	// with the same defect every time, so the Merge to master row rejects it on
	// the same terms until the stage's own attempt limit is spent and the
	// implementer's dispatch escalates — the way it already does for the
	// Implementation row's own loop, per [path.mergeUntilQueued]'s doc comment.
	d.in = strings.NewReader(manyApprovals)
	d.model = &retriedWithNoFix{inner: d.model}
	_, err := run(ctx, d, []asked{across(storeBreak, theService)})
	if err == nil {
		t.Fatalf("a store gained an always-populated column and the run finished without escalating:\n%s", out)
	}
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Errorf("the error is %v, want a stage out of attempts — every rebuild reproduces the same forward-promise defect", err)
	}
	if !strings.Contains(out.String(), gate.AutoRejectedByContractDiff) {
		t.Errorf("the run does not show the contract diff rejecting at the merge row:\n%s", out)
	}
	if !strings.Contains(out.String(), "rollback restores") {
		t.Errorf("the rejection does not name the store's own consumer:\n%s", out)
	}
	if !strings.Contains(out.String(), "goes back to implementation against what the Merge to master row found wrong") {
		t.Errorf("the run does not show the item being built again against what the row found wrong:\n%s", out)
	}

	d.model = &contractModel{}
	optional := only(t, runOne(t, ctx, d, out, storeMigrate, theService))
	if !optional.merged {
		t.Fatalf("the same column added optional is refused too:\n%s", out)
	}
	if len(optional.published) != 1 || optional.published[0].Version.Semver != (contract.Semver{Major: 1, Minor: 1}) {
		t.Fatalf("the optional addition published %+v, want 1.1.0", optional.published)
	}
}
