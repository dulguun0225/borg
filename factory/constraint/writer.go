package constraint

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// DateLayout is how a calendar date on a constraint is stored: the calendar
// date alone, the zone it is read in being a field beside it.
const DateLayout = "2006-01-02"

var (
	// ErrActorNotHuman is returned for a write whose actor is not
	// [record.KindHuman]. Intake calls this writer, but the actor it
	// supplies is the owner who supplied the constraint at a screen, never
	// intake itself.
	ErrActorNotHuman = errors.New("constraint: only a human may supply a constraint")
	// ErrKindUnknown is returned for a kind outside [Kinds].
	ErrKindUnknown = errors.New("constraint: kind is unknown, or not one this milestone builds")
	// ErrReachUnknown is returned for a reach outside [Reaches].
	ErrReachUnknown = errors.New("constraint: reach is unknown")
	// ErrSubjectIDRequired is returned for a reach other than [ReachFactory]
	// naming no subject.
	ErrSubjectIDRequired = errors.New("constraint: a constraint whose reach is not the factory names a subject")
	// ErrSubjectIDMustBeEmpty is returned for [ReachFactory] naming a
	// subject: the factory reach binds everything and names none.
	ErrSubjectIDMustBeEmpty = errors.New("constraint: a constraint whose reach is the factory names no subject")
	// ErrStatementEmpty is returned for a constraint stating nothing.
	ErrStatementEmpty = errors.New("constraint: a constraint states what it binds")
	// ErrCalendarDateIncomplete is returned for a [CalendarDate] naming a
	// date or a zone and not the other.
	ErrCalendarDateIncomplete = errors.New("constraint: a calendar date names both a date and the zone it is read in")
	// ErrCalendarDateFormat is returned for a date that is not [DateLayout].
	ErrCalendarDateFormat = errors.New("constraint: a calendar date is YYYY-MM-DD")
	// ErrCalendarDateZoneUnknown is returned for a zone [time.LoadLocation]
	// does not know.
	ErrCalendarDateZoneUnknown = errors.New("constraint: a calendar date names a zone nothing can load")
	// ErrNotFound is returned where no row of [Table] has the id.
	ErrNotFound = errors.New("constraint: no constraint has that id")
	// ErrAlreadyWithdrawn is returned by [Writer.Withdraw] and
	// [Writer.Replace] for a constraint already withdrawn.
	ErrAlreadyWithdrawn = errors.New("constraint: this constraint is already withdrawn")
)

// CalendarDate is a date an owner authored, and the IANA time zone it was
// authored in — the shape every authored calendar value in the factory
// carries.
type CalendarDate struct {
	Date string
	Zone string
}

// validate reports whether d may be stored: both fields present, the date in
// [DateLayout], and the zone one [time.LoadLocation] knows.
func (d CalendarDate) validate() error {
	if d.Date == "" || d.Zone == "" {
		return fmt.Errorf("%w: %q in %q", ErrCalendarDateIncomplete, d.Date, d.Zone)
	}
	if _, err := time.Parse(DateLayout, d.Date); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrCalendarDateFormat, d.Date, err)
	}
	if _, err := time.LoadLocation(d.Zone); err != nil {
		return fmt.Errorf("%w: %q: %w", ErrCalendarDateZoneUnknown, d.Zone, err)
	}
	return nil
}

// StartIn is the instant d's date starts in loc, which the caller loads from
// d.Zone — [Constraint.InForceAt] is what does that.
func (d CalendarDate) StartIn(loc *time.Location) (time.Time, error) {
	start, err := time.ParseInLocation(DateLayout, d.Date, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q: %w", ErrCalendarDateFormat, d.Date, err)
	}
	return start, nil
}

// EndIn is the instant d's date ends in loc: the start of the day after it.
func (d CalendarDate) EndIn(loc *time.Location) (time.Time, error) {
	start, err := d.StartIn(loc)
	if err != nil {
		return time.Time{}, err
	}
	return start.AddDate(0, 0, 1), nil
}

