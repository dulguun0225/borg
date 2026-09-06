// Tests of an implementation that changes nothing: the reply parses, applies,
// and leaves the checkout as it was, so there is nothing to build, and the
// stage tries again with that as what was found wrong rather than the run
// stopping on an empty commit.
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/principal"
)

// noChangeModel wraps a model and has the implementer's first reply remove a
// file the checkout does not hold — a reply inside the protocol that changes
// nothing, which is what a live run's implementer returned on its third try.
// Every call after the first passes through untouched.
type noChangeModel struct {
	inner agent.Model
	calls int
}

func (m *noChangeModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, as, call)
	if err != nil || call.System != agent.ShippedImplementerPrompt {
		return reply, err
	}
	m.calls++
	if m.calls == 1 {
		reply.Text = "=== DELETE nothing_here.go ===\n"
	}
	return reply, nil
}

// TestAnImplementationThatChangesNothingIsBuiltAgain: the first reply changes
// no file, the run says so and hands it back to the implementer as what was
// found wrong, and the second reply ships.
func TestAnImplementationThatChangesNothingIsBuiltAgain(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	model := &noChangeModel{inner: &fakeModel{}}
	d.model = model

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a reply that changes nothing is handed back now: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if !strings.Contains(out.String(), "changed no file") {
		t.Errorf("the run does not say the reply changed no file:\n%s", out)
	}
	if model.calls < 2 {
		t.Errorf("the implementer was called %d time(s), want a second attempt", model.calls)
	}
	if !c.merged {
		t.Errorf("the second attempt did not ship: merged %v", c.merged)
	}
}
