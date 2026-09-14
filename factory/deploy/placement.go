package deploy

import (
	"context"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/targetseam"
)

type controlledTarget interface {
	DeployWithControl(context.Context, principal.Principal, targetseam.Deployment) (targetseam.Placement, error)
}

func place(ctx context.Context, p Performance, reach Reach, d targetseam.Deployment) (targetseam.Placement, error) {
	if p.IntoProduction && p.StrategyPicked == StrategyWithControl && reach.ServesAShare {
		if target, ok := reach.Target.(controlledTarget); ok {
			return target.DeployWithControl(ctx, p.Principal, d)
		}
	}
	return reach.Target.Deploy(ctx, p.Principal, d)
}
