// What the Decomposition row rejects on beside what the set answers, what its
// open event names of the set, when it fires over one item, and the count it is
// the intent's rather than an item's.
package gate_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
)

// TestASetWhoseDependenciesFormACycleIsRejected: two items each holding a
// deploy gate on the other is a wait nothing lifts and no instrument shows, so
// the order comes back as feedback rather than as a stage that could not write.
func TestASetWhoseDependenciesFormACycleIsRejected(t *testing.T) {
	if !slices.Contains(gate.DecompositionChecks, gate.AutoRejectedByACycle) {
		t.Fatal("the cycle is not one of the checks the Decomposition row rejects on")
	}

	acyclic := []gate.SetMember{
		{ItemID: "it_a", ServiceID: "svc_a"},
		{ItemID: "it_b", ServiceID: "svc_b", WaitsOn: []string{"it_a"}},
		{ItemID: "it_c", ServiceID: "svc_c", WaitsOn: []string{"it_a", "it_b"}},
	}
	if check, _, rejects := gate.SetCycleRejection(acyclic, nil); rejects {
		t.Errorf("an acyclic set rejected with %q", check)
	}

	cyclic := []gate.SetMember{
		{ItemID: "it_a", ServiceID: "svc_a", WaitsOn: []string{"it_c"}},
		{ItemID: "it_b", ServiceID: "svc_b", WaitsOn: []string{"it_a"}},
		{ItemID: "it_c", ServiceID: "svc_c", WaitsOn: []string{"it_b"}},
	}
	check, found, rejects := gate.SetCycleRejection(cyclic, nil)
	if !rejects || check != gate.AutoRejectedByACycle {
		t.Fatalf("a set whose dependencies form a cycle rejected %v by %q", rejects, check)
	}
	if found == "" {
		t.Error("the rejection names no edge, and what was decomposed wrong is read off the edge")
	}

	// An item waiting on itself is the shortest cycle there is.
	if _, _, rejects := gate.SetCycleRejection([]gate.SetMember{
		{ItemID: "it_a", ServiceID: "svc_a", WaitsOn: []string{"it_a"}},
	}, nil); !rejects {
		t.Error("an item waiting on itself passed")
	}

	// A hold naming a service no member touches, and one naming a member's own
	// service whose revert is not among the set's items, leave the verdict
	// unchanged: the set's own edges are what decides it either way.
	holds := []item.Hold{
		{ServiceID: "svc_unrelated", RevertIntentID: "in_0000000000000000000000000000000z"},
		{ServiceID: "svc_a", RevertIntentID: "in_0000000000000000000000000000000z"},
	}
	if check, _, rejects := gate.SetCycleRejection(acyclic, holds); rejects {
		t.Errorf("an acyclic set rejected with %q once holds are given", check)
	}
	if check, _, rejects := gate.SetCycleRejection(cyclic, holds); !rejects || check != gate.AutoRejectedByACycle {
		t.Fatalf("a cyclic set with holds given rejected %v by %q, want the same cycle caught with none", rejects, check)
	}

	// It rejects mechanically, the way the other set checks do: the row fires,
	// the vector exists, and the factory's own reject closes it.
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.2, "it_c": 0.2}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGate(t, s, p)
	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID: "in_0000000000000000000000000000000a", EnvironmentID: "env_000000000000000000000000000000a",
		Members: cyclic,
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	if _, err := g.AutoReject(ctx, opened, check, found); err != nil {
		t.Fatalf("AutoReject on the cycle: %v", err)
	}
}

// TestTheOpenEventNamesTheDerivedRequirementsOfTheSet: the derived requirements
// are part of the set the gate decides, beside what waits on what, so a human
// at the row reads the shares the split wrote and not only how many there are.
func TestTheOpenEventNamesTheDerivedRequirementsOfTheSet(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.2}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGate(t, s, p)

	opened, err := g.FireSet(ctx, gate.SetFiring{
		IntentID: "in_0000000000000000000000000000000a", EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1, DerivedRequirements: []string{"rq_share_a"}},
			{ItemID: "it_b", ServiceID: "svc_b", Requirements: 1, DerivedRequirements: []string{"rq_share_b"}},
		},
	})
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}
	var payload gate.SetOpeningPayload
	if err := json.Unmarshal([]byte(opened.Row.Payload), &payload); err != nil {
		t.Fatalf("reading the opening payload: %v", err)
	}
	if len(payload.Set) != 2 ||
		!slices.Equal(payload.Set[0].DerivedRequirements, []string{"rq_share_a"}) ||
		!slices.Equal(payload.Set[1].DerivedRequirements, []string{"rq_share_b"}) {
		t.Errorf("the open event names the set as %+v, want each member's derived requirements on it", payload.Set)
	}
}

