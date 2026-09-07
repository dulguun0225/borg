package people

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// Rederive rewrites everything the newest policy version's declaration names
// that this package's tables do not already hold standing — the factory's
// start finishing a write a stop interrupted between the version and the
// declaration. It appends no version: it writes nothing the version does not
// already name.
//
// A factory that has appended no policy version yet re-derives nothing,
// which is [policy.ErrNoVersion] read and swallowed rather than returned: an
// empty declaration is a working factory.
//
// It re-derives every duty and every obligation a key holds, and, for a
// credential the version names, the account kind and the ceiling's currency
// and period — everything [policy.PersonDeclaration] now carries, since a
// version says what the declaration holds and not a subset of it. It returns
// the key of every row it rewrote, once per row.
func Rederive(ctx context.Context, pool *pgxpool.Pool, token lease.Token, reader *policy.Reader,
	p principal.Principal) ([]string, error) {
	newest, err := reader.Newest(ctx, p)
	if errors.Is(err, policy.ErrNoVersion) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	standing, err := All(ctx, pool)
	if err != nil {
		return nil, err
	}
	holds := map[[2]string]bool{}
	for _, d := range standing {
		if !d.Holds() {
			continue
		}
		holds[[2]string{d.Key, holdingKey(d.Duty, d.Obligation)}] = true
	}

	standingCredentials, err := Credentials(ctx, pool)
	if err != nil {
		return nil, err
	}
	byName := map[string]Credential{}
	for _, c := range standingCredentials {
		byName[c.Name] = c
	}

	var rewritten []string
	for _, person := range newest.Declaration.People {
		for _, duty := range person.Duties {
			if holds[[2]string{person.Key, holdingKey(Duty(duty), "")}] {
				continue
			}
			if err := rederiveHolding(ctx, pool, token, newest.Actor, person.Key, Duty(duty), ""); err != nil {
				return rewritten, err
			}
			rewritten = append(rewritten, person.Key)
		}
		for _, obligation := range person.Obligations {
			if holds[[2]string{person.Key, holdingKey(0, Obligation(obligation))}] {
				continue
			}
			if err := rederiveHolding(ctx, pool, token, newest.Actor, person.Key, 0, Obligation(obligation)); err != nil {
				return rewritten, err
			}
			rewritten = append(rewritten, person.Key)
		}
		if person.CredentialName == "" {
			continue
		}
		want := Credential{
			Key: person.Key, Name: person.CredentialName, Kind: AccountKind(person.AccountKind),
			Ceiling: Ceiling{
				Amount: person.SpendCeiling, Currency: person.Currency, Length: person.PeriodLength,
				Unit: PeriodUnit(person.PeriodUnit), StartDate: person.PeriodStartDate,
				StartZone: person.PeriodStartZone,
			},
		}
		if current, found := byName[person.CredentialName]; found && credentialMatches(current, want) {
			continue
		}
		if err := rederiveCredential(ctx, pool, token, newest.Actor, want); err != nil {
			return rewritten, err
		}
		rewritten = append(rewritten, person.Key)
	}
	return rewritten, nil
}

// holdingKey is the second half of the map key [Rederive] compares a
// standing row against: a duty and an obligation never both name the same
// row, so one string covers either.
func holdingKey(duty Duty, obligation Obligation) string {
	if obligation != "" {
		return "obligation:" + string(obligation)
	}
	return fmt.Sprintf("duty:%d", duty)
}

// credentialMatches reports whether the standing row already carries what
// the version names, so a re-derivation writes nothing where the table
// already agrees.
func credentialMatches(standing, want Credential) bool {
	return standing.Kind == want.Kind && standing.Ceiling == want.Ceiling
}

// rederiveHolding writes back the one row the version names but the table
// does not hold standing, in its own fenced transaction. The actor is the
// human who authored the version being re-derived, the table admitting no
// other kind — the version already carries who decided this, and the
// re-derivation is that decision reaching a stop's table. duty is zero where
// the row names an obligation instead, the shape [Holding] already keeps.
func rederiveHolding(ctx context.Context, pool *pgxpool.Pool, token lease.Token, actor record.Actor, key string, duty Duty, obligation Obligation) error {
	holding := Holding{Duty: duty, Obligation: obligation}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: beginning the re-derivation of %s's %s: %w", key, holding, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, '')
		on conflict (person_key, duty, obligation) do update set withdrawn_at = ''`,
		record.NewID(HoldingIDPrefix), FormatVersion, string(actor.Kind), actor.Key, string(actor.Basis),
		record.Now(), key, int(duty), string(obligation),
	); err != nil {
		return fmt.Errorf("people: re-deriving that %s holds %s: %w", key, holding, err)
	}
	return tx.Commit(ctx)
}

// rederiveCredential writes back the account kind and the ceiling the newest
// version names for one lent credential, in its own fenced transaction. It
// upserts on the credential name the way [Writer.Lend] does, so a row a stop
// left without one is created rather than only ever updated, and it leaves
// withdrawn_at untouched on a row that already exists — a re-derivation
// re-lends nothing and takes nothing back.
func rederiveCredential(ctx context.Context, pool *pgxpool.Pool, token lease.Token, actor record.Actor, want Credential) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: beginning the re-derivation of %s: %w", want.Name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into `+CredentialTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, credential_name,
		 account_kind, currency, ceiling_amount, period_length, period_unit, period_start_date,
		 period_start_zone, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, nullif($11, 0), $12, $13, $14, $15, '')
		on conflict (credential_name) do update
		set person_key = excluded.person_key, account_kind = excluded.account_kind,
		    currency = excluded.currency, ceiling_amount = excluded.ceiling_amount,
		    period_length = excluded.period_length, period_unit = excluded.period_unit,
		    period_start_date = excluded.period_start_date, period_start_zone = excluded.period_start_zone`,
		record.NewID(CredentialIDPrefix), CredentialFormatVersion, string(actor.Kind), actor.Key,
		string(actor.Basis), record.Now(), want.Key, want.Name, string(want.Kind), want.Ceiling.Currency,
		want.Ceiling.Amount, want.Ceiling.Length, string(want.Ceiling.Unit), want.Ceiling.StartDate,
		want.Ceiling.StartZone,
	); err != nil {
		return fmt.Errorf("people: re-deriving the credential %s: %w", want.Name, err)
	}
	return tx.Commit(ctx)
}
