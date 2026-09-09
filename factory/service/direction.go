package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// Direction is the way a safeguard's write may move a field against the value
// already in force. It is checked only for a component actor — the way a
// safeguard's write is marked, as against a human's — because an owner sets a
// field either way and a safeguard may only narrow what it constrains.
type Direction int

const (
	// RaiseOnly is a field a safeguard may raise and never lower, lowering it
	// being what removes a check rather than adding one.
	RaiseOnly Direction = iota
	// LowerOnly is a field a safeguard may lower and never raise, for the
	// symmetric reason.
	LowerOnly
)

// ErrSafeguardDirection is returned where a component actor's write would
// move a field against the direction stated for it. The error names the
// field, so the refusal is legible without the caller re-deriving which one
// it was.
var ErrSafeguardDirection = errors.New("service: a safeguard may only move this field one way")

// enforceDirection refuses a component actor's write of value into column
// where it moves the field away from the value in force, in the direction not
// its own. A human actor is not checked here — the field is an owner's to set
// either way. The value in force is the column where it carries one, or
// fallback where it does not — a field with a shipped default is enforced
// against that default even on a first write, there being a value in force
// even though nothing is authored yet. fallback is nil where this package
// knows of no shipped default for the field, the score supplying one
// elsewhere instead, and there a first write with nothing authored places no
// bound.
func enforceDirection(ctx context.Context, tx pgx.Tx, actor record.Actor, serviceID, column, field string,
	direction Direction, value float64, fallback *float64) error {
	if actor.Kind != record.KindComponent {
		return nil
	}
	var current *float64
	err := tx.QueryRow(ctx, `select `+column+` from `+Table+` where id = $1`, serviceID).Scan(&current)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("service: reading %s in force on %s: %w", field, serviceID, err)
	}
	if current == nil {
		if fallback == nil {
			return nil
		}
		current = fallback
	}
	switch direction {
	case RaiseOnly:
		if value < *current {
			return fmt.Errorf("%w: %s may only be raised, %v is below the %v in force",
				ErrSafeguardDirection, field, value, *current)
		}
	case LowerOnly:
		if value > *current {
			return fmt.Errorf("%w: %s may only be lowered, %v is above the %v in force",
				ErrSafeguardDirection, field, value, *current)
		}
	}
	return nil
}

// setDirectional validates actor, enforces direction against the value in
// force, and then writes value into column the way [set] does. It is what
// every setter with a stated direction calls in place of [set] itself.
// fallback is [enforceDirection]'s.
func setDirectional(ctx context.Context, tx pgx.Tx, actor record.Actor, serviceID, column, field string,
	direction Direction, value float64, fallback *float64) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if err := enforceDirection(ctx, tx, actor, serviceID, column, field, direction, value, fallback); err != nil {
		return err
	}
	return set(ctx, tx, serviceID, column, value)
}
