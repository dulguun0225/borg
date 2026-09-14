package deploy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// ProductionReaches is the deployer's production reach: the release's fleet,
// the selected rollback target's control and the kept fleet are assembled here
// from the deploy record or the target report. The selected share is copied
// from the score's pick by the composition; no production composition owns a
// rollout value.
// adoptionReleaseID names the adoption when the customer-running control has
// no factory release of its own.
func ProductionReaches(ctx context.Context, pool *pgxpool.Pool, p principal.Principal,
	credential secretref.Ref, serviceName, environmentID string, control Deploy, controlFound bool,
	adoptionReleaseID string, reaches []Reach, strategy Strategy, share float64) ([]Reach, Deploy, bool, error) {
	for n := range reaches {
		if reaches[n].ReleaseInstances == 0 {
			reaches[n].ReleaseInstances = 1
		}
	}
	if strategy != StrategyWithControl || len(reaches) == 0 {
		return reaches, Deploy{}, false, nil
	}
	addresses := make([]string, 0, len(reaches))
	for _, reach := range reaches {
		addresses = append(addresses, reach.Address)
	}
	counts := map[string]int{}
	if controlFound {
		targets, err := Targets(ctx, pool, control.ID)
		if err != nil {
			return nil, Deploy{}, false, err
		}
		for _, target := range targets {
			counts[target.Address] = target.Fleets.Release.Instances
		}
	} else if adoptionReleaseID != "" {
		var build string
		for _, reach := range reaches {
			running, err := reach.Target.ReadRunning(ctx, p, serviceName, credential)
			if err != nil {
				return nil, Deploy{}, false, fmt.Errorf("deploy: reading the adopted control on %s: %w", reach.Address, err)
			}
			if running.Build == "" || running.Instances <= 0 {
				return reaches, Deploy{}, false, nil
			}
			if build == "" {
				build = running.Build
			} else if build != running.Build {
				return reaches, Deploy{}, false, fmt.Errorf("deploy: adopted controls disagree on their build: %s and %s", build, running.Build)
			}
			counts[reach.Address] = running.Instances
		}
		control = Deploy{ReleaseID: adoptionReleaseID, BuildID: build}
		controlFound = build != ""
	}
	if !controlFound {
		return reaches, control, false, nil
	}
	for n := range reaches {
		instances := counts[reaches[n].Address]
		if instances == 0 {
			instances = 1
		}
		reaches[n].ReleaseInstances = 1
		reaches[n].ControlInstances = instances
		reaches[n].KeptInstances = instances
		reaches[n].Share = share
	}
	return reaches, control, true, nil
}
