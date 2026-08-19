package people

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// Duty is one of the owner's twelve duties, held as its number. doc.go says why
// the names are not here.
type Duty int

// Duties is the twelve, in order. The CHECK in [DDL] holds the same range, and
// TestDDLHoldsEveryDuty fails if the two stop agreeing.
var Duties = func() []Duty {
	all := make([]Duty, 0, 12)
	for d := Duty(1); d <= 12; d++ {
		all = append(all, d)
	}
	return all
}()

// Obligation is something a human holds that is not one of the twelve, because it
// is substrate rather than work.
type Obligation string

const (
	// ObligationHosting is hosting the factory, which includes bringing it up to
	// date: a self-hosted product is upgraded by whoever runs it.
	ObligationHosting Obligation = "hosting"
	// ObligationReconciler is installing the reconciler beside the factory. It is
	// the one obligation this milestone reads: a mismatch belongs to no duty, so
	// the page it fires reaches whoever this record says installed it.
	ObligationReconciler Obligation = "reconciler"
	// ObligationFleet is composing the fleet — the entries the factory dispatches
	// against, the credentials they run on, and giving a fleet proposal a
	// disposition. Nothing reads it until the fleet is built.
	ObligationFleet Obligation = "fleet"
)

// Obligations is every obligation outside the twelve. The CHECK in [DDL] lists
// the same three, and TestDDLListsEveryObligation fails if the two stop agreeing.
var Obligations = []Obligation{ObligationHosting, ObligationReconciler, ObligationFleet}

var (
	// ErrNotAnOwner is returned for an actor that is not a human. Distributing the
	// twelve is the owner's, and a component doing it would be the factory deciding
	// who holds the factory's obligations.
	ErrNotAnOwner = errors.New("people: the declaration is written by a human")
	// ErrHumanEmpty is returned for a declaration naming no human.
	ErrHumanEmpty = errors.New("people: a declaration names the human who holds it")
	// ErrHoldingUnknown is returned for a declaration that names neither a duty in
	// range nor an obligation, or that names both.
	ErrHoldingUnknown = errors.New("people: a declaration names one duty or one obligation, and not both")
	// ErrNotFound is returned where no declaration has that id.
	ErrNotFound = errors.New("people: no declaration has that id")
)

// Declaration is one row: a named human holding one duty or one obligation.
type Declaration struct {
	ID    string
	Actor record.Actor
	At    string
	// Human is the name the notifier reaches. It is not resolved against
	// anything — no directory, no account — which is the same rule an actor's name
	// keeps.
	Human string
	// Duty is the duty held, and is zero where the row names an obligation.
	Duty Duty
	// Obligation is the obligation held, and is empty where the row names a duty.
	Obligation Obligation
	// WithdrawnAt is when the holding ended, and is empty while it stands. The row
	// is kept, so a page delivered to a holder who has since stopped holding is
	// still readable against the row that routed it.
	WithdrawnAt string
}

// Holds reports whether the declaration still stands.
func (d Declaration) Holds() bool { return d.WithdrawnAt == "" }

// OfDuty is a holding of one duty, for [Writer.Declare].
func OfDuty(duty Duty) Holding { return Holding{Duty: duty} }

// OfObligation is a holding of one obligation outside the twelve.
func OfObligation(obligation Obligation) Holding { return Holding{Obligation: obligation} }

// Holding is which of the two a declaration names. It is one value rather than
// two arguments so that a caller cannot pass both, and [OfDuty] and
// [OfObligation] are what a caller says it with.
type Holding struct {
	Duty       Duty
	Obligation Obligation
}

func (h Holding) validate() error {
	switch {
	case h.Duty != 0 && h.Obligation != "":
		return fmt.Errorf("%w: duty %d and obligation %q", ErrHoldingUnknown, h.Duty, h.Obligation)
	case h.Duty != 0:
		if !slices.Contains(Duties, h.Duty) {
			return fmt.Errorf("%w: duty %d is not one of the twelve", ErrHoldingUnknown, h.Duty)
		}
	case h.Obligation != "":
		if !slices.Contains(Obligations, h.Obligation) {
			return fmt.Errorf("%w: %q is not an obligation outside the twelve", ErrHoldingUnknown, h.Obligation)
		}
	default:
		return fmt.Errorf("%w: it names neither", ErrHoldingUnknown)
	}
	return nil
}

// String is how a holding reads in a message the notifier delivers.
func (h Holding) String() string {
	if h.Obligation != "" {
		return "the obligation of " + string(h.Obligation)
	}
	return fmt.Sprintf("duty %d", h.Duty)
}

// Writer is the one writer of declarations: the owner, at People.
type Writer struct {
	pool *pgxpool.Pool
}

// NewWriter returns the writer over pool.
func NewWriter(pool *pgxpool.Pool) *Writer { return &Writer{pool: pool} }

