// The attempt limit: a refused reply retried, a stage that runs out of
// attempts, and an error that is not a protocol failure and so is not retried.
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/principal"
)

// TestARefusedReplyIsRetried is the limit doing its work: the implementer's
// first reply is prose the protocol refuses, the second is a change, and the
// take ships. The item's implementation stage records two attempts — a refused
// reply sends the item back to the stage to be entered again, and the entry is
// what counts, which is what makes the limit the item's own count and not one
// run's — and the agentrun records name both calls the retry made.
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
	}

	runs, err := agentrun.ForItem(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the agent runs: %v", err)
	}
	var implementerCalls int
	for _, r := range runs {
		if r.Stage == string(item.StageImplementation) {
			implementerCalls++
		}
	}
	if implementerCalls != 2 {
		t.Errorf("the implementer was called %d times, want the refused one and the one that succeeded", implementerCalls)
	}
	// The spec author's call is the interview's and is recorded against the
	// intent, upstream of the item's first stage; the implementer's is the
	// item's own.
	if spendOnIntent(t, ctx, d, c.intentID) <= 0 {
		t.Error("the intent's interview spent nothing")
	}
	if spendOn(t, ctx, d, c.itemID, item.StageImplementation) <= 0 {
		t.Error("stage implementation spent nothing, and a refused attempt spent units too")
	}
}

// TestAStageOutOfAttemptsStops is the other end of the limit: every reply
// refused, so the factory stops retrying, escalates the item, and says so.
// Nothing ships, and the item carries the whole count — which is what an
// escalation is read from once there is a screen to read it on.
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
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Errorf("the error is %q, and a stage out of attempts is the factory saying it cannot do this one", err)
	}
	if model.callsMade != attemptLimit {
		t.Errorf("the implementer was called %d times, the limit is %d", model.callsMade, attemptLimit)
	}
	if len(res.candidates) != 0 {
		t.Errorf("the run reports %d candidates, a stage out of attempts finishes none", len(res.candidates))
	}

	// The item carries one attempt per entry, which is what the limit was
	// compared against: the stage was entered once and then again after each
	// refused reply, up to the limit, and the item is escalated at it.
	var itemID string
	var attempts int
	if err := d.pool.QueryRow(ctx, `select item_id, attempts from `+item.StageTable+` where stage = $1`,
		string(item.StageImplementation)).Scan(&itemID, &attempts); err != nil {
		t.Fatalf("reading the implementation stage's attempts: %v", err)
	}
	// One entry more than the limit allows: the item is entered again after
	// each refused reply, and the entry whose count exceeds the limit is the
	// one that escalates rather than authoring.
	if attempts != attemptLimit+1 {
		t.Errorf("the implementation stage records %d attempts, want the %d the limit allows and the entry that exceeded it",
			attempts, attemptLimit)
	}
	var stage string
	if err := d.pool.QueryRow(ctx, `select stage from `+item.Table+` where id = $1`, itemID).Scan(&stage); err != nil {
		t.Fatalf("reading the item's stage: %v", err)
	}
	if stage != string(item.StageEscalated) {
		t.Errorf("the item is at %s, and an item that exceeded the limit is escalated", stage)
	}
	if calls := spendCallsOn(t, ctx, d, itemID, item.StageImplementation); calls != attemptLimit {
		t.Errorf("the agentrun records name %d implementer calls, the limit spent %d", calls, attemptLimit)
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

func (m *erroringModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	if call.System == agent.ShippedImplementerPrompt {
		m.calls++
		return agent.Reply{}, errNotTheProtocol
	}
	return m.inner.Complete(ctx, as, call)
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
