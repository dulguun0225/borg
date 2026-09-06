package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/score"
)

// Applied is what a gate firing applied: the policy version it was decided
// under, the threshold in force and where it came from, and whether a safeguard
// put a human at the row. It is written onto the open event, which is what
// makes a decision readable against the policy it was taken under rather than
// against today's.
type Applied struct {
	PolicyVersion string
	Threshold     float64
	ThresholdFrom Source
	// HumanBySafeguard is whether a safeguard adds a human at this row. A
	// safeguard on the risk threshold adds a human rather than moving the number,
	// so this is the whole of what such a safeguard does.
	HumanBySafeguard bool
	// Safeguards are the ids of the safeguards that applied, so a reader of the
	// decision can follow them to the records rather than being told a number
	// moved.
	Safeguards []string
	// Supplied is the score's own row behind the threshold where the score
	// supplied it, and is empty where an owner authored one. It is what a firing
	// prints beside the number so that an owner reading a gate can see which
	// outcomes moved the threshold it was compared against.
	Supplied score.Supplied
	// ScoreVersion is the score version in force at this row: the newest, where
	// nobody authored a threshold here, and the last one confirmed at this scope
	// where somebody did. The firing computes its vector under it and names it on
	// the open event, so that a version which redefined the number does not decide
	// a gate an authored threshold binds until its owner has confirmed it.
	ScoreVersion string
	// ScoreVersionWaiting is the newest score version where it is not the one in
	// force here, and is empty where the two are the same. It is what the screen
	// the threshold is authored from shows beside the threshold it waits on.
	ScoreVersionWaiting string
}

// RolePromptOrSkillRow is the name of the gate row whose threshold is authored
// on the factory-wide settings record rather than on an environment: that row
// belongs to no item, so it has no project and no production environment to
// read one from. It is package gate's own name for the row, declared here
// because the direction between the two packages is this one to that one, and
// gate's TestTheRowNameThePolicyKeysAThresholdBy is what holds the two
// spellings together.
const RolePromptOrSkillRow = "a_role_prompt_or_a_skill"

// AtGate is what applies at one gate firing: the threshold in force for the row
// and whether a safeguard adds a human. Both reads run at the moment of firing,
// which is what the design requires of every check a gate makes.
//
// p is who the firing is read as: naming the version in force is a read of the
// log, and the log appends a read event for every one.
func (r *Reader) AtGate(ctx context.Context, p principal.Principal, s Subjects) (Applied, error) {
	version, err := r.Newest(ctx, p)
	if err != nil {
		return Applied{}, err
	}

	authored, scope, err := r.authoredThreshold(ctx, s)
	if err != nil {
		return Applied{}, err
	}

	inForce, waiting, err := r.scoreVersionAt(ctx, authored.Present, scope)
	if err != nil {
		return Applied{}, err
	}
	supplied, _ := inForce.Value(gatepolicy.RiskThreshold, s.GateRow)

	safeguards, err := r.safeguardsOn(ctx, gatepolicy.RiskThreshold, s)
	if err != nil {
		return Applied{}, err
	}

	applied := Applied{
		PolicyVersion:       version.ID,
		Threshold:           authored.Or(supplied.Value),
		ThresholdFrom:       sourceOf(authored),
		ScoreVersion:        inForce.ID,
		ScoreVersionWaiting: waiting,
	}
	if !authored.Present {
		applied.Supplied = supplied
	}
	for _, p := range safeguards {
		applied.HumanBySafeguard = true
		applied.Safeguards = append(applied.Safeguards, p.ID)
	}
	return applied, nil
}

// authoredThreshold is what an owner authored for the risk threshold at one gate
// row, and the scope they authored it on. There are two scopes and no third: the
// environment record per row, which every row of an item's path reads, and the
// factory-wide settings record for [RolePromptOrSkillRow], the one row with no
// environment. A row the subjects name no record for reads as unauthored, which
// is the same answer as a field nobody wrote.
//
// The scope is answered beside the value because it is what the score version in
// force at the row is read at: a version that redefined the number does not
// decide a gate an authored threshold binds until its owner has confirmed it at
// that scope, and the confirmation names the scope the value was authored on.
func (r *Reader) authoredThreshold(ctx context.Context, s Subjects) (gatepolicy.Authored, Scope, error) {
	if s.GateRow == RolePromptOrSkillRow {
		settings, err := factorysettings.Get(ctx, r.pool)
		if err != nil {
			return gatepolicy.Authored{}, Scope{}, err
		}
		return settings.RolePromptOrSkillThreshold,
			Scope{Kind: ScopeFactorySettings, ID: settings.ID, Key: s.GateRow}, nil
	}
	if s.EnvironmentID == "" || s.GateRow == "" {
		return gatepolicy.Authored{}, Scope{}, nil
	}
	authored, err := environment.GateThreshold(ctx, r.pool, s.EnvironmentID, s.GateRow)
	return authored, Scope{Kind: ScopeEnvironment, ID: s.EnvironmentID, Key: s.GateRow}, err
}

// scoreVersionAt is the score version that decides one gate row, and the newest
// version where that is not the one in force there.
//
// Where nobody authored a threshold at the scope, the newest version is in force
// and the one this reader was composed with is it: every value one firing reads
// comes from the version its own decision row names. Where somebody did,
// [score.InForceAt] is the read — the newest version, where every version that
// redefined the number since has been confirmed at that scope, and the last one
// confirmed there otherwise.
func (r *Reader) scoreVersionAt(ctx context.Context, authored bool, scope Scope) (score.Version, string, error) {
	if !authored || scope.ID == "" || scope.Key == "" {
		return r.score, "", nil
	}
	inForce, found, err := score.InForceAt(ctx, r.pool, r.token, scope.String())
	if err != nil {
		return score.Version{}, "", fmt.Errorf("policy: reading the score version in force at %s: %w", scope, err)
	}
	if !found {
		return r.score, "", nil
	}
	waiting := ""
	if inForce.ID != r.score.ID {
		waiting = r.score.ID
	}
	return inForce, waiting, nil
}
