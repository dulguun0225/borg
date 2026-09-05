package factorysettings

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrNotFound is returned by [Get] and by every authoring call where the
	// record has not been created.
	ErrNotFound = errors.New("factorysettings: the factory-wide settings record does not exist yet")
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

// Settings is the factory-wide settings record as it is stored: the fields that
// are one value per record. The parameters this record holds per stage, per duty,
// per severity and per service are rows of the tables beside it, read by
// [AttemptLimit], [ReviewSampleRate], [RemediationPeriod], [ReportChannelRate]
// and [HarmMarkPageCap].
//
// Every authored field is a [gatepolicy.Authored], absent where an owner authored
// nothing, because absent and zero are different answers and both are real.
type Settings struct {
	ID    string
	Actor record.Actor
	At    string
	// AllowedPredicateKinds is what an owner authored, and empty where they
	// authored nothing — where the value in force is the kinds this factory can
	// decide. Nothing reads it until contracts are built.
	AllowedPredicateKinds []string
	// RolePromptOrSkillThreshold is what an owner authored for the gate row that
	// decides a version of what an agent is told, which reads this record having
	// no project and so no production environment to read. Nothing reads it until
	// that row is built.
	RolePromptOrSkillThreshold gatepolicy.Authored
	// AdvisorySeverity is the bound at or above which a matching advisory rejects
	// at Implementation and holds at Deploy to production. Nothing reads it until
	// the advisory detector is built.
	AdvisorySeverity gatepolicy.Authored
	// HeldOutSampleRate is how often the score auto-passes a change it would have
	// gated. The sample is one formula's and no service's, which is why the rate
	// is a field of this record.
	HeldOutSampleRate gatepolicy.Authored
	// DecisionLogRetentionSeconds is how long the decision log is kept, and absent
	// is the life of the install.
	DecisionLogRetentionSeconds gatepolicy.Authored
	// ReportRetentionSeconds is how long the report store keeps a report, and
	// absent is the life of the install. Nothing reads it until the report store
	// is built.
	ReportRetentionSeconds gatepolicy.Authored
	// BackupRetentionSeconds is how far back a backup may reach. Its one reader is
	// the erasure list's retirement, which is not built.
	BackupRetentionSeconds gatepolicy.Authored
	// RetentionFloorSeconds is how low an authored value or a safeguard may ever
	// take decision-log retention.
	RetentionFloorSeconds gatepolicy.Authored
	// ReportChannelRate is the factory-wide rate bounding arrival at the way in,
	// and absent is unbounded. The per-service rate beside it is [ReportChannelRate].
	// Nothing reads either until the report channel is built.
	ReportChannelRate gatepolicy.Authored
	// HarmMarkPages is whether a report marked as describing harm to a person
	// pages at all. It ships on, so an owner who will not be woken by a stranger
	// turns it off. Nothing reads it until the report channel is built.
	HarmMarkPages bool
	// Seam5Enforced is whether seam 5 is enforced. It is off at install and an
	// owner turns it on once; nothing turns it off again.
	Seam5Enforced bool
}

// Writer creates the record, as Factory.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

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
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, only_row, allowed_predicate_kinds)
		values ($1, $2, $3, $4, $5, $6, true, '')
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

// update writes one field of the record inside tx. assignment is a constant of
// this package and never input, so writing it into the statement is not a place
// anything can be injected.
func update(ctx context.Context, tx pgx.Tx, settingsID, assignment string, value any) error {
	tag, err := tx.Exec(ctx, `update `+Table+` set `+assignment+` where id = $2`, value, settingsID)
	if err != nil {
		return fmt.Errorf("factorysettings: authoring on %s: %w", settingsID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, settingsID)
	}
	return nil
}

