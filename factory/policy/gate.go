package policy

import (
	"context"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
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
}

// AtGate is what applies at one gate firing: the threshold in force for the row
// and whether a safeguard adds a human. Both reads run at the moment of firing,
// which is what the design requires of every check a gate makes.
//
// principal is who the firing is read as: naming the version in force is a read
// of the log, and the log appends a read event for every one.
func (r *Reader) AtGate(ctx context.Context, principal record.Actor, s Subjects) (Applied, error) {
	version, err := r.Newest(ctx, principal)
	if err != nil {
		return Applied{}, err
	}

	authored := gatepolicy.Authored{}
	if s.EnvironmentID != "" && s.GateRow != "" {
		authored, err = environment.GateThreshold(ctx, r.pool, s.EnvironmentID, s.GateRow)
		if err != nil {
			return Applied{}, err
		}
	}
	supplied, _ := r.score.Value(gatepolicy.RiskThreshold, s.GateRow)

	safeguards, err := r.safeguardsOn(ctx, gatepolicy.RiskThreshold, s)
	if err != nil {
		return Applied{}, err
	}

	applied := Applied{
		PolicyVersion: version.ID,
		Threshold:     authored.Or(supplied.Value),
		ThresholdFrom: sourceOf(authored),
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
