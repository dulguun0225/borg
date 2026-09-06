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

// Rederive rewrites every duty the newest policy version's declaration names
// that this table does not already hold standing — the factory's start
// finishing a write a stop interrupted between the version and the
// declaration. It appends no version: it writes no holding the version does
// not already name.
//
// A factory that has appended no policy version yet re-derives nothing,
// which is [policy.ErrNoVersion] read and swallowed rather than returned:
// an empty declaration is a working factory.
//
// It re-derives duties only. [policy.PersonDeclaration] carries no
// obligation — declare.go's snapshotOf says why — so a key whose only
// standing holding was an obligation is not re-created here; that gap is
// policy's type and not this package's write.
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
		if d.Holds() && d.Duty != 0 {
			holds[[2]string{d.Key, fmt.Sprint(d.Duty)}] = true
		}
	}

	var rewritten []string
	for _, p := range newest.Declaration.People {
		for _, duty := range p.Duties {
			if holds[[2]string{p.Key, fmt.Sprint(duty)}] {
				continue
			}
			if err := rederiveOne(ctx, pool, token, newest.Actor, p.Key, Duty(duty)); err != nil {
				return rewritten, err
			}
			rewritten = append(rewritten, p.Key)
		}
	}
	return rewritten, nil
}

// rederiveOne writes back the one row the version names but the table does
// not hold standing, in its own fenced transaction. The actor is the human
// who authored the version being re-derived, the table admitting no other
// kind — the version already carries who decided this, and the
// re-derivation is that decision reaching a stop's table.
func rederiveOne(ctx context.Context, pool *pgxpool.Pool, token lease.Token, actor record.Actor, key string, duty Duty) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("people: beginning the re-derivation of %s's duty %d: %w", key, duty, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, duty, obligation,
		 credential_account, spend_ceiling, withdrawn_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, '', '', 0, '')
		on conflict (person_key, duty, obligation) do update set withdrawn_at = ''`,
		record.NewID(HoldingIDPrefix), FormatVersion, string(actor.Kind), actor.Key, string(actor.Basis),
		record.Now(), key, int(duty),
	); err != nil {
		return fmt.Errorf("people: re-deriving that %s holds duty %d: %w", key, duty, err)
	}
	return tx.Commit(ctx)
}