// Constraint is one row of [Table] as it is stored.
type Constraint struct {
	ID                    string
	Actor                 record.Actor
	At                    string
	Kind                  Kind
	Reach                 Reach
	SubjectID             string
	Statement             string
	RequiresSeam5Enforced bool
	// BindsFrom is nil where the constraint binds from arrival. Drafting
	// reads it as in force from the start of its date in its zone, and not
	// from arrival.
	BindsFrom *CalendarDate
	// ReviewBy is nil where the owner named no review date.
	ReviewBy *CalendarDate
	// ReplacesID is empty, or the id of the constraint this one replaced.
	ReplacesID string
	// WithdrawnAt is empty until [Writer.Withdraw] or [Writer.Replace] sets
	// it; the row is kept either way.
	WithdrawnAt string
}

// InForceAt reports whether c is in force at at: not withdrawn, and, where it
// names a date it binds from, that date has started in its zone by at.
func (c Constraint) InForceAt(at time.Time) bool {
	if c.WithdrawnAt != "" {
		return false
	}
	if c.BindsFrom == nil {
		return true
	}
	loc, err := time.LoadLocation(c.BindsFrom.Zone)
	if err != nil {
		return false
	}
	start, err := c.BindsFrom.StartIn(loc)
	if err != nil {
		return false
	}
	return !start.After(at)
}

// reviewEndedBy reports whether c names a review date whose day has ended in
// its zone by at.
func (c Constraint) reviewEndedBy(at time.Time) bool {
	if c.ReviewBy == nil {
		return false
	}
	loc, err := time.LoadLocation(c.ReviewBy.Zone)
	if err != nil {
		return false
	}
	end, err := c.ReviewBy.EndIn(loc)
	if err != nil {
		return false
	}
	return !end.After(at)
}

// New is what arrives to make a constraint: the fields an owner supplies. Its
// reach and subject, its calendar fields, and its own validation are the
// same on [Writer.Arrive] and [Writer.Replace].
type New struct {
	Kind                  Kind
	Reach                 Reach
	SubjectID             string
	Statement             string
	RequiresSeam5Enforced bool
	BindsFrom             *CalendarDate
	ReviewBy              *CalendarDate
}

func (n New) validate() error {
	if !slices.Contains(Kinds, n.Kind) {
		return fmt.Errorf("%w: %q", ErrKindUnknown, n.Kind)
	}
	if !slices.Contains(Reaches, n.Reach) {
		return fmt.Errorf("%w: %q", ErrReachUnknown, n.Reach)
	}
	if n.Reach == ReachFactory {
		if n.SubjectID != "" {
			return fmt.Errorf("%w: %q", ErrSubjectIDMustBeEmpty, n.SubjectID)
		}
	} else if n.SubjectID == "" {
		return ErrSubjectIDRequired
	}
	if n.Statement == "" {
		return ErrStatementEmpty
	}
	if n.BindsFrom != nil {
		if err := n.BindsFrom.validate(); err != nil {
			return err
		}
	}
	if n.ReviewBy != nil {
		if err := n.ReviewBy.validate(); err != nil {
			return err
		}
	}
	return nil
}

// Writer is this table's one writer: intake, at arrival, at a withdrawal, and
// at a replacement. The actor every method takes is the owner who supplied
// the constraint at a screen, and every method refuses one that is not
// [record.KindHuman] — the write intake performs on their behalf, not a
// write of intake's own.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// Arrive writes a new constraint. See [New] for what it validates.
func (w *Writer) Arrive(ctx context.Context, actor record.Actor, n New) (Constraint, error) {
	var c Constraint
	err := w.write(ctx, func(tx pgx.Tx) error {
		var err error
		c, err = insert(ctx, tx, actor, n, "")
		return err
	})
	return c, err
}

// Withdraw ends id: the row is kept and withdrawn_at is set. A constraint
// already withdrawn is refused with [ErrAlreadyWithdrawn].
func (w *Writer) Withdraw(ctx context.Context, actor record.Actor, id string) (Constraint, error) {
	var c Constraint
	err := w.write(ctx, func(tx pgx.Tx) error {
		var err error
		c, err = withdraw(ctx, tx, actor, id)
		return err
	})
	return c, err
}

