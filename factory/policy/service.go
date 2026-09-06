package policy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
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
// one, and then calls the deployer's removal. The three counts are what still
// names the service, read by the caller inside no transaction of this package's:
// package service refuses the write where any is not nothing, and may not read
// those records itself.
//
// The removal is [Factory.Removal], supplied by whatever composes the factory,
// and a factory composed with none refuses the write with [ErrNoDeployer]
// rather than recording a retirement nothing performed. It runs after the
// version and the field commit, which is the order the design states — the write
// calls the deployer — so a removal that stopped leaves a service retired whose
// removal is performed again, and never software still running that no record
// says was retired.
func (f *Factory) RetireService(ctx context.Context, actor record.Actor, serviceID string,
	consumerContractsInForce, unmergedItems, unmergedItemsDependingOnIt int) (Version, error) {
	if f.Removal == nil {
		return Version{}, fmt.Errorf("%w: %s", ErrNoDeployer, serviceID)
	}
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawn,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "retired"},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.Retire(ctx, tx, serviceID,
				consumerContractsInForce, unmergedItems, unmergedItemsDependingOnIt)
		},
	})
	if err != nil {
		return Version{}, err
	}
	if err := f.Removal(ctx, serviceID); err != nil {
		return version, fmt.Errorf("policy: removing %s from every persistent environment: %w", serviceID, err)
	}
	return version, nil
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
	return f.authorOnService(ctx, actor, gatepolicy.SnapshotRetention, serviceID, seconds,
		service.SetSnapshotRetention)
}

// AuthorMutantCap authors how many mutants the mutation score may spend per
// item, a fixed default on the service record leaving a safeguard nothing to
// constrain.
func (f *Factory) AuthorMutantCap(ctx context.Context, actor record.Actor,
	serviceID string, cap float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.MutantCap, serviceID, cap, service.SetMutantCap)
}

// AuthorFailureRecordKeyCap authors the cap on a release's distinct failure
// record keys, which a safeguard may lower and never raise.
func (f *Factory) AuthorFailureRecordKeyCap(ctx context.Context, actor record.Actor,
	serviceID string, cap float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.FailureRecordKeyCap, serviceID, cap,
		service.SetFailureRecordKeyCap)
}

// AuthorUnreliableBound authors the rate of disagreement above which a
// criterion of this service is unreliable, which a safeguard may raise and
// never lower, lowering being what takes a criterion out of the gate.
func (f *Factory) AuthorUnreliableBound(ctx context.Context, actor record.Actor,
	serviceID string, bound float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.UnreliableBound, serviceID, bound,
		service.SetUnreliableBound)
}

// AuthorIncidentItemBound authors how long an incident-raised item may be
// worked before a human is reached.
func (f *Factory) AuthorIncidentItemBound(ctx context.Context, actor record.Actor,
	serviceID string, seconds float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.IncidentItemBound, serviceID, seconds,
		service.SetIncidentItemBound)
}

// AuthorBakeVolume authors the traffic the targets a rollout has already
// reached serve before the next is reached. Where an owner authors none the
// score supplies it, and a safeguard may raise it and never lower it.
func (f *Factory) AuthorBakeVolume(ctx context.Context, actor record.Actor,
	serviceID string, volume float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.BakeVolume, serviceID, volume, service.SetBakeVolume)
}

// AuthorBacklogCap authors how many releases may wait behind a rollback hold
// before the merge queue stops fast-forwarding this service's candidates.
func (f *Factory) AuthorBacklogCap(ctx context.Context, actor record.Actor,
	serviceID string, releases float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.BacklogCap, serviceID, releases, service.SetBacklogCap)
}

// AuthorMutationFloor authors the mutation score below which Merge to master
// rejects, which a safeguard may raise and never lower.
func (f *Factory) AuthorMutationFloor(ctx context.Context, actor record.Actor,
	serviceID string, floor float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.MutationFloor, serviceID, floor, service.SetMutationFloor)
}

// AuthorKeptFraction authors the fraction of its instances a release keeps
// while a rollback could return to it, authored outright with a fixed default
// of all of them.
func (f *Factory) AuthorKeptFraction(ctx context.Context, actor record.Actor,
	serviceID string, fraction float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.KeptFraction, serviceID, fraction, service.SetKeptFraction)
}

