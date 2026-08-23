package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
	// ErrNotUndoable is returned by [Writer.Undo] for a deploy already rolled
	// back. A deploy is undone once; a release rolled back twice would be a second
	// rollback of something nothing is running.
	ErrNotUndoable = errors.New("deploy: only a deploy that has not been rolled back is undone")
	// ErrUndoingIncomplete is returned by [Writer.StartUndoing] for a rollback
	// missing something every rollback names, or naming one release as both
	// condemned and swept.
	ErrUndoingIncomplete = errors.New("deploy: the rollback is missing something every rollback names")
)

// Writer is the one writer of deploy records.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Start writes the deploy record: status started, strategy without a control —
// the only strategy a target that moves a process rather than traffic can
// perform, so nothing here chooses one. The environment is the id of an
// environment record, which is what keys a service's current release per
// environment. What names the build the deploy put there and, where the deploy is
// of one, the release.
func (w *Writer) Start(ctx context.Context, actor record.Actor, serviceID, environmentID string, what What) (Deploy, error) {
	return w.start(ctx, actor, serviceID, environmentID, what, Undoing{})
}

// StartUndoing writes the deploy record of a rollback: a deploy of the release it
// returns to, naming what it condemned, what it swept, the source that called for
// it, and the intent it raised. Every other field is an ordinary deploy's, because
// a rollback is a deploy event and not a record of its own — every field it would
// need is on this record already, and a second writer on the fact of what is
// running is the fact the independent checker exists to check.
func (w *Writer) StartUndoing(ctx context.Context, actor record.Actor, serviceID, environmentID string,
	what What, undoing Undoing) (Deploy, error) {
	if !undoing.Any() {
		return Deploy{}, ErrUndoingIncomplete
	}
	if undoing.Source == "" {
		return Deploy{}, fmt.Errorf("%w: it names no source", ErrUndoingIncomplete)
	}
	for _, swept := range undoing.SweptReleaseIDs {
		if swept == "" {
			return Deploy{}, fmt.Errorf("%w: one of the releases it swept", ErrUndoingIncomplete)
		}
		if swept == undoing.CondemnedReleaseID {
			return Deploy{}, fmt.Errorf("%w: %s is condemned and swept, and the two are kept apart",
				ErrUndoingIncomplete, swept)
		}
	}
	return w.start(ctx, actor, serviceID, environmentID, what, undoing)
}

func (w *Writer) start(ctx context.Context, actor record.Actor, serviceID, environmentID string,
	what What, undoing Undoing) (Deploy, error) {
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
		Strategy:      StrategyWithoutControl,
		Status:        StatusStarted,
		Undoing:       undoing,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, service_id, environment_id, release_id, build_id, strategy, status,
		 condemned_release_id, swept_release_ids, source, revert_intent_id)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
		d.ID, string(d.Actor.Kind), d.Actor.Name, d.At, d.ServiceID, d.EnvironmentID,
		d.ReleaseID, d.BuildID, string(d.Strategy), string(d.Status),
		d.Undoing.CondemnedReleaseID, joinReleases(d.Undoing.SweptReleaseIDs),
		d.Undoing.Source, d.Undoing.RevertIntentID,
	)
	if err != nil {
		return Deploy{}, fmt.Errorf("deploy: starting %s: %w", d.ID, err)
	}
	return d, nil
}

// Complete advances the deploy from started to complete. Any other starting
// state is refused: complete does not advance further here, and rolled back is
// [Writer.Undo]'s.
//
// It takes no actor, where every other mutating writer validates one. The
// reason is that the record already names the deploy agent that performed the
// deploy, and completion is that same agent advancing its own record — there
// is no second party to name. [Writer.Undo] takes none either, and for a reason
// that had to be found rather than assumed: a rollback does name a source beside
// its actor, but the source is a fact of the rollback and is written on the
// rollback's own record, so the deploys that rollback undoes carry no second
// copy of it.
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

