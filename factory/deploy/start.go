package deploy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// The writes that begin a deploy: what the deployer names, and the one
// transaction that mints the number, writes the record and writes a row per
// target.

// Reaching is one target the deploy will reach: its address, and the three
// fleets' instance counts there — the release's own, the control's where a
// control runs on that target, and how many instances of the release a rollback
// of this one would return to the deployer is keeping. The order of the slice is
// the environment's own, which is the order the deployer reaches them in.
type Reaching struct {
	Address string
	// ReleaseInstances is how many instances of this deploy's own build run
	// here, and ControlInstances how many the control runs, which is nothing on
	// every target but the control's.
	ReleaseInstances int
	ControlInstances int
	// KeptInstances is the capacity the release being replaced had, times the
	// fraction its owner authored, kept here while any open window's rollback
	// could return to that release.
	KeptInstances int
}

// Beginning is what the deployer names when it begins a deploy. Everything on
// it is written at the start, which is when the window opens over the deploy:
// the targets with their three instance counts, the digests, and the strategy
// where the deploy is into production. What is not written at the start is the
// strategy performed, which is written once something has been performed.
type Beginning struct {
	ServiceID     string
	EnvironmentID string
	What          What
	// Targets are the environment's targets in the environment's order. A row is
	// written for each at the start, not reached.
	Targets []Reaching
	// IntoProduction is whether the environment is the production one, which is
	// the only place a strategy attaches.
	IntoProduction bool
	// StrategyPicked is what the score picked, required where IntoProduction and
	// refused elsewhere. What was performed is written by the rollout.
	StrategyPicked Strategy
	// DeliveredReleaseIDs is a revert's deploy listing the releases it delivers.
	DeliveredReleaseIDs []string
	// SchemaChanges are the changes the build carries, in the order they apply,
	// and empty where it carries none. There is more than one on a revert's
	// deploy alone.
	SchemaChanges []string
	// ConfigurationDigest is over the resolved value set the build runs under.
	ConfigurationDigest string
	// WayInTokenDigest is the digest of the token minted for the way in.
	WayInTokenDigest string
	// ControlTarget is the target a control runs on, under a strategy with one,
	// and ControlReleaseID the release that control runs: the release a rollback
	// of this deploy would return to, which is what defines a control. The two
	// arrive together, and a control naming no release is refused.
	ControlTarget    string
	ControlReleaseID string
	// Backfill is what a backfill item's release copies between, and is empty on
	// every other deploy.
	Backfill Backfill
}

// Start writes the deploy record and its rows per target in one transaction:
// the sequence number is one above the highest this service and environment
// have, read under an advisory lock per pair so two deploys onto one pair
// serialise, and every target of the environment gets a row marked not reached
// with the kept-instance count for that target.
func (w *Writer) Start(ctx context.Context, actor record.Actor, b Beginning) (Deploy, error) {
	return w.start(ctx, actor, b, Undoing{})
}

// StartUndoing writes the deploy record of a rollback: a deploy of the release
// it returns to, naming what it failed, what it skipped, and the source that
// called for it. Every other field is an ordinary deploy's, because a rollback
// is a deploy event and not a record of its own — every field it would need is
// on this record already, and a second writer on the fact of what is running is
// the fact the drift detector exists to check.
func (w *Writer) StartUndoing(ctx context.Context, actor record.Actor, b Beginning, undoing Undoing) (Deploy, error) {
	if !undoing.Any() {
		return Deploy{}, ErrUndoingIncomplete
	}
	if undoing.Source == "" {
		return Deploy{}, fmt.Errorf("%w: it names no source", ErrUndoingIncomplete)
	}
	for _, skipped := range undoing.SkippedReleaseIDs {
		if skipped == "" {
			return Deploy{}, fmt.Errorf("%w: one of the releases it skipped", ErrUndoingIncomplete)
		}
		if skipped == undoing.FailedReleaseID {
			return Deploy{}, fmt.Errorf("%w: %s is failed and skipped, and the two are kept apart",
				ErrUndoingIncomplete, skipped)
		}
	}
	return w.start(ctx, actor, b, undoing)
}