// AuthorMaxConcurrentKeptFleets authors how many kept fleets this service may
// hold at once. A service at the cap stops deploying rather than losing a
// recovery a window could still call for.
func (f *Factory) AuthorMaxConcurrentKeptFleets(ctx context.Context, actor record.Actor,
	serviceID string, fleets float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.MaxConcurrentKeptFleets, serviceID, fleets,
		service.SetMaxConcurrentKeptFleets)
}

// AuthorRecentHistoryRunLength authors the average run length the reading
// against this service's own recent history is taken at. A safeguard may only
// shorten it, which adds a check rather than removing one.
func (f *Factory) AuthorRecentHistoryRunLength(ctx context.Context, actor record.Actor,
	serviceID string, volume float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.RecentHistoryRunLength, serviceID, volume,
		service.SetRecentHistoryRunLength)
}

// AuthorRecentHistorySize authors the smallest change that reading has to
// detect on one quantity, per quantity as the window's own size is.
func (f *Factory) AuthorRecentHistorySize(ctx context.Context, actor record.Actor, serviceID string,
	quantity gatepolicy.Quantity, size float64) (Version, error) {
	return f.author(ctx, actor, gatepolicy.RecentHistorySize,
		Scope{Kind: ScopeService, ID: serviceID, Key: string(quantity)}, size,
		func(ctx context.Context, tx pgx.Tx) error {
			return service.SetRecentHistorySize(ctx, tx, f.token, actor, serviceID, quantity, size)
		})
}

// AuthorProofTestRate authors how often the deployer, inside an open window,
// shifts a share of traffic onto the instances of the rollback's target and
// back again. No proof test runs at all where an owner authors no rate.
func (f *Factory) AuthorProofTestRate(ctx context.Context, actor record.Actor,
	serviceID string, rate float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.ProofTestRate, serviceID, rate, service.SetProofTestRate)
}

// AuthorInstanceHourRate authors what one instance-hour converts to, in the
// currency the owner's rates are authored in.
func (f *Factory) AuthorInstanceHourRate(ctx context.Context, actor record.Actor,
	serviceID string, rate float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.InstanceHourRate, serviceID, rate, service.SetInstanceHourRate)
}

// AuthorEnvironmentHourRate authors what one environment-hour converts to, the
// second of the two rates that price hosting outside the factory.
func (f *Factory) AuthorEnvironmentHourRate(ctx context.Context, actor record.Actor,
	serviceID string, rate float64) (Version, error) {
	return f.authorOnService(ctx, actor, gatepolicy.EnvironmentHourRate, serviceID, rate,
		service.SetEnvironmentHourRate)
}

// AuthorSearchBudget authors what a search may spend before it stops: a maximum
// count of builds and a maximum total time production spends on them. The two
// are one write, so the version names the field by its key and no parameter for
// the reason [Factory.AuthorObjective]'s does.
func (f *Factory) AuthorSearchBudget(ctx context.Context, actor record.Actor,
	serviceID string, builds, seconds float64) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "search_budget"}, number: builds, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetSearchBudget(ctx, tx, serviceID, builds, seconds)
		},
	})
}

// AuthorOperationCap authors how many operations one release may hold open per
// interval and the overflow operation the excess lands in. The two are one
// write: a cap with no overflow operation truncates the count and leaves
// nowhere for the rest to land.
func (f *Factory) AuthorOperationCap(ctx context.Context, actor record.Actor,
	serviceID string, operations float64, overflow string) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "operation_cap"},
		number: operations, list: []string{overflow}, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.SetOperationCap(ctx, tx, serviceID, operations, overflow)
		},
	})
}

// AuthorChangeFreezePeriod authors one period within which this service's
// production deploys are held. It is authored outright with nothing supplied,
// ahead of what it is for, and a period is added rather than edited — so the
// version names the periods by key and no parameter, one write per period.
func (f *Factory) AuthorChangeFreezePeriod(ctx context.Context, actor record.Actor,
	serviceID, startsAt, endsAt string) (Version, error) {
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionAuthored,
		scope: Scope{Kind: ScopeService, ID: serviceID, Key: "change_freeze"},
		list:  []string{startsAt, endsAt}, authored: true,
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return service.AddFreezePeriod(ctx, tx, f.token, actor, serviceID, startsAt, endsAt)
		},
	})
}

// A write on this record that sets a second value beside the first — the
// objective and its period, the paging hours' three, the operation cap and its
// overflow, the search budget's two, and a freeze period — names the field on
// the version by its key and no parameter, because re-deriving one number of a
// pair would leave the record in a state its own CHECK refuses. Every write of
// one number names its parameter and is re-derived.