// Replace withdraws replacesID and inserts n naming it in ReplacesID, in one
// transaction. A replacesID already withdrawn is refused with
// [ErrAlreadyWithdrawn] and inserts nothing.
func (w *Writer) Replace(ctx context.Context, actor record.Actor, replacesID string, n New) (Constraint, error) {
	var c Constraint
	err := w.write(ctx, func(tx pgx.Tx) error {
		if _, err := withdraw(ctx, tx, actor, replacesID); err != nil {
			return err
		}
		var err error
		c, err = insert(ctx, tx, actor, n, replacesID)
		return err
	})
	return c, err
}

// write runs statement inside its own fenced transaction, the shape every
// write in this package takes.
func (w *Writer) write(ctx context.Context, statement func(pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("constraint: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return err
	}
	if err := statement(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("constraint: committing: %w", err)
	}
	return nil
}

// validateHuman is the refusal every write in this package makes: the writer
// is intake's, and the actor it stores is the owner who supplied the
// constraint, never intake or any other component.
func validateHuman(actor record.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s %q", ErrActorNotHuman, actor.Kind, actor.Key)
	}
	return nil
}

func insert(ctx context.Context, tx pgx.Tx, actor record.Actor, n New, replacesID string) (Constraint, error) {
	if err := validateHuman(actor); err != nil {
		return Constraint{}, err
	}
	if err := n.validate(); err != nil {
		return Constraint{}, err
	}
	c := Constraint{
		ID:                    record.NewID(IDPrefix),
		Actor:                 actor,
		At:                    record.Now(),
		Kind:                  n.Kind,
		Reach:                 n.Reach,
		SubjectID:             n.SubjectID,
		Statement:             n.Statement,
		RequiresSeam5Enforced: n.RequiresSeam5Enforced,
		BindsFrom:             n.BindsFrom,
		ReviewBy:              n.ReviewBy,
		ReplacesID:            replacesID,
	}

	var bindsFromDate, bindsFromZone, reviewByDate, reviewByZone *string
	if n.BindsFrom != nil {
		bindsFromDate, bindsFromZone = &n.BindsFrom.Date, &n.BindsFrom.Zone
	}
	if n.ReviewBy != nil {
		reviewByDate, reviewByZone = &n.ReviewBy.Date, &n.ReviewBy.Zone
	}

	_, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		 kind, reach, subject_id, statement, requires_seam5_enforced,
		 binds_from_date, binds_from_zone, review_by_date, review_by_zone, replaces_id, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, null)`,
		c.ID, FormatVersion, string(c.Actor.Kind), c.Actor.Key, string(c.Actor.Basis), c.At,
		string(c.Kind), string(c.Reach), c.SubjectID, c.Statement, c.RequiresSeam5Enforced,
		bindsFromDate, bindsFromZone, reviewByDate, reviewByZone, c.ReplacesID,
	)
	if err != nil {
		return Constraint{}, fmt.Errorf("constraint: writing a constraint: %w", err)
	}
	return c, nil
}

func withdraw(ctx context.Context, tx pgx.Tx, actor record.Actor, id string) (Constraint, error) {
	if err := validateHuman(actor); err != nil {
		return Constraint{}, err
	}
	var withdrawnAt *string
	if err := tx.QueryRow(ctx, `select withdrawn_at from `+Table+` where id = $1 for update`, id).
		Scan(&withdrawnAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Constraint{}, fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return Constraint{}, fmt.Errorf("constraint: reading %s: %w", id, err)
	}
	if withdrawnAt != nil {
		return Constraint{}, fmt.Errorf("%w: %s", ErrAlreadyWithdrawn, id)
	}
	if _, err := tx.Exec(ctx, `update `+Table+` set withdrawn_at = $2 where id = $1`,
		id, record.Now()); err != nil {
		return Constraint{}, fmt.Errorf("constraint: withdrawing %s: %w", id, err)
	}
	return get(ctx, tx, id)
}
