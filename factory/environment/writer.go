package environment

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Writer is the persistent kinds' one writer: an owner at Factory. Its methods
// begin a transaction of their own and call the package function beside each,
// which is what package policy calls instead so that the write and the policy
// version it appends commit together or not at all.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Create writes a persistent environment in a transaction of its own. It is
// [Insert] with the transaction opened here.
func (w *Writer) Create(ctx context.Context, actor record.Actor, spec Spec) (Environment, error) {
	var created Environment
	err := w.inTransaction(ctx, "creating "+spec.Name, func(tx pgx.Tx) error {
		var err error
		created, err = Insert(ctx, tx, w.token, actor, spec)
		return err
	})
	return created, err
}

// Withdraw ends a persistent environment in a transaction of its own. It is
// [Withdraw] with the transaction opened here.
func (w *Writer) Withdraw(ctx context.Context, actor record.Actor, id string, completeDeployRecords int) error {
	return w.inTransaction(ctx, "withdrawing "+id, func(tx pgx.Tx) error {
		return Withdraw(ctx, tx, w.token, actor, id, completeDeployRecords)
	})
}

// AddTarget appends one target in a transaction of its own. It is [AddTarget]
// with the transaction opened here.
func (w *Writer) AddTarget(ctx context.Context, actor record.Actor, id string, target Target) error {
	return w.inTransaction(ctx, "adding a target to "+id, func(tx pgx.Tx) error {
		return AddTarget(ctx, tx, w.token, actor, id, target)
	})
}

// RemoveTarget removes one target in a transaction of its own. It is
// [RemoveTarget] with the transaction opened here.
func (w *Writer) RemoveTarget(ctx context.Context, actor record.Actor, id, address string, completeDeployRecords int) error {
	return w.inTransaction(ctx, "removing a target from "+id, func(tx pgx.Tx) error {
		return RemoveTarget(ctx, tx, w.token, actor, id, address, completeDeployRecords)
	})
}

func (w *Writer) inTransaction(ctx context.Context, doing string, write func(pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("environment: beginning %s: %w", doing, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := write(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("environment: committing %s: %w", doing, err)
	}
	return nil
}

// Insert writes a persistent environment inside tx: production's at the creation
// of the project, which an owner does not choose because production exists
// everywhere, and one a customer defines when they define it. The candidate kind
// is refused with [ErrNotAnOwnersKind]: the kind is the seam between this writer
// and [Candidates], and neither writes a record of the other's.
//
// A production environment whose platform cannot compose an environment on
// demand is refused here, which is the refusal the design makes at adoption and
// at decomposition for its services: an environment per candidate is the shape
// admitted, and a platform that cannot compose one leaves that shape with no
// implementation.
//
// token fences this write the way every write transaction in the module does: tx
// is begun by the caller — package policy's version transaction, or
// [Writer.Create] — so this is where the fence is called rather than at a Begin
// of this package's own.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, spec Spec) (Environment, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Environment{}, err
	}
	if err := actor.Validate(); err != nil {
		return Environment{}, err
	}
	if !contains(Kinds, spec.Kind) {
		return Environment{}, fmt.Errorf("%w: %q", ErrKindUnknown, spec.Kind)
	}
	if spec.Kind == KindCandidate {
		return Environment{}, fmt.Errorf("%w: %q is the deployer's to compose", ErrNotAnOwnersKind, spec.Name)
	}
	if spec.ProjectID == "" {
		return Environment{}, fmt.Errorf("%w: %q", ErrProjectIDEmpty, spec.Name)
	}
	if err := validTargets(spec.Targets); err != nil {
		return Environment{}, err
	}
	if spec.Credential.Name() == "" {
		return Environment{}, fmt.Errorf("environment: %s names no credential", spec.Name)
	}
	if spec.Platform.Name == "" || spec.Platform.Credential.Name() == "" {
		return Environment{}, fmt.Errorf("%w: %s", ErrPlatformIncomplete, spec.Name)
	}
	if spec.Kind == KindProduction && !spec.Platform.CanComposeOnDemand {
		return Environment{}, fmt.Errorf("%w: %s declares %s", ErrPlatformCannotComposeOnDemand, spec.Name, spec.Platform.Name)
	}

	e := Environment{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		Kind:       spec.Kind,
		ProjectID:  spec.ProjectID,
		Name:       spec.Name,
		Targets:    spec.Targets,
		Credential: spec.Credential,
		Platform:   spec.Platform,
	}
	if err := insert(ctx, tx, e); err != nil {
		return Environment{}, err
	}
	return e, nil
}

