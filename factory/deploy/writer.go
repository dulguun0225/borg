package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrEnvironmentEmpty is returned by [Writer.Start] for a deploy naming
	// no environment.
	ErrEnvironmentEmpty = errors.New("deploy: the environment is empty")
	// ErrServiceIDEmpty is returned by [Writer.Start] for a deploy naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("deploy: the service id is empty")
	// ErrBuildIDEmpty is returned by [Writer.Start] for a deploy naming no
	// build. Every deploy names the build it put there; the release is what a
	// candidate deploy has none of.
	ErrBuildIDEmpty = errors.New("deploy: the build id is empty")
	// ErrNotFound is returned where the named deploy does not exist.
	ErrNotFound = errors.New("deploy: no deploy has that id")
	// ErrNotStarted is returned by [Writer.Complete] for a deploy whose
	// status is not started. Complete does not run twice, and nothing here
	// un-completes.
	ErrNotStarted = errors.New("deploy: only a started deploy is completed")
)

// Writer is the one writer of deploy records.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Start writes the deploy record: status started, strategy straight — straight
// being the one strategy until the health signal and the control are built, and
// on a substrate that moves a process rather than traffic there is no other. The
// environment is the id of an environment record, which is what keys a service's
// current release per environment. What names the build the deploy put there and,
// where the deploy is of one, the release.
func (w *Writer) Start(ctx context.Context, actor record.Actor, serviceID, environmentID string, what What) (Deploy, error) {
	if err := actor.Validate(); err != nil {
		return Deploy{}, err
	}
	if serviceID == "" {
		return Deploy{}, ErrServiceIDEmpty
	}
	if environmentID == "" {
		return Deploy{}, ErrEnvironmentEmpty
	}
	if what.BuildID == "" {
		return Deploy{}, ErrBuildIDEmpty
	}

	d := Deploy{
		ID:            record.NewID(IDPrefix),
		Actor:         actor,
		At:            record.Now(),
		ServiceID:     serviceID,
		EnvironmentID: environmentID,
		ReleaseID:     what.ReleaseID,
		BuildID:       what.BuildID,
		Strategy:      StrategyStraight,
		Status:        StatusStarted,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, service_id, environment_id, release_id, build_id, strategy, status)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		d.ID, string(d.Actor.Kind), d.Actor.Name, d.At, d.ServiceID, d.EnvironmentID,
		d.ReleaseID, d.BuildID, string(d.Strategy), string(d.Status),
	)
	if err != nil {
		return Deploy{}, fmt.Errorf("deploy: starting %s: %w", d.ID, err)
	}
	return d, nil
}

// Complete advances the deploy from started to complete, the one transition
// this package writes. Any other starting state is refused: complete does not
// advance further here, and rolled back is M4's write.
//
// It takes no actor, where every other mutating writer validates one. The
// reason is that the record already names the deploy agent that performed the
// deploy, and completion is that same agent advancing its own record — there
// is no second party to name. What this does not say is that a status change
// needs no actor. The design's rollback keeps the actor the deploy agent and
// names a source beside it — the comparison at the watch window's harm exit,
// or the named human at Ops with the reason they state — so the rolled-back
// transition M4 adds beside this one takes that source, which is what its
// caller has that this one's does not.
func (w *Writer) Complete(ctx context.Context, id string) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("deploy: beginning the completion of %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `select status from `+Table+` where id = $1 for update`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return fmt.Errorf("deploy: reading %s: %w", id, err)
	}
	if Status(status) != StatusStarted {
		return fmt.Errorf("%w: %s is %s", ErrNotStarted, id, status)
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set status = $1 where id = $2`, string(StatusComplete), id); err != nil {
		return fmt.Errorf("deploy: completing %s: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("deploy: committing the completion of %s: %w", id, err)
	}
	return nil
}

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

const selectDeploy = `select id, actor_kind, actor_name, at, service_id, environment_id,
	release_id, build_id, strategy, status
	from ` + Table

func scan(row pgx.Row) (Deploy, error) {
	var d Deploy
	var kind, strategy, status string
	if err := row.Scan(&d.ID, &kind, &d.Actor.Name, &d.At, &d.ServiceID, &d.EnvironmentID,
		&d.ReleaseID, &d.BuildID, &strategy, &status); err != nil {
		return Deploy{}, err
	}
	d.Actor.Kind = record.Kind(kind)
	d.Strategy = Strategy(strategy)
	d.Status = Status(status)
	return d, nil
}

// Current is the most recently completed deploy for the service and
// environment, or false where nothing has completed there. It takes the pool
// and not a [Writer], because reading what runs is not a reason to be handed
// the thing that deploys.
//
// A service's current release is the one this names — what is running, not
// what is newest — so a deploy that started and has not completed does not
// change the answer, and neither does a release minted and never deployed.
// Completed deploys are ordered by at, the time each record was written: the
// record advances in place, so when it completed is not a stored fact. The
// two orders differ only where a deploy completes after a later-started one,
// which one caller deploying one at a time — the crude path — does not
// produce.
//
// It reads only the deploys that name a release. A candidate deploy names a build
// instead, and a candidate environment is a place where nothing is current: what
// composes a dependent's environment reads what is running in production, and a
// candidate never is.
func Current(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string) (Deploy, bool, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeploy+`
		where service_id = $1 and environment_id = $2 and status = $3 and release_id <> ''
		order by at desc limit 1`,
		serviceID, environmentID, string(StatusComplete)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Deploy{}, false, nil
	} else if err != nil {
		return Deploy{}, false, fmt.Errorf("deploy: reading the current deploy of %s in %s: %w", serviceID, environmentID, err)
	}
	return d, true, nil
}
