package factorysettings

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned by [Get] where the record has not been created.
	ErrNotFound = errors.New("factorysettings: the factory-wide settings record does not exist yet")
	// ErrLimitNotPositive is returned by [SetAttemptLimit] for a limit that is
	// not above zero. A limit of zero would retry a stage no times and escalate
	// every item before an agent was asked anything.
	ErrLimitNotPositive = errors.New("factorysettings: an attempt limit is above zero")
	// ErrStageUnknown is returned by [SetAttemptLimit] for a stage outside
	// [item.StageOrder]. A limit authored on a stage that does not exist is a
	// value nothing will ever read, and the store has no foreign key to refuse
	// it.
	ErrStageUnknown = errors.New("factorysettings: the limit names a stage an item cannot be at")
	// ErrThresholdOutOfRange is returned by [SetRolePromptOrSkillThreshold] for a
	// threshold outside nothing to one, which is the scale the score's number
	// is on.
	ErrThresholdOutOfRange = errors.New("factorysettings: a threshold is between 0 and 1")
	// ErrAllowedPredicateKindsEmpty is returned by [SetAllowedPredicateKinds] for an
	// empty list. A safeguard on the list may only extend it, and authoring it empty
	// would be an owner removing every kind of assertion a consumer contract may
	// draw from.
	ErrAllowedPredicateKindsEmpty = errors.New("factorysettings: an authored list of allowed predicate kinds names at least one kind of assertion")
)

// Settings is the factory-wide settings record as it is stored.
type Settings struct {
	ID    string
	Actor record.Actor
	At    string
	// AllowedPredicateKinds is what an owner authored, and empty where they
	// authored nothing. Nothing reads it until contracts are built.
	AllowedPredicateKinds []string
	// RolePromptOrSkillThreshold is what an owner authored for the gate row that
	// decides a version of what an agent is told, and absent where they
	// authored nothing. Nothing reads it until that row is built.
	RolePromptOrSkillThreshold gatepolicy.Authored
}

// Writer creates the record, as Factory.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer { return &Writer{pool: pool, token: token} }

// Ensure returns the factory-wide settings record, creating it with nothing authored
// where it does not exist. It is idempotent: the insert does nothing on the
// singleton conflict, so two callers ensuring at once leave one record.
func (w *Writer) Ensure(ctx context.Context, actor record.Actor) (Settings, error) {
	if err := actor.Validate(); err != nil {
		return Settings{}, err
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("factorysettings: beginning the ensure: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Settings{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, only_row, allowed_predicate_kinds, role_prompt_or_skill_threshold)
		values ($1, $2, $3, $4, $5, $6, true, '', null)
		on conflict (only_row) do nothing`,
		record.NewID(IDPrefix), FormatVersion, string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
	)
	if err != nil {
		return Settings{}, fmt.Errorf("factorysettings: creating the record: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("factorysettings: committing the ensure: %w", err)
	}
	return Get(ctx, w.pool)
}

// SetAttemptLimit writes the limit an owner authored for one stage, inside tx.
// Its one caller is package policy, which calls it inside the transaction that
// appends the policy version.
func SetAttemptLimit(ctx context.Context, tx pgx.Tx, actor record.Actor, settingsID string, stage item.Stage, limit int) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if !slices.Contains(item.StageOrder, stage) {
		return fmt.Errorf("%w: %q", ErrStageUnknown, stage)
	}
	if limit <= 0 {
		return fmt.Errorf("%w: %d", ErrLimitNotPositive, limit)
	}
	_, err := tx.Exec(ctx, `insert into `+LimitTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, factory_settings_id, stage, attempt_limit)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		on conflict (factory_settings_id, stage) do update set attempt_limit = excluded.attempt_limit`,
		record.NewID(LimitIDPrefix), FormatVersionLimit, string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
		settingsID, string(stage), limit,
	)
	if err != nil {
		return fmt.Errorf("factorysettings: authoring the %s attempt limit: %w", stage, err)
	}
	return nil
}

// SetAllowedPredicateKinds writes the list an owner authored, inside tx.
func SetAllowedPredicateKinds(ctx context.Context, tx pgx.Tx, settingsID string, allowed []string) error {
	if len(allowed) == 0 {
		return ErrAllowedPredicateKindsEmpty
	}
	return update(ctx, tx, settingsID, `allowed_predicate_kinds = $1`, strings.Join(allowed, "\n"))
}

// SetRolePromptOrSkillThreshold writes the threshold an owner authored for the gate
// row that decides a version of what an agent is told, inside tx.
func SetRolePromptOrSkillThreshold(ctx context.Context, tx pgx.Tx, settingsID string, threshold float64) error {
	if threshold < 0 || threshold > 1 {
		return fmt.Errorf("%w: %v", ErrThresholdOutOfRange, threshold)
	}
	return update(ctx, tx, settingsID, `role_prompt_or_skill_threshold = $1`, threshold)
}

func update(ctx context.Context, tx pgx.Tx, settingsID, assignment string, value any) error {
	// assignment is a constant of this package and never input, so writing it
	// into the statement is not a place anything can be injected.
	tag, err := tx.Exec(ctx, `update `+Table+` set `+assignment+` where id = $2`, value, settingsID)
	if err != nil {
		return fmt.Errorf("factorysettings: authoring on %s: %w", settingsID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, settingsID)
	}
	return nil
}

// Get is the factory-wide settings record. It takes the pool and not a [Writer],
// because reading it is not a reason to be handed the thing that creates it.
func Get(ctx context.Context, pool *pgxpool.Pool) (Settings, error) {
	var p Settings
	var kind, basis, allowed string
	var threshold *float64
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, allowed_predicate_kinds, role_prompt_or_skill_threshold
		from `+Table+` where only_row`).
		Scan(&p.ID, &kind, &p.Actor.Key, &basis, &p.At, &allowed, &threshold)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotFound
	} else if err != nil {
		return Settings{}, fmt.Errorf("factorysettings: reading the record: %w", err)
	}
	p.Actor.Kind = record.Kind(kind)
	p.Actor.Basis = record.Basis(basis)
	if allowed != "" {
		p.AllowedPredicateKinds = strings.Split(allowed, "\n")
	}
	if threshold != nil {
		p.RolePromptOrSkillThreshold = gatepolicy.Authored{Number: *threshold, Present: true}
	}
	return p, nil
}

// AttemptLimit is the limit an owner authored for one stage, and absent where
// they authored none — where the value in force is what the score supplies. The
// limit is a whole number of attempts and is read back as the same type every
// authored parameter is, because what clamps it is one arithmetic for all of
// them.
func AttemptLimit(ctx context.Context, pool *pgxpool.Pool, settingsID string, stage item.Stage) (gatepolicy.Authored, error) {
	var limit int
	err := pool.QueryRow(ctx, `select attempt_limit from `+LimitTable+`
		where factory_settings_id = $1 and stage = $2`, settingsID, string(stage)).Scan(&limit)
	if errors.Is(err, pgx.ErrNoRows) {
		return gatepolicy.Authored{}, nil
	} else if err != nil {
		return gatepolicy.Authored{}, fmt.Errorf("factorysettings: reading the %s attempt limit: %w", stage, err)
	}
	return gatepolicy.Authored{Number: float64(limit), Present: true}, nil
}
