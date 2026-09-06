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
	if f.Row.Kind != KindImplementation && len(f.Screens.Screens) > 0 {
		return fmt.Errorf("%w: %s decides no build, so no screen was derived for it",
			ErrFiringIncomplete, f.Row)
	}
	if f.Row.DecidesARecord() && f.RecordID == "" {
		return fmt.Errorf("%w: %s names no record under decision", ErrFiringIncomplete, f.Row)
	}
	if !f.Row.DecidesARecord() && f.RecordID != "" {
		return fmt.Errorf("%w: %s decides no record and named %q", ErrFiringIncomplete, f.Row, f.RecordID)
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
// The five rows outside every item take no such read: they belong to no
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

// nothingPending refuses a firing on a row and a subject that already has an
// open event pending, so one gate on one subject has at most one pending row.
// The one exception is the open event Edit in place appends naming the row it
// supersedes, and the one a refer re-fires.
//
// The subject is the item at every row on an item's path, the version under
// decision at A role prompt or a skill, which names a version and no item at
// all, and the record itself at the four rows that decide one — so one pending
// withdrawal is the subject of its own row and not of every row of that kind.
func (g *Gate) nothingPending(ctx context.Context, f Firing) error {
	if f.supersedes != "" || f.referredFrom != "" {
		return nil
	}
	pending, err := g.Pending(ctx)
	if err != nil {
		return err
	}
	for _, open := range pending {
		if open.Gate != f.Row {
			continue
		}
		switch {
		case f.Row.DecidesAnItem():
			if open.Subject.ItemID != f.ItemID {
				continue
			}
		case f.Row.DecidesARecord():
			if open.Subject.RecordID != f.RecordID {
				continue
			}
		default:
			if open.ArtifactID != f.ArtifactID {
				continue
			}
		}
		return fmt.Errorf("%w: %s on %s is open as %s",
			ErrRowPending, f.Row, pendingSubjectOf(f), open.Row.ID)
	}
	return nil
}

// pendingSubjectOf is the subject the refusal above names, in the words a reader
// of the error reads: the item, the record under decision, or the version.
func pendingSubjectOf(f Firing) string {
	switch {
	case f.Row.DecidesAnItem():
		return f.ItemID
	case f.Row.DecidesARecord():
		return f.RecordID
	default:
		return f.ArtifactID
	}
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

// pendingReader is the principal the read event of [Gate.Pending] names: the
// gate component asking which of its own rows are still waiting.
var pendingReader = componentPrincipal(Row{Kind: KindDecomposition})

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

// authorOf is the actor that wrote the artifact version an open event names,
// which is what the refusal of a close by that version's own author compares
// against. It is the version's actor and not its author field: the actor is the
// per-person key of whoever wrote it, which is what the close event and the
// People declaration are both written with, where the author field is a model
// version or a person's name kept for the score's per-author prior. An open
// event naming no version has no author to refuse.
func (g *Gate) authorOf(ctx context.Context, artifactID string) (record.Actor, error) {
	if artifactID == "" {
		return record.Actor{}, nil
	}
	version, err := artifact.Get(ctx, g.pool, artifactID)
	if err != nil {
		return record.Actor{}, fmt.Errorf("gate: reading who wrote %s: %w", artifactID, err)
	}
	return version.Actor, nil
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
//
// A halt takes the two exceptions the exhausted error budget hold takes, a
// revert and an item the health monitor raised on that service, so the hold is
// not appended for either: a halt stops the factory acting forward and never
// stops it undoing what it did.
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
			excepted, err := g.passesAHalt(ctx, s.ItemID)
			if err != nil {
				return nil, err
			}
			if !excepted {
				standing = append(standing, HoldHalt)
			}
		}
	}
	return checkHolds(s.Row, standing)
}

// passesAHalt reports whether this item is one of the two a halt lets through: a
// revert, and an item the health monitor raised on that service. Both are read
// as one question of the item's intent, a revert being an item of the intent the
// health monitor raised at the rollback, which is the reading the merge queue's
// own stop makes.
//
// A gate composed with no reader of it excepts nothing, so every item holds
// while a halt stands, and the row outside every item — a halt's own withdrawal
// among them — names no item and reaches no read.
func (g *Gate) passesAHalt(ctx context.Context, itemID string) (bool, error) {
	if itemID == "" || g.raisedByTheHealthMonitor == nil {
		return false, nil
	}
	raised, err := g.raisedByTheHealthMonitor(ctx, itemID)
	if err != nil {
		return false, fmt.Errorf("gate: reading whether the health monitor raised the intent %s answers: %w",
			itemID, err)
	}
	return raised, nil
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

// rollout is what the score picks the strategy over besides the vector, read at
// the moment of firing like every other read a gate makes: whether every target
// of the environment serves a share, the strategy default an owner authored on
// production's environment record, whether a safeguard on that default keeps a
// control on this service, and whether the item's area is graded irreversible.
// It is read at a deploy to production row and at no other: a strategy decides
// whether a control runs, and a control is a comparison against organic traffic,
// which only production has. Whether the sample selected the item is set on the
// answer after the selection is made, the selection being read after the policy.
func (g *Gate) rollout(ctx context.Context, f Firing) (score.Rollout, error) {
	if f.Row.Kind != KindDeployToProduction {
		return score.Rollout{}, nil
	}
	env, err := environment.Get(ctx, g.pool, f.EnvironmentID)
	if err != nil {
		return score.Rollout{}, fmt.Errorf("gate: reading whether every target of %s serves a share: %w",
			f.EnvironmentID, err)
	}
	keepsAControl := false
	if g.strategySafeguard != nil {
		keepsAControl, err = g.strategySafeguard.KeepsAControl(ctx, f.ServiceID)
		if err != nil {
			return score.Rollout{}, fmt.Errorf("gate: reading whether a safeguard keeps a control on %s: %w",
				f.ServiceID, err)
		}
	}
	irreversible := false
	if f.AreaID != "" {
		grade, err := area.SeverityInForce(ctx, g.pool, f.AreaID)
		if err != nil {
			return score.Rollout{}, fmt.Errorf("gate: reading the hazard severity of %s: %w", f.AreaID, err)
		}
		irreversible = grade == area.GradeIrreversible
	}
	return score.Rollout{
		ReplacesReleaseID:       f.ReplacesReleaseID,
		EveryTargetServesAShare: env.EveryTargetServesAShare(),
		Irreversible:            irreversible,
		Default:                 score.Strategy(env.StrategyDefault),
		KeepsAControl:           keepsAControl,
	}, nil
}

// strategy is the rollout strategy this row picked, which is the score's pick in
// the words this package stores it in. It is picked at a deploy to production row
// and at no other, so every other row writes an empty pick.
func (g *Gate) strategy(f Firing, a score.Assessment, r score.Rollout) Pick {
	if f.Row.Kind != KindDeployToProduction {
		return Pick{}
	}
	return pickedBy(a, r)
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
