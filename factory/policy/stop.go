package policy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/record"
)

// SetHalt writes the one authored record whose subject is the factory. While
// one stands, every firing of a deploy to production row on every service
// holds and the merge queue stops fast-forwarding every service's candidates.
// It is never edited: it ends at a second record naming it, approved at a gate
// row of its own.
func (f *Factory) SetHalt(ctx context.Context, actor record.Actor, reason string) (halt.Halt, Version, error) {
	var set halt.Halt
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionHaltSet,
		scope: Scope{Kind: "factory", ID: "factory"},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			set, err = halt.Insert(ctx, tx, f.token, actor, reason)
			if err != nil {
				return Created{}, err
			}
			return Created{HaltID: set.ID}, nil
		},
	})
	return set, version, err
}

// WriteHaltWithdrawal writes a withdrawal of one halt, pending. The halt stands
// until [Factory.ApproveHaltWithdrawal]: the gate row A halt's withdrawal
// decides it, held by a human always and routed to the owner, the halt's
// subject being the factory.
func (f *Factory) WriteHaltWithdrawal(ctx context.Context, actor record.Actor,
	haltID string) (halt.Withdrawal, Version, error) {
	var written halt.Withdrawal
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalWritten,
		scope: Scope{Kind: "halt", ID: haltID},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			written, err = halt.InsertWithdrawal(ctx, tx, f.token, actor, haltID)
			if err != nil {
				return Created{}, err
			}
			return Created{HaltID: haltID, WithdrawalID: written.ID}, nil
		},
	})
	return written, version, err
}

// ApproveHaltWithdrawal is what that gate row calls at its close, and it is
// where the halt lifts. So the interval the factory stood halted is a fact of
// the trail with an actor at each end: the version this appends and the one
// [Factory.SetHalt] appended.
func (f *Factory) ApproveHaltWithdrawal(ctx context.Context, actor record.Actor,
	withdrawalID string) (Version, error) {
	haltID, err := oneColumn(ctx, f, `select halt_id from `+halt.WithdrawalTable+` where id = $1`,
		withdrawalID, halt.ErrWithdrawalNotFound)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalApproved,
		scope: Scope{Kind: "halt", ID: haltID}, dropHalt: haltID,
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			return Created{WithdrawalID: withdrawalID}, halt.ApproveWithdrawal(ctx, tx, f.token, withdrawalID)
		},
	})
}

// SetLegalHold writes a legal hold over one subject. While it stands,
// truncation and report expiry are refused wherever it reaches, and so is
// deleting the People mapping of any actor key a record within its reach names.
func (f *Factory) SetLegalHold(ctx context.Context, actor record.Actor,
	subject legalhold.Subject, reason string) (legalhold.Hold, Version, error) {
	var set legalhold.Hold
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionLegalHoldSet,
		scope: Scope{Kind: string(subject.Kind), ID: subject.ID},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			set, err = legalhold.Insert(ctx, tx, f.token, actor, subject, reason)
			if err != nil {
				return Created{}, err
			}
			return Created{LegalHoldID: set.ID}, nil
		},
	})
	return set, version, err
}

// WriteLegalHoldWithdrawal writes a withdrawal of one hold, pending. The hold
// stands until [Factory.ApproveLegalHoldWithdrawal]: it ends only at a gate row
// of its own, held by a human always and routed away from the human who wrote
// it.
func (f *Factory) WriteLegalHoldWithdrawal(ctx context.Context, actor record.Actor,
	holdID string) (legalhold.Withdrawal, Version, error) {
	var written legalhold.Withdrawal
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalWritten,
		scope: Scope{Kind: "legal_hold", ID: holdID},
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			var err error
			written, err = legalhold.InsertWithdrawal(ctx, tx, f.token, actor, holdID)
			if err != nil {
				return Created{}, err
			}
			return Created{LegalHoldID: holdID, WithdrawalID: written.ID}, nil
		},
	})
	return written, version, err
}

// ApproveLegalHoldWithdrawal is what that gate row calls at its close, and it
// is where the hold lifts.
func (f *Factory) ApproveLegalHoldWithdrawal(ctx context.Context, actor record.Actor,
	withdrawalID string) (Version, error) {
	holdID, err := oneColumn(ctx, f, `select legal_hold_id from `+legalhold.WithdrawalTable+` where id = $1`,
		withdrawalID, legalhold.ErrWithdrawalNotFound)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalApproved,
		scope: Scope{Kind: "legal_hold", ID: holdID}, dropLegalHold: holdID,
		mint: func(ctx context.Context, tx pgx.Tx) (Created, error) {
			return Created{WithdrawalID: withdrawalID}, legalhold.ApproveWithdrawal(ctx, tx, f.token, withdrawalID)
		},
	})
}

// oneColumn reads which record a withdrawal names, so the version can take it
// out of the state it carries. The statement is a constant of this package and
// the id is a parameter, so there is nowhere here anything is injected.
func oneColumn(ctx context.Context, f *Factory, statement, id string, notFound error) (string, error) {
	var value string
	if err := f.pool.QueryRow(ctx, statement, id).Scan(&value); err != nil {
		return "", fmt.Errorf("%w: %s", notFound, id)
	}
	return value, nil
}
