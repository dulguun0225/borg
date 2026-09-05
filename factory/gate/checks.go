package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// Everything [Gate.Fire] reads before it appends anything. Each of these runs at
// the moment of firing and at no other, which is the rule every check a gate
// makes keeps.

// complete refuses a firing missing something its row always has and one naming
// something its row never has. An artifact version is required at an artifact
// gate and refused everywhere else: there is no document under decision at an
// event gate, and one named there would say a version was decided over when
// nothing was.
//
// Decomposition is refused outright: it decides over a set and not over one
// item's build, so it is fired through [Gate.FireSet].
func complete(f Firing) error {
	if err := f.Row.Validate(); err != nil {
		return err
	}
	if f.Row.Kind == KindDecomposition {
		return fmt.Errorf("%w: %s decides over a set and is fired through FireSet",
			ErrFiringIncomplete, f.Row)
	}
	if err := checkDerivations(f.Row, f.CouldNotDerive); err != nil {
		return err
	}
	if f.Row.ArtifactGate() && f.ArtifactID == "" {
		return fmt.Errorf("%w: %s names no artifact version under decision", ErrFiringIncomplete, f.Row)
	}
	if !f.Row.ArtifactGate() && f.ArtifactID != "" {
		return fmt.Errorf("%w: %s names an artifact, and no document is under decision at an event gate",
			ErrFiringIncomplete, f.Row)
	}
	if !f.Row.DecidesAnItem() {
		if f.ItemID != "" {
			return fmt.Errorf("%w: %s belongs to no item and named %q", ErrFiringIncomplete, f.Row, f.ItemID)
		}
		return nil
	}
	required := []struct{ what, value string }{{"item", f.ItemID}, {"service", f.ServiceID}}
	if f.Row.Kind != KindSpec && f.Row.Kind != KindImplementationPlan && f.Row.Kind != KindTasks {
		required = append(required, struct{ what, value string }{"build", f.BuildID})
	}
	if f.Row.ReadsAThreshold() {
		required = append(required, struct{ what, value string }{"environment", f.EnvironmentID})
	}
	for _, r := range required {
		if r.value == "" {
			return fmt.Errorf("%w: %s names no %s", ErrFiringIncomplete, f.Row, r.what)
		}
	}
	return nil
}

// intentPermits refuses a firing on an item whose intent stops every component
// that could move it. No open event is appended, so nothing is decided, no
// attempt is counted, and the score learns nothing.
//
// The four rows outside every item take no such read: they belong to no
// timeline, so there is no intent to have a state.
func (g *Gate) intentPermits(ctx context.Context, f Firing) error {
	if !f.Row.DecidesAnItem() || f.ItemID == "" {
		return nil
	}
	if g.intentState == nil {
		return fmt.Errorf("%w: %s on %s", ErrIntentStateNotComposed, f.Row, f.ItemID)
	}
	state, err := g.intentState(ctx, f.ItemID)
	if err != nil {
		return fmt.Errorf("gate: reading the state of the intent %s was decomposed from: %w", f.ItemID, err)
	}
	if stops(state) {
		return fmt.Errorf("%w: %s is %s", ErrIntentStops, f.ItemID, state)
	}
	return nil
}

// nothingPending refuses a firing on a row and an item that already has an open
// event pending, so one gate on one item has at most one pending row. The one
// exception is the open event Edit in place appends naming the row it
// supersedes, and the one a refer re-fires.
func (g *Gate) nothingPending(ctx context.Context, f Firing) error {
	if f.supersedes != "" || f.referredFrom != "" {
		return nil
	}
	pending, err := g.Pending(ctx)
	if err != nil {
		return err
	}
	for _, open := range pending {
		if open.Gate == f.Row && open.Subject.ItemID == f.ItemID {
			return fmt.Errorf("%w: %s on %s is open as %s", ErrRowPending, f.Row, f.ItemID, open.Row.ID)
		}
	}
	return nil
}

// Pending is every decision this package wrote whose open event has neither a
// close event nor an abandonment. A row some other component wrote in a shape
// this package does not know is passed over rather than returned as an error,
// the way every other reader of this log treats one: a payload is unconstrained
// bytes by decisionlog's contract.
func (g *Gate) Pending(ctx context.Context) ([]Opened, error) {
	rows, err := decisionlog.NewReader(g.pool, g.token).Pending(ctx, pendingReader)
	if err != nil {
		return nil, err
	}
	var pending []Opened
	for _, row := range rows {
		opened, err := openedFrom(row)
		if err != nil {
			continue
		}
		pending = append(pending, opened)
	}
	return pending, nil
}

// pendingReader is the actor the read event of [Gate.Pending] names: the gate
// component asking which of its own rows are still waiting.
var pendingReader = Component(Row{Kind: KindDecomposition})

// artifactUnderDecision is the version under decision and its content digest,
// read from the artifact store at the firing. The identifier says which version
// was decided over and the digest says what it said, so the chain covers the
// text and not only the name.
func (g *Gate) artifactUnderDecision(ctx context.Context, f Firing) (id, digest string, author record.Actor, err error) {
	if f.ArtifactID == "" {
		return "", "", record.Actor{}, nil
	}
	version, err := artifact.Get(ctx, g.pool, f.ArtifactID)
	if err != nil {
		return "", "", record.Actor{}, fmt.Errorf("gate: reading the version under decision at %s: %w", f.Row, err)
	}
	return version.ID, version.ContentDigest, version.Actor, nil
}

