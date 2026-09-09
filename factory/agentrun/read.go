package agentrun

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

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
	item_id, stage, intent_id, project_id, input_manifest_id,
	units_by_kind, units_at, sources, rates_by_kind, converted_amount, currency,
	started_at, finished_at, outcome`

func scan(row pgx.Row) (Run, error) {
	var r Run
	var kind, basis, accountKind, skills, units, sources, rates string
	var amount *float64
	err := row.Scan(&r.ID, &kind, &r.Actor.Key, &basis, &r.At,
		&r.Role, &r.RolePromptVersionID, &skills, &r.ModelVersion, &r.Effort,
		&r.CredentialName, &r.ProcessingLocation, &r.LenderKey, &accountKind,
		&r.ItemID, &r.Stage, &r.IntentID, &r.ProjectID, &r.InputManifestID,
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
// amounts over one period, the period it was taken over, and the runs that had
// none.
type Spend struct {
	// Amount is the sum over the priced runs, in [Spend.Currency].
	Amount float64
	// Currency is the currency the amounts were converted into, and is empty
	// where no run in the period was priced. It is one currency: a credential is
	// one account at one provider and one invoice, and a rate in a second
	// currency on it is refused where the rates are authored, so two currencies
	// among the runs of one period is a state no sum can be taken over.
	Currency string
	// PeriodStart and PeriodEnd are the period the sum covers, derived here and
	// on no record: start is the first instant in it and end the first instant
	// after it, both in [record.TimeLayout].
	PeriodStart string
	PeriodEnd   string
	// Unpriced is the runs whose converted amount is absent because a kind they
	// returned has no rate. A credential under a ceiling fails closed on any of
	// them: the hold names the kind, the model version, and the effort that want
	// a rate, and it is cleared by authoring the rate rather than by authorising
	// an overage.
	Unpriced []Run
}

// PeriodUnit is the unit a ceiling's period length is authored in. It is the
// spelling package people authors the ceiling in, repeated here because the sum
// is derived against it and the two packages are the spend ceiling's two halves.
type PeriodUnit string

const (
	// PeriodDay is a length in days.
	PeriodDay PeriodUnit = "day"
	// PeriodMonth is a length in calendar months, which is what a provider's
	// billing cycle is usually authored in.
	PeriodMonth PeriodUnit = "month"
)

// PeriodUnits is the two.
var PeriodUnits = []PeriodUnit{PeriodDay, PeriodMonth}

// DateLayout is how a ceiling's start date is authored: the calendar date
// alone, the zone it was authored in being a field beside it.
const DateLayout = "2006-01-02"

// Period is the ceiling's period as the owner authored it: the date one period
// starts, the zone that date was authored in, and the length one period runs
// for. Which period a run falls in is on no record, so [SpendByCredentialIn]
// derives it from these at the read — a period lengthened or re-anchored later
// re-buckets every run already written rather than stranding it in a period
// that no longer exists.
type Period struct {
	StartDate string
	StartZone string
	Length    int
	Unit      PeriodUnit
}

// containing is the period that holds at: the first instant in it and the first
// instant after it. One period follows another from the start date, and a
// length in calendar months is added as months so a period authored to a
// provider's billing cycle stays on the same day of the month. A time before
// the start date is before every period, and the first period is what it
// answers with.
func (p Period) containing(at time.Time) (time.Time, time.Time, error) {
	if p.Length <= 0 || !slices.Contains(PeriodUnits, p.Unit) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %d %q", ErrPeriodUnknown, p.Length, p.Unit)
	}
	// An empty zone is refused rather than read as UTC, which is what
	// time.LoadLocation makes of it: the start date carries the zone it was
	// authored in, and a period ends at that zone's midnight.
	if p.StartDate == "" || p.StartZone == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %q in %q", ErrStartDateUnknown, p.StartDate, p.StartZone)
	}
	zone, err := time.LoadLocation(p.StartZone)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %q: %w", ErrStartDateUnknown, p.StartZone, err)
	}
	// The date is parsed in the zone it was authored in, so a period ends at
	// that zone's midnight and not at UTC's.
	start, err := time.ParseInLocation(DateLayout, p.StartDate, zone)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: %q: %w", ErrStartDateUnknown, p.StartDate, err)
	}
	for {
		end := p.next(start)
		if end.After(at) {
			return start, end, nil
		}
		start = end
	}
}

func (p Period) next(start time.Time) time.Time {
	if p.Unit == PeriodMonth {
		return start.AddDate(0, p.Length, 0)
	}
	return start.AddDate(0, 0, p.Length)
}

// SpendByCredentialIn is the sum a spend ceiling compares: the converted
// amounts of the runs naming one credential whose time falls in the period that
// contains at, reading nothing at the provider. The period is derived here from
// the start date, the zone and the length in force rather than read off any
// record, and the sum is bounded at both ends — a run in the next period is a
// run this one does not count.
//
// The time compared is units_at, the time the provider returned the units, and
// not when the record was written.
func SpendByCredentialIn(ctx context.Context, pool *pgxpool.Pool, credentialName string,
	period Period, at time.Time) (Spend, error) {
	if credentialName == "" {
		return Spend{}, ErrCredentialNameEmpty
	}
	start, end, err := period.containing(at)
	if err != nil {
		return Spend{}, fmt.Errorf("agentrun: the period of %s: %w", credentialName, err)
	}
	spend := Spend{PeriodStart: record.FormatTime(start), PeriodEnd: record.FormatTime(end)}
	runs, err := query(ctx, pool, "the spend of "+credentialName,
		`where credential_name = $1 and units_at >= $2 and units_at < $3 order by units_at, id`,
		credentialName, spend.PeriodStart, spend.PeriodEnd)
	if err != nil {
		return Spend{}, err
	}

	for _, r := range runs {
		if !r.Priced {
			spend.Unpriced = append(spend.Unpriced, r)
			continue
		}
		if spend.Currency != "" && r.Currency != spend.Currency {
			return Spend{}, fmt.Errorf("%w: %s and %s among the runs of %s since %s",
				ErrCurrenciesDiffer, spend.Currency, r.Currency, credentialName, spend.PeriodStart)
		}
		spend.Amount += r.ConvertedAmount
		spend.Currency = r.Currency
	}
	return spend, nil
}
