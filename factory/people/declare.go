package people

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// Writer is the one writer of holdings: the owner, at People.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
	// versions is where a write other than to the mapping appends a policy
	// version, with People as caller, before this package's own table is
	// written — the version first and the declaration second, the order
	// every owner write takes. A nil value appends no version, which is a
	// factory composed with no policy writer: What defines it in doc.go says
	// nothing calls that path outside a test.
	versions *policy.Factory
}

// NewWriter returns the writer over pool, fencing every write with token and
// appending a policy version for every write through versions.
func NewWriter(pool *pgxpool.Pool, token lease.Token, versions *policy.Factory) *Writer {
	return &Writer{pool: pool, token: token, versions: versions}
}

// Declare writes that key holds this duty or this obligation. Declaring the
// same pair twice is one row: the insert conflicts on the key and the
// holding and clears any withdrawal, so re-declaring a holding somebody gave
// up is how they take it back. The row keeps the first declaration's actor
// and time.
func (w *Writer) Declare(ctx context.Context, actor record.Actor, key string, holding Holding) (Declaration, error) {
	if err := validateOwnerWrite(actor, key, holding); err != nil {
		return Declaration{}, err
	}

	if err := w.appendVersion(ctx, actor, func(current []Declaration) []Declaration {
		return withDeclared(current, key, holding)
	}); err != nil {
		return Declaration{}, err
	}

	d := Declaration{
		ID: record.NewID(HoldingIDPrefix), Actor: actor, At: record.Now(),
		Key: key, Duty: holding.Duty, Obligation: holding.Obligation,
	}
	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Declaration{}, fmt.Errorf("people: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Declaration{}, err
	}
	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 credential_account, spend_ceiling, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', 0, '')
		on conflict (person_key, duty, obligation) do update set withdrawn_at = ''`,
		d.ID, FormatVersion, string(d.Actor.Kind), d.Actor.Key, string(d.Actor.Basis), d.At, d.Key, int(d.Duty), string(d.Obligation),
	)
	if err != nil {
		return Declaration{}, fmt.Errorf("people: declaring that %s holds %s: %w", key, holding, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Declaration{}, fmt.Errorf("people: committing: %w", err)
	}
	// The row that stands may be the one this call inserted or the one a
	// previous call did, and the conflict target does not say which.
	// Reading it back is what makes the returned declaration the row rather
	// than what was attempted.
	return ByHolding(ctx, w.pool, key, holding)
}

// Withdraw marks the holding ended and keeps the row.
func (w *Writer) Withdraw(ctx context.Context, actor record.Actor, id string) (Declaration, error) {
	if err := actor.Validate(); err != nil {
		return Declaration{}, err
	}
	if actor.Kind != record.KindHuman {
		return Declaration{}, fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Key)
	}

	existing, err := Get(ctx, w.pool, id)
	if err != nil {
		return Declaration{}, err
	}
	if err := w.appendVersion(ctx, actor, func(current []Declaration) []Declaration {
		return withWithdrawn(current, id)
	}); err != nil {
		return Declaration{}, err
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Declaration{}, fmt.Errorf("people: beginning: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Declaration{}, err
	}
	if _, err := tx.Exec(ctx, `update `+Table+`
		set withdrawn_at = $1 where id = $2 and withdrawn_at = ''`, record.Now(), id); err != nil {
		return Declaration{}, fmt.Errorf("people: withdrawing %s: %w", id, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Declaration{}, fmt.Errorf("people: committing: %w", err)
	}
	// Either it did not exist, it was withdrawn already, or this call just
	// withdrew it. All three read back the same way: what Withdraw
	// promises is that the holding does not stand after it returns.
	_ = existing
	return Get(ctx, w.pool, id)
}

func validateOwnerWrite(actor record.Actor, key string, holding Holding) error {
	if err := actor.Validate(); err != nil {
		return err
	}
	if actor.Kind != record.KindHuman {
		return fmt.Errorf("%w: %s %q", ErrNotAnOwner, actor.Kind, actor.Key)
	}
	if key == "" {
		return ErrKeyEmpty
	}
	return holding.validate()
}

// appendVersion appends the policy version for a write to the holding table:
// it reads every row that still stands, hands it to effect for the caller to
// say what this write changes, and builds the snapshot [policy.Factory]
// carries from the result — before this package's own table is touched, the
// order every owner write to the declaration takes. A nil [Writer.versions]
// appends nothing.
func (w *Writer) appendVersion(ctx context.Context, actor record.Actor, effect func([]Declaration) []Declaration) error {
	if w.versions == nil {
		return nil
	}
	current, err := All(ctx, w.pool)
	if err != nil {
		return err
	}
	after := effect(current)
	_, err = w.versions.AppendPeopleVersion(ctx, actor, snapshotOf(after))
	return err
}

// withDeclared is current with key's holding of holding standing, added or
// un-withdrawn — the in-memory effect of what [Writer.Declare]'s insert is
// about to do, computed before it runs so the version named can be appended
// first.
func withDeclared(current []Declaration, key string, holding Holding) []Declaration {
	for n, d := range current {
		if d.Key == key && d.Duty == holding.Duty && d.Obligation == holding.Obligation {
			current[n].WithdrawnAt = ""
			return current
		}
	}
	return append(current, Declaration{Key: key, Duty: holding.Duty, Obligation: holding.Obligation})
}

// withWithdrawn is current with the row named id marked withdrawn — the
// in-memory effect of what [Writer.Withdraw]'s update is about to do.
func withWithdrawn(current []Declaration, id string) []Declaration {
	for n, d := range current {
		if d.ID == id {
			current[n].WithdrawnAt = record.Now()
		}
	}
	return current
}

// snapshotOf is the [policy.DeclarationSnapshot] a set of holdings reads as:
// one [policy.PersonDeclaration] per key that still holds something,
// naming its duties and the lent credential fields this package's schema
// carries.
//
// It carries no obligation: [policy.PersonDeclaration] has no field for
// one, so a write recording that a key holds an obligation appends a
// version whose snapshot is silent about it. Extending that type is
// package policy's, and this package does not import it for writing.
func snapshotOf(rows []Declaration) policy.DeclarationSnapshot {
	byKey := map[string]*policy.PersonDeclaration{}
	var order []string
	for _, d := range rows {
		if !d.Holds() {
			continue
		}
		p, found := byKey[d.Key]
		if !found {
			p = &policy.PersonDeclaration{Key: d.Key}
			byKey[d.Key] = p
			order = append(order, d.Key)
		}
		if d.Duty != 0 && !slices.Contains(p.Duties, int(d.Duty)) {
			p.Duties = append(p.Duties, int(d.Duty))
		}
		if d.CredentialAccount != "" {
			p.CredentialName = d.CredentialAccount
		}
		if d.SpendCeiling != 0 {
			p.SpendCeiling = d.SpendCeiling
		}
	}
	snap := policy.DeclarationSnapshot{}
	for _, key := range order {
		p := byKey[key]
		slices.Sort(p.Duties)
		snap.People = append(snap.People, *p)
	}
	return snap
}
