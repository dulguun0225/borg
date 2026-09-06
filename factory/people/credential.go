package people

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

// AccountKind is whether the account behind a lent credential is a person's
// own or an organisation's. The factory takes either, a reference resolving
// the same way whichever it is, and it is read off this declaration onto each
// agent run record at the run.
type AccountKind string

const (
	// AccountPerson is a person's own account.
	AccountPerson AccountKind = "person"
	// AccountOrganisation is an organisation's.
	AccountOrganisation AccountKind = "organisation"
)

// AccountKinds is the two. The CHECK in [DDL] lists the same two, and
// TestDDLListsEveryAccountKind fails if the two stop agreeing.
var AccountKinds = []AccountKind{AccountPerson, AccountOrganisation}

// PeriodUnit is the unit a ceiling's period length is authored in.
type PeriodUnit string

const (
	// PeriodDay is a length in days.
	PeriodDay PeriodUnit = "day"
	// PeriodMonth is a length in calendar months, which is what a provider's
	// billing cycle is usually authored in.
	PeriodMonth PeriodUnit = "month"
)

// PeriodUnits is the two. The CHECK in [DDL] lists the same two, and
// TestDDLListsEveryPeriodUnit fails if the two stop agreeing.
var PeriodUnits = []PeriodUnit{PeriodDay, PeriodMonth}

// DateLayout is how a ceiling's start date is stored: the calendar date alone,
// the zone it was authored in being a field beside it.
const DateLayout = "2006-01-02"

var (
	// ErrCredentialNameEmpty is returned for a write naming no credential.
	ErrCredentialNameEmpty = errors.New("people: a lent credential is named")
	// ErrAccountKindUnknown is returned for an account kind outside
	// [AccountKinds]. The declaration holds whether the account is a person's
	// own or an organisation's, so a lend names one.
	ErrAccountKindUnknown = errors.New("people: the account is a person's own or an organisation's")
	// ErrNoCredential is returned where no row of [CredentialTable] names
	// that credential.
	ErrNoCredential = errors.New("people: no lent credential has that name")
	// ErrCeilingNotPositive is returned for a ceiling that is not above zero.
	// Where an owner authors none the credential is unbounded, which is the
	// absence of a ceiling and not a ceiling of nothing.
	ErrCeilingNotPositive = errors.New("people: a spend ceiling is above zero")
	// ErrCurrencyEmpty is returned for a ceiling or a rate naming no currency.
	ErrCurrencyEmpty = errors.New("people: the currency the rates are authored in is named")
	// ErrCurrencyDiffers is returned for a currency other than the one already
	// standing on that credential: one credential is one account at one
	// provider and one invoice, and a ceiling is in the currency its rates are
	// authored in.
	ErrCurrencyDiffers = errors.New("people: the credential already carries another currency")
	// ErrPeriodUnknown is returned for a period whose length is not above zero
	// or whose unit is outside [PeriodUnits].
	ErrPeriodUnknown = errors.New("people: a period is a length above zero in one of the units")
	// ErrStartDateUnknown is returned for a start date that is not a date in
	// [DateLayout] or that names no time zone.
	ErrStartDateUnknown = errors.New("people: a start date is a date and the zone it was authored in")
)

// Ceiling is the limit an owner authors on what the factory spends through one
// credential: an amount in the currency the owner's rates are authored in,
// over a period authored as a length and a start date, one period following
// another from that date.
type Ceiling struct {
	// Amount is the limit, and zero where none is authored — where none is,
	// the credential is unbounded and the provider's quota is the only stop.
	Amount float64
	// Currency is what Amount and the rates on this credential are in.
	Currency string
	// Length and Unit are the period an owner authored.
	Length int
	Unit   PeriodUnit
	// StartDate is the date the first period starts, in [DateLayout], and
	// StartZone the IANA time zone it was authored in. A period ends at that
	// zone's midnight.
	StartDate string
	StartZone string
}

// Authored reports whether a ceiling stands on the credential.
func (c Ceiling) Authored() bool { return c.Amount > 0 }

// PeriodStartAt is the start of the period that contains at, as a timestamp in
// [record.TimeLayout] — the period start the sum of a credential's converted
// amounts is taken from. Which period a run falls in is on no record: it is
// derived here from the start date, the zone and the length in force, so a
// period lengthened or re-anchored later re-buckets every run already written.
//
// A time before the start date is before every period, and the first period's
// start is what it answers with, so the sum then covers the first period.
func (c Ceiling) PeriodStartAt(at time.Time) (string, error) {
	if err := c.validate(); err != nil {
		return "", err
	}
	zone, err := time.LoadLocation(c.StartZone)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrStartDateUnknown, c.StartZone, err)
	}
	day, err := time.ParseInLocation(DateLayout, c.StartDate, zone)
	if err != nil {
		return "", fmt.Errorf("%w: %q: %w", ErrStartDateUnknown, c.StartDate, err)
	}

	start := day
	for {
		next := c.next(start)
		if next.After(at) {
			return record.FormatTime(start), nil
		}
		start = next
	}
}

