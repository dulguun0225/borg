package main

import (
	"context"

	"github.com/dulguun0225/borg/factory/deploy"
)

// The removal a retirement calls for, composed here because the deployer is:
// package policy writes retired on the service record and calls this, reaching
// no deploy target itself.

// removeService is [policy.Factory.Removal]: the deployer ends every instance of
// the service on every target of every persistent environment and writes a
// deploy record per environment naming no release, which is what makes the
// service's current release nothing wherever it ran.
//
// The environments this install knows are production's alone — a customer's is a
// record nothing here creates — so the removal reaches production's targets and
// says so by reaching no other.
func (p *path) removeService(ctx context.Context, serviceID string) error {
	svc, err := p.serviceOf(ctx, serviceID)
	if err != nil {
		return err
	}
	_, err = deploy.Remove(ctx, p.deploys, deploy.Removal{
		Actor:       deployActor,
		Principal:   deployerPrincipal,
		ServiceID:   svc.ID,
		ServiceName: svc.Name,
		From: []deploy.Environment{{
			EnvironmentID: p.production.ID,
			Credential:    p.d.credential,
			Reaches:       p.reaches(p.production),
		}},
	})
	return err
}
