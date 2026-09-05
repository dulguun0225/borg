package legalhold

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// SubjectKind is what a legal hold reaches.
type SubjectKind string

const (
	// SubjectService is a service, and the hold reaches it alone.
	SubjectService SubjectKind = "service"
	// SubjectProject is a project — a persistent environment and every
	// service in it.
	SubjectProject SubjectKind = "project"
	// SubjectFactory is the whole install: every project, every service, and
	// the log itself.
	SubjectFactory SubjectKind = "factory"
)

// SubjectKinds is every subject kind this package stores. The CHECK in [DDL]
// lists the same three, and TestDDLListsEverySubjectKind fails if they stop
// agreeing.
var SubjectKinds = []SubjectKind{SubjectService, SubjectProject, SubjectFactory}

var (
	// ErrSubjectKindUnknown is returned for a subject kind outside
	// [SubjectKinds].
	ErrSubjectKindUnknown = errors.New("legalhold: the subject is not one of the three kinds")
	// ErrSubjectIDEmpty is returned for a hold on [SubjectService] or
	// [SubjectProject] naming no subject.
	ErrSubjectIDEmpty = errors.New("legalhold: this subject kind names the thing it reaches")
	// ErrSubjectIDRefused is returned for a hold on [SubjectFactory] naming a
	// subject: the whole install has nothing to name.
	ErrSubjectIDRefused = errors.New("legalhold: a hold on the whole factory names no subject")
	// ErrReasonEmpty is returned for a hold naming no reason.
	ErrReasonEmpty = errors.New("legalhold: a legal hold names its reason")
	// ErrHoldIDEmpty is returned by [InsertWithdrawal] for a withdrawal
	// naming no hold.
	ErrHoldIDEmpty = errors.New("legalhold: a withdrawal names the hold it ends")
	// ErrWithdrawalNotFound is returned by [ApproveWithdrawal] where no
	// withdrawal has the id.
	ErrWithdrawalNotFound = errors.New("legalhold: no withdrawal has that id")
	// ErrAlreadyApproved is returned by [ApproveWithdrawal] for a withdrawal
	// already approved.
	ErrAlreadyApproved = errors.New("legalhold: this withdrawal is already approved")
)

// Subject is what a legal hold reaches: a kind, and the id or name of the
// thing where the kind names one.
type Subject struct {
	Kind SubjectKind
	ID   string
}

func (s Subject) String() string {
	if s.ID == "" {
		return string(s.Kind)
	}
	return string(s.Kind) + ":" + s.ID
}

// Hold is one legal hold as it is stored. It is never edited.
type Hold struct {
	ID      string
	Actor   record.Actor
	At      string
	Subject Subject
	Reason  string
}

// Withdrawal is a legal hold's withdrawal as it is stored: a second record
// naming the hold it ends, written pending and marked approved by a second
// write.
type Withdrawal struct {
	ID         string
	Actor      record.Actor
	At         string
	HoldID     string
	Approved   bool
	ApprovedAt string
}

// Writer is the table's one writer: Factory. It wraps the tx-taking calls
// below with a pool and a token for a caller that holds no transaction of its
// own — package policy's own writes, once it wires this in, call the
// tx-taking calls directly.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Insert sets a legal hold, in its own transaction. See [Writer] for why a
// wrapper exists.
func (w *Writer) Insert(ctx context.Context, actor record.Actor, subject Subject, reason string) (Hold, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Hold{}, fmt.Errorf("legalhold: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	h, err := Insert(ctx, tx, w.token, actor, subject, reason)
	if err != nil {
		return Hold{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Hold{}, fmt.Errorf("legalhold: committing: %w", err)
	}
	return h, nil
}

// InsertWithdrawal writes a pending withdrawal, in its own transaction.
func (w *Writer) InsertWithdrawal(ctx context.Context, actor record.Actor, holdID string) (Withdrawal, error) {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("legalhold: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	wd, err := InsertWithdrawal(ctx, tx, w.token, actor, holdID)
	if err != nil {
		return Withdrawal{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Withdrawal{}, fmt.Errorf("legalhold: committing: %w", err)
	}
	return wd, nil
}

// ApproveWithdrawal marks one withdrawal approved, in its own transaction.
func (w *Writer) ApproveWithdrawal(ctx context.Context, withdrawalID string) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("legalhold: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := ApproveWithdrawal(ctx, tx, w.token, withdrawalID); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("legalhold: committing: %w", err)
	}
	return nil
}

// Insert writes one legal hold inside tx. Its caller is package policy,
// appending the policy version in the same transaction, once policy wires
// this in — see doc.go for what is not built yet.
func Insert(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor, subject Subject,
	reason string) (Hold, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Hold{}, err
	}
	if err := actor.Validate(); err != nil {
		return Hold{}, err
	}
	if !slices.Contains(SubjectKinds, subject.Kind) {
		return Hold{}, fmt.Errorf("%w: %q", ErrSubjectKindUnknown, subject.Kind)
	}
	if subject.Kind == SubjectFactory && subject.ID != "" {
		return Hold{}, fmt.Errorf("%w: %q", ErrSubjectIDRefused, subject.ID)
	}
	if subject.Kind != SubjectFactory && subject.ID == "" {
		return Hold{}, fmt.Errorf("%w: %s", ErrSubjectIDEmpty, subject.Kind)
	}
	if reason == "" {
		return Hold{}, ErrReasonEmpty
	}
	h := Hold{ID: record.NewID(IDPrefix), Actor: actor, At: record.Now(), Subject: subject, Reason: reason}
	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, subject_kind, subject_id, reason)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		h.ID, FormatVersion, string(h.Actor.Kind), h.Actor.Key, string(h.Actor.Basis), h.At,
		string(subject.Kind), subject.ID, reason,
	)
	if err != nil {
		return Hold{}, fmt.Errorf("legalhold: setting a hold on %s: %w", subject, err)
	}
	return h, nil
}

