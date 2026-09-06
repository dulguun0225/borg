package environment

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

// Kind is what an environment is, fixed at creation. There are three.
type Kind string

const (
	// KindProduction is the environment every project has and nobody chooses,
	// because production exists everywhere. It is one record per project.
	KindProduction Kind = "production"
	// KindCustomer is an environment a customer defines, one of any number per
	// project. Master's releases are deployed to it and nothing makes a release
	// pass through one before production.
	KindCustomer Kind = "customer"
	// KindCandidate is a candidate's own environment, composed from master plus
	// that candidate and torn down for good when the item merges, is dropped, or
	// is superseded by a re-decomposition. It holds nothing an owner authored,
	// being created at the gate that decides its deploy, and its writer is
	// [Candidates] and never [Insert].
	KindCandidate Kind = "candidate"
)

// Kinds is every kind an environment record may have. The CHECK in [DDL] lists
// the same ones, and TestDDLListsEveryKind fails if the two stop agreeing.
var Kinds = []Kind{KindProduction, KindCustomer, KindCandidate}

// Persistent is whether the kind is one an owner writes and withdraws. The two
// persistent kinds hold a platform, a project, and the thresholds an owner
// authors; a candidate's holds none of the three.
func (k Kind) Persistent() bool { return k == KindProduction || k == KindCustomer }

// ProductionName is the name production's record carries. It is a constant
// rather than a caller's choice, production being the one environment an owner
// does not define, and it is unique within the project rather than within the
// install because production is one record per project.
const ProductionName = "production"

var (
	// ErrKindUnknown is returned for a kind outside [Kinds].
	ErrKindUnknown = errors.New("environment: the kind is not one of the three")
	// ErrProjectIDEmpty is returned where a record that names a project names
	// none.
	ErrProjectIDEmpty = errors.New("environment: the environment names the project it belongs to")
	// ErrTargetsEmpty is returned for an environment naming no target. An
	// environment with no address is one no deploy can reach.
	ErrTargetsEmpty = errors.New("environment: an environment names at least one target")
	// ErrTargetAddressEmpty is returned for a target with no address.
	ErrTargetAddressEmpty = errors.New("environment: a target is an address a deploy is performed against")
	// ErrTargetNotHeld is returned by [RemoveTarget] for an address the
	// environment does not hold.
	ErrTargetNotHeld = errors.New("environment: the environment does not hold that target")
	// ErrTargetAlreadyHeld is returned by [AddTarget] for an address the
	// environment already holds. The field is ordered and an address is in it
	// once.
	ErrTargetAlreadyHeld = errors.New("environment: the environment already holds that target")
	// ErrPlatformIncomplete is returned for a persistent environment declaring
	// no platform name or no platform credential.
	ErrPlatformIncomplete = errors.New("environment: a persistent environment declares a platform and the credential it is composed through")
	// ErrPlatformCannotComposeOnDemand is returned by [Insert] for a production
	// environment whose platform cannot compose an environment on demand. An
	// environment per candidate is the shape the design admits and nothing else.
	ErrPlatformCannotComposeOnDemand = errors.New("environment: a production environment's platform composes an environment on demand")
	// ErrNotFound is returned where no environment has the id or the name.
	ErrNotFound = errors.New("environment: no environment has that id")
	// ErrThresholdOutOfRange is returned by [SetGateThreshold] for a threshold
	// outside nothing to one, which is the scale the score's number is on.
	ErrThresholdOutOfRange = errors.New("environment: a gate threshold is between 0 and 1")
	// ErrGateRowEmpty is returned by [SetGateThreshold] for a threshold naming
	// no gate row.
	ErrGateRowEmpty = errors.New("environment: a threshold names the gate row it applies at")
	// ErrNotAnOwnersKind is returned by [Insert] for the candidate kind and by
	// every owner's write for a candidate's record. An owner writes the
	// persistent kinds and authors on them; a candidate's environment is the
	// deployer's and holds nothing an owner authored, being created at the gate
	// that decides its deploy and so unable to hold the threshold that decided
	// it.
	ErrNotAnOwnersKind = errors.New("environment: a candidate's environment is not an owner's to write or to author on")
	// ErrNotAProductionEnvironment is returned by
	// [SetMaxConcurrentCandidateEnvironments] and [CountLiveCandidates] for a
	// record that is not production's. Room is the platform's and the platform
	// is production's declaration, so the ceiling and the count are keyed by
	// that record.
	ErrNotAProductionEnvironment = errors.New("environment: the record is not a production environment")
	// ErrSoftwareStandsOnIt is returned by [Withdraw] and [RemoveTarget] where a
	// deploy record still marks a target complete for a release. The owner has
	// the deployer remove each service first.
	ErrSoftwareStandsOnIt = errors.New("environment: a deploy record still marks a target complete for a release")
	// ErrAlreadyWithdrawn is returned by an owner's write on an environment
	// already withdrawn.
	ErrAlreadyWithdrawn = errors.New("environment: the environment is already withdrawn")
)

