package main

import (
	"context"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
)

// criteriaTheBuildDecided is every result the build's own process wrote for
// one build; candidate-environment results are left to the merge row.
func (p *path) criteriaTheBuildDecided(ctx context.Context, buildID string) ([]gate.CriterionResult, error) {
	if buildID == "" {
		return nil, nil
	}
	latest, err := criterion.Latest(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	var decided []gate.CriterionResult
	for _, r := range latest {
		if r.Place == criterion.PlaceBuild {
			decided = append(decided, gate.CriterionResult{CriterionID: r.CriterionID, Outcome: r.Outcome, Place: r.Place})
		}
	}
	return decided, nil
}