// TestAReDecompositionOfOneItemStillFires: the one-item condition is the first
// decomposition's. A re-decomposition of work in progress always fires, because
// what it decides is which existing items are superseded rather than how many
// are proposed.
func TestAReDecompositionOfOneItemStillFires(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGate(t, s, p)

	first := gate.SetFiring{
		IntentID: "in_0000000000000000000000000000000a", EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1}},
	}
	if _, err := g.FireSet(ctx, first); !errors.Is(err, gate.ErrSetIncomplete) {
		t.Fatalf("a first decomposition of one item = %v, want ErrSetIncomplete", err)
	}

	again := first
	again.ReDecomposition = true
	opened, err := g.FireSet(ctx, again)
	if err != nil {
		t.Fatalf("a re-decomposition yielding one item: %v", err)
	}
	var payload gate.SetOpeningPayload
	if err := json.Unmarshal([]byte(opened.Row.Payload), &payload); err != nil {
		t.Fatalf("reading the opening payload: %v", err)
	}
	if !payload.ReDecomposition {
		t.Error("the open event does not say the firing was a re-decomposition")
	}

	// A set of no members is still refused: there is nothing to decide.
	empty := again
	empty.Members = nil
	if _, err := g.FireSet(ctx, empty); !errors.Is(err, gate.ErrSetIncomplete) {
		t.Errorf("a re-decomposition naming no item = %v, want ErrSetIncomplete", err)
	}
}

// TestTheDecompositionRowIsNotFiredOnAnIntentThatEnded: the intent's state is
// read before the firing here as it is at every other row. Dropped and
// escalated are ends, and a set decided over either would decide work nobody is
// going to do.
func TestTheDecompositionRowIsNotFiredOnAnIntentThatEnded(t *testing.T) {
	state := intent.StateDropped
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.2}}, &fakePolicy{applied: applied(0.5)}
	ctx, _, _, g := newGateWith(t, s, p, func(c *gate.Composition) {
		c.IntentState = func(context.Context, string) (intent.State, error) { return state, nil }
	})

	f := gate.SetFiring{
		IntentID: "in_0000000000000000000000000000000a", EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1},
			{ItemID: "it_b", ServiceID: "svc_b", Requirements: 1},
		},
	}
	if _, err := g.FireSet(ctx, f); !errors.Is(err, gate.ErrIntentStops) {
		t.Fatalf("FireSet over a dropped intent = %v, want ErrIntentStops", err)
	}
	state = intent.StateEscalated
	if _, err := g.FireSet(ctx, f); !errors.Is(err, gate.ErrIntentStops) {
		t.Fatalf("FireSet over an escalated intent = %v, want ErrIntentStops", err)
	}
	pending, err := g.Pending(ctx)
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("the refused firings left %d pending row(s), and they append nothing", len(pending))
	}

	// Re-decomposing is the state this firing is itself opened under, so it is
	// not one of the two: a row that refused it could never close the
	// re-decomposition it was opened for.
	state = intent.StateReDecomposing
	if _, err := g.FireSet(ctx, f); err != nil {
		t.Errorf("FireSet over a re-decomposing intent: %v", err)
	}
}

