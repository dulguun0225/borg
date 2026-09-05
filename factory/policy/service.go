package policy

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
)

// The service record has three writers and this package is one of them:
// decomposition writes the identity, the deployer writes the four fields it
// populates at adoption and at every first release, and an owner writes the
// parameters and the two marks below. Each write here appends a version, every
// owner write at Factory being one.

// MarkServiceProvisioned writes provisioned on one service, with the shape the
// repository host gave the credentials and the credentials themselves. An owner
// writes it once the repository and the stores exist; decomposition never does.
func (f *Factory) MarkServiceProvisioned(ctx context.Context, actor record.Actor, serviceID string,
	shape service.CredentialShape, branch, master secretref.Ref) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "provisioned"},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetProvisioned(ctx, tx, serviceID, shape, branch, master)
		},
	})
}

// RetireService writes retired on one service, which is the one thing that ends
// one. The three counts are what still names the service, read by the caller
// inside no transaction of this package's: package service refuses the write
// where any is not nothing, and may not read those records itself.
func (f *Factory) RetireService(ctx context.Context, actor record.Actor, serviceID string,
	consumerContractsInForce, unmergedItems, unmergedItemsDependingOnIt int) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawn,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "retired"},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.Retire(ctx, tx, serviceID,
				consumerContractsInForce, unmergedItems, unmergedItemsDependingOnIt)
		},
	})
}

// SetServiceTargets writes which of an environment's targets the service runs
// on, in the order a rollout reaches them. environmentTargets is that
// environment's own list, read by the caller: package service checks each named
// target against it and cannot read an environment record itself.
func (f *Factory) SetServiceTargets(ctx context.Context, actor record.Actor, serviceID string,
	targets, environmentTargets []string) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "targets"},
		list:  targets, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetTargets(ctx, tx, serviceID, targets, environmentTargets)
		},
	})
}

// AuthorObjective authors the service level objective and the period it is
// counted over, authored outright with nothing supplied.
func (f *Factory) AuthorObjective(ctx context.Context, actor record.Actor, serviceID string,
	target, periodSeconds float64) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "objective"}, number: target, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetObjective(ctx, tx, serviceID, target, periodSeconds)
		},
	})
}

// AuthorPagingHours authors the hours within which the service pages. The
// default is every hour, because nothing the factory observes says which
// service may wait for morning.
func (f *Factory) AuthorPagingHours(ctx context.Context, actor record.Actor, serviceID string,
	hours service.PagingHours) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "paging_hours"},
		list:  []string{hours.Start, hours.End, hours.Zone}, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetPagingHours(ctx, tx, serviceID, hours)
		},
	})
}

// AuthorProductLicence authors the licence the service's own product is under.
func (f *Factory) AuthorProductLicence(ctx context.Context, actor record.Actor,
	serviceID, licence string) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "product_licence"},
		list:  []string{licence}, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetProductLicence(ctx, tx, serviceID, licence)
		},
	})
}

// AuthorSnapshotRetention authors how long a schema-change snapshot is kept.
// What it retains is a copy of production data the factory made, so a safeguard
// may shorten it and never lengthen it.
func (f *Factory) AuthorSnapshotRetention(ctx context.Context, actor record.Actor,
	serviceID string, seconds float64) (Version, error) {
	return f.authorField(ctx, actor, serviceID, "snapshot_retention", seconds, service.SetSnapshotRetention)
}

// AuthorMutantCap authors how many mutants the mutation score may spend per
// item, a fixed default on the service record leaving a safeguard nothing to
// constrain.
func (f *Factory) AuthorMutantCap(ctx context.Context, actor record.Actor,
	serviceID string, cap float64) (Version, error) {
	return f.authorField(ctx, actor, serviceID, "mutant_cap", cap, service.SetMutantCap)
}

// AuthorFailureRecordKeyCap authors the cap on a release's distinct failure
// record keys, which a safeguard may lower and never raise.
func (f *Factory) AuthorFailureRecordKeyCap(ctx context.Context, actor record.Actor,
	serviceID string, cap float64) (Version, error) {
	return f.authorField(ctx, actor, serviceID, "failure_record_key_cap", cap, service.SetFailureRecordKeyCap)
}

// AuthorUnreliableBound authors the rate of disagreement above which a
// criterion of this service is unreliable, which a safeguard may raise and
// never lower, lowering being what takes a criterion out of the gate.
func (f *Factory) AuthorUnreliableBound(ctx context.Context, actor record.Actor,
	serviceID string, bound float64) (Version, error) {
	return f.authorField(ctx, actor, serviceID, "unreliable_bound", bound, service.SetUnreliableBound)
}

// AuthorIncidentItemBound authors how long an incident-raised item may be
// worked before a human is reached.
func (f *Factory) AuthorIncidentItemBound(ctx context.Context, actor record.Actor,
	serviceID string, seconds float64) (Version, error) {
	return f.authorField(ctx, actor, serviceID, "incident_item_bound", seconds, service.SetIncidentItemBound)
}

// authorField is one number authored on a service record that is not one of
// gate policy's eleven rows, so the version names the field by its key and no
// parameter: [gatepolicy.Definitions] is the eleven, and these are beside them.
func (f *Factory) authorField(ctx context.Context, actor record.Actor, serviceID, key string,
	value float64, set func(context.Context, pgx.Tx, string, float64) error) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: key}, number: value, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return set(ctx, tx, serviceID, value)
		},
	})
}
