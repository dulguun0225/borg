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
	schema_changes, schema_changes_completed, snapshot_name, snapshot_digest, snapshot_deleted_at,
	configuration_digest, way_in_token_digest, control_target, control_release_id,
	backfill_contract, backfill_element, backfill_from_element,
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
	rows, err := pool.Query(ctx, `select deploy_id, position, address, completion,
		release_instances, control_instances, kept_instances, replacement, reached_at, complete_at,
		release_torn_down_at, control_torn_down_at, kept_torn_down_at,
		release_instance_hours, control_instance_hours, kept_instance_hours,
		instance_hours, amount, rate
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
		err := rows.Scan(&t.DeployID, &t.Position, &t.Address, &completion,
			&t.Fleets.Release.Instances, &t.Fleets.Control.Instances, &t.Fleets.Kept.Instances,
			&replacement, &t.ReachedAt, &t.CompleteAt,
			&t.Fleets.Release.TornDownAt, &t.Fleets.Control.TornDownAt, &t.Fleets.Kept.TornDownAt,
			&t.Fleets.Release.Hours, &t.Fleets.Control.Hours, &t.Fleets.Kept.Hours,
			&t.InstanceHours, &amount, &rate)
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
//
// The record's own status is not read. Completion on the addresses the service
// runs on is the whole of the rule, and the status says something else: it is
// complete once every target of the record is, which on an environment holding
// targets the service does not run on is a stricter thing than the design's. A
// record marked failed cannot match either, no target of one being complete, and
// a rollback advances the targets it undoes to rolled back, so a deploy the
// rollback has reached is no longer complete on them.
func Current(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string, addresses []string) (Deploy, bool, error) {
	if len(addresses) == 0 {
		return Deploy{}, false, nil
	}

	current, found, err := scanOne(ctx, pool, selectDeploy+`
		where service_id = $1 and environment_id = $2 and release_id <> ''
		and (select count(*) from `+TargetTable+` t
			where t.deploy_id = `+Table+`.id and t.address = any($3) and t.completion = $4) = $5
		order by (select number from `+release.Table+` r where r.id = release_id) desc nulls last
		limit 1`,
		serviceID, environmentID, addresses, string(CompletionComplete), len(addresses))
	if err != nil || !found {
		return Deploy{}, false, err
	}

	removal, removed, err := scanOne(ctx, pool, selectDeploy+`
		where service_id = $1 and environment_id = $2
		and release_id = '' and build_id = ''
		and (select count(*) from `+TargetTable+` t
			where t.deploy_id = `+Table+`.id and t.address = any($3) and t.completion = $4) = $5
		order by number desc limit 1`,
		serviceID, environmentID, addresses, string(CompletionComplete), len(addresses))
	if err != nil {
		return Deploy{}, false, err
	}
	if removed && removal.Number > current.Number {
		return Deploy{}, false, nil
	}
	return current, true, nil
}

// BackfillComplete is the deploy record that marks the backfill for one element
// of one store contract complete, and false where none does. The element is
// either side of the pair a backfill fills: the one it filled and the one it
// filled from are one backfill, and the deployer's record names both.
//
// A backfill's deploy record is marked complete only once every row the old form
// holds is present in the new, so a complete record naming the element is the
// fact enforcement reads before it admits the item that moves reads to that
// element and the drop after it. A started record is not one: the copy is still
// running, and a release reading the new form while it runs reads every row it
// has not reached as absent.
func BackfillComplete(ctx context.Context, pool *pgxpool.Pool, serviceID, contractName, element string) (string, bool, error) {
	if serviceID == "" || contractName == "" || element == "" {
		return "", false, nil
	}
	var id string
	err := pool.QueryRow(ctx, `select id from `+Table+`
		where service_id = $1 and status = $2 and backfill_contract = $3
		and (backfill_element = $4 or backfill_from_element = $4)
		order by number desc limit 1`,
		serviceID, string(StatusComplete), contractName, element).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	} else if err != nil {
		return "", false, fmt.Errorf("deploy: reading whether %s of %s is backfilled: %w",
			element, contractName, err)
	}
	return id, true, nil
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
	var kind, basis, picked, performed, status, delivered, skipped, changes string
	if err := row.Scan(&d.ID, &kind, &d.Actor.Key, &basis, &d.At, &d.ServiceID, &d.EnvironmentID, &d.Number,
		&d.ReleaseID, &d.BuildID, &delivered, &picked, &performed, &status, &d.FailedStep,
		&changes, &d.SchemaChangesCompleted, &d.Snapshot.Name, &d.Snapshot.Digest, &d.Snapshot.DeletedAt,
		&d.ConfigurationDigest, &d.WayInTokenDigest, &d.ControlTarget, &d.ControlReleaseID,
		&d.Backfill.Contract, &d.Backfill.Element, &d.Backfill.FromElement,
		&d.Undoing.FailedReleaseID, &skipped, &d.Undoing.Source); err != nil {
		return Deploy{}, err
	}
	d.SchemaChanges = splitLines(changes)
	d.Actor.Kind = record.Kind(kind)
	d.Actor.Basis = record.Basis(basis)
	d.StrategyPicked = Strategy(picked)
	d.StrategyPerformed = Strategy(performed)
	d.Status = Status(status)
	d.DeliveredReleaseIDs = splitLines(delivered)
	d.Undoing.SkippedReleaseIDs = splitLines(skipped)
	return d, nil
}
