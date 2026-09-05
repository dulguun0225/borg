package intent

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// requirementColumns is the requirement's stored fields in the order
// [scanRequirement] reads them.
const requirementColumns = `id, actor_kind, actor_key, actor_key_basis, at, intent_id, statement,
	pattern, escape_reason, kind, derived_from, item_id, superseded_at, superseded_by, unanswerable_reason`

// scanRequirement reads one row of [requirementColumns] into a [Requirement].
func scanRequirement(row pgx.Row) (Requirement, error) {
	var r Requirement
	var kind, basis, pattern, requirementKind, superseded string
	err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At, &r.IntentID, &r.Statement,
		&pattern, &r.EscapeReason, &requirementKind, &r.DerivedFrom, &r.ItemID,
		&r.SupersededAt, &superseded, &r.UnanswerableReason)
	if err != nil {
		return Requirement{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	r.Actor.Basis = record.Basis(basis)
	r.Pattern = Pattern(pattern)
	r.Kind = Kind(requirementKind)
	r.SupersededBy, err = decodeSuperseded(superseded)
	if err != nil {
		return Requirement{}, err
	}
	return r, nil
}

// patternOf is which of the six a statement fits, refusing a statement that
// fits none and carries no tagged reason, and a tagged reason on one that
// fits. A form everything can escape is not a form, and a reason on a
// statement inside the form would be counted as an escape that is not one.
func patternOf(statement, escapeReason string) (Pattern, error) {
	if statement == "" {
		return "", ErrStatementEmpty
	}
	pattern, fits := Classify(statement)
	if fits && escapeReason != "" {
		return "", fmt.Errorf("%w: %q is a %s", ErrEscapeReasonUnwanted, statement, pattern)
	}
	if !fits && escapeReason == "" {
		return "", fmt.Errorf("%w: %q", ErrEscapeReasonMissing, statement)
	}
	return pattern, nil
}

// insertRequirement writes one requirement row.
func insertRequirement(ctx context.Context, tx pgx.Tx, r Requirement) error {
	_, err := tx.Exec(ctx, `insert into `+RequirementTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, intent_id, statement,
		 pattern, escape_reason, kind, derived_from, item_id, superseded_at, superseded_by, unanswerable_reason)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, '', '', '')`,
		r.ID, FormatVersionRequirement, string(r.Actor.Kind), r.Actor.Key, string(r.Actor.Basis),
		r.At, r.IntentID, r.Statement, string(r.Pattern), r.EscapeReason, string(r.Kind),
		r.DerivedFrom, r.ItemID)
	if err != nil {
		return fmt.Errorf("intent: writing requirement %s: %w", r.ID, err)
	}
	return nil
}

// requirementsInForce is the intent's requirements that no later confirming
// round superseded, read inside a transaction.
func requirementsInForce(ctx context.Context, tx pgx.Tx, intentID string) ([]Requirement, error) {
	rows, err := tx.Query(ctx, `select `+requirementColumns+` from `+RequirementTable+`
		where intent_id = $1 and superseded_at = '' order by at, id`, intentID)
	if err != nil {
		return nil, fmt.Errorf("intent: reading the requirements of %s: %w", intentID, err)
	}
	defer rows.Close()
	return collectRequirements(rows, intentID)
}

// collectRequirements reads every row of a requirement query.
func collectRequirements(rows pgx.Rows, of string) ([]Requirement, error) {
	var read []Requirement
	for rows.Next() {
		r, err := scanRequirement(rows)
		if err != nil {
			return nil, fmt.Errorf("intent: reading a requirement of %s: %w", of, err)
		}
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("intent: reading the requirements of %s: %w", of, err)
	}
	return read, nil
}

// DeriveForItem writes one item's share of a requirement several items answer,
// called by decomposition. Each share is a requirement record of its own,
// attached to the intent, pointing at the one it was derived from, and named
// by the item that answers it — so a requirement is answered by the item
// assigned it or by the items answering every requirement derived from it.
//
// What the derivation costs is a statement the requester never confirmed
// standing where a confirmed one did.
func (i *Intake) DeriveForItem(ctx context.Context, actor record.Actor, derivation Derivation) (Requirement, error) {
	if err := actor.Validate(); err != nil {
		return Requirement{}, err
	}
	if derivation.ItemID == "" {
		return Requirement{}, ErrItemIDEmpty
	}
	if derivation.DerivedFrom == "" {
		return Requirement{}, fmt.Errorf("%w: the share names nothing", ErrDerivedFromNotInForce)
	}
	pattern, err := patternOf(derivation.Statement, derivation.EscapeReason)
	if err != nil {
		return Requirement{}, err
	}

	written := Requirement{
		ID:           record.NewID(RequirementIDPrefix),
		Actor:        actor,
		At:           record.Now(),
		IntentID:     derivation.IntentID,
		Statement:    derivation.Statement,
		Pattern:      pattern,
		EscapeReason: derivation.EscapeReason,
		Kind:         KindDerived,
		DerivedFrom:  derivation.DerivedFrom,
		ItemID:       derivation.ItemID,
	}
	err = i.write(ctx, derivation.IntentID, "deriving a requirement of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		from, err := scanRequirement(tx.QueryRow(ctx,
			`select `+requirementColumns+` from `+RequirementTable+` where id = $1`, derivation.DerivedFrom))
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrRequirementNotFound, derivation.DerivedFrom)
		} else if err != nil {
			return fmt.Errorf("intent: reading requirement %s: %w", derivation.DerivedFrom, err)
		}
		if from.IntentID != in.ID || !from.InForce() {
			return fmt.Errorf("%w: %s", ErrDerivedFromNotInForce, from.ID)
		}
		return insertRequirement(ctx, tx, written)
	})
	if err != nil {
		return Requirement{}, err
	}
	return written, nil
}

// MarkUnanswerable is decomposition's mark on a requirement it judged no item
// can answer, with a tagged reason, which is the treatment a statement fitting
// no pattern already gets. The mark is write-once: a second judgment on the
// same requirement is [ErrAlreadyUnanswerable].
//
// The count of them on an intent is what shows a requirement stopped by
// nothing, a decomposition yielding one item firing no gate.
func (i *Intake) MarkUnanswerable(ctx context.Context, actor record.Actor, requirementID, reason string) (Requirement, error) {
	if err := actor.Validate(); err != nil {
		return Requirement{}, err
	}
	if reason == "" {
		return Requirement{}, ErrReasonEmpty
	}

	tx, err := i.pool.Begin(ctx)
	if err != nil {
		return Requirement{}, fmt.Errorf("intent: beginning the unanswerable mark on %s: %w", requirementID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, i.token); err != nil {
		return Requirement{}, err
	}

	r, err := scanRequirement(tx.QueryRow(ctx,
		`select `+requirementColumns+` from `+RequirementTable+` where id = $1 for update`, requirementID))
	if errors.Is(err, pgx.ErrNoRows) {
		return Requirement{}, fmt.Errorf("%w: %s", ErrRequirementNotFound, requirementID)
	} else if err != nil {
		return Requirement{}, fmt.Errorf("intent: reading requirement %s: %w", requirementID, err)
	}
	if r.Unanswerable() {
		return Requirement{}, fmt.Errorf("%w: %s", ErrAlreadyUnanswerable, r.ID)
	}
	r.UnanswerableReason = reason
	if _, err := tx.Exec(ctx, `update `+RequirementTable+` set unanswerable_reason = $1 where id = $2`,
		reason, r.ID); err != nil {
		return Requirement{}, fmt.Errorf("intent: marking requirement %s unanswerable: %w", r.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Requirement{}, fmt.Errorf("intent: committing the unanswerable mark on %s: %w", r.ID, err)
	}
	return r, nil
}