// insert writes one environment row, whichever of the two writers composed it.
func insert(ctx context.Context, tx pgx.Tx, e Environment) error {
	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, kind, project_id, name,
		 targets, credential, platform_name, platform_credential, can_compose_on_demand,
		 max_concurrent_candidate_environments, strategy_default,
		 item_id, composed_from, seed_version, value_set_version,
		 torn_down_at, torn_down_reason, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23)`,
		e.ID, FormatVersion, string(e.Actor.Kind), e.Actor.Key, string(e.Actor.Basis), e.At,
		string(e.Kind), e.ProjectID, e.Name, joinTargets(e.Targets), e.Credential.Name(),
		e.Platform.Name, e.Platform.Credential.Name(), e.Platform.CanComposeOnDemand,
		e.MaxConcurrentCandidateEnvironments, string(e.StrategyDefault),
		e.ItemID, joinComposed(e.Composition.From),
		e.Composition.SeedVersion, e.Composition.ValueSetVersion,
		e.TornDownAt, string(e.TornDownReason), e.WithdrawnAt,
	)
	if err != nil {
		return fmt.Errorf("environment: creating %q: %w", e.Name, err)
	}
	return nil
}

// Withdraw ends a persistent environment: an owner's write at Factory, refused
// while any deploy record on it marks a target complete for a release. The count
// of those records is the caller's argument rather than a read of this package's,
// because a deploy record is another package's and the direction between the two
// is deploy -> environment. The owner has the deployer remove each service from
// it first, which is what makes the count nothing.
//
// Production's is withdrawn the same way, as part of a project ending once every
// service in it is retired. A candidate's is refused: it is torn down instead.
func Withdraw(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, id string, completeDeployRecords int) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	e, err := lockPersistent(ctx, tx, id)
	if err != nil {
		return err
	}
	if completeDeployRecords != 0 {
		return fmt.Errorf("%w: %d on %s", ErrSoftwareStandsOnIt, completeDeployRecords, id)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set withdrawn_at = $1 where id = $2`, record.Now(), e.ID); err != nil {
		return fmt.Errorf("environment: withdrawing %s: %w", id, err)
	}
	return nil
}

// AddTarget appends one address to the environment's ordered target list. It is
// appended rather than placed, the order being the one a rollout reaches the
// targets in and a new target being reached last.
func AddTarget(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, id string, target Target) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	if target.Address == "" {
		return ErrTargetAddressEmpty
	}
	e, err := lockPersistent(ctx, tx, id)
	if err != nil {
		return err
	}
	for _, held := range e.Targets {
		if held.Address == target.Address {
			return fmt.Errorf("%w: %s on %s", ErrTargetAlreadyHeld, target.Address, id)
		}
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set targets = $1 where id = $2`,
		joinTargets(append(e.Targets, target)), id); err != nil {
		return fmt.Errorf("environment: adding %s to %s: %w", target.Address, id, err)
	}
	return nil
}

// RemoveTarget removes one address from the environment's target list, refused
// while any service's deploy record marks that target complete for a release —
// the deployer's removal on that one target comes first. The count is the
// caller's argument for the reason [Withdraw]'s is. The last target may not be
// removed: an environment with no address is one no deploy can reach, so an
// environment down to one target is withdrawn rather than emptied.
func RemoveTarget(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, id, address string, completeDeployRecords int) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	e, err := lockPersistent(ctx, tx, id)
	if err != nil {
		return err
	}
	kept := make([]Target, 0, len(e.Targets))
	for _, held := range e.Targets {
		if held.Address != address {
			kept = append(kept, held)
		}
	}
	if len(kept) == len(e.Targets) {
		return fmt.Errorf("%w: %s on %s", ErrTargetNotHeld, address, id)
	}
	if len(kept) == 0 {
		return fmt.Errorf("%w: %s is the last target of %s", ErrTargetsEmpty, address, id)
	}
	if completeDeployRecords != 0 {
		return fmt.Errorf("%w: %d on %s", ErrSoftwareStandsOnIt, completeDeployRecords, address)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set targets = $1 where id = $2`, joinTargets(kept), id); err != nil {
		return fmt.Errorf("environment: removing %s from %s: %w", address, id, err)
	}
	return nil
}

