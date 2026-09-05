package agentrun

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// columns is every column of the table, in the order [scan] reads them. It is
// written once because every read goes through it, and a column added to one of
// several select lists is a bug the compiler cannot see.
const columns = `id, actor_kind, actor_key, actor_key_basis, at,
	role, role_prompt_version_id, skill_version_ids, model_version, effort,
	credential_name, processing_location, lender_key, account_kind,
	item_id, stage, intent_id, input_manifest_id,
	units_by_kind, units_at, sources, rates_by_kind, converted_amount, currency,
	started_at, finished_at, outcome`

func scan(row pgx.Row) (Run, error) {
	var r Run
	var kind, basis, accountKind, skills, units, sources, rates string
	var amount *float64
	err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At,
		&r.Role, &r.RolePromptVersionID, &skills, &r.ModelVersion, &r.Effort,
		&r.CredentialName, &r.ProcessingLocation, &r.LenderKey, &accountKind,
		&r.ItemID, &r.Stage, &r.IntentID, &r.InputManifestID,
		&units, &r.UnitsAt, &sources, &rates, &amount, &r.Currency,
		&r.StartedAt, &r.FinishedAt, &r.Outcome)
	if err != nil {
		return Run{}, err
	}
	r.Actor.Kind = record.Kind(kind)
	r.Actor.Basis = record.Basis(basis)
	r.AccountKind = AccountKind(accountKind)
	r.SkillVersionIDs = splitLines(skills)
	r.Sources = splitLines(sources)
	if r.UnitsByKind, err = unmarshalUnits(units); err != nil {
		return Run{}, err
	}
	if r.RatesByKind, err = unmarshalRates(rates); err != nil {
		return Run{}, err
	}
	if amount != nil {
		r.ConvertedAmount, r.Priced = *amount, true
	}
	return r, nil
}

func query(ctx context.Context, pool *pgxpool.Pool, what, where string, args ...any) ([]Run, error) {
	rows, err := pool.Query(ctx, `select `+columns+` from `+Table+` `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("agentrun: reading %s: %w", what, err)
	}
	defer rows.Close()

	var read []Run
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("agentrun: reading a run of %s: %w", what, err)
		}
		read = append(read, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("agentrun: reading %s: %w", what, err)
	}
	return read, nil
}

// Get is one run record by id. It takes the pool and not a [Writer], because
// reading a run is not a reason to be handed the thing that writes them.
func Get(ctx context.Context, pool *pgxpool.Pool, id string) (Run, error) {
	r, err := scan(pool.QueryRow(ctx, `select `+columns+` from `+Table+` where id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return Run{}, fmt.Errorf("%w: %s", ErrNotFound, id)
	} else if err != nil {
		return Run{}, fmt.Errorf("agentrun: reading %s: %w", id, err)
	}
	return r, nil
}

// ForItem is every run that served one item, oldest first. What a stage cost is
// this read narrowed by the stage: the spend is on these records and never a
// field of the item, so an item's total derives from the records naming it
// rather than being written a second time.
func ForItem(ctx context.Context, pool *pgxpool.Pool, itemID string) ([]Run, error) {
	if itemID == "" {
		return nil, nil
	}
	return query(ctx, pool, "the runs of "+itemID, `where item_id = $1 order by at, id`, itemID)
}

// ForIntent is every run that served one intent, oldest first — the interview's
// rounds, the decompositions, and anything else put on the intent rather than
// on an item. Cost per feature is this read plus [ForItem] over the items
// decomposed from the intent.
func ForIntent(ctx context.Context, pool *pgxpool.Pool, intentID string) ([]Run, error) {
	if intentID == "" {
		return nil, nil
	}
	return query(ctx, pool, "the runs of "+intentID, `where intent_id = $1 order by at, id`, intentID)
}

// ByAuthorModel is every run of one model version, oldest first. The
// per-author prior is kept per model version, and two agents in different roles
// on one model are one author — so this is the read that answers what one
// author has done, and it is by version because a version that behaved
// differently under one name would average two authors into one prior.
func ByAuthorModel(ctx context.Context, pool *pgxpool.Pool, modelVersion string) ([]Run, error) {
	if modelVersion == "" {
		return nil, nil
	}
	return query(ctx, pool, "the runs of "+modelVersion, `where model_version = $1 order by at, id`, modelVersion)
}

// Spend is what a spend ceiling compares against: the sum of the converted
// amounts over a period, and the runs that had none.
type Spend struct {
	// Amount is the sum over the priced runs, in [Spend.Currency].
	Amount float64
	// Currency is the currency the amounts were converted into, and is empty
	// where no run in the period was priced.
	Currency string
	// Unpriced is the runs whose converted amount is absent because a kind they
	// returned has no rate. A credential under a ceiling fails closed on any of
	// them: the hold names the kind, the model version, and the effort that want
	// a rate, and it is cleared by authoring the rate rather than by authorising
	// an overage.
	Unpriced []Run
}

// SpendByCredentialSince is the sum a spend ceiling compares: the converted
// amounts of the runs naming one credential whose time falls in the period,
// reading nothing at the provider. periodStart is a stored timestamp in
// [record.TimeLayout], and the period is open at the far end — which period a
// run falls in is on no record, so it is derived at the read from the run's
// time against the start date and length in force, and a period lengthened
// later re-buckets every run already written.
//
// The time compared is units_at, the time the provider returned the units, and
// not when the record was written.
func SpendByCredentialSince(ctx context.Context, pool *pgxpool.Pool, credentialName, periodStart string) (Spend, error) {
	if credentialName == "" {
		return Spend{}, ErrCredentialNameEmpty
	}
	runs, err := query(ctx, pool, "the spend of "+credentialName,
		`where credential_name = $1 and units_at >= $2 order by units_at, id`, credentialName, periodStart)
	if err != nil {
		return Spend{}, err
	}

	var spend Spend
	for _, r := range runs {
		if !r.Priced {
			spend.Unpriced = append(spend.Unpriced, r)
			continue
		}
		spend.Amount += r.ConvertedAmount
		spend.Currency = r.Currency
	}
	return spend, nil
}