// Target is one address a deploy is performed against, and whether the platform
// behind it serves a share.
type Target struct {
	// Address is where the deploy is performed. It is named the way a registry
	// or a repository is: what it means is the platform's.
	Address string
	// ServesAShare is whether the platform behind this target can decide what
	// fraction of arriving traffic reaches each of two builds. It is the fact
	// the score reads when it picks a strategy, so a strategy the platform
	// cannot perform is not something the deployer discovers.
	ServesAShare bool
}

// Platform is what an environment is composed on: the name of the platform, the
// credential a candidate environment is composed through, and whether it can
// compose one on demand.
type Platform struct {
	Name string
	// Credential is the reference the platform is reached with, resolved by
	// name. It is a reference and never a value.
	Credential secretref.Ref
	// CanComposeOnDemand is whether the platform can compose an environment when
	// asked. A production environment declaring one that cannot is refused.
	CanComposeOnDemand bool
}

// Spec is what an owner declares when they create a persistent environment.
type Spec struct {
	Kind      Kind
	ProjectID string
	Name      string
	// Targets are the addresses a deploy is performed against, in the order a
	// rollout reaches them.
	Targets []Target
	// Credential is the reference a deploy into this environment is performed
	// with.
	Credential secretref.Ref
	Platform   Platform
}

// Environment is one environment as it is stored.
type Environment struct {
	ID    string
	Actor record.Actor
	At    string
	Kind  Kind
	// ProjectID is the project the environment belongs to. A persistent
	// environment always names one; a candidate's names the project of the item's
	// service, which is what its ceiling is counted against.
	ProjectID string
	Name      string
	// Targets are the addresses a deploy is performed against, ordered, and the
	// order is the one a rollout reaches them in.
	Targets []Target
	// Credential is the reference a deploy is performed with. It is a reference
	// and never a value, so nothing that renders this record renders a secret.
	Credential secretref.Ref
	// Platform is what the environment is composed on, declared on a persistent
	// kind and empty on a candidate's.
	Platform Platform
	// MaxConcurrentCandidateEnvironments is the count an owner authored on
	// production's record, and zero where they authored none. It holds at Deploy
	// to candidate environment beside the platform's own room, whichever of the
	// two is reached first.
	MaxConcurrentCandidateEnvironments int
	// StrategyDefault is the rollout strategy an owner authored on production's
	// record, and is empty where they authored none and on every other kind: a
	// strategy decides whether a control runs, and a control is a comparison
	// against organic traffic, which no other kind has.
	StrategyDefault gatepolicy.Strategy
	// ItemID is the item a candidate's environment belongs to, and is empty on a
	// persistent kind. It is the item and not the build, because the environment
	// persists across a rebuild.
	ItemID string
	// Composition is what the deployer put in place beside the candidate, and is
	// empty on a persistent kind.
	Composition Composition
	// TornDownAt is when the environment was torn down for good, and is empty
	// while it stands — a reclamation closes a cycle and leaves this empty. The
	// row is kept rather than deleted, because the deploy records naming it would
	// otherwise point at nothing.
	TornDownAt string
	// TornDownReason is which of the three teardown-for-good events happened, and
	// is empty where none has.
	TornDownReason Reason
	// WithdrawnAt is when an owner withdrew a persistent environment, and is
	// empty while it stands.
	WithdrawnAt string
}