// insertKeyed writes one row of a table beside the record inside tx, replacing
// the row that key already has. Every keyed parameter is authored this way, so
// what re-authoring conflicts on is one constraint per table and not one rule per
// caller. table, prefix, version, columns and conflict are constants of this
// package and never input.
func insertKeyed(ctx context.Context, tx pgx.Tx, actor record.Actor, table, prefix, version,
	columns, placeholders, conflict, set string, values ...any) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	args := append([]any{
		record.NewID(prefix), version, string(actor.Kind), actor.Key, string(actor.Basis), record.Now(),
	}, values...)
	_, err := tx.Exec(ctx, `insert into `+table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, `+columns+`)
		values ($1, $2, $3, $4, $5, $6, `+placeholders+`)
		on conflict (`+conflict+`) do update set `+set, args...)
	if err != nil {
		return fmt.Errorf("factorysettings: authoring a row of %s: %w", table, err)
	}
	return nil
}

// Get is the factory-wide settings record. It takes the pool and not a [Writer],
// because reading it is not a reason to be handed the thing that creates it.
func Get(ctx context.Context, pool *pgxpool.Pool) (Settings, error) {
	var s Settings
	var kind, basis, allowed string
	var threshold, severity, heldOut *float64
	var decisionLog, report, backup, floor, rate *int64
	err := pool.QueryRow(ctx, `select id, actor_kind, actor_key, actor_key_basis, at, allowed_predicate_kinds,
		role_prompt_or_skill_threshold, advisory_severity, held_out_sample_rate,
		decision_log_retention_seconds, report_retention_seconds, backup_retention_seconds,
		retention_floor_seconds, report_channel_rate, harm_mark_pages, seam_5_enforced
		from `+Table+` where only_row`).
		Scan(&s.ID, &kind, &s.Actor.Key, &basis, &s.At, &allowed,
			&threshold, &severity, &heldOut,
			&decisionLog, &report, &backup, &floor, &rate, &s.HarmMarkPages, &s.Seam5Enforced)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotFound
	} else if err != nil {
		return Settings{}, fmt.Errorf("factorysettings: reading the record: %w", err)
	}
	s.Actor.Kind = record.Kind(kind)
	s.Actor.Basis = record.Basis(basis)
	if allowed != "" {
		s.AllowedPredicateKinds = strings.Split(allowed, "\n")
	}
	s.RolePromptOrSkillThreshold = authoredFloat(threshold)
	s.AdvisorySeverity = authoredFloat(severity)
	s.HeldOutSampleRate = authoredFloat(heldOut)
	s.DecisionLogRetentionSeconds = authoredSeconds(decisionLog)
	s.ReportRetentionSeconds = authoredSeconds(report)
	s.BackupRetentionSeconds = authoredSeconds(backup)
	s.RetentionFloorSeconds = authoredSeconds(floor)
	s.ReportChannelRate = authoredSeconds(rate)
	return s, nil
}

// authoredFloat reads a nullable column back as what an owner authored, absent
// being different from zero.
func authoredFloat(value *float64) gatepolicy.Authored {
	if value == nil {
		return gatepolicy.Authored{}
	}
	return gatepolicy.Authored{Number: *value, Present: true}
}

// authoredSeconds is [authoredFloat] for a column stored as a whole number. Every
// authored parameter reads back as the same type, because what clamps them is one
// arithmetic for all of them.
func authoredSeconds(value *int64) gatepolicy.Authored {
	if value == nil {
		return gatepolicy.Authored{}
	}
	return gatepolicy.Authored{Number: float64(*value), Present: true}
}

// keyedValue is the one read every keyed parameter's own read performs: the value
// under one key, and absent where an owner authored none. table, column and key
// are constants of this package and never input.
func keyedValue(ctx context.Context, pool *pgxpool.Pool, table, column, keyColumn string,
	settingsID string, key any) (gatepolicy.Authored, error) {
	var value float64
	err := pool.QueryRow(ctx, `select `+column+` from `+table+`
		where factory_settings_id = $1 and `+keyColumn+` = $2`, settingsID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		return gatepolicy.Authored{}, nil
	} else if err != nil {
		return gatepolicy.Authored{}, fmt.Errorf("factorysettings: reading %s of %s: %w", column, table, err)
	}
	return gatepolicy.Authored{Number: value, Present: true}, nil
}
