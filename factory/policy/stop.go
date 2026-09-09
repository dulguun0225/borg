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
	id := record.NewID(halt.IDPrefix)
	var set halt.Halt
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionHaltSet,
		scope:    Scope{Kind: "factory", ID: "factory"},
		keyExtra: reason, minted: Created{HaltID: id},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			set, err = halt.Insert(ctx, tx, f.token, actor, id, reason)
			return err
		},
	})
	if err != nil || set.ID != "" {
		return set, version, err
	}
	set, err = f.haltByID(ctx, version.HaltID)
	return set, version, err
}

// WriteHaltWithdrawal writes a withdrawal of one halt, pending. The halt stands
// until [Factory.ApproveHaltWithdrawal]: the gate row A halt's withdrawal
// decides it, held by a human always and routed to the owner, the halt's
// subject being the factory.
func (f *Factory) WriteHaltWithdrawal(ctx context.Context, actor record.Actor,
	haltID string) (halt.Withdrawal, Version, error) {
	id := record.NewID(halt.WithdrawalIDPrefix)
	var written halt.Withdrawal
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalWritten,
		scope:  Scope{Kind: "halt", ID: haltID},
		minted: Created{HaltID: haltID, WithdrawalID: id},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			written, err = halt.InsertWithdrawal(ctx, tx, f.token, actor, id, haltID)
			return err
		},
	})
	if err != nil || written.ID != "" {
		return written, version, err
	}
	written, err = halt.GetWithdrawal(ctx, f.pool, version.WithdrawalID)
	return written, version, err
}

// ApproveHaltWithdrawal is what that gate row calls at its close, and it is
// where the halt lifts. So the interval the factory stood halted is a fact of
// the trail with an actor at each end: the version this appends and the one
// [Factory.SetHalt] appended. decision is that close event, required with
// [ErrNotDecidedAtARow].
func (f *Factory) ApproveHaltWithdrawal(ctx context.Context, actor record.Actor,
	withdrawalID, decision string) (Version, error) {
	if decision == "" {
		return Version{}, fmt.Errorf("%w: the withdrawal %s", ErrNotDecidedAtARow, withdrawalID)
	}
	haltID, err := oneColumn(ctx, f, `select halt_id from `+halt.WithdrawalTable+` where id = $1`,
		withdrawalID, halt.ErrWithdrawalNotFound)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalApproved,
		scope: Scope{Kind: "halt", ID: haltID}, dropHalt: haltID, decision: decision,
		minted: Created{WithdrawalID: withdrawalID},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return halt.ApproveWithdrawal(ctx, tx, f.token, withdrawalID)
		},
	})
}

// SetLegalHold writes a legal hold over one subject. While it stands,
// truncation and report expiry are refused wherever it reaches, and so is
// deleting the People mapping of any actor key a record within its reach names.
func (f *Factory) SetLegalHold(ctx context.Context, actor record.Actor,
	subject legalhold.Subject, reason string) (legalhold.Hold, Version, error) {
	id := record.NewID(legalhold.IDPrefix)
	var set legalhold.Hold
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionLegalHoldSet,
		scope:    Scope{Kind: string(subject.Kind), ID: subject.ID},
		keyExtra: reason, minted: Created{LegalHoldID: id},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			set, err = legalhold.Insert(ctx, tx, f.token, actor, id, subject, reason)
			return err
		},
	})
	if err != nil || set.ID != "" {
		return set, version, err
	}
	set, err = f.legalHoldByID(ctx, version.LegalHoldID)
	return set, version, err
}

// WriteLegalHoldWithdrawal writes a withdrawal of one hold, pending. The hold
// stands until [Factory.ApproveLegalHoldWithdrawal]: it ends only at a gate row
// of its own, held by a human always and routed away from the human who wrote
// it.
func (f *Factory) WriteLegalHoldWithdrawal(ctx context.Context, actor record.Actor,
	holdID string) (legalhold.Withdrawal, Version, error) {
	id := record.NewID(legalhold.WithdrawalIDPrefix)
	var written legalhold.Withdrawal
	version, err := f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalWritten,
		scope:  Scope{Kind: "legal_hold", ID: holdID},
		minted: Created{LegalHoldID: holdID, WithdrawalID: id},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			var err error
			written, err = legalhold.InsertWithdrawal(ctx, tx, f.token, actor, id, holdID)
			return err
		},
	})
	if err != nil || written.ID != "" {
		return written, version, err
	}
	written, err = legalhold.GetWithdrawal(ctx, f.pool, version.WithdrawalID)
	return written, version, err
}

// ApproveLegalHoldWithdrawal is what the gate row A legal hold's withdrawal
// calls at its close, and it is where the hold lifts. decision is that close
// event, required with [ErrNotDecidedAtARow].
func (f *Factory) ApproveLegalHoldWithdrawal(ctx context.Context, actor record.Actor,
	withdrawalID, decision string) (Version, error) {
	if decision == "" {
		return Version{}, fmt.Errorf("%w: the withdrawal %s", ErrNotDecidedAtARow, withdrawalID)
	}
	holdID, err := oneColumn(ctx, f, `select legal_hold_id from `+legalhold.WithdrawalTable+` where id = $1`,
		withdrawalID, legalhold.ErrWithdrawalNotFound)
	if err != nil {
		return Version{}, err
	}
	return f.append(ctx, write{
		caller: CallerFactory, actor: actor, action: ActionWithdrawalApproved,
		scope: Scope{Kind: "legal_hold", ID: holdID}, dropLegalHold: holdID, decision: decision,
		minted: Created{WithdrawalID: withdrawalID},
		apply: func(ctx context.Context, tx pgx.Tx) error {
			return legalhold.ApproveWithdrawal(ctx, tx, f.token, withdrawalID)
		},
	})
}

// haltByID and legalHoldByID are what a step taken again reads back: the
// version in force already carries this write's key, so nothing was written
// and the record in hand is the one the first performance wrote. It still
// stands, an approved withdrawal of it having appended a version of its own and
// so left this write's key no longer the one in force.
func (f *Factory) haltByID(ctx context.Context, id string) (halt.Halt, error) {
	standing, err := halt.Standing(ctx, f.pool)
	if err != nil {
		return halt.Halt{}, err
	}
	for _, h := range standing {
		if h.ID == id {
			return h, nil
		}
	}
	return halt.Halt{}, fmt.Errorf("policy: no halt with the id %s stands", id)
}

func (f *Factory) legalHoldByID(ctx context.Context, id string) (legalhold.Hold, error) {
	standing, err := legalhold.Standing(ctx, f.pool)
	if err != nil {
		return legalhold.Hold{}, err
	}
	for _, h := range standing {
		if h.ID == id {
			return h, nil
		}
	}
	return legalhold.Hold{}, fmt.Errorf("policy: no legal hold with the id %s stands", id)
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