// Live says whether the environment has neither been torn down for good nor
// withdrawn.
func (e Environment) Live() bool { return e.TornDownAt == "" && e.WithdrawnAt == "" }

// Addresses is the targets' addresses in order, which is what a caller that
// reaches them one at a time walks.
func (e Environment) Addresses() []string {
	addresses := make([]string, 0, len(e.Targets))
	for _, t := range e.Targets {
		addresses = append(addresses, t.Address)
	}
	return addresses
}

// EveryTargetServesAShare is whether every target of the environment is behind a
// platform that serves a share. The score picks the row with a control only
// where it holds, there being no control where no share can be served.
func (e Environment) EveryTargetServesAShare() bool {
	for _, t := range e.Targets {
		if !t.ServesAShare {
			return false
		}
	}
	return len(e.Targets) > 0
}

const selectEnvironment = `select id, actor_kind, actor_key, actor_key_basis, at, kind, project_id, name,
	targets, credential, platform_name, platform_credential, can_compose_on_demand,
	max_concurrent_candidate_environments, strategy_default,
	item_id, composed_from, seed_version, value_set_version,
	torn_down_at, torn_down_reason, withdrawn_at
	from ` + Table

// Get is one environment by id. It takes the pool and not a writer, because
// reading an environment is not a reason to be handed the thing that creates
// them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Environment, error) {
	return scan(pool.QueryRow(ctx, selectEnvironment+` where id = $1`, id), id)
}

// ByName is the environment of that name in that project, and false where the
// project has none. The name is unique within the project and not within the
// install, so both are named.
func ByName(ctx context.Context, pool *pgxpool.Pool, projectID, name string) (Environment, bool, error) {
	e, err := scan(pool.QueryRow(ctx, selectEnvironment+` where project_id = $1 and name = $2`, projectID, name), name)
	if errors.Is(err, ErrNotFound) {
		return Environment{}, false, nil
	} else if err != nil {
		return Environment{}, false, err
	}
	return e, true, nil
}

// Production is the production environment of one project, and false where the
// project has none — which is every project until it is created, production's
// record being written in the same event as the project's.
func Production(ctx context.Context, pool *pgxpool.Pool, projectID string) (Environment, bool, error) {
	return ByName(ctx, pool, projectID, ProductionName)
}

// ForItem is the candidate environment of one item, and false where the item has
// none — which is every item until the candidate deploy gate approves. A
// torn-down one is still returned, the row being kept: a caller that wants a
// place to deploy into reads [Environment.Live] on what comes back.
func ForItem(ctx context.Context, pool *pgxpool.Pool, itemID string) (Environment, bool, error) {
	if itemID == "" {
		return Environment{}, false, nil
	}
	e, err := scan(pool.QueryRow(ctx, selectEnvironment+` where item_id = $1`, itemID), itemID)
	if errors.Is(err, ErrNotFound) {
		return Environment{}, false, nil
	} else if err != nil {
		return Environment{}, false, err
	}
	return e, true, nil
}

// CountLiveCandidates is how many candidate environments of one project stand.
// It is scoped to the production environment named, because the ceiling and the
// platform's own room are both that record's: an install whose projects run on
// two platforms adds neither count across them.
func CountLiveCandidates(ctx context.Context, pool *pgxpool.Pool, productionEnvironmentID string) (int, error) {
	production, err := Get(ctx, pool, productionEnvironmentID)
	if err != nil {
		return 0, err
	}
	if production.Kind != KindProduction {
		return 0, fmt.Errorf("%w: %s is %s", ErrNotAProductionEnvironment, productionEnvironmentID, production.Kind)
	}
	var count int
	err = pool.QueryRow(ctx, `select count(*) from `+Table+`
		where kind = $1 and project_id = $2 and torn_down_at = ''`,
		string(KindCandidate), production.ProjectID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("environment: counting the live candidate environments of %s: %w", production.ProjectID, err)
	}
	return count, nil
}