// next is the start of the period after the one starting at start. A length in
// days is added as days and one in months as calendar months, so a period
// authored to a provider's billing cycle stays on the same day of the month.
func (c Ceiling) next(start time.Time) time.Time {
	if c.Unit == PeriodMonth {
		return start.AddDate(0, c.Length, 0)
	}
	return start.AddDate(0, 0, c.Length)
}

func (c Ceiling) validate() error {
	if c.Amount <= 0 {
		return fmt.Errorf("%w: %v", ErrCeilingNotPositive, c.Amount)
	}
	if c.Currency == "" {
		return ErrCurrencyEmpty
	}
	if c.Length <= 0 || !slices.Contains(PeriodUnits, c.Unit) {
		return fmt.Errorf("%w: %d %q", ErrPeriodUnknown, c.Length, c.Unit)
	}
	if c.StartDate == "" || c.StartZone == "" {
		return fmt.Errorf("%w: %q in %q", ErrStartDateUnknown, c.StartDate, c.StartZone)
	}
	return nil
}

// Credential is one row of [CredentialTable]: a credential somebody lent the
// factory, the account behind it, and the ceiling authored on it.
type Credential struct {
	ID    string
	Actor record.Actor
	At    string
	// Key is the per-person key of whoever lent it, never a name.
	Key string
	// Name is the credential's name, which is the whole of the handle on the
	// account: the factory stores a reference and never the credential.
	Name string
	// Kind is whether the account is a person's own or an organisation's.
	Kind AccountKind
	// Ceiling is the limit authored on it, and is unauthored where nobody has
	// authored one. Its Currency stands as soon as a rate or a ceiling names
	// one, whether or not there is an amount.
	Ceiling Ceiling
	// WithdrawnAt is when the credential was taken back, and is empty while it
	// stands.
	WithdrawnAt string
}

// Lent reports whether the credential still stands.
func (c Credential) Lent() bool { return c.WithdrawnAt == "" }

const selectCredential = `select id, actor_kind, actor_key, actor_key_basis, at, person_key, credential_name,
	account_kind, currency, ceiling_amount, period_length, period_unit, period_start_date, period_start_zone,
	withdrawn_at
	from ` + CredentialTable

func scanCredential(row pgx.Row) (Credential, error) {
	var c Credential
	var kind, basis, accountKind, unit string
	var amount *float64
	if err := row.Scan(&c.ID, &kind, &c.Actor.Key, &basis, &c.At, &c.Key, &c.Name,
		&accountKind, &c.Ceiling.Currency, &amount, &c.Ceiling.Length, &unit,
		&c.Ceiling.StartDate, &c.Ceiling.StartZone, &c.WithdrawnAt); err != nil {
		return Credential{}, err
	}
	c.Actor.Kind = record.Kind(kind)
	c.Actor.Basis = record.Basis(basis)
	c.Kind = AccountKind(accountKind)
	c.Ceiling.Unit = PeriodUnit(unit)
	if amount != nil {
		c.Ceiling.Amount = *amount
	}
	return c, nil
}

// CredentialNamed is the lent credential of that name, standing or taken back,
// and false where nobody has lent one under it. It takes the pool and not a
// [Writer], because reading who lent what is not a reason to be handed the
// thing that declares it.
func CredentialNamed(ctx context.Context, pool *pgxpool.Pool, name string) (Credential, bool, error) {
	if name == "" {
		return Credential{}, false, ErrCredentialNameEmpty
	}
	c, err := scanCredential(pool.QueryRow(ctx, selectCredential+` where credential_name = $1`, name))
	if errors.Is(err, pgx.ErrNoRows) {
		return Credential{}, false, nil
	} else if err != nil {
		return Credential{}, false, fmt.Errorf("people: reading the credential %s: %w", name, err)
	}
	return c, true, nil
}

