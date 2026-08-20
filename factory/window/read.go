package window

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Every read here takes the pool and not a [Writer], because reading a window is
// not a reason to be handed the thing that opens and closes them. That is the
// arrangement every record package in the factory has.

const selectWindow = `select id, actor_kind, actor_name, at, deploy_id, release_id, service_id,
	clean_available, held_out, size, confidence, cap_seconds, formula, policy_version, score_version,
	exit, closed_at, closed_on_units, closed_on_failures,
	closed_on_baseline_units, closed_on_baseline_failures
	from ` + Table

func scan(row pgx.Row) (Window, error) {
	var w Window
	var kind, exit string
	err := row.Scan(&w.ID, &kind, &w.Actor.Name, &w.At, &w.DeployID, &w.ReleaseID, &w.ServiceID,
		&w.CleanAvailable, &w.HeldOut, &w.Size, &w.Confidence, &w.CapSeconds, &w.Formula,
		&w.PolicyVersion, &w.ScoreVersion, &exit, &w.ClosedAt,
		&w.ClosedOn.Units, &w.ClosedOn.Failures,
		&w.ClosedOn.BaselineUnits, &w.ClosedOn.BaselineFailures)
	if err != nil {
		return Window{}, err
	}
	w.Actor.Kind = record.Kind(kind)
	w.Exit = Exit(exit)
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
// order they opened, which is the order a rollback sweeps and the order the
// comparison evaluates them in.
func AllOpen(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Window, error) {
	return list(ctx, pool, serviceID, ` and exit = '' order by at, id`, "the open windows")
}

// CountOpen is how many windows the service holds open, which is what K is
// compared against. K itself is what an owner authored on the service record or
// what the score supplies, read through package policy — not a field here.
func CountOpen(ctx context.Context, pool *pgxpool.Pool, serviceID string) (int, error) {
	var count int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+`
		where service_id = $1 and exit = ''`, serviceID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("window: counting the open windows of %s: %w", serviceID, err)
	}
	return count, nil
}

// ClosedWithoutHarm is every window of the service whose exit leaves a release
// the factory can return to — clean or at the cap, which is what [Exit.Counts]
// says. It is what both the restore floor and a rollback's target are computed
// from, and neither is computed here: the order is the release's number, which
// this package does not read, and copying that number onto a window would be one
// fact in two places able to disagree.
//
// The rows come back newest close first, which is a stable order and not the
// answer to either question — a caller ordering by number is the answer.
func ClosedWithoutHarm(ctx context.Context, pool *pgxpool.Pool, serviceID string) ([]Window, error) {
	return list(ctx, pool, serviceID,
		` and exit in ('`+string(ExitClean)+`', '`+string(ExitCap)+`') order by closed_at desc, id`,
		"the windows closed without harm")
}

// All is every window of one service, oldest open first. It is what a reader of
// the service's history walks, and what the crude interface prints.
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
// learning over every outcome costs while the store is small, and it is the same
// cost the decision log's own whole-log read already carries.
func Closed(ctx context.Context, pool *pgxpool.Pool) ([]Window, error) {
	rows, err := pool.Query(ctx, selectWindow+` where exit <> '' order by closed_at, id`)
	if err != nil {
		return nil, fmt.Errorf("window: reading the closed windows: %w", err)
	}
	defer rows.Close()

	var read []Window
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("window: reading a closed window: %w", err)
		}
		read = append(read, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("window: reading the closed windows: %w", err)
	}
	return read, nil
}

// list is every read that returns more than one window. The suffix is a constant
// of this package at each call site and never input, so writing it into the
// statement is not a place anything can be injected.
func list(ctx context.Context, pool *pgxpool.Pool, serviceID, suffix, what string) ([]Window, error) {
	rows, err := pool.Query(ctx, selectWindow+` where service_id = $1`+suffix, serviceID)
	if err != nil {
		return nil, fmt.Errorf("window: reading %s of %s: %w", what, serviceID, err)
	}
	defer rows.Close()

	var read []Window
	for rows.Next() {
		w, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("window: reading a window of %s: %w", serviceID, err)
		}
		read = append(read, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("window: reading %s of %s: %w", what, serviceID, err)
	}
	return read, nil
}
