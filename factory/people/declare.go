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

// Writer is the one writer of the declaration: the owner, at People. Holdings
// are declare.go's, the lent credential and its ceiling credential.go's, and
// the rates rate.go's.
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

	if err := w.appendVersion(ctx, actor, func(current declared) declared {
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
		 withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, '')
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
	if err := validateOwner(actor); err != nil {
		return Declaration{}, err
	}

	existing, err := Get(ctx, w.pool, id)
	if err != nil {
		return Declaration{}, err
	}
	if err := w.appendVersion(ctx, actor, func(current declared) declared {
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
	if err := validateOwner(actor); err != nil {
		return err
	}
	if key == "" {
		return ErrKeyEmpty
	}
	return holding.validate()
}

// declared is the whole of what the People declaration holds, as one write is
// about to leave it: the holdings, the lent credentials, and the rates on
// them. It is what an effect below is handed and what a version's snapshot is
// built from.
type declared struct {
	holdings    []Declaration
	credentials []Credential
	rates       []Rate
}

// appendVersion appends the policy version for a write to this package's
// tables: it reads everything that stands, hands it to effect for the caller
// to say what this write changes, and builds the snapshot [policy.Factory]
// carries from the result — before any table is touched, the order every
// owner write to the declaration takes. A nil [Writer.versions] appends
// nothing.
func (w *Writer) appendVersion(ctx context.Context, actor record.Actor, effect func(declared) declared) error {
	if w.versions == nil {
		return nil
	}
	holdings, err := All(ctx, w.pool)
	if err != nil {
		return err
	}
	credentials, err := Credentials(ctx, w.pool)
	if err != nil {
		return err
	}
	rates, err := AllRates(ctx, w.pool)
	if err != nil {
		return err
	}
	after := effect(declared{holdings: holdings, credentials: credentials, rates: rates})
	_, err = w.versions.AppendPeopleVersion(ctx, actor, snapshotOf(after))
	return err
}

// withDeclared is current with key's holding of holding standing, added or
// un-withdrawn — the in-memory effect of what [Writer.Declare]'s insert is
// about to do, computed before it runs so the version named can be appended
// first.
func withDeclared(current declared, key string, holding Holding) declared {
	for n, d := range current.holdings {
		if d.Key == key && d.Duty == holding.Duty && d.Obligation == holding.Obligation {
			current.holdings[n].WithdrawnAt = ""
			return current
		}
	}
	current.holdings = append(current.holdings,
		Declaration{Key: key, Duty: holding.Duty, Obligation: holding.Obligation})
	return current
}

// withWithdrawn is current with the holding named id marked withdrawn — the
// in-memory effect of what [Writer.Withdraw]'s update is about to do.
func withWithdrawn(current declared, id string) declared {
	for n, d := range current.holdings {
		if d.ID == id {
			current.holdings[n].WithdrawnAt = record.Now()
		}
	}
	return current
}

// withLent is current with key lending that credential, added or lent again —
// the in-memory effect of [Writer.Lend]'s insert.
func withLent(current declared, key, name string, kind AccountKind) declared {
	for n, c := range current.credentials {
		if c.Name == name {
			current.credentials[n].Key = key
			current.credentials[n].Kind = kind
			current.credentials[n].WithdrawnAt = ""
			return current
		}
	}
	current.credentials = append(current.credentials, Credential{Key: key, Name: name, Kind: kind})
	return current
}

// withCeiling is current with that ceiling on the named credential — the
// in-memory effect of [Writer.AuthorCeiling]'s update.
func withCeiling(current declared, name string, ceiling Ceiling) declared {
	for n, c := range current.credentials {
		if c.Name == name {
			current.credentials[n].Ceiling = ceiling
		}
	}
	return current
}

// withTakenBack is current with the named credential taken back — the
// in-memory effect of [Writer.TakeBack]'s update.
func withTakenBack(current declared, name string) declared {
	for n, c := range current.credentials {
		if c.Name == name {
			current.credentials[n].WithdrawnAt = record.Now()
		}
	}
	return current
}

// withRate is current with that rate authored on the named credential, added
// or corrected — the in-memory effect of [Writer.AuthorRate]'s insert.
func withRate(current declared, name, unit, modelVersion, effort string, rate float64) declared {
	for n, r := range current.rates {
		if r.CredentialName == name && r.Unit == unit && r.ModelVersion == modelVersion && r.Effort == effort {
			current.rates[n].Rate = rate
			return current
		}
	}
	current.rates = append(current.rates, Rate{
		CredentialName: name, Unit: unit, ModelVersion: modelVersion, Effort: effort, Rate: rate,
	})
	return current
}

// snapshotOf is the [policy.DeclarationSnapshot] the declaration reads as: one
// [policy.PersonDeclaration] per key that still holds a duty, and one more per
// credential that still stands, naming the key that lent it, the ceiling's
// amount and the rates authored on it.
//
// A credential is a row of its own because [policy.PersonDeclaration] holds
// one credential name and a key may lend more than one. It carries no
// obligation, no account kind, no currency and no period, none of which that
// type has a field for. Extending it is package policy's, and this package
// does not import it for writing.
func snapshotOf(state declared) policy.DeclarationSnapshot {
	byKey := map[string]*policy.PersonDeclaration{}
	var order []string
	for _, d := range state.holdings {
		if !d.Holds() || d.Duty == 0 {
			continue
		}
		p, found := byKey[d.Key]
		if !found {
			p = &policy.PersonDeclaration{Key: d.Key}
			byKey[d.Key] = p
			order = append(order, d.Key)
		}
		if !slices.Contains(p.Duties, int(d.Duty)) {
			p.Duties = append(p.Duties, int(d.Duty))
		}
	}
	snap := policy.DeclarationSnapshot{}
	for _, key := range order {
		p := byKey[key]
		slices.Sort(p.Duties)
		snap.People = append(snap.People, *p)
	}
	for _, c := range state.credentials {
		if !c.Lent() {
			continue
		}
		lent := policy.PersonDeclaration{
			Key: c.Key, CredentialName: c.Name, SpendCeiling: c.Ceiling.Amount,
		}
		for _, r := range state.rates {
			if r.CredentialName != c.Name {
				continue
			}
			lent.Rates = append(lent.Rates, policy.UnitRate{
				Unit: r.Unit, ModelVersion: r.ModelVersion, Effort: r.Effort, Rate: r.Rate,
			})
		}
		snap.People = append(snap.People, lent)
	}
	return snap
}
