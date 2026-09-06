package people

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrUnitEmpty is returned for a rate naming no kind of unit.
	ErrUnitEmpty = errors.New("people: a rate names the kind of unit it prices")
	// ErrModelVersionEmpty is returned for a rate naming no model version. A
	// rate is per kind of unit, per model version and effort.
	ErrModelVersionEmpty = errors.New("people: a rate names the model version it prices")
	// ErrRateNegative is returned for a rate below zero. A provider's price is
	// what the owner agreed, and a negative one prices nothing.
	ErrRateNegative = errors.New("people: a rate is not below zero")
)

// Rate is one rate an owner authored on one lent credential: the price of one
// kind of unit the provider returns, for one model version at one effort. It
// is the owner's agreed price and not a fact read from the provider.
type Rate struct {
	ID    string
	Actor record.Actor
	At    string
	// CredentialName is the lent credential this rate is authored beside.
	CredentialName string
	// Unit is the kind the provider counts apart — input, output, cached
	// input, or any other it returns.
	Unit string
	// ModelVersion is the model version the rate prices, and Effort the effort
	// it prices at, empty where the provider offers none.
	ModelVersion string
	Effort       string
	// Rate is the price of one unit, in the currency the credential carries.
	Rate float64
}

const selectRate = `select id, actor_kind, actor_key, actor_key_basis, at, credential_name, unit,
	model_version, effort, rate
	from ` + RateTable

func scanRate(row pgx.Row) (Rate, error) {
	var r Rate
	var kind, basis string
	if err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At, &r.CredentialName, &r.Unit,
		&r.ModelVersion, &r.Effort, &r.Rate); err != nil {
		return Rate{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	r.Actor.Basis = record.Basis(basis)
	return r, nil
}

// RatesFor is every rate authored on one credential, oldest first. It takes
// the pool and not a [Writer], because reading what an owner priced is not a
// reason to be handed the thing that authors it.
func RatesFor(ctx context.Context, pool *pgxpool.Pool, credentialName string) ([]Rate, error) {
	if credentialName == "" {
		return nil, ErrCredentialNameEmpty
	}
	return queryRates(ctx, pool, `where credential_name = $1 order by at, id`, credentialName)
}

// AllRates is every rate authored on any credential, oldest first.
func AllRates(ctx context.Context, pool *pgxpool.Pool) ([]Rate, error) {
	return queryRates(ctx, pool, `order by at, id`)
}

func queryRates(ctx context.Context, pool *pgxpool.Pool, where string, args ...any) ([]Rate, error) {
	rows, err := pool.Query(ctx, selectRate+` `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("people: reading the rates: %w", err)
	}
	defer rows.Close()

	var read []Rate
	for rows.Next() {
		r, err := scanRate(rows)
		if err != nil {
			return nil, fmt.Errorf("people: reading a rate: %w", err)
		}
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("people: reading the rates: %w", err)
	}
	return read, nil
}

// RateFor is the rate among rates for one kind of unit at one model version
// and effort, and false where the owner authored none — which is what makes a
// run's converted amount absent and a credential under a ceiling fail closed.
func RateFor(rates []Rate, unit, modelVersion, effort string) (float64, bool) {
	for _, r := range rates {
		if r.Unit == unit && r.ModelVersion == modelVersion && r.Effort == effort {
			return r.Rate, true
		}
	}
	return 0, false
}

// Convert is the converted amount a run's units come to at these rates: each
// kind's units at the rate authored for that kind, model version and effort,
// summed. The second answer is the kinds the rates do not cover, sorted; where
// it is not empty there is no amount, which is the absent converted amount a
// spend ceiling fails closed on.
//
// It is what the component that performed a run reads the declaration for. The
// amount goes onto that run's own record, so a rate corrected later does not
// reprice what the record already wrote.
func Convert(rates []Rate, modelVersion, effort string, units map[string]int64) (float64, []string) {
	var amount float64
	var unpriced []string
	for unit, count := range units {
		rate, found := RateFor(rates, unit, modelVersion, effort)
		if !found {
			unpriced = append(unpriced, unit)
			continue
		}
		amount += float64(count) * rate
	}
	sort.Strings(unpriced)
	if len(unpriced) > 0 {
		return 0, unpriced
	}
	return amount, nil
}

// AuthorRate authors the price of one kind of unit on one lent credential, for
// one model version at one effort, in currency. Authoring the same kind, model
// version and effort again corrects the rate, and the correction names who
// made it: what a past run recorded is on that run's record and is not
// repriced.
func (w *Writer) AuthorRate(ctx context.Context, actor record.Actor, credentialName, currency,
	unit, modelVersion, effort string, rate float64) (Rate, error) {
	if err := validateOwner(actor); err != nil {
		return Rate{}, err
	}
	if unit == "" {
		return Rate{}, ErrUnitEmpty
	}
	if modelVersion == "" {
		return Rate{}, ErrModelVersionEmpty
	}
	if currency == "" {
		return Rate{}, ErrCurrencyEmpty
	}
	if rate < 0 {
		return Rate{}, fmt.Errorf("%w: %v", ErrRateNegative, rate)
	}
	standing, found, err := CredentialNamed(ctx, w.pool, credentialName)
	if err != nil {
		return Rate{}, err
	}
	if !found {
		return Rate{}, fmt.Errorf("%w: %s", ErrNoCredential, credentialName)
	}
	if standing.Ceiling.Currency != "" && standing.Ceiling.Currency != currency {
		return Rate{}, fmt.Errorf("%w: %s is in %s and the rate is in %s",
			ErrCurrencyDiffers, credentialName, standing.Ceiling.Currency, currency)
	}

	if err := w.appendVersion(ctx, actor, func(current declared) declared {
		return withRate(current, credentialName, unit, modelVersion, effort, rate)
	}); err != nil {
		return Rate{}, err
	}

	if err := w.write(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `update `+CredentialTable+`
			set currency = $1 where credential_name = $2 and currency = ''`, currency, credentialName); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `insert into `+RateTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, credential_name, unit,
			 model_version, effort, rate)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			on conflict (credential_name, unit, model_version, effort) do update
			set rate = excluded.rate, actor_kind = excluded.actor_kind, actor_key = excluded.actor_key,
			    actor_key_basis = excluded.actor_key_basis, at = excluded.at`,
			record.NewID(RateIDPrefix), RateFormatVersion, string(actor.Kind), actor.Key, string(actor.Basis),
			record.Now(), credentialName, unit, modelVersion, effort, rate)
		return err
	}); err != nil {
		return Rate{}, fmt.Errorf("people: authoring the rate of %s on %s: %w", unit, credentialName, err)
	}

	authored, err := scanRate(w.pool.QueryRow(ctx, selectRate+`
		where credential_name = $1 and unit = $2 and model_version = $3 and effort = $4`,
		credentialName, unit, modelVersion, effort))
	if err != nil {
		return Rate{}, fmt.Errorf("people: reading the rate of %s on %s back: %w", unit, credentialName, err)
	}
	return authored, nil
}