// TestDecompositionRoundsCountAgainstTheIntentsAttemptLimit: each round counts
// against the attempt limit on the intent rather than on an item — the items of
// a rejected round are superseded and their replacements start at nothing, so a
// count kept on them would never reach a limit however many rounds were spent.
func TestDecompositionRoundsCountAgainstTheIntentsAttemptLimit(t *testing.T) {
	s, p := &varyingScore{by: map[string]float64{"it_a": 0.2, "it_b": 0.2}}, &fakePolicy{applied: applied(0.5)}
	told := &tellings{}
	ctx, pool, token := schemaPool(t)
	intake := intent.NewIntake(pool, token, told)
	g := gate.New(gate.Composition{
		Pool: pool, Token: token, Log: decisionlog.NewWriter(pool, token),
		Score: s, Policy: p, IntentState: refined, Intake: intake,
	})

	intentID := confirmedIntent(t, ctx, intake)
	f := gate.SetFiring{
		IntentID: intentID, EnvironmentID: "env_000000000000000000000000000000a",
		Members: []gate.SetMember{
			{ItemID: "it_a", ServiceID: "svc_a", Requirements: 1},
			{ItemID: "it_b", ServiceID: "svc_b", Requirements: 1},
		},
	}
	opened, err := g.FireSet(ctx, f)
	if err != nil {
		t.Fatalf("FireSet: %v", err)
	}

	// One round spent against a limit of two: nothing is escalated and the
	// pending row is left alone.
	if _, err := intake.MarkReDecomposing(ctx, owner, intentID); err != nil {
		t.Fatalf("MarkReDecomposing: %v", err)
	}
	escalated, err := g.EnforceDecompositionRounds(ctx, owner, intentID, 2)
	if err != nil {
		t.Fatalf("EnforceDecompositionRounds: %v", err)
	}
	if escalated.Reached || escalated.Attempts != 1 || escalated.Limit != 2 {
		t.Fatalf("one round against a limit of two = %+v, want the count and the limit and no escalation", escalated)
	}

	// Three rounds against the same limit: the intent is escalated and the row
	// nobody is deciding is ended.
	if err := intake.ClearReDecomposing(ctx, owner, intentID); err != nil {
		t.Fatalf("ClearReDecomposing: %v", err)
	}
	if _, err := intake.MarkReDecomposing(ctx, owner, intentID); err != nil {
		t.Fatalf("MarkReDecomposing again: %v", err)
	}
	if err := intake.ClearReDecomposing(ctx, owner, intentID); err != nil {
		t.Fatalf("ClearReDecomposing again: %v", err)
	}
	if _, err := intake.MarkReDecomposing(ctx, owner, intentID); err != nil {
		t.Fatalf("MarkReDecomposing a third time: %v", err)
	}
	escalated, err = g.EnforceDecompositionRounds(ctx, owner, intentID, 2)
	if err != nil {
		t.Fatalf("EnforceDecompositionRounds over the limit: %v", err)
	}
	if !escalated.Reached || escalated.Attempts != 3 {
		t.Fatalf("three rounds against a limit of two = %+v, want the limit reached", escalated)
	}
	if len(escalated.Abandoned) != 1 || escalated.Abandoned[0].Closes != opened.Row.ID {
		t.Fatalf("the escalation abandoned %d row(s), want the intent's pending Decomposition row", len(escalated.Abandoned))
	}
	read, err := intent.Get(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateEscalated {
		t.Errorf("the intent is %s, want escalated", read.State)
	}
	if len(told.escalated) != 1 || told.escalated[0] != intentID {
		t.Errorf("a human was told about %v, want the escalated intent", told.escalated)
	}

	// A gate composed with no intake refuses the call rather than comparing a
	// count nothing can act on.
	bare := gate.New(gate.Composition{
		Pool: pool, Token: token, Log: decisionlog.NewWriter(pool, token),
		Score: s, Policy: p, IntentState: refined,
	})
	if _, err := bare.EnforceDecompositionRounds(ctx, owner, intentID, 2); !errors.Is(err, gate.ErrIntakeNotComposed) {
		t.Errorf("EnforceDecompositionRounds with no intake = %v, want ErrIntakeNotComposed", err)
	}
}

// tellings records the one call the escalation makes on intake's notifier.
type tellings struct{ escalated []string }

func (t *tellings) Interviewed(context.Context, string, string, string) error { return nil }
func (t *tellings) Escalated(_ context.Context, intentID string) error {
	t.escalated = append(t.escalated, intentID)
	return nil
}
func (t *tellings) AcceptanceRound(context.Context, string, string, string) error { return nil }

// confirmedIntent is an intent past its interview, which is the state a
// re-decomposition is opened from. It is written through intake's own calls
// rather than with a statement of its own: the counts this test compares are
// intake's writes and a test that set them directly would test nothing.
func confirmedIntent(t *testing.T, ctx context.Context, in *intent.Intake) string {
	t.Helper()
	taken, err := in.TakeIn(ctx, owner, intent.Arrival{
		Source: intent.SourceOwner, Statement: "checkout should retry a failed charge",
	})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if _, err := in.OpenRound(ctx, owner, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, owner, taken.ID, "Is this what you asked for?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if _, err := in.Confirm(ctx, owner, intent.Confirmation{
		IntentID:       taken.ID,
		QuestionID:     asked.ID,
		Answer:         "Yes.",
		IntendedEffect: "A shopper whose card fails once still completes the order.",
		Tier:           intent.Tier{Value: 2, PolicyVersion: testPolicyVersion},
		Requirements:   []intent.NewRequirement{{Statement: "The system shall retry a failed charge."}},
	}); err != nil {
		t.Fatalf("Confirm: %v", err)
	}
	return taken.ID
}
