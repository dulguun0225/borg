package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrRoleEmpty is returned by [Writer.Record] for a run naming no role.
	ErrRoleEmpty = errors.New("agentrun: the role is empty")
	// ErrModelVersionEmpty is returned for a run naming no model version. The
	// per-author prior is kept per version, so a run that named none would be a
	// run no prior can read.
	ErrModelVersionEmpty = errors.New("agentrun: the model version is empty")
	// ErrCredentialNameEmpty is returned for a run naming no credential. The
	// credential name is the whole of the handle on the account, and the spend
	// ceiling's sum is over it.
	ErrCredentialNameEmpty = errors.New("agentrun: the credential name is empty")
	// ErrAccountKindUnknown is returned for an account kind that is neither of
	// [AccountKinds], the empty one included: the account behind a credential
	// is a person's own or an organisation's, and the run reads which off the
	// People declaration.
	ErrAccountKindUnknown = errors.New("agentrun: the account kind is unknown")
	// ErrProcessingLocationEmpty is returned for a run naming no processing
	// location. It is the provider and the region the credential resolved to,
	// read off the fleet entry at the run, and a run without one names no party
	// the material was sent to.
	ErrProcessingLocationEmpty = errors.New("agentrun: the processing location is empty")
	// ErrLenderKeyEmpty is returned for a run naming no lender. It is the
	// per-person key the People declaration maps to whoever lent the credential,
	// read off that declaration at the run.
	ErrLenderKeyEmpty = errors.New("agentrun: the lender's per-person key is empty")
	// ErrServedNothing is returned for a run naming no item, no intent and no
	// project. What a run served is one of the five the design names and never
	// none; doc.go says which three of the five have a record to name.
	ErrServedNothing = errors.New("agentrun: the run names no item, no intent and no project")
	// ErrStageWithoutAnItem is returned for a stage on a run that names no item.
	// A stage is the item's, so one without an item names nothing.
	ErrStageWithoutAnItem = errors.New("agentrun: the run names a stage and no item")
	// ErrOutcomeEmpty is returned for a run with no outcome.
	ErrOutcomeEmpty = errors.New("agentrun: the outcome is empty")
	// ErrUnitsNegative is returned for a negative count of units. A provider
	// returns what it counted, and taking units back is not a run.
	ErrUnitsNegative = errors.New("agentrun: a count of units is negative")
	// ErrUnitsAtEmpty is returned for a run naming no time the units were
	// returned at. It is the time the provider returned them, which is the
	// caller's to supply: the time the record was written is a different fact,
	// and the sum a spend ceiling compares is over this one.
	ErrUnitsAtEmpty = errors.New("agentrun: the time the provider returned the units is empty")
	// ErrCurrencyEmpty is returned for a converted amount with no currency. The
	// amount is in the currency the owner's rates are authored in, and one
	// without a currency bounds nothing.
	ErrCurrencyEmpty = errors.New("agentrun: the converted amount has no currency")
	// ErrAmountNotTheSum is returned for a converted amount that is not what the
	// units the caller supplied come to at the rates it supplied — a run priced
	// on a kind that has no rate included. The amount is computed here from the
	// two fields the record stores, so an amount the caller worked out
	// differently is a disagreement about what the run cost and not a value to
	// store.
	ErrAmountNotTheSum = errors.New("agentrun: the converted amount is not the units at the rates")
	// ErrPeriodUnknown is returned by [SpendByCredentialIn] for a period whose
	// length is not above zero or whose unit is outside [PeriodUnits].
	ErrPeriodUnknown = errors.New("agentrun: a period is a length above zero in one of the units")
	// ErrStartDateUnknown is returned by [SpendByCredentialIn] for a start date
	// that is not a date in [DateLayout] or that names no time zone.
	ErrStartDateUnknown = errors.New("agentrun: a start date is a date and the zone it was authored in")
	// ErrCurrenciesDiffer is returned by [SpendByCredentialIn] where the priced
	// runs of one period are in two currencies. One credential is one account at
	// one provider and one invoice, and what the ceiling compares is a sum in
	// one currency, so there is no total to report.
	ErrCurrenciesDiffer = errors.New("agentrun: the runs of the period are in two currencies")
	// ErrNotFound is returned where no run record has the id.
	ErrNotFound = errors.New("agentrun: no run record has that id")
)

// Writer is the one writer of agent run records. The component that performed
// the run holds it; every component that runs an agent writes through this
// rather than into the table, so the record's rules are implemented once.
type Writer struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// NewWriter returns the writer over pool, fencing every write with token.
func NewWriter(pool *pgxpool.Pool, token lease.Token) *Writer {
	return &Writer{pool: pool, token: token}
}

