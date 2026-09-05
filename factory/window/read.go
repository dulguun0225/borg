package window

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// Every read here takes the pool and not a [Writer], because reading a window is
// not a reason to be handed the thing that opens and closes them. That is the
// arrangement every record package in the factory has.

const selectWindow = `select id, actor_kind, actor_key, actor_key_basis, at,
	deploy_id, release_id, build_id, service_id, measures_nothing, passed_available, held_out,
	sizes, powers, confidence, cap_seconds, boundary_version, targets, operations_read_alone,
	emission_version_release, emission_version_control, quantities_outside,
	own_history_sizes, own_history_run_length, threshold_sizes, threshold_run_length,
	policy_version, score_version, exit, closed_at, closed_on, finest_size_reached
	from ` + Table

func scan(row pgx.Row) (Window, error) {
	var w Window
	var kind, basis, exit string
	var sizes, powers, targets, operations, outside string
	var ownHistorySizes, thresholdSizes, closedOn, finest string
	err := row.Scan(&w.ID, &kind, &w.Actor.Key, &basis, &w.At,
		&w.DeployID, &w.ReleaseID, &w.BuildID, &w.ServiceID,
		&w.MeasuresNothing, &w.PassedAvailable, &w.HeldOut,
		&sizes, &powers, &w.Confidence, &w.CapSeconds, &w.BoundaryVersion, &targets, &operations,
		&w.EmissionVersionRelease, &w.EmissionVersionControl, &outside,
		&ownHistorySizes, &w.OwnHistoryRunLength, &thresholdSizes, &w.ThresholdRunLength,
		&w.PolicyVersion, &w.ScoreVersion, &exit, &w.ClosedAt, &closedOn, &finest)
	if err != nil {
		return Window{}, err
	}
	w.Actor.Kind = record.Kind(kind)
	w.Actor.Basis = record.Basis(basis)
	w.Exit = Exit(exit)
	w.Targets = decodeNames(targets)
	w.OperationsReadAlone = decodeNames(operations)
	w.QuantitiesOutside = decodeQuantities(outside)
	for _, into := range []struct {
		stored string
		field  *map[gatepolicy.Quantity]float64
	}{
		{sizes, &w.Size}, {powers, &w.Power},
		{ownHistorySizes, &w.OwnHistorySize}, {thresholdSizes, &w.ThresholdSize},
		{finest, &w.FinestSizeReached},
	} {
		shares, err := decodeShares(into.stored)
		if err != nil {
			return Window{}, err
		}
		*into.field = shares
	}
	if w.ClosedOn, err = decodeRead(closedOn); err != nil {
		return Window{}, err
	}
	return w, nil
}

// Get is one window by id.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Window, error) {
	w, err := scan(pool.QueryRow(ctx, selectWindow+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Window{}, fmt.Errorf("%w: id %s", ErrNotFound, id)
	} else if err != nil {
		return Window{}, fmt.Errorf("window: reading %s: %w", id, err)
	}
	return w, nil
}