// TornDownCandidates is every candidate environment of the project the named
// production environment belongs to whose record marks it torn down. It is what
// the deployer's pass over the platform compares against what the platform
// reports holding: a candidate environment the platform holds and the records
// mark torn down is a teardown that failed, and the deployer tears it down again
// on its next pass, keyed on the environment, so a leak does not consume the
// room for good.
//
// The rows are kept rather than deleted at teardown, which is what makes this a
// read and not a history: the deploy records naming them would otherwise point
// at nothing.
//
// Nothing calls it yet. The pass is the deployer's, which lives in the
// command-line interface.
func TornDownCandidates(ctx context.Context, pool *pgxpool.Pool, productionEnvironmentID string) ([]Environment, error) {
	production, err := Get(ctx, pool, productionEnvironmentID)
	if err != nil {
		return nil, err
	}
	if production.Kind != KindProduction {
		return nil, fmt.Errorf("%w: %s is %s", ErrNotAProductionEnvironment, productionEnvironmentID, production.Kind)
	}
	rows, err := pool.Query(ctx, selectEnvironment+`
		where kind = $1 and project_id = $2 and torn_down_at <> '' order by torn_down_at, id`,
		string(KindCandidate), production.ProjectID)
	if err != nil {
		return nil, fmt.Errorf("environment: reading the torn-down candidate environments of %s: %w",
			production.ProjectID, err)
	}
	defer rows.Close()

	var torn []Environment
	for rows.Next() {
		e, err := scan(rows, production.ProjectID)
		if err != nil {
			return nil, err
		}
		torn = append(torn, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("environment: reading the torn-down candidate environments of %s: %w",
			production.ProjectID, err)
	}
	return torn, nil
}

func scan(row pgx.Row, named string) (Environment, error) {
	var e Environment
	var kind, actorKind, actorBasis, targets, credential, platformCredential, composed, reason string
	var strategyDefault string
	err := row.Scan(&e.ID, &actorKind, &e.Actor.Key, &actorBasis, &e.At, &kind, &e.ProjectID, &e.Name,
		&targets, &credential, &e.Platform.Name, &platformCredential, &e.Platform.CanComposeOnDemand,
		&e.MaxConcurrentCandidateEnvironments, &strategyDefault, &e.ItemID, &composed,
		&e.Composition.SeedVersion, &e.Composition.ValueSetVersion,
		&e.TornDownAt, &reason, &e.WithdrawnAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Environment{}, fmt.Errorf("%w: %s", ErrNotFound, named)
	} else if err != nil {
		return Environment{}, fmt.Errorf("environment: reading %s: %w", named, err)
	}
	e.Actor.Kind = record.Kind(actorKind)
	e.Actor.Basis = record.Basis(actorBasis)
	e.Kind = Kind(kind)
	e.StrategyDefault = gatepolicy.Strategy(strategyDefault)
	e.TornDownReason = Reason(reason)
	e.Targets, err = splitTargets(targets)
	if err != nil {
		return Environment{}, fmt.Errorf("environment: the targets stored on %s: %w", named, err)
	}
	ref, err := secretref.New(credential)
	if err != nil {
		return Environment{}, fmt.Errorf("environment: the credential stored on %s: %w", named, err)
	}
	e.Credential = ref
	if platformCredential != "" {
		e.Platform.Credential, err = secretref.New(platformCredential)
		if err != nil {
			return Environment{}, fmt.Errorf("environment: the platform credential stored on %s: %w", named, err)
		}
	}
	e.Composition.From, err = splitComposed(composed)
	if err != nil {
		return Environment{}, fmt.Errorf("environment: what %s was composed from: %w", named, err)
	}
	return e, nil
}

func contains(kinds []Kind, kind Kind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}