// Undo advances a deploy to rolled back, which is the transition [Writer.Complete]
// predicted and this milestone writes. It is what happens to the deploy of the
// condemned release and to the deploy of every release the same rollback swept.
//
// It takes no source, where the rollback's own record does. The source is a fact
// of the rollback and is written once, on the record of the rollback that named it —
// so a reader asking why a deploy was undone follows the rollback rather than
// finding the reason copied onto every deploy the same event touched.
//
// A started deploy is undone as readily as a completed one: a release whose deploy
// never completed can still be the one a comparison condemns, and leaving it
// started would say the factory is still deploying it.
func (w *Writer) Undo(ctx context.Context, id string) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("deploy: beginning the rollback of %s: %w", id, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var status string
	err = tx.QueryRow(ctx, `select status from `+Table+` where id = $1 for update`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return fmt.Errorf("deploy: reading %s: %w", id, err)
	}
	if Status(status) == StatusRolledBack {
		return fmt.Errorf("%w: %s is %s", ErrNotUndoable, id, status)
	}

	if _, err := tx.Exec(ctx, `update `+Table+` set status = $1 where id = $2`, string(StatusRolledBack), id); err != nil {
		return fmt.Errorf("deploy: rolling back %s: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("deploy: committing the rollback of %s: %w", id, err)
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
	release_id, build_id, strategy, status,
	condemned_release_id, swept_release_ids, source, revert_intent_id
	from ` + Table

func scan(row pgx.Row) (Deploy, error) {
	var d Deploy
	var kind, strategy, status, swept string
	if err := row.Scan(&d.ID, &kind, &d.Actor.Name, &d.At, &d.ServiceID, &d.EnvironmentID,
		&d.ReleaseID, &d.BuildID, &strategy, &status,
		&d.Undoing.CondemnedReleaseID, &swept, &d.Undoing.Source, &d.Undoing.RevertIntentID); err != nil {
		return Deploy{}, err
	}
	d.Actor.Kind = record.Kind(kind)
	d.Strategy = Strategy(strategy)
	d.Status = Status(status)
	d.Undoing.SweptReleaseIDs = splitReleases(swept)
	return d, nil
}

// The releases a rollback swept are stored as one column holding one id per line,
// the arrangement item's waits_on and environment's targets already have: an id is
// record.NewID's alphabet, which holds no line ending, so the separator needs no
// escaping. It is a column rather than a table because what reads it reads all of
// one rollback's at once, and a table would be a row per edge for a list bounded
// by the window limit.

func joinReleases(ids []string) string { return strings.Join(ids, "\n") }

func splitReleases(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
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
// which one caller deploying one at a time — the crude interface — does
// not produce.
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

// ByRelease is every deploy of one release into one environment, oldest first. It
// is what a rollback advances to rolled back — the condemned release's own deploys
// and those of every release it sweeps — and there is more than one where a release
// was deployed, held, and deployed again.
func ByRelease(ctx context.Context, pool *pgxpool.Pool, environmentID, releaseID string) ([]Deploy, error) {
	if releaseID == "" {
		return nil, nil
	}
	rows, err := pool.Query(ctx, selectDeploy+`
		where environment_id = $1 and release_id = $2 order by at, id`, environmentID, releaseID)
	if err != nil {
		return nil, fmt.Errorf("deploy: reading the deploys of release %s: %w", releaseID, err)
	}
	defer rows.Close()

	var read []Deploy
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("deploy: reading a deploy of release %s: %w", releaseID, err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deploy: reading the deploys of release %s: %w", releaseID, err)
	}
	return read, nil
}

// Rollbacks is every rollback in the store, oldest first, whatever the service
// and whatever the environment. It is what the score learns from: a rollback is
// an outcome on the release it condemned and on every release it swept, and the
// score asks about every service at once, so a read per service would first have
// to be told which services to ask about.
//
// It reads the condemned release for the reason [NewestRollback] does: what makes
// a record a rollback's is that it names what it condemned, not its status.
func Rollbacks(ctx context.Context, pool *pgxpool.Pool) ([]Deploy, error) {
	rows, err := pool.Query(ctx, selectDeploy+`
		where condemned_release_id <> '' order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("deploy: reading the rollbacks: %w", err)
	}
	defer rows.Close()

	var read []Deploy
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("deploy: reading a rollback: %w", err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deploy: reading the rollbacks: %w", err)
	}
	return read, nil
}

// NewestRollback is the most recent rollback of one service in one environment,
// and false where none has happened. It is what the hold a rollback leaves is
// computed from: the hold stands until the item decomposed from that rollback's revert
// intent has a release running, and the newest rollback is the one whose revert is
// outstanding.
//
// It reads the condemned release rather than the status, because a rollback is a
// completed deploy of the release it returned to and its status says so — what
// makes a record a rollback's is that it names what it condemned.
func NewestRollback(ctx context.Context, pool *pgxpool.Pool, serviceID, environmentID string) (Deploy, bool, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeploy+`
		where service_id = $1 and environment_id = $2 and condemned_release_id <> ''
		order by at desc limit 1`, serviceID, environmentID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Deploy{}, false, nil
	} else if err != nil {
		return Deploy{}, false, fmt.Errorf("deploy: reading the newest rollback of %s: %w", serviceID, err)
	}
	return d, true, nil
}