// ForRelease is the window of one release, and false where the release has
// never been watched. One release is watched once, so there is at most one — and
// this is what says whether a deploy of that release opens a window at all.
func ForRelease(ctx context.Context, pool *pgxpool.Pool, releaseID string) (Window, bool, error) {
	if releaseID == "" {
		return Window{}, false, nil
	}
	w, err := scan(pool.QueryRow(ctx, selectWindow+` where release_id = $1`, releaseID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Window{}, false, nil
	} else if err != nil {
		return Window{}, false, fmt.Errorf("window: reading the window of release %s: %w", releaseID, err)
	}
	return w, true, nil
}

// ForDeploy is the window opened over one deploy, and false where none was.
func ForDeploy(ctx context.Context, pool *pgxpool.Pool, deployID string) (Window, bool, error) {
	if deployID == "" {
		return Window{}, false, nil
	}
	w, err := scan(pool.QueryRow(ctx, selectWindow+` where deploy_id = $1`, deployID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Window{}, false, nil
	} else if err != nil {
		return Window{}, false, fmt.Errorf("window: reading the window over deploy %s: %w", deployID, err)
	}
	return w, true, nil
}

// AllOpen is every open window of one service, oldest first. That order is the
// order they opened, which is the order a rollback skips over and the order the
// health monitor evaluates them in.
func AllOpen(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Window, error) {
	return list(ctx, pool, serviceID, ` and exit = '' order by at, id`, "the open windows")
}

// CountOpen is how many windows the service holds open, which is what the window
// limit is compared against. The limit itself is what an owner authored on the
// service record or what the score supplies, read through package policy — not a
// field here.
func CountOpen(ctx context.Context, pool *pgxpool.Pool, serviceID string) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+`
		where service_id = $1 and exit = ''`, serviceID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("window: counting the open windows of %s: %w", serviceID, err)
	}
	return count, nil
}

// ClosedPassedOrTimedOut is every window of the service that closed at one of
// those two exits, newest close first. It is what both the last known-good
// release and a rollback's target are computed from, and neither is computed
// here: the order is the release's number, which this package does not read, and
// so is the deploy record that says whether the release's build ever took
// traffic, which both queries also descend past.
//
// It names the two exits it admits rather than the three that close without
// failing the release, because skipped leaves nothing running to return to.
func ClosedPassedOrTimedOut(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Window, error) {
	return list(ctx, pool, serviceID,
		` and exit in ('`+string(ExitPassed)+`', '`+string(ExitTimedOut)+`') order by closed_at desc, id`,
		"the windows that closed passed or timed out")
}

// LastKnownGood is the window whose release is the service's last known-good
// release: the newest closed window whose exit is passed or timed out. It is the
// standing value, where a rollback's target is computed for one rollback, and it
// is false where the service has none.
//
// The caller still has to descend past a release whose deploy stopped before its
// build took traffic, which is a fact of the deploy record and not of this one.
func LastKnownGood(ctx context.Context, pool *pgxpool.Pool, serviceID string) (Window, bool, error) {
	closed, err := ClosedPassedOrTimedOut(ctx, pool, serviceID)
	if err != nil || len(closed) == 0 {
		return Window{}, false, err
	}
	return closed[0], true, nil
}

// All is every window of one service, oldest open first. It is what a reader of
// the service's history walks, and what the command-line interface prints.
func All(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Window, error) {
	return list(ctx, pool, serviceID, ` order by at, id`, "the windows")
}

// Closed is every closed window of every service, oldest close first. It is the
// one read here that is not per service: what the score learns from a window is
// its exit, and the subjects it learns about are the services the windows name,
// so a reader asking per service would first have to be told which services to
// ask about.
//
// Open windows are left out because an open window has no exit and so says
// nothing about an outcome yet. The whole table is read at once, which is what
// learning over every outcome costs while the store is small.
func Closed(ctx context.Context, pool *pgxpool.Pool) ([]Window, error) {
	return query(ctx, pool, selectWindow+` where exit <> '' order by closed_at, id`, "the closed windows")
}

// list is every read that returns more than one window of one service. The
// suffix is a constant of this package at each call site and never input, so
// writing it into the statement is not a place anything can be injected.
func list(ctx context.Context, pool *pgxpool.Pool, serviceID, suffix, what string) ([]Window, error) {
	return query(ctx, pool, selectWindow+` where service_id = $1`+suffix, what, serviceID)
}

func query(ctx context.Context, pool *pgxpool.Pool, statement, what string, args ...any) ([]Window, error) {
	rows, err := pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("window: reading %s: %w", what, err)
	}
	defer rows.Close()

	var read []Window
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("window: reading a window: %w", err)
		}
		read = append(read, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("window: reading %s: %w", what, err)
	}
	return read, nil
}
