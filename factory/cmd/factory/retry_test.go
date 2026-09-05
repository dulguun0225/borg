// The attempt limit: a refused reply retried, a stage that runs out of
// attempts, and an error that is not a protocol failure and so is not retried.
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/item"
)

// TestARefusedReplyIsRetried is the limit doing its work: the implementer's
// first reply is prose the protocol refuses, the second is a change, and the
// take ships — with the item's implementation stage recording both attempts,
// because the count the limit is compared against is the item's own.
func TestARefusedReplyIsRetried(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &refusingModel{inner: &fakeModel{}, refusals: 1}

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped, and one refused reply is inside the limit: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if c.deployID == "" {
		t.Fatal("the run shipped nothing after a retry that succeeded")
	}
	if !strings.Contains(out.String(), "reply was refused") {
		t.Errorf("the run does not report the refused reply:\n%s", out)
	}

	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		want := 1
		if st.Stage == item.StageImplementation {
			want = 2
		}
		if st.Attempts != want {
			t.Errorf("stage %s attempts = %d, want %d", st.Stage, st.Attempts, want)
		}
		if st.SpendTokens <= 0 {
			t.Errorf("stage %s spend = %d, a refused attempt spent tokens too", st.Stage, st.SpendTokens)
		}
	}
}

// TestAStageOutOfAttemptsStops is the other end of the limit: every reply
// refused, so the factory stops retrying and says it is stuck. Nothing ships,
// and the item carries the whole count — which is what an escalation is read
// from once there is a screen to read it on.
func TestAStageOutOfAttemptsStops(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	model := &refusingModel{inner: &fakeModel{}, refusals: attemptLimit + 5}
	d.model = model

	res, err := run(ctx, d, of(theStatement))
	if err == nil {
		t.Fatalf("the path finished, and every implementer reply was refused:\n%s", out)
	}
	if !errors.Is(err, agent.ErrReply) {
		t.Errorf("the error is %v, want the refused reply wrapped in it", err)
	}
	if !strings.Contains(err.Error(), "stuck on this item") {
		t.Errorf("the error is %q, and a stage out of attempts is the factory saying it cannot do this one", err)
	}
	if model.callsMade != attemptLimit {
		t.Errorf("the implementer was called %d times, the limit is %d", model.callsMade, attemptLimit)
	}
	if len(res.candidates) != 0 {
		t.Errorf("the run reports %d candidates, a stage out of attempts finishes none", len(res.candidates))
	}

	// The item exists and carries the whole count, the stage having reported each
	// attempt as it was made.
	var attempts int
	if err := d.pool.QueryRow(ctx, `select attempts from `+item.StageTable+` where stage = $1`,
		string(item.StageImplementation)).Scan(&attempts); err != nil {
		t.Fatalf("reading the implementation stage's attempts: %v", err)
	}
	if attempts != attemptLimit {
		t.Errorf("the implementation stage records %d attempts, the limit spent %d", attempts, attemptLimit)
	}
}

// erroringModel answers the implementer with an error that is not a protocol
// failure, standing for the rate-limited account the design holds rather than
// retries.
type erroringModel struct {
	inner agent.Model
	calls int
}

var errNotTheProtocol = errors.New("the model API answered 429")

func (m *erroringModel) Complete(ctx context.Context, system, user string) (agent.Reply, error) {
	if system == agent.ImplementerSystemPrompt {
		m.calls++
		return agent.Reply{}, errNotTheProtocol
	}
	return m.inner.Complete(ctx, system, user)
}

// TestAnErrorThatIsNotAProtocolFailureIsNotRetried is what the limit is not
// for: an account that refuses the call is a hold in the design and not an
// attempt at the work, so the first failure stops the run with its own error
// and the remaining attempts are never spent.
func TestAnErrorThatIsNotAProtocolFailureIsNotRetried(t *testing.T) {
	ctx, d, _ := newPath(t, theAnswer+"\n"+approvals)
	model := &erroringModel{inner: &fakeModel{}}
	d.model = model

	_, err := run(ctx, d, of(theStatement))
	if !errors.Is(err, errNotTheProtocol) {
		t.Fatalf("the path stopped with %v, want the model's own error", err)
	}
	if strings.Contains(err.Error(), "stuck on this item") {
		t.Errorf("the error is %q, and this failure is not the factory running out of attempts", err)
	}
	if model.calls != 1 {
		t.Errorf("the implementer was called %d times, an error the limit does not cover is not retried", model.calls)
	}
}
