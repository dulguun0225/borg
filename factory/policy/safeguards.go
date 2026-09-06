package policy

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/service"
)

// ErrNotDecidedAtARow is returned by the four approvals a gate row decides —
// the three withdrawals' and the shortening of decision-log retention's — for a
// call naming no close event. Each of those writes removes a protection, and the
// design gives each of them a row: a call with no decision behind it would be
// the record moving with nothing having decided it, which is the mechanism those
// rows exist to refuse.
var ErrNotDecidedAtARow = errors.New("policy: this write is decided at a gate row, and the call names no close event")

// AddSafeguard places one, in the direction the parameter's definition gives
// it, and appends the version in the same transaction. The bound is one value
// of three shapes — a number, a list, or a predicate — which is
// [safeguard.Bound], so this signature does not grow an argument each time a
// shape arrives. routing is the duty or the named human its rows go to, and is
// meaningful only where the direction adds a human at a gate.
func (f *Factory) AddSafeguard(ctx context.Context, actor record.Actor, parameter gatepolicy.Parameter,
	subject safeguard.Subject, bound safeguard.Bound, routing safeguard.Routing) (safeguard.Safeguard, Version, error) {
	var placed safeguard.Safeguard
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionSafeguardAdded, parameter: parameter,
		scope: Scope{Kind: string(subject.Kind), ID: subject.ID, Key: subject.Key},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			placed, err = safeguard.Insert(ctx, tx, f.token, actor, parameter, subject, bound, routing)
			if err != nil {
				return Created{}, err
			}
			if err := f.writeExplicitThreshold(ctx, tx, actor, parameter, subject, bound); err != nil {
				return Created{}, err
			}
			return Created{SafeguardID: placed.ID}, nil
		},
	})
	return placed, version, err
}

// writeExplicitThreshold is the one place a safeguard writes a field rather
// than clamping a value. The explicit threshold adds a reading beside the
// comparison rather than narrowing one, and what reads it is the health
// monitor, off the service record — so placing the safeguard is what puts the
// number there.
//
// It is the pair or nothing: the owner sets the size when they set the number,
// so the field is written once both safeguards stand and neither alone writes
// anything. The counterpart is read through the pool rather than tx, every
// safeguard before this one having committed, and the one being placed is the
// bound in hand.
func (f *Factory) writeExplicitThreshold(ctx context.Context, tx pgx.Tx, actor record.Actor,
	parameter gatepolicy.Parameter, subject safeguard.Subject, bound safeguard.Bound) error {
	if parameter != gatepolicy.ExplicitThreshold && parameter != gatepolicy.ExplicitThresholdSize {
		return nil
	}
	if subject.Kind != safeguard.SubjectService || subject.ID == "" {
		return nil
	}
	quantity, err := gatepolicy.DecidableQuantity(subject.Key)
	if err != nil {
		return err
	}

	counterpart := gatepolicy.ExplicitThresholdSize
	if parameter == gatepolicy.ExplicitThresholdSize {
		counterpart = gatepolicy.ExplicitThreshold
	}
	standing, err := safeguard.BySubjects(ctx, f.pool, counterpart, []safeguard.Subject{subject})
	if err != nil {
		return err
	}
	if len(standing) == 0 {
		return nil
	}

	number, size := bound.Number, standing[0].Bound.Number
	if parameter == gatepolicy.ExplicitThresholdSize {
		number, size = standing[0].Bound.Number, bound.Number
	}
	return service.SetExplicitThreshold(ctx, tx, f.token, actor, subject.ID, quantity, number, size)
}

// WriteSafeguardWithdrawal writes a withdrawal of one safeguard, pending. The
// safeguard stands until [Factory.ApproveSafeguardWithdrawal]: a withdrawal is
// decided and not merely written, taking a gate row of its own held by a human
// always and routed to the human the safeguard names rather than to whoever
// wrote the withdrawal. A safeguard is never edited, and there is no call here
// that edits one.
func (f *Factory) WriteSafeguardWithdrawal(ctx context.Context, actor record.Actor,
	safeguardID string) (safeguard.Withdrawal, Version, error) {
	withdrawing, err := f.safeguardByID(ctx, safeguardID)
	if err != nil {
		return safeguard.Withdrawal{}, Version{}, err
	}
	var written safeguard.Withdrawal
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalWritten,
		parameter: withdrawing.Parameter,
		scope:     Scope{Kind: "safeguard", ID: safeguardID},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			written, err = safeguard.InsertWithdrawal(ctx, tx, f.token, actor, safeguardID)
			if err != nil {
				return Created{}, err
			}
			return Created{SafeguardID: safeguardID, WithdrawalID: written.ID}, nil
		},
	})
	return written, version, err
}

// ApproveSafeguardWithdrawal is what the gate row A safeguard's withdrawal
// calls at its close, and it is where the safeguard leaves force. actor is the
// human who decided the row, which is never the one who wrote the withdrawal —
// the row is routed away from them, and this package does not fire it. decision
// is that close event, required with [ErrNotDecidedAtARow], so a safeguard
// cannot leave force on a call nobody decided.
func (f *Factory) ApproveSafeguardWithdrawal(ctx context.Context, actor record.Actor,
	withdrawalID, decision string) (Version, error) {
	if decision == "" {
		return Version{}, fmt.Errorf("%w: the withdrawal %s", ErrNotDecidedAtARow, withdrawalID)
	}
	safeguardID, err := f.safeguardOfWithdrawal(ctx, withdrawalID)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalApproved,
		scope: Scope{Kind: "safeguard", ID: safeguardID}, dropSafeguard: safeguardID,
		decision: decision,
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			return Created{WithdrawalID: withdrawalID},
				safeguard.ApproveWithdrawal(ctx, tx, f.token, withdrawalID)
		},
	})
}

// safeguardByID is one safeguard by id, so a withdrawal naming one that was
// never placed is refused before anything is written.
func (f *Factory) safeguardByID(ctx context.Context, safeguardID string) (safeguard.Safeguard, error) {
	placed, err := safeguard.All(ctx, f.pool)
	if err != nil {
		return safeguard.Safeguard{}, err
	}
	for _, p := range placed {
		if p.ID == safeguardID {
			return p, nil
		}
	}
	return safeguard.Safeguard{}, fmt.Errorf("%w: %s", safeguard.ErrNotFound, safeguardID)
}

// safeguardOfWithdrawal is which safeguard a withdrawal names, which the
// version needs to take out of the state it carries.
func (f *Factory) safeguardOfWithdrawal(ctx context.Context, withdrawalID string) (string, error) {
	var safeguardID string
	err := f.pool.QueryRow(ctx, `select safeguard_id from `+safeguard.WithdrawalTable+` where id = $1`,
		withdrawalID).Scan(&safeguardID)
	if err != nil {
		return "", fmt.Errorf("%w: %s", safeguard.ErrWithdrawalNotFound, withdrawalID)
	}
	return safeguardID, nil
}
