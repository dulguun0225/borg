package agentrun

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

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
	// [AccountKinds] and not empty.
	ErrAccountKindUnknown = errors.New("agentrun: the account kind is unknown")
	// ErrServedNothing is returned for a run naming neither an item nor an
	// intent. What a run served is one of the five the design names and never
	// none; doc.go says which two of the five have a record to name.
	ErrServedNothing = errors.New("agentrun: the run names neither an item nor an intent")
	// ErrStageWithoutAnItem is returned for a stage on a run that names no item.
	// A stage is the item's, so one without an item names nothing.
	ErrStageWithoutAnItem = errors.New("agentrun: the run names a stage and no item")
	// ErrOutcomeEmpty is returned for a run with no outcome.
	ErrOutcomeEmpty = errors.New("agentrun: the outcome is empty")
	// ErrUnitsNegative is returned for a negative count of units. A provider
	// returns what it counted, and taking units back is not a run.
	ErrUnitsNegative = errors.New("agentrun: a count of units is negative")
	// ErrCurrencyEmpty is returned for a converted amount with no currency. The
	// amount is in the currency the owner's rates are authored in, and one
	// without a currency bounds nothing.
	ErrCurrencyEmpty = errors.New("agentrun: the converted amount has no currency")
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
	InputManifestID string

	UnitsByKind map[string]int64
	UnitsAt     string
	Sources     []string
	RatesByKind map[string]float64
	// ConvertedAmount is the sum over the kinds at the rates, and Priced says
	// whether there is one: a run a kind of which has no rate carries none, and
	// the ceiling fails closed on it rather than summing a number that is not
	// there.
	ConvertedAmount float64
	Priced          bool
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
	if n.AccountKind != "" && !slices.Contains(AccountKinds, n.AccountKind) {
		return Run{}, fmt.Errorf("%w: %q", ErrAccountKindUnknown, n.AccountKind)
	}
	if n.Stage != "" && n.ItemID == "" {
		return Run{}, ErrStageWithoutAnItem
	}
	if n.ItemID == "" && n.IntentID == "" {
		return Run{}, ErrServedNothing
	}
	if n.Outcome == "" {
		return Run{}, ErrOutcomeEmpty
	}
	for kind, units := range n.UnitsByKind {
		if units < 0 {
			return Run{}, fmt.Errorf("%w: %s returned %d", ErrUnitsNegative, kind, units)
		}
	}
	if n.Priced && n.Currency == "" {
		return Run{}, ErrCurrencyEmpty
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
		InputManifestID:     n.InputManifestID,
		UnitsByKind:         n.UnitsByKind,
		UnitsAt:             n.UnitsAt,
		Sources:             n.Sources,
		RatesByKind:         n.RatesByKind,
		ConvertedAmount:     n.ConvertedAmount,
		Priced:              n.Priced,
		Currency:            n.Currency,
		StartedAt:           n.StartedAt,
		FinishedAt:          n.FinishedAt,
		Outcome:             n.Outcome,
	}
	if r.UnitsAt == "" {
		r.UnitsAt = r.At
	}
	if r.StartedAt == "" {
		r.StartedAt = r.At
	}
	if r.FinishedAt == "" {
		r.FinishedAt = r.At
	}

	units, err := marshalUnits(r.UnitsByKind)
	if err != nil {
		return Run{}, err
	}
	rates, err := marshalRates(r.RatesByKind)
	if err != nil {
		return Run{}, err
	}
	var amount any
	if r.Priced {
		amount = r.ConvertedAmount
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
		item_id, stage, intent_id, input_manifest_id,
		units_by_kind, units_at, sources, rates_by_kind, converted_amount, currency,
		started_at, finished_at, outcome)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19,
		$20, $21, $22, $23, $24, $25, $26, $27, $28)`,
		r.ID, FormatVersion, string(r.Actor.Kind), r.Actor.Key, string(r.Actor.Basis), r.At,
		r.Role, r.RolePromptVersionID, joinLines(r.SkillVersionIDs), r.ModelVersion, r.Effort,
		r.CredentialName, r.ProcessingLocation, r.LenderKey, string(r.AccountKind),
		r.ItemID, r.Stage, r.IntentID, r.InputManifestID,
		units, r.UnitsAt, joinLines(r.Sources), rates, amount, r.Currency,
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