// Credentials is every lent credential, standing or taken back, in the order
// it was lent.
func Credentials(ctx context.Context, pool *pgxpool.Pool) ([]Credential, error) {
	rows, err := pool.Query(ctx, selectCredential+` order by at, id`)
	if err != nil {
		return nil, fmt.Errorf("people: reading the lent credentials: %w", err)
	}
	defer rows.Close()

	var read []Credential
	for rows.Next() {
		c, err := scanCredential(rows)
		if err != nil {
			return nil, fmt.Errorf("people: reading a lent credential: %w", err)
		}
		read = append(read, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: reading the lent credentials: %w", err)
	}
	return read, nil
}

// Lend declares that key lent this credential and whether the account behind
// it is a person's own or an organisation's. Lending the same name twice is
// one row: the insert conflicts on the name and clears any taking back, so
// re-lending a credential somebody took back is how they lend it again. The
// ceiling and the rates on it are untouched.
func (w *Writer) Lend(ctx context.Context, actor record.Actor, key, name string, kind AccountKind) (Credential, error) {
	if err := validateOwner(actor); err != nil {
		return Credential{}, err
	}
	if key == "" {
		return Credential{}, ErrKeyEmpty
	}
	if name == "" {
		return Credential{}, ErrCredentialNameEmpty
	}
	if !slices.Contains(AccountKinds, kind) {
		return Credential{}, fmt.Errorf("%w: %q", ErrAccountKindUnknown, kind)
	}

	if err := w.appendVersion(ctx, actor, func(current declared) declared {
		return withLent(current, key, name, kind)
	}); err != nil {
		return Credential{}, err
	}

	if err := w.write(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into `+CredentialTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, credential_name,
			 account_kind, currency, ceiling_amount, period_length, period_unit, period_start_date,
			 period_start_zone, withdrawn_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', null, 0, '', '', '', '')
			on conflict (credential_name) do update
			set withdrawn_at = '', person_key = excluded.person_key, account_kind = excluded.account_kind`,
			record.NewID(CredentialIDPrefix), CredentialFormatVersion, string(actor.Kind), actor.Key,
			string(actor.Basis), record.Now(), key, name, string(kind))
		return err
	}); err != nil {
		return Credential{}, fmt.Errorf("people: declaring that %s lent %s: %w", key, name, err)
	}

	lent, _, err := CredentialNamed(ctx, w.pool, name)
	return lent, err
}

// AuthorCeiling authors the spend ceiling on one lent credential: the amount,
// the currency it and the rates on this credential are in, and the period as a
// length and a start date with the zone it was authored in. Authoring it again
// replaces it, which is how one is raised, lowered, lengthened or re-anchored.
func (w *Writer) AuthorCeiling(ctx context.Context, actor record.Actor, name string, ceiling Ceiling) (Credential, error) {
	if err := validateOwner(actor); err != nil {
		return Credential{}, err
	}
	if err := ceiling.validate(); err != nil {
		return Credential{}, err
	}
	if _, err := ceiling.PeriodStartAt(time.Now()); err != nil {
		return Credential{}, err
	}
	standing, found, err := CredentialNamed(ctx, w.pool, name)
	if err != nil {
		return Credential{}, err
	}
	if !found {
		return Credential{}, fmt.Errorf("%w: %s", ErrNoCredential, name)
	}
	if standing.Ceiling.Currency != "" && standing.Ceiling.Currency != ceiling.Currency {
		return Credential{}, fmt.Errorf("%w: %s is in %s and the ceiling is in %s",
			ErrCurrencyDiffers, name, standing.Ceiling.Currency, ceiling.Currency)
	}

	if err := w.appendVersion(ctx, actor, func(current declared) declared {
		return withCeiling(current, name, ceiling)
	}); err != nil {
		return Credential{}, err
	}

	if err := w.write(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update `+CredentialTable+`
			set currency = $1, ceiling_amount = $2, period_length = $3, period_unit = $4,
			    period_start_date = $5, period_start_zone = $6
			where credential_name = $7`,
			ceiling.Currency, ceiling.Amount, ceiling.Length, string(ceiling.Unit),
			ceiling.StartDate, ceiling.StartZone, name)
		return err
	}); err != nil {
		return Credential{}, fmt.Errorf("people: authoring the ceiling on %s: %w", name, err)
	}

	authored, _, err := CredentialNamed(ctx, w.pool, name)
	return authored, err
}

// TakeBack marks a lent credential taken back and keeps the row, the way
// [Writer.Withdraw] keeps a holding: the rows that named it are still readable
// against what lent it.
func (w *Writer) TakeBack(ctx context.Context, actor record.Actor, name string) (Credential, error) {
	if err := validateOwner(actor); err != nil {
		return Credential{}, err
	}
	if _, found, err := CredentialNamed(ctx, w.pool, name); err != nil {
		return Credential{}, err
	} else if !found {
		return Credential{}, fmt.Errorf("%w: %s", ErrNoCredential, name)
	}

	if err := w.appendVersion(ctx, actor, func(current declared) declared {
		return withTakenBack(current, name)
	}); err != nil {
		return Credential{}, err
	}

	if err := w.write(ctx, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `update `+CredentialTable+`
			set withdrawn_at = $1 where credential_name = $2 and withdrawn_at = ''`, record.Now(), name)
		return err
	}); err != nil {
		return Credential{}, fmt.Errorf("people: taking back %s: %w", name, err)
	}

	taken, _, err := CredentialNamed(ctx, w.pool, name)
	return taken, err
}

// write runs one statement in a fenced transaction, which every write in this
// file does the same way.
func (w *Writer) write(ctx context.Context, statement func(pgx.Tx) error) error {
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return err
	}
	if err := statement(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("people: committing: %w", err)
	}
	return nil
}

// validateOwner is the refusal every write in this package makes: the
// declaration is the owner's, and a component writing it would be the factory
// deciding what it may spend and on whose account.
func validateOwner(actor record.Actor) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Key)
	}
	return nil
}
