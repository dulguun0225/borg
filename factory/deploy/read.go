package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/targetseam"
)

const selectDeploy = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, environment_id, number,
	release_id, build_id, delivered_release_ids, strategy_picked, strategy_performed, status, failed_step,
	schema_change, schema_change_completed, snapshot_name, snapshot_digest, snapshot_deleted_at,
	configuration_digest, way_in_token_digest, control_target,
	failed_release_id, skipped_release_ids, source
	from ` + Table

// Get is the deploy record with that id, or [ErrNotFound]. It takes the pool
// and not a [Writer], because reading a deploy is not a reason to be handed
// the thing that deploys. The walk from a deploy back to its intent starts
// here — the record's release_id is the first field it follows.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Deploy, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeploy+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Deploy{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Deploy{}, fmt.Errorf("deploy: reading %s: %w", id, err)
	}
	return d, nil
}

// Targets is the deploy's completion per target, in the environment's order.
// Every reader of what is running reads a target marked complete, so this is
// what a reader of one deploy reads beside the record.
func Targets(ctx context.Context, pool *pgxpool.Pool, deployID string) ([]Target, error) {
	rows, err := pool.Query(ctx, `select deploy_id, position, address, completion, kept_instances,
		replacement, reached_at, complete_at, replaced_at, instance_hours, amount, rate
		from `+TargetTable+` where deploy_id = $1 order by position`, deployID)
	if err != nil {
		return nil, fmt.Errorf("deploy: reading the targets of %s: %w", deployID, err)
	}
	defer rows.Close()

	var read []Target
	for rows.Next() {
		var t Target
		var completion, replacement string
		var amount, rate *float64
		err := rows.Scan(&t.DeployID, &t.Position, &t.Address, &completion, &t.KeptInstances,
			&replacement, &t.ReachedAt, &t.CompleteAt, &t.ReplacedAt, &t.InstanceHours, &amount, &rate)
		if err != nil {
			return nil, fmt.Errorf("deploy: reading a target of %s: %w", deployID, err)
		}
		t.Completion = Completion(completion)
		t.Replacement = targetseam.Replacement(replacement)
		if amount != nil && rate != nil {
			t.Priced = Priced{Amount: *amount, Rate: *rate, InForce: true}
		}
		read = append(read, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deploy: reading the targets of %s: %w", deployID, err)
	}
	return read, nil
}

// CompleteOnEvery reports whether the deploy is marked complete on every one of
// those addresses. It is the rule every reader of the current release
// evaluates, and it is the strictest one: a release is current only once every
// production target the service runs on is marked complete, and until then the
// previous release is.
func CompleteOnEvery(ctx context.Context, pool *pgxpool.Pool, deployID string, addresses []string) (bool, error) {
	if len(addresses) == 0 {
		return false, nil
	}
	var complete int
	err := pool.QueryRow(ctx, `select count(*) from `+TargetTable+`
		where deploy_id = $1 and address = any($2) and completion = $3`,
		deployID, addresses, string(CompletionComplete)).Scan(&complete)
	if err != nil {
		return false, fmt.Errorf("deploy: reading how much of %s is complete: %w", deployID, err)
	}
	return complete == len(addresses), nil
}

// Current is the service's current release in one environment: the deploy of
// the highest-numbered release marked complete on every one of the addresses
// the service runs on, and false where none is. It takes the pool and not a
// [Writer], because reading what runs is not a reason to be handed the thing
// that deploys.
//
// The number and never the completion time is what orders them, because
// rollouts overlap and differ in length: a short one completing while a longer
// one below it is still widening would make an older release current under a
// recency reading, and nothing later would move it. The sequence number orders
// nothing here either — it is per pair and says which deploy, not which
// release.
//
// It is none once a removal, naming no release and no build, is complete on
// every target and was begun after that release's deploy. A removal is what
// takes a service off an environment, and a reader that ignored it would keep
// naming a release nothing is running.
//
// It reads only deploys that name a release. A candidate deploy names a build
// instead and a candidate environment is a place where nothing is current; a
// search's deploy names a build too, so the service's current release stays the
// rollback's target throughout a search.
func Current(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string, addresses []string) (Deploy, bool, error) {
	if len(addresses) == 0 {
		return Deploy{}, false, nil
	}

	current, found, err := scanOne(ctx, pool, selectDeploy+`
		where service_id = $1 and environment_id = $2 and status = $3 and release_id <> ''
		and (select count(*) from `+TargetTable+` t
			where t.deploy_id = `+Table+`.id and t.address = any($4) and t.completion = $5) = $6
		order by (select number from `+release.Table+` r where r.id = release_id) desc nulls last
		limit 1`,
		serviceID, environmentID, string(StatusComplete), addresses, string(CompletionComplete), len(addresses))
	if err != nil || !found {
		return Deploy{}, false, err
	}

	removal, removed, err := scanOne(ctx, pool, selectDeploy+`
		where service_id = $1 and environment_id = $2 and status = $3
		and release_id = '' and build_id = ''
		and (select count(*) from `+TargetTable+` t
			where t.deploy_id = `+Table+`.id and t.address = any($4) and t.completion = $5) = $6
		order by number desc limit 1`,
		serviceID, environmentID, string(StatusComplete), addresses, string(CompletionComplete), len(addresses))
	if err != nil {
		return Deploy{}, false, err
	}
	if removed && removal.Number > current.Number {
		return Deploy{}, false, nil
	}
	return current, true, nil
}

// ByRelease is every deploy of one release into one environment, oldest first. It
// is what a rollback advances to rolled back — the failed release's own deploys
// and those of every release it skips — and there is more than one where a release
// was deployed, held, and deployed again.
func ByRelease(ctx context.Context, pool *pgxpool.Pool, environmentID, releaseID string) ([]Deploy, error) {
	if releaseID == "" {
		return nil, nil
	}
	return query(ctx, pool, "the deploys of release "+releaseID, selectDeploy+`
		where environment_id = $1 and release_id = $2 order by number, id`, environmentID, releaseID)
}

// Unfinished is every deploy still started, oldest first, whatever the service
// and whatever the environment. It is what the deployer's restart reads: a
// record no target has finished is one the deployer stopped in the middle of,
// and a failed one is left alone.
func Unfinished(ctx context.Context, pool *pgxpool.Pool) ([]Deploy, error) {
	return query(ctx, pool, "the unfinished deploys", selectDeploy+`
		where status = $1 order by at, id`, string(StatusStarted))
}

// Rollbacks is every rollback in the store, oldest first, whatever the service
// and whatever the environment. It is what the score learns from: a rollback is
// an outcome on the release it failed and on every release it skipped, and the
// score asks about every service at once, so a read per service would first have
// to be told which services to ask about.
//
// It reads the failed release for the reason [NewestRollback] does: what makes
// a record a rollback's is that it names what it failed, not its status.
func Rollbacks(ctx context.Context, pool *pgxpool.Pool) ([]Deploy, error) {
	return query(ctx, pool, "the rollbacks", selectDeploy+`
		where failed_release_id <> '' order by at, id`)
}

// NewestRollback is the most recent rollback of one service in one environment,
// and false where none has happened. It is what the hold a rollback leaves is
// computed from: the hold stands until the revert item ships, and the newest
// rollback is the one whose revert is outstanding.
//
// It reads the failed release rather than the status, because a rollback is a
// deploy of the release it returned to and its own targets say so — what makes
// a record a rollback's is that it names what it failed.
func NewestRollback(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string) (Deploy, bool, error) {
	return scanOne(ctx, pool, selectDeploy+`
		where service_id = $1 and environment_id = $2 and failed_release_id <> ''
		order by number desc limit 1`, serviceID, environmentID)
}

func scanOne(ctx context.Context, pool *pgxpool.Pool, statement string, args ...any) (Deploy, bool, error) {
	d, err := scan(pool.QueryRow(ctx, statement, args...))
	if errors.Is(err, pgx.ErrNoRows) {
		return Deploy{}, false, nil
	} else if err != nil {
		return Deploy{}, false, fmt.Errorf("deploy: reading a deploy: %w", err)
	}
	return d, true, nil
}

func query(ctx context.Context, pool *pgxpool.Pool, reading, statement string, args ...any) ([]Deploy, error) {
	rows, err := pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("deploy: reading %s: %w", reading, err)
	}
	defer rows.Close()

	var read []Deploy
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("deploy: reading one of %s: %w", reading, err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deploy: reading %s: %w", reading, err)
	}
	return read, nil
}

func scan(row pgx.Row) (Deploy, error) {
	var d Deploy
	var kind, basis, picked, performed, status, delivered, skipped string
	if err := row.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At, &d.ServiceID, &d.EnvironmentID, &d.Number,
		&d.ReleaseID, &d.BuildID, &delivered, &picked, &performed, &status, &d.FailedStep,
		&d.SchemaChange, &d.SchemaChangeCompleted, &d.Snapshot.Name, &d.Snapshot.Digest, &d.Snapshot.DeletedAt,
		&d.ConfigurationDigest, &d.WayInTokenDigest, &d.ControlTarget,
		&d.Undoing.FailedReleaseID, &skipped, &d.Undoing.Source); err != nil {
		return Deploy{}, err
	}
	d.Actor.Kind = record.Kind(kind)
	d.Actor.Basis = record.Basis(basis)
	d.StrategyPicked = Strategy(picked)
	d.StrategyPerformed = Strategy(performed)
	d.Status = Status(status)
	d.DeliveredReleaseIDs = splitReleases(delivered)
	d.Undoing.SkippedReleaseIDs = splitReleases(skipped)
	return d, nil
}