// SetMaxConcurrentCandidateEnvironments authors the count that holds at Deploy
// to candidate environment. It is authored on production's record beside the
// platform it declares, one per platform since room is the platform's, and
// nothing supplies a value for it: zero is unauthored and the platform's own
// room is then the only hold there is.
func SetMaxConcurrentCandidateEnvironments(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, id string, count int) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	if count < 0 {
		return fmt.Errorf("environment: a maximum concurrent candidate environments is not negative: %d", count)
	}
	e, err := lockPersistent(ctx, tx, id)
	if err != nil {
		return err
	}
	if e.Kind != KindProduction {
		return fmt.Errorf("%w: %s is %s", ErrNotAProductionEnvironment, id, e.Kind)
	}
	if _, err := tx.Exec(ctx, `update `+Table+`
		set max_concurrent_candidate_environments = $1 where id = $2`, count, id); err != nil {
		return fmt.Errorf("environment: authoring the ceiling on %s: %w", id, err)
	}
	return nil
}

// SetStrategyDefault authors the rollout strategy production takes where nothing
// narrows the pick. It is production's record alone: a strategy decides whether
// a control runs, and a control is a comparison against organic traffic, which
// no other kind has. Nothing supplies a value for it, so the empty value is a
// default nobody authored and not a strategy of nothing.
//
// What reads it is the production deploy row's pick, which is package gate's.
// This package holds the field and never the pick.
func SetStrategyDefault(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	id string, strategy gatepolicy.Strategy) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	if _, err := gatepolicy.DecidableStrategy(string(strategy)); err != nil {
		return err
	}
	e, err := lockPersistent(ctx, tx, id)
	if err != nil {
		return err
	}
	if e.Kind != KindProduction {
		return fmt.Errorf("%w: %s is %s", ErrNotAProductionEnvironment, id, e.Kind)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set strategy_default = $1 where id = $2`,
		string(strategy), id); err != nil {
		return fmt.Errorf("environment: authoring the strategy default on %s: %w", id, err)
	}
	return nil
}

// lockPersistent reads one environment for update and refuses a candidate's and
// an already withdrawn one. Every owner's write after creation goes through it,
// so the kind and the withdrawal are read inside the same transaction as the
// write they guard.
func lockPersistent(ctx context.Context, tx pgx.Tx, id string) (Environment, error) {
	e, err := scan(tx.QueryRow(ctx, selectEnvironment+` where id = $1 for update`, id), id)
	if err != nil {
		return Environment{}, err
	}
	if !e.Kind.Persistent() {
		return Environment{}, fmt.Errorf("%w: %s is a candidate's", ErrNotAnOwnersKind, id)
	}
	if e.WithdrawnAt != "" {
		return Environment{}, fmt.Errorf("%w: %s at %s", ErrAlreadyWithdrawn, id, e.WithdrawnAt)
	}
	return e, nil
}

func validTargets(targets []Target) error {
	if len(targets) == 0 {
		return ErrTargetsEmpty
	}
	seen := make(map[string]bool, len(targets))
	for _, t := range targets {
		if t.Address == "" {
			return ErrTargetAddressEmpty
		}
		if seen[t.Address] {
			return fmt.Errorf("%w: %s", ErrTargetAlreadyHeld, t.Address)
		}
		seen[t.Address] = true
	}
	return nil
}
