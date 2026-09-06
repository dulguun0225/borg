package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
)

// The safeguard that keeps a control: what the production deploy row's fourth
// action places, and what every production deploy firing after it reads when the
// score picks the strategy. It is one record — package policy writes it at
// Factory and package safeguard reads it — so the write and the read are one
// value here.

// strategySafeguard is that record's two halves for package gate, which writes
// no record of its own.
type strategySafeguard struct {
	pool    *pgxpool.Pool
	factory *policy.Factory
}

// KeepAControl places the safeguard on one service as the human who asked for it
// at the row. It bounds no value: of the two rollout strategies only the one
// with a control adds anything, so the parameter is the whole of what the
// safeguard says.
func (s strategySafeguard) KeepAControl(ctx context.Context, actor record.Actor, serviceID string) error {
	_, _, err := s.factory.AddSafeguard(ctx, actor, gatepolicy.StrategyDefault,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: serviceID},
		safeguard.Bound{}, safeguard.Routing{})
	return err
}

// KeepsAControl is whether such a safeguard stands on the service, read at the
// moment of firing like every other check a gate makes. A safeguard an approved
// withdrawal names is not in force, which [safeguard.BySubjects] already
// answers.
func (s strategySafeguard) KeepsAControl(ctx context.Context, serviceID string) (bool, error) {
	if serviceID == "" {
		return false, nil
	}
	standing, err := safeguard.BySubjects(ctx, s.pool, gatepolicy.StrategyDefault,
		[]safeguard.Subject{{Kind: safeguard.SubjectService, ID: serviceID}})
	if err != nil {
		return false, err
	}
	return len(standing) > 0, nil
}
