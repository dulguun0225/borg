package deploy

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrEnvironmentEmpty is returned by [Writer.Start] for a deploy naming
	// no environment.
	ErrEnvironmentEmpty = errors.New("deploy: the environment is empty")
	// ErrServiceIDEmpty is returned by [Writer.Start] for a deploy naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("deploy: the service id is empty")
	// ErrBuildIDEmpty is returned by [Writer.Start] for a deploy naming a
	// release and no build. A removal names neither and is admitted; every other
	// deploy names the build it put there.
	ErrBuildIDEmpty = errors.New("deploy: the deploy names a release and no build")
	// ErrNoTargets is returned by [Writer.Start] for a deploy naming no target.
	// Completion is per target, so a deploy with none could never complete and
	// no reader could ever read it as running anywhere.
	ErrNoTargets = errors.New("deploy: the deploy names no target")
	// ErrStrategyNotProduction is returned for a strategy on a deploy that is
	// not into production, and for a production deploy naming none. A strategy
	// attaches to a production deploy and to no other.
	ErrStrategyNotProduction = errors.New("deploy: a strategy attaches to a production deploy and to no other")
	// ErrNotFound is returned where the named deploy does not exist.
	ErrNotFound = errors.New("deploy: no deploy has that id")
	// ErrTargetNotFound is returned where the deploy has no row for that
	// address.
	ErrTargetNotFound = errors.New("deploy: the deploy names no such target")
	// ErrNotStarted is returned by the writes that advance a started deploy.
	// Neither completion nor failure runs twice, and nothing here un-completes.
	ErrNotStarted = errors.New("deploy: only a started deploy is advanced")
	// ErrTargetsIncomplete is returned by [Writer.Complete] for a deploy some
	// target of which is not complete. The record as a whole is complete when
	// every target is.
	ErrTargetsIncomplete = errors.New("deploy: a target of the deploy is not complete")
	// ErrATargetCompleted is returned by [Writer.MarkFailed] for a deploy a
	// target of which is already complete. Failed is where the deployer stopped
	// before any target was complete; a deploy that reached one and stopped is a
	// recorded partial deploy and stays started.
	ErrATargetCompleted = errors.New("deploy: a deploy with a target complete is not marked failed")
	// ErrUndoingIncomplete is returned by [Writer.StartUndoing] for a rollback
	// missing something every rollback names, or naming one release as both
	// failed and skipped.
	ErrUndoingIncomplete = errors.New("deploy: the rollback is missing something every rollback names")
	// ErrNoSnapshot is returned by [Writer.DeleteSnapshot] for a record naming
	// no copy. There is nothing for a deletion to stand beside.
	ErrNoSnapshot = errors.New("deploy: the deploy names no snapshot")
)

// Writer is the one writer of the deploy record, its completion per target, and
// the mitigation. All three are the deployer's.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Pool is the pool this writer holds, for the reads a deploy makes of its own
// records while it runs. Reading a deploy needs no writer; performing one needs
// both.
func (w *Writer) Pool() *pgxpool.Pool { return w.pool }

// Reaching is one target the deploy will reach: its address, and how many
// instances of the release a rollback of this one would return to the deployer
// is keeping there. The order of the slice is the environment's own, which is
// the order the deployer reaches them in.
type Reaching struct {
	Address       string
	KeptInstances int
}