// authorOf is the author named on the artifact version an open event names,
// which is what the refusal of a close by that version's own author compares
// against. An open event naming no version has no author to refuse.
func (g *Gate) authorOf(ctx context.Context, artifactID string) (string, error) {
	if artifactID == "" {
		return "", nil
	}
	version, err := artifact.Get(ctx, g.pool, artifactID)
	if err != nil {
		return "", fmt.Errorf("gate: reading the author of %s: %w", artifactID, err)
	}
	return version.Author, nil
}

// mismatch is the drift detector's own store, read at a deploy to production row
// and at no other: what it holds is a disagreement about what is running in
// production, and no other row decides a deploy into it. A mismatch puts a human
// here whatever the number reads, because nothing the factory can decide on the
// record is worth deciding while the record is the thing in doubt.
func (g *Gate) mismatch(ctx context.Context, f Firing) (string, error) {
	if f.Row.Kind != KindDeployToProduction {
		return "", nil
	}
	found, why, err := g.driftdetector.Mismatch(ctx, f.ServiceID)
	if err != nil {
		return "", fmt.Errorf("gate: reading the drift detector's store for %s: %w", f.ServiceID, err)
	}
	if !found {
		return "", nil
	}
	return why, nil
}

// standingHolds is every hold standing at this firing: what the composition
// computes, plus the halt, which this package reads itself because the refusal
// it carries is the gate's — no approve passes one.
func (g *Gate) standingHolds(ctx context.Context, s Subjects) ([]string, error) {
	if !s.Row.Deploys() {
		return nil, nil
	}
	standing, err := g.holds.Standing(ctx, s)
	if err != nil {
		return nil, fmt.Errorf("gate: recomputing the holds standing at %s: %w", s.Row, err)
	}
	if slices.Contains(HoldsAt(s.Row), HoldHalt) {
		halts, err := halt.Standing(ctx, g.pool)
		if err != nil {
			return nil, fmt.Errorf("gate: reading whether a halt stands: %w", err)
		}
		if len(halts) > 0 && !slices.Contains(standing, HoldHalt) {
			standing = append(standing, HoldHalt)
		}
	}
	return checkHolds(s.Row, standing)
}

// unmeasured is which of the deployer's four fields the service is missing, in
// words, and empty where it is missing none. A service missing one cannot
// auto-pass a deploy to production row whatever the score computes: the
// measurement those fields exist for cannot be read without them, so a human
// decides in their place, the same way a resolved factor already does.
func (g *Gate) unmeasured(ctx context.Context, f Firing) (string, error) {
	if f.Row.Kind != KindDeployToProduction || f.ServiceID == "" {
		return "", nil
	}
	s, err := service.Get(ctx, g.pool, f.ServiceID)
	if err != nil {
		return "", fmt.Errorf("gate: reading what the deployer found on %s: %w", f.ServiceID, err)
	}
	var missing []string
	for _, field := range []struct {
		found bool
		what  string
	}{
		{s.Reachability.TargetReached, "a target the deployer reaches"},
		{s.Reachability.InstancesReplaceable, "instances the platform can replace"},
		{s.Reachability.RollbackPathPresent, "a rollback path"},
		{s.Reachability.EmissionReadable, "an emission the health monitor can read"},
	} {
		if !field.found {
			missing = append(missing, field.what)
		}
	}
	if len(missing) == 0 {
		return "", nil
	}
	return "the service is missing " + join(missing), nil
}

// strategy is the rollout strategy this row picked. It is picked at a deploy to
// production row and at no other: a strategy decides whether a control runs, and
// a control is a comparison against organic traffic, which only production has.
func (g *Gate) strategy(ctx context.Context, f Firing, a score.Assessment, heldOut bool) (Pick, error) {
	if f.Row.Kind != KindDeployToProduction {
		return Pick{}, nil
	}
	env, err := environment.Get(ctx, g.pool, f.EnvironmentID)
	if err != nil {
		return Pick{}, fmt.Errorf("gate: reading whether every target of %s serves a share: %w", f.EnvironmentID, err)
	}
	irreversible := false
	if f.AreaID != "" {
		grade, err := area.SeverityInForce(ctx, g.pool, f.AreaID)
		if err != nil {
			return Pick{}, fmt.Errorf("gate: reading the hazard severity of %s: %w", f.AreaID, err)
		}
		irreversible = grade == area.GradeIrreversible
	}
	return pickStrategy(a, f.ReplacesReleaseID, env.EveryTargetServesAShare(), heldOut, irreversible)
}

// join reads a list of missing fields the way a sentence does.
func join(what []string) string {
	joined := ""
	for i, one := range what {
		switch {
		case i == 0:
			joined = one
		case i == len(what)-1:
			joined += " and " + one
		default:
			joined += ", " + one
		}
	}
	return joined
}

// referrersOf is every holder who has referred the decision one open event
// continues, read off that event rather than walked back through the chain.
func referrersOf(row decisionlog.Row) []string {
	var opening OpeningPayload
	if json.Unmarshal([]byte(row.Payload), &opening) != nil {
		return nil
	}
	return opening.Referrers
}