// New is what the component that performed a run knows about it. It is a struct
// and not twenty arguments because most of them are strings and several are
// ids: a caller that swapped two would compile.
type New struct {
	Role                string
	RolePromptVersionID string
	SkillVersionIDs     []string
	ModelVersion        string
	Effort              string

	CredentialName     string
	ProcessingLocation string
	LenderKey          string
	AccountKind        AccountKind

	ItemID          string
	Stage           string
	IntentID        string
	ProjectID       string
	InputManifestID string

	// UnitsByKind is what the provider returned per kind and UnitsAt the time it
	// returned them, which the caller supplies: it is not the time this record
	// is written.
	UnitsByKind map[string]int64
	UnitsAt     string
	Sources     []string
	RatesByKind map[string]float64
	// ConvertedAmount is what the caller worked the amount out to be. The
	// amount stored is computed here from UnitsByKind and RatesByKind, and one
	// supplied that is not that sum is refused rather than stored: a run a kind
	// of which has no rate carries no amount at all, and the ceiling fails
	// closed on it rather than summing a number that is not there.
	ConvertedAmount float64
	Currency        string

	StartedAt  string
	FinishedAt string
	Outcome    string
}

// Record writes the run record, once. There is no update method: what ran and
// what it ran on are on the record rather than resolved through the fleet entry
// or the People declaration, because the owner may change both without changing
// what any past record says — so a rate corrected later does not reprice what
// this already wrote.
func (w *Writer) Record(ctx context.Context, actor record.Actor, n New) (Run, error) {
	if err := actor.Validate(); err != nil {
		return Run{}, err
	}
	if n.Role == "" {
		return Run{}, ErrRoleEmpty
	}
	if n.ModelVersion == "" {
		return Run{}, ErrModelVersionEmpty
	}
	if n.CredentialName == "" {
		return Run{}, ErrCredentialNameEmpty
	}
	if n.ProcessingLocation == "" {
		return Run{}, ErrProcessingLocationEmpty
	}
	if n.LenderKey == "" {
		return Run{}, ErrLenderKeyEmpty
	}
	if !slices.Contains(AccountKinds, n.AccountKind) {
		return Run{}, fmt.Errorf("%w: %q", ErrAccountKindUnknown, n.AccountKind)
	}
	if n.Stage != "" && n.ItemID == "" {
		return Run{}, ErrStageWithoutAnItem
	}
	if n.ItemID == "" && n.IntentID == "" && n.ProjectID == "" {
		return Run{}, ErrServedNothing
	}
	if n.Outcome == "" {
		return Run{}, ErrOutcomeEmpty
	}
	if n.UnitsAt == "" {
		return Run{}, ErrUnitsAtEmpty
	}
	for kind, units := range n.UnitsByKind {
		if units < 0 {
			return Run{}, fmt.Errorf("%w: %s returned %d", ErrUnitsNegative, kind, units)
		}
	}
	amount, unpriced, err := amountOf(n)
	if err != nil {
		return Run{}, err
	}

	r := Run{
		ID:                  record.NewID(IDPrefix),
		Actor:               actor,
		At:                  record.Now(),
		Role:                n.Role,
		RolePromptVersionID: n.RolePromptVersionID,
		SkillVersionIDs:     n.SkillVersionIDs,
		ModelVersion:        n.ModelVersion,
		Effort:              n.Effort,
		CredentialName:      n.CredentialName,
		ProcessingLocation:  n.ProcessingLocation,
		LenderKey:           n.LenderKey,
		AccountKind:         n.AccountKind,
		ItemID:              n.ItemID,
		Stage:               n.Stage,
		IntentID:            n.IntentID,
		ProjectID:           n.ProjectID,
		InputManifestID:     n.InputManifestID,
		UnitsByKind:         n.UnitsByKind,
		UnitsAt:             n.UnitsAt,
		Sources:             n.Sources,
		RatesByKind:         n.RatesByKind,
		ConvertedAmount:     amount,
		// A run on a credential naming no currency is stored unpriced even
		// where every kind it returned has a rate, the store holding an
		// amount only beside its currency. Nothing fails closed on that: a
		// ceiling is authored in a currency, so no ceiling stands on such a
		// credential and no sum is asked of its runs.
		Priced:     len(unpriced) == 0 && n.Currency != "",
		Currency:   n.Currency,
		StartedAt:  n.StartedAt,
		FinishedAt: n.FinishedAt,
		Outcome:    n.Outcome,
	}
	if !r.Priced {
		// The currency stands exactly where the amount does: a currency beside
		// an absent amount names the currency of nothing, and the store refuses
		// the pair.
		r.ConvertedAmount, r.Currency = 0, ""
	}
	if r.StartedAt == "" {
		r.StartedAt = r.At
	}
	if r.FinishedAt == "" {
		r.FinishedAt = r.At
	}

	stored, err := marshalUnits(r.UnitsByKind)
	if err != nil {
		return Run{}, err
	}
	rates, err := marshalRates(r.RatesByKind)
	if err != nil {
		return Run{}, err
	}
	var converted any
	if r.Priced {
		converted = r.ConvertedAmount
	}

	tx, err := w.pool.Begin(ctx)
	if err != nil {
		return Run{}, fmt.Errorf("agentrun: beginning the record of %s: %w", r.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, w.token); err != nil {
		return Run{}, err
	}

	_, err = tx.Exec(ctx, `insert into `+Table+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at,
		role, role_prompt_version_id, skill_version_ids, model_version, effort,
		credential_name, processing_location, lender_key, account_kind,
		item_id, stage, intent_id, project_id, input_manifest_id,
		units_by_kind, units_at, sources, rates_by_kind, converted_amount, currency,
		started_at, finished_at, outcome)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19,
		$20, $21, $22, $23, $24, $25, $26, $27, $28, $29)`,
		r.ID, FormatVersion, string(r.Actor.Kind), r.Actor.Key, string(r.Actor.Basis), r.At,
		r.Role, r.RolePromptVersionID, joinLines(r.SkillVersionIDs), r.ModelVersion, r.Effort,
		r.CredentialName, r.ProcessingLocation, r.LenderKey, string(r.AccountKind),
		r.ItemID, r.Stage, r.IntentID, r.ProjectID, r.InputManifestID,
		stored, r.UnitsAt, joinLines(r.Sources), rates, converted, r.Currency,
		r.StartedAt, r.FinishedAt, r.Outcome,
	)
	if err != nil {
		return Run{}, fmt.Errorf("agentrun: recording %s: %w", r.ID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Run{}, fmt.Errorf("agentrun: committing %s: %w", r.ID, err)
	}
	return r, nil
}

// amountOf is the converted amount the record stores and the kinds that have no
// rate. The amount is computed from the units and the rates the record itself
// stores rather than taken from the caller, so what the record says a run cost
// is the record's own two fields multiplied out; an amount the caller supplied
// that is not that sum is refused, and so is one supplied for a run a kind of
// which has no rate — that run's amount is absent, which is what a credential
// under a spend ceiling fails closed on.
func amountOf(n New) (float64, []string, error) {
	computed, unpriced := convert(n.UnitsByKind, n.RatesByKind)
	if len(unpriced) > 0 && n.ConvertedAmount != 0 {
		return 0, nil, fmt.Errorf("%w: %v, and no rate prices %s",
			ErrAmountNotTheSum, n.ConvertedAmount, strings.Join(unpriced, ", "))
	}
	if n.ConvertedAmount != 0 && n.Currency == "" {
		return 0, nil, ErrCurrencyEmpty
	}
	if len(unpriced) == 0 && differs(n.ConvertedAmount, computed) {
		return 0, nil, fmt.Errorf("%w: the caller gave %v and the units at the rates come to %v",
			ErrAmountNotTheSum, n.ConvertedAmount, computed)
	}
	return computed, unpriced, nil
}

// differs is how the caller's amount is compared against the computed one. Both
// are sums of float64 products over the same kinds, and a sum in another order
// lands within an ulp or two of this one, so the comparison is to a tolerance
// that scales with the amount rather than to the bit.
func differs(supplied, computed float64) bool {
	scale := math.Max(math.Abs(computed), 1)
	return math.Abs(supplied-computed) > 1e-9*scale
}

// unmarshalUnits and unmarshalRates read the two JSON columns back. They are
// here beside the writes that produce them, so the two spellings of one
// encoding sit together.
func unmarshalUnits(stored string) (map[string]int64, error) {
	units := map[string]int64{}
	if stored == "" {
		return units, nil
	}
	if err := json.Unmarshal([]byte(stored), &units); err != nil {
		return nil, fmt.Errorf("agentrun: decoding the units: %w", err)
	}
	return units, nil
}

func unmarshalRates(stored string) (map[string]float64, error) {
	rates := map[string]float64{}
	if stored == "" {
		return rates, nil
	}
	if err := json.Unmarshal([]byte(stored), &rates); err != nil {
		return nil, fmt.Errorf("agentrun: decoding the rates: %w", err)
	}
	return rates, nil
}