// Beginning is what the deployer names when it begins a deploy. Everything on
// it is written at the start, which is when the window opens over the deploy:
// the targets with their kept-instance counts, the digests, and the strategy
// where the deploy is into production.
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
	// SchemaChange is the change the build carries, and empty where it carries
	// none.
	SchemaChange string
	// ConfigurationDigest is over the resolved value set the build runs under.
	ConfigurationDigest string
	// WayInTokenDigest is the digest of the token minted for the way in.
	WayInTokenDigest string
	// ControlTarget is the target a control runs on, under a strategy with one.
	ControlTarget string
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
		StrategyPerformed:   b.StrategyPicked,
		Status:              StatusStarted,
		SchemaChange:        b.SchemaChange,
		ConfigurationDigest: b.ConfigurationDigest,
		WayInTokenDigest:    b.WayInTokenDigest,
		ControlTarget:       b.ControlTarget,
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
		 schema_change, configuration_digest, way_in_token_digest, control_target,
		 failed_release_id, skipped_release_ids, source)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22)`,
		d.ID, FormatVersion, string(d.Actor.Kind), d.Actor.Key, string(d.Actor.Basis), d.At,
		d.ServiceID, d.EnvironmentID, d.Number, d.ReleaseID, d.BuildID, joinReleases(d.DeliveredReleaseIDs),
		string(d.StrategyPicked), string(d.StrategyPerformed), string(d.Status),
		d.SchemaChange, d.ConfigurationDigest, d.WayInTokenDigest, d.ControlTarget,
		d.Undoing.FailedReleaseID, joinReleases(d.Undoing.SkippedReleaseIDs), d.Undoing.Source,
	)
	if err != nil {
		return Deploy{}, fmt.Errorf("deploy: starting %s: %w", d.ID, err)
	}

	for position, target := range b.Targets {
		_, err = tx.Exec(ctx, `insert into `+TargetTable+`
			(deploy_id, position, address, completion, kept_instances)
			values ($1, $2, $3, $4, $5)`,
			d.ID, position, target.Address, string(CompletionNotReached), target.KeptInstances)
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

// Complete advances the deploy from started to complete, and refuses a deploy
// any target of which is not complete: the record as a whole is complete when
// every target is, which is also when the release it names becomes current.
func (w *Writer) Complete(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "completing "+id, func(tx pgx.Tx) error {
		status, err := lockStatus(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != StatusStarted {
			return fmt.Errorf("%w: %s is %s", ErrNotStarted, id, status)
		}
		var unfinished int
		err = tx.QueryRow(ctx, `select count(*) from `+TargetTable+`
			where deploy_id = $1 and completion <> $2`, id, string(CompletionComplete)).Scan(&unfinished)
		if err != nil {
			return err
		}
		if unfinished != 0 {
			return fmt.Errorf("%w: %d of %s", ErrTargetsIncomplete, unfinished, id)
		}
		_, err = tx.Exec(ctx, `update `+Table+` set status = $1 where id = $2`, string(StatusComplete), id)
		return err
	})
}

// MarkFailed marks the record failed at the step that stopped it, and refuses
// where a target is already complete. A failure stands for Ops here, the
// restart leaves it alone, and both queries over overlapping windows descend
// past it — all three read a record no target completed.
func (w *Writer) MarkFailed(ctx context.Context, id, step string) error {
	if step == "" {
		return fmt.Errorf("%w: %s names no step", ErrNotStarted, id)
	}
	return w.inTransaction(ctx, "failing "+id, func(tx pgx.Tx) error {
		status, err := lockStatus(ctx, tx, id)
		if err != nil {
			return err
		}
		if status != StatusStarted {
			return fmt.Errorf("%w: %s is %s", ErrNotStarted, id, status)
		}
		var complete int
		err = tx.QueryRow(ctx, `select count(*) from `+TargetTable+`
			where deploy_id = $1 and completion = $2`, id, string(CompletionComplete)).Scan(&complete)
		if err != nil {
			return err
		}
		if complete != 0 {
			return fmt.Errorf("%w: %d of %s", ErrATargetCompleted, complete, id)
		}
		_, err = tx.Exec(ctx, `update `+Table+` set status = $1, failed_step = $2 where id = $3`,
			string(StatusFailed), step, id)
		return err
	})
}

// PerformedWithoutControl records that the deployer performed the row without a
// control on a deploy the score picked one for, which is what a target declared
// as serving a share refusing the shift produces. The picked field is left as
// it was, so an owner reading a rollout that ran no comparison reads on one
// record whether the platform was the reason.
func (w *Writer) PerformedWithoutControl(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "recording the strategy performed on "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+Table+` set strategy_performed = $1
			where id = $2 and strategy_picked <> ''`, string(StrategyWithoutControl), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s is not a production deploy", ErrStrategyNotProduction, id)
		}
		return nil
	})
}

// inTransaction runs one write, fenced with this writer's token, so that the
// refusal and the write commit together or neither does.
func (w *Writer) inTransaction(ctx context.Context, doing string, write func(pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("deploy: beginning %s: %w", doing, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return err
	}
	if err := write(tx); err != nil {
		if errors.Is(err, ErrNotFound) || errors.Is(err, ErrNotStarted) || errors.Is(err, ErrTargetNotFound) ||
			errors.Is(err, ErrTargetsIncomplete) || errors.Is(err, ErrATargetCompleted) ||
			errors.Is(err, ErrStrategyNotProduction) || errors.Is(err, ErrNoSnapshot) {
			return err
		}
		return fmt.Errorf("deploy: %s: %w", doing, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("deploy: committing %s: %w", doing, err)
	}
	return nil
}

// lockStatus reads and locks the deploy's status for the rest of the
// transaction, which is what keeps two advances of one record from interleaving.
func lockStatus(ctx context.Context, tx pgx.Tx, id string) (Status, error) {
	var status string
	err := tx.QueryRow(ctx, `select status from `+Table+` where id = $1 for update`, id).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return "", err
	}
	return Status(status), nil
}

// The releases a rollback skipped and the ones a revert's deploy delivers are
// stored as one column holding one id per line, the arrangement item's waits_on
// and environment's targets already have: an id is record.NewID's alphabet,
// which holds no line ending, so the separator needs no escaping. It is a column
// rather than a table because what reads it reads all of one deploy's at once,
// and a table would be a row per edge for a list bounded by the window limit and
// the backlog cap.

func joinReleases(ids []string) string { return strings.Join(ids, "\n") }

func splitReleases(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}