func (w *Writer) start(ctx context.Context, actor record.Actor, b Beginning, undoing Undoing) (Deploy, error) {
	if err := actor.Validate(); err != nil {
		return Deploy{}, err
	}
	if b.ServiceID == "" {
		return Deploy{}, ErrServiceIDEmpty
	}
	if b.EnvironmentID == "" {
		return Deploy{}, ErrEnvironmentEmpty
	}
	if b.What.ReleaseID != "" && b.What.BuildID == "" {
		return Deploy{}, ErrBuildIDEmpty
	}
	if len(b.Targets) == 0 {
		return Deploy{}, ErrNoTargets
	}
	for _, target := range b.Targets {
		if target.Address == "" {
			return Deploy{}, fmt.Errorf("%w: one of them names no address", ErrNoTargets)
		}
	}
	if b.IntoProduction != (b.StrategyPicked != "") {
		return Deploy{}, fmt.Errorf("%w: into production %v, strategy %q",
			ErrStrategyNotProduction, b.IntoProduction, b.StrategyPicked)
	}
	if (b.ControlTarget == "") != (b.ControlReleaseID == "") {
		return Deploy{}, fmt.Errorf("%w: target %q, release %q",
			ErrControlIncomplete, b.ControlTarget, b.ControlReleaseID)
	}
	if b.Backfill.Any() && (b.Backfill.Contract == "" || b.Backfill.FromElement == "") {
		return Deploy{}, fmt.Errorf("%w: %+v", ErrBackfillIncomplete, b.Backfill)
	}

	d := Deploy{
		ID:                  record.NewID(IDPrefix),
		Actor:               actor,
		At:                  record.Now(),
		ServiceID:           b.ServiceID,
		EnvironmentID:       b.EnvironmentID,
		ReleaseID:           b.What.ReleaseID,
		BuildID:             b.What.BuildID,
		DeliveredReleaseIDs: b.DeliveredReleaseIDs,
		StrategyPicked:      b.StrategyPicked,
		Status:              StatusStarted,
		SchemaChanges:       b.SchemaChanges,
		Backfill:            b.Backfill,
		ConfigurationDigest: b.ConfigurationDigest,
		WayInTokenDigest:    b.WayInTokenDigest,
		ControlTarget:       b.ControlTarget,
		ControlReleaseID:    b.ControlReleaseID,
		Undoing:             undoing,
	}

	tx, err := w.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return Deploy{}, fmt.Errorf("deploy: beginning the start of %s: %w", d.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Deploy{}, err
	}

	if _, err := tx.Exec(ctx, `select pg_advisory_xact_lock($1)`,
		AdvisoryLockKey(b.ServiceID, b.EnvironmentID)); err != nil {
		return Deploy{}, fmt.Errorf("deploy: taking the sequence lock for %s in %s: %w",
			b.ServiceID, b.EnvironmentID, err)
	}
	err = tx.QueryRow(ctx, `select coalesce(max(number), 0) + 1 from `+Table+`
		where service_id = $1 and environment_id = $2`, b.ServiceID, b.EnvironmentID).Scan(&d.Number)
	if err != nil {
		return Deploy{}, fmt.Errorf("deploy: reading the highest sequence number of %s in %s: %w",
			b.ServiceID, b.EnvironmentID, err)
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, environment_id, number,
		 release_id, build_id, delivered_release_ids, strategy_picked, strategy_performed, status,
		 schema_changes, configuration_digest, way_in_token_digest, control_target, control_release_id,
		 backfill_contract, backfill_element, backfill_from_element,
		 failed_release_id, skipped_release_ids, source)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20,
		 $21, $22, $23, $24, $25, $26)`,
		d.ID, FormatVersion, string(d.Actor.Kind), d.Actor.Key, string(d.Actor.Basis), d.At,
		d.ServiceID, d.EnvironmentID, d.Number, d.ReleaseID, d.BuildID, joinLines(d.DeliveredReleaseIDs),
		string(d.StrategyPicked), string(d.StrategyPerformed), string(d.Status),
		joinLines(d.SchemaChanges), d.ConfigurationDigest, d.WayInTokenDigest,
		d.ControlTarget, d.ControlReleaseID,
		d.Backfill.Contract, d.Backfill.Element, d.Backfill.FromElement,
		d.Undoing.FailedReleaseID, joinLines(d.Undoing.SkippedReleaseIDs), d.Undoing.Source,
	)
	if err != nil {
		return Deploy{}, fmt.Errorf("deploy: starting %s: %w", d.ID, err)
	}

	for position, target := range b.Targets {
		_, err = tx.Exec(ctx, `insert into `+TargetTable+`
			(deploy_id, position, address, completion,
			 release_instances, control_instances, kept_instances)
			values ($1, $2, $3, $4, $5, $6, $7)`,
			d.ID, position, target.Address, string(CompletionNotReached),
			target.ReleaseInstances, target.ControlInstances, target.KeptInstances)
		if err != nil {
			return Deploy{}, fmt.Errorf("deploy: writing the row of target %s of %s: %w",
				target.Address, d.ID, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return Deploy{}, fmt.Errorf("deploy: committing the start of %s: %w", d.ID, err)
	}
	return d, nil
}