// InsertWithdrawal writes a pending withdrawal naming holdID, inside tx. It is
// not in force until [ApproveWithdrawal] marks it: it ends only at a gate row
// of its own, held by a human always and routed away from the human who wrote
// it — the treatment the gate row A safeguard's withdrawal already gets. That
// row is not built, so this and [ApproveWithdrawal] are the two writes it
// will call.
func InsertWithdrawal(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	holdID string) (Withdrawal, error) {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return Withdrawal{}, err
	}
	if err := actor.Validate(); err != nil {
		return Withdrawal{}, err
	}
	if holdID == "" {
		return Withdrawal{}, ErrHoldIDEmpty
	}
	w := Withdrawal{ID: record.NewID(WithdrawalIDPrefix), Actor: actor, At: record.Now(), HoldID: holdID}
	_, err := tx.Exec(ctx, `insert into `+WithdrawalTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, legal_hold_id, approved, approved_at)
		values ($1, $2, $3, $4, $5, $6, $7, false, null)`,
		w.ID, FormatVersionWithdrawal, string(w.Actor.Kind), w.Actor.Key, string(w.Actor.Basis), w.At, holdID,
	)
	if err != nil {
		return Withdrawal{}, fmt.Errorf("legalhold: writing a withdrawal of %s: %w", holdID, err)
	}
	return w, nil
}

// ApproveWithdrawal marks one withdrawal approved, inside tx. A withdrawal
// already approved is refused with [ErrAlreadyApproved].
func ApproveWithdrawal(ctx context.Context, tx pgx.Tx, token lease.Token, withdrawalID string) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	var approved bool
	if err := tx.QueryRow(ctx, `select approved from `+WithdrawalTable+` where id = $1 for update`,
		withdrawalID).Scan(&approved); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("%w: %s", ErrWithdrawalNotFound, withdrawalID)
		}
		return fmt.Errorf("legalhold: reading withdrawal %s: %w", withdrawalID, err)
	}
	if approved {
		return fmt.Errorf("%w: %s", ErrAlreadyApproved, withdrawalID)
	}
	if _, err := tx.Exec(ctx, `update `+WithdrawalTable+` set approved = true, approved_at = $2 where id = $1`,
		withdrawalID, record.Now()); err != nil {
		return fmt.Errorf("legalhold: approving withdrawal %s: %w", withdrawalID, err)
	}
	return nil
}

// Reaching says whether a legal hold stands over subject: one naming it
// exactly, or one on [SubjectFactory], which reaches everything. Both are
// read with no approved withdrawal.
func Reaching(ctx context.Context, pool *pgxpool.Pool, subject Subject) (bool, error) {
	var n int
	err := pool.QueryRow(ctx, `select count(*) from `+Table+` h
		where not exists (select 1 from `+WithdrawalTable+` w where w.legal_hold_id = h.id and w.approved)
		and (h.subject_kind = $1 or (h.subject_kind = $2 and h.subject_id = $3))`,
		string(SubjectFactory), string(subject.Kind), subject.ID).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("legalhold: reading whether a hold reaches %s: %w", subject, err)
	}
	return n > 0, nil
}