// Declare writes that human holds this duty or this obligation. Declaring the
// same pair twice is one row: the insert conflicts on the human and the holding
// and clears any withdrawal, so re-declaring a holding somebody gave up is how
// they take it back. The row keeps the first declaration's actor and time, and
// what says who last wrote it is nothing — the declaration is not gate policy and
// appends no version.
func (w *Writer) Declare(ctx context.Context, actor record.Actor, human string, holding Holding) (Declaration, error) {
	if err := actor.Validate(); err != nil {
		return Declaration{}, err
	}
	if actor.Kind != record.KindHuman {
		return Declaration{}, fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Name)
	}
	if human == "" {
		return Declaration{}, ErrHumanEmpty
	}
	if err := holding.validate(); err != nil {
		return Declaration{}, err
	}

	d := Declaration{
		ID:         record.NewID(IDPrefix),
		Actor:      actor,
		At:         record.Now(),
		Human:      human,
		Duty:       holding.Duty,
		Obligation: holding.Obligation,
	}
	_, err := w.pool.Exec(ctx, `insert into `+Table+`
		(id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, '')
		on conflict (human, duty, obligation) do update set withdrawn_at = ''`,
		d.ID, string(d.Actor.Kind), d.Actor.Name, d.At, d.Human, int(d.Duty), string(d.Obligation),
	)
	if err != nil {
		return Declaration{}, fmt.Errorf("people: declaring that %s holds %s: %w", human, holding, err)
	}
	// The row that stands may be the one this call inserted or the one a previous
	// call did, and the conflict target does not say which. Reading it back is what
	// makes the returned declaration the row rather than what was attempted.
	return ByHolding(ctx, w.pool, human, holding)
}

// Withdraw marks the holding ended and keeps the row.
func (w *Writer) Withdraw(ctx context.Context, id string) (Declaration, error) {
	tag, err := w.pool.Exec(ctx, `update `+Table+`
		set withdrawn_at = $1 where id = $2 and withdrawn_at = ''`, record.Now(), id)
	if err != nil {
		return Declaration{}, fmt.Errorf("people: withdrawing %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		// Either it does not exist or it was withdrawn already. The second is not an
		// error: what Withdraw promises is that the holding does not stand after it
		// returns, and that already holds.
		return Get(ctx, w.pool, id)
	}
	return Get(ctx, w.pool, id)
}

const selectDeclaration = `select id, actor_kind, actor_name, at, human, duty, obligation, withdrawn_at
	from ` + Table

func scan(row pgx.Row) (Declaration, error) {
	var d Declaration
	var kind, obligation string
	var duty int
	if err := row.Scan(&d.ID, &kind, &d.Actor.Name, &d.At, &d.Human, &duty, &obligation, &d.WithdrawnAt); err != nil {
		return Declaration{}, err
	}
	d.Actor.Kind = record.Kind(kind)
	d.Duty = Duty(duty)
	d.Obligation = Obligation(obligation)
	return d, nil
}

// Get is one declaration by id. It takes the pool and not a [Writer], because
// reading who holds what is not a reason to be handed the thing that declares it.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Declaration, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeclaration+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Declaration{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Declaration{}, fmt.Errorf("people: reading %s: %w", id, err)
	}
	return d, nil
}

// ByHolding is the declaration of one human and one holding, whether or not it
// still stands.
func ByHolding(ctx context.Context, pool *pgxpool.Pool, human string, holding Holding) (Declaration, error) {
	d, err := scan(pool.QueryRow(ctx, selectDeclaration+`
		where human = $1 and duty = $2 and obligation = $3`,
		human, int(holding.Duty), string(holding.Obligation)))
	if errors.Is(err, pgx.ErrNoRows) {
		return Declaration{}, fmt.Errorf("%w: %s holding %s", ErrNotFound, human, holding)
	} else if err != nil {
		return Declaration{}, fmt.Errorf("people: reading the declaration that %s holds %s: %w", human, holding, err)
	}
	return d, nil
}

// Holders is every human who holds this duty or obligation and has not withdrawn
// from it, in the order they were declared. It is what the notifier routes on, and
// no holders is a routing answer and not a missing one: the page widens to the
// owner, who is the person that would have written the row.
func Holders(ctx context.Context, pool *pgxpool.Pool, holding Holding) ([]string, error) {
	if err := holding.validate(); err != nil {
		return nil, err
	}
	rows, err := pool.Query(ctx, `select human from `+Table+`
		where duty = $1 and obligation = $2 and withdrawn_at = '' order by at, id`,
		int(holding.Duty), string(holding.Obligation))
	if err != nil {
		return nil, fmt.Errorf("people: reading who holds %s: %w", holding, err)
	}
	defer rows.Close()

	var holders []string
	for rows.Next() {
		var human string
		if err := rows.Scan(&human); err != nil {
			return nil, fmt.Errorf("people: reading a holder of %s: %w", holding, err)
		}
		holders = append(holders, human)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: reading who holds %s: %w", holding, err)
	}
	return holders, nil
}

// All is every declaration, standing or withdrawn, in the order they were
// declared. It is what the crude interface prints.
func All(ctx context.Context, pool *pgxpool.Pool) ([]Declaration, error) {
	rows, err := pool.Query(ctx, selectDeclaration+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("people: reading the declaration: %w", err)
	}
	defer rows.Close()

	var read []Declaration
	for rows.Next() {
		d, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("people: reading a row of the declaration: %w", err)
		}
		read = append(read, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: reading the declaration: %w", err)
	}
	return read, nil
}
