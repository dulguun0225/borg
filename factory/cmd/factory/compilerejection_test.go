// Tests of a build that does not compile rejecting at the Implementation row on
// its own terms, before a verdict is asked for — the mechanical rejection
// [gate.AutoRejectedByCompile] names, which nothing called until this
// milestone. Before this, the implementation stage returned the error the
// build runner raised and the run stopped with no attempt counted and nothing
// retried; DEMO.md's "When it fails" table named it.
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
)

// noncompilingModel wraps a model and has the implementer's first reply add an
// import nothing in main.go uses — the exact defect a live run found: a build
// the compiler refuses outright, over a protocol reply that otherwise parses
// clean. Every call after the first passes through to the wrapped model
// untouched, so the implementer's second attempt compiles.
type noncompilingModel struct {
	inner agent.Model
	calls int
}

func (m *noncompilingModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, as, call)
	if err != nil || call.System != agent.ShippedImplementerPrompt {
		return reply, err
	}
	m.calls++
	if m.calls == 1 {
		reply.Text = strings.Replace(reply.Text, `"time"`, "\"fmt\"\n\t\"time\"", 1)
	}
	return reply, nil
}

// TestABuildThatDoesNotCompileIsRejectedAtImplementationBeforeAVerdict is the
// defect DEMO.md recorded: the implementer's first build carries an import
// nothing uses, so the build runner refuses it. It is caught here
// mechanically, with an attempt counted and the implementer building again,
// rather than stopping the run.
func TestABuildThatDoesNotCompileIsRejectedAtImplementationBeforeAVerdict(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &noncompilingModel{inner: &fakeModel{}}

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a build that does not compile is caught mechanically now: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	if !strings.Contains(out.String(), "does not compile") {
		t.Errorf("the run does not report the compile failure:\n%s", out)
	}

	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		if st.Stage == item.StageImplementation && st.Attempts != 2 {
			t.Errorf("implementation attempts = %d, want 2 — the rejected attempt and the one that compiled", st.Attempts)
		}
	}

	closing := firstImplementationClose(t, ctx, d, c.itemID)
	if closing.AutoRejectedBy != gate.AutoRejectedByCompile {
		t.Fatalf("the first Implementation close event's auto_rejected_by is %q, want %q",
			closing.AutoRejectedBy, gate.AutoRejectedByCompile)
	}
	if !strings.Contains(closing.Reason, "fmt") {
		t.Errorf("the close event's reason does not carry the compiler's words: %s", closing.Reason)
	}

	if !c.queued {
		t.Fatalf("the item did not reach the merge queue on its second, compiling build")
	}
}

// firstImplementationClose is the first close event at the Implementation row
// for one item, found by walking the log and pairing each close row with the
// opening it names. It is needed because the candidate's own [candidate.implementationGate]
// is overwritten by the second attempt's firing, and this test asserts over
// the first.
func firstImplementationClose(t *testing.T, ctx context.Context, d deps, itemID string) gate.ClosingPayload {
	t.Helper()
	rows := readLog(t, ctx, d)
	opened := map[string]decisionlog.Row{}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeDecision && row.Part == decisionlog.PartOpen {
			opened[row.ID] = row
		}
	}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartClose {
			continue
		}
		origin, found := opened[row.Closes]
		if !found {
			continue
		}
		opening := openingPayload(t, origin)
		if opening.Gate == gate.Implementation.String() && opening.ItemID == itemID {
			return closingPayload(t, row)
		}
	}
	t.Fatalf("the log holds no close event at the Implementation row for item %s", itemID)
	return gate.ClosingPayload{}
}
