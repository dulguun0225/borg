// credential_test.go is the lent credential: who lent it, whether the account
// is a person's own or an organisation's, the spend ceiling authored on it and
// the period that ceiling is compared over, and the rates an owner authors per
// kind of unit, per model version and effort. It shares db_test.go's newTable
// fixture and the owner it writes as.
package people_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// TestLendingRecordsTheAccountKindAndTwoCredentialsUnderOneKey is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md's
// "whether the account is a person's own or an organisation's", and the scope
// ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md gives the
// declaration: per credential name, so one lender may lend two.
func TestLendingRecordsTheAccountKindAndTwoCredentialsUnderOneKey(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	own, err := w.Lend(ctx, owner, "hk_alice", "model.anthropic", people.AccountPerson)
	if err != nil {
		t.Fatalf("Lend of a personal account: %v", err)
	}
	if own.Key != "hk_alice" || own.Name != "model.anthropic" || own.Kind != people.AccountPerson {
		t.Errorf("Lend = %+v, which does not name what it was lent with", own)
	}
	if !own.Lent() || own.Ceiling.Authored() {
		t.Errorf("a freshly lent credential reads as %+v, want standing and unbounded", own)
	}

	if _, err := w.Lend(ctx, owner, "hk_alice", "model.openrouter", people.AccountOrganisation); err != nil {
		t.Fatalf("Lend of a second credential under one key: %v", err)
	}
	lent, err := people.Credentials(ctx, pool)
	if err != nil {
		t.Fatalf("Credentials: %v", err)
	}
	if len(lent) != 2 {
		t.Fatalf("Credentials = %+v, want the two one key lent", lent)
	}
	organisation, found, err := people.CredentialNamed(ctx, pool, "model.openrouter")
	if err != nil || !found {
		t.Fatalf("CredentialNamed = %v, %v", found, err)
	}
	if organisation.Kind != people.AccountOrganisation {
		t.Errorf("the second credential's account kind = %q, want %q",
			organisation.Kind, people.AccountOrganisation)
	}

	if _, err := w.Lend(ctx, owner, "hk_alice", "model.anthropic", "borrowed"); !errors.Is(err, people.ErrAccountKindUnknown) {
		t.Errorf("Lend with an account kind outside the two = %v, want ErrAccountKindUnknown", err)
	}
	component := record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}
	if _, err := w.Lend(ctx, component, "hk_alice", "model.fake", people.AccountPerson); !errors.Is(err, people.ErrNotAnOwner) {
		t.Errorf("Lend by a component = %v, want ErrNotAnOwner", err)
	}
}

// TestACeilingCarriesItsCurrencyAndPeriod is
// ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md: an
// amount in the currency the rates are authored in, over a period authored as
// a length and a start date, the start date carrying the zone it was authored
// in.
func TestACeilingCarriesItsCurrencyAndPeriod(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	if _, err := w.Lend(ctx, owner, "hk_alice", "model.anthropic", people.AccountPerson); err != nil {
		t.Fatalf("Lend: %v", err)
	}
	ceiling := people.Ceiling{
		Amount: 250, Currency: "EUR", Length: 1, Unit: people.PeriodMonth,
		StartDate: "2026-01-15", StartZone: "UTC",
	}
	if _, err := w.AuthorCeiling(ctx, owner, "model.anthropic", ceiling); err != nil {
		t.Fatalf("AuthorCeiling: %v", err)
	}
	bounded, found, err := people.CredentialNamed(ctx, pool, "model.anthropic")
	if err != nil || !found {
		t.Fatalf("CredentialNamed = %v, %v", found, err)
	}
	if bounded.Ceiling != ceiling {
		t.Errorf("the ceiling read back as %+v, want %+v", bounded.Ceiling, ceiling)
	}

	// A ceiling on a credential nobody lent, and one in a second currency, are
	// both refused: the scope is the credential, and one credential is one
	// invoice in one currency.
	if _, err := w.AuthorCeiling(ctx, owner, "model.nobody", ceiling); !errors.Is(err, people.ErrNoCredential) {
		t.Errorf("AuthorCeiling on a credential nobody lent = %v, want ErrNoCredential", err)
	}
	other := ceiling
	other.Currency = "USD"
	if _, err := w.AuthorCeiling(ctx, owner, "model.anthropic", other); !errors.Is(err, people.ErrCurrencyDiffers) {
		t.Errorf("AuthorCeiling in a second currency = %v, want ErrCurrencyDiffers", err)
	}
	unbounded := ceiling
	unbounded.Amount = 0
	if _, err := w.AuthorCeiling(ctx, owner, "model.anthropic", unbounded); !errors.Is(err, people.ErrCeilingNotPositive) {
		t.Errorf("AuthorCeiling of nothing = %v, want ErrCeilingNotPositive", err)
	}
	noPeriod := ceiling
	noPeriod.Unit = ""
	if _, err := w.AuthorCeiling(ctx, owner, "model.anthropic", noPeriod); !errors.Is(err, people.ErrPeriodUnknown) {
		t.Errorf("AuthorCeiling with no period unit = %v, want ErrPeriodUnknown", err)
	}
}

// TestThePeriodAStartDateAndLengthPutARunIn is what which period a run falls
// in is derived from at the read: one period follows another from the start
// date, and a period ends at the authored zone's midnight.
func TestThePeriodAStartDateAndLengthPutARunIn(t *testing.T) {
	monthly := people.Ceiling{
		Amount: 250, Currency: "EUR", Length: 1, Unit: people.PeriodMonth,
		StartDate: "2026-01-15", StartZone: "UTC",
	}
	for _, at := range []struct {
		when string
		want string
	}{
		{"2026-01-15T00:00:00.000000000Z", "2026-01-15T00:00:00.000000000Z"},
		{"2026-02-14T23:59:59.000000000Z", "2026-01-15T00:00:00.000000000Z"},
		{"2026-02-15T00:00:00.000000000Z", "2026-02-15T00:00:00.000000000Z"},
		{"2026-04-02T10:00:00.000000000Z", "2026-03-15T00:00:00.000000000Z"},
		// Before the start date there is no period, and the first is what the
		// sum is taken over.
		{"2025-12-31T00:00:00.000000000Z", "2026-01-15T00:00:00.000000000Z"},
	} {
		when, err := record.ParseTime(at.when)
		if err != nil {
			t.Fatalf("parsing %s: %v", at.when, err)
		}
		start, err := monthly.PeriodStartAt(when)
		if err != nil {
			t.Fatalf("PeriodStartAt(%s): %v", at.when, err)
		}
		if start != at.want {
			t.Errorf("PeriodStartAt(%s) = %s, want %s", at.when, start, at.want)
		}
	}

	// The zone the start date was authored in is what the period's midnight is
	// read in, so a period anchored in Asia/Ulaanbaatar starts eight hours
	// before the same date's midnight in UTC.
	zoned := people.Ceiling{
		Amount: 100, Currency: "EUR", Length: 7, Unit: people.PeriodDay,
		StartDate: "2026-01-15", StartZone: "Asia/Ulaanbaatar",
	}
	at, err := record.ParseTime("2026-01-16T00:00:00.000000000Z")
	if err != nil {
		t.Fatalf("parsing the time: %v", err)
	}
	start, err := zoned.PeriodStartAt(at)
	if err != nil {
		t.Fatalf("PeriodStartAt in a named zone: %v", err)
	}
	if start != "2026-01-14T16:00:00.000000000Z" {
		t.Errorf("PeriodStartAt in Asia/Ulaanbaatar = %s, want the zone's own midnight", start)
	}

	unknown := zoned
	unknown.StartZone = "Nowhere/Nowhere"
	if _, err := unknown.PeriodStartAt(at); !errors.Is(err, people.ErrStartDateUnknown) {
		t.Errorf("PeriodStartAt in a zone nobody has = %v, want ErrStartDateUnknown", err)
	}
}

// TestARateIsPerKindPerModelVersionAndEffort is the declaration's other
// credential field: a rate an owner may author per kind of unit the provider
// returns, per model version and effort, which is what a run's converted
// amount is summed at and what a credential fails closed without.
func TestARateIsPerKindPerModelVersionAndEffort(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	if _, err := w.Lend(ctx, owner, "hk_alice", "model.anthropic", people.AccountPerson); err != nil {
		t.Fatalf("Lend: %v", err)
	}
	for _, authored := range []struct {
		unit   string
		effort string
		rate   float64
	}{
		{"input", "high", 0.000003},
		{"output", "high", 0.000015},
		{"input", "", 0.000002},
	} {
		if _, err := w.AuthorRate(ctx, owner, "model.anthropic", "EUR",
			authored.unit, "claude-x-1", authored.effort, authored.rate); err != nil {
			t.Fatalf("AuthorRate of %s at effort %q: %v", authored.unit, authored.effort, err)
		}
	}

	rates, err := people.RatesFor(ctx, pool, "model.anthropic")
	if err != nil {
		t.Fatalf("RatesFor: %v", err)
	}
	if len(rates) != 3 {
		t.Fatalf("RatesFor = %+v, want the three authored", rates)
	}
	if rate, found := people.RateFor(rates, "input", "claude-x-1", "high"); !found || rate != 0.000003 {
		t.Errorf("RateFor input at high = %v, %v, want the rate authored at that effort", rate, found)
	}
	if rate, found := people.RateFor(rates, "input", "claude-x-1", ""); !found || rate != 0.000002 {
		t.Errorf("RateFor input at no effort = %v, %v, want the rate authored with none", rate, found)
	}
	if _, found := people.RateFor(rates, "input", "claude-x-2", "high"); found {
		t.Error("RateFor found a rate for a model version nobody priced")
	}

	// The currency stands on the credential once a rate names one, and a
	// second currency on the same credential is refused.
	lent, found, err := people.CredentialNamed(ctx, pool, "model.anthropic")
	if err != nil || !found {
		t.Fatalf("CredentialNamed = %v, %v", found, err)
	}
	if lent.Ceiling.Currency != "EUR" {
		t.Errorf("the credential's currency = %q, want the one the rates were authored in", lent.Ceiling.Currency)
	}
	if _, err := w.AuthorRate(ctx, owner, "model.anthropic", "USD", "input", "claude-x-1", "low", 1); !errors.Is(err, people.ErrCurrencyDiffers) {
		t.Errorf("AuthorRate in a second currency = %v, want ErrCurrencyDiffers", err)
	}
	if _, err := w.AuthorRate(ctx, owner, "model.nobody", "EUR", "input", "claude-x-1", "", 1); !errors.Is(err, people.ErrNoCredential) {
		t.Errorf("AuthorRate on a credential nobody lent = %v, want ErrNoCredential", err)
	}

	// Authoring the same kind, model version and effort again corrects the
	// rate rather than adding a second row.
	if _, err := w.AuthorRate(ctx, owner, "model.anthropic", "EUR", "input", "claude-x-1", "high", 0.000004); err != nil {
		t.Fatalf("AuthorRate correcting one: %v", err)
	}
	corrected, err := people.RatesFor(ctx, pool, "model.anthropic")
	if err != nil {
		t.Fatalf("RatesFor: %v", err)
	}
	if len(corrected) != 3 {
		t.Errorf("RatesFor after a correction = %+v, want the same three rows", corrected)
	}
	if rate, _ := people.RateFor(corrected, "input", "claude-x-1", "high"); rate != 0.000004 {
		t.Errorf("the corrected rate = %v, want the value authored second", rate)
	}
}

// TestConvertIsAbsentWhereAKindHasNoRate is what makes a credential under a
// ceiling fail closed: a run whose kind has no rate for that model version and
// effort has no converted amount, and the kinds that want a rate are what the
// hold names.
func TestConvertIsAbsentWhereAKindHasNoRate(t *testing.T) {
	rates := []people.Rate{
		{CredentialName: "model.anthropic", Unit: "input", ModelVersion: "claude-x-1", Effort: "high", Rate: 2},
		{CredentialName: "model.anthropic", Unit: "output", ModelVersion: "claude-x-1", Effort: "high", Rate: 10},
	}
	amount, unpriced := people.Convert(rates, "claude-x-1", "high", map[string]int64{"input": 100, "output": 10})
	if len(unpriced) != 0 || amount != 300 {
		t.Errorf("Convert = %v, %v, want 300 and nothing unpriced", amount, unpriced)
	}

	amount, unpriced = people.Convert(rates, "claude-x-1", "high",
		map[string]int64{"input": 100, "cached input": 40})
	if amount != 0 || len(unpriced) != 1 || unpriced[0] != "cached input" {
		t.Errorf("Convert over an unpriced kind = %v, %v, want no amount and the kind named", amount, unpriced)
	}
	_, unpriced = people.Convert(rates, "claude-x-1", "low", map[string]int64{"input": 100})
	if len(unpriced) != 1 {
		t.Errorf("Convert at an effort nobody priced = %v, want the kind unpriced", unpriced)
	}
}

// TestALentCredentialAppendsAPolicyVersionBeforeTheTable is the order every
// owner write to the declaration takes, for the three writes this file adds:
// the version first, the declaration second.
func TestALentCredentialAppendsAPolicyVersionBeforeTheTable(t *testing.T) {
	ctx, pool, token, w := newTable(t)
	reader := policy.NewReader(pool, token, score.Version{})

	if _, err := w.Declare(ctx, owner, "hk_alice", people.OfDuty(4)); err != nil {
		t.Fatalf("Declare: %v", err)
	}
	if _, err := w.Lend(ctx, owner, "hk_alice", "model.anthropic", people.AccountPerson); err != nil {
		t.Fatalf("Lend: %v", err)
	}
	if _, err := w.AuthorRate(ctx, owner, "model.anthropic", "EUR", "input", "claude-x-1", "high", 3); err != nil {
		t.Fatalf("AuthorRate: %v", err)
	}
	if _, err := w.AuthorCeiling(ctx, owner, "model.anthropic", people.Ceiling{
		Amount: 250, Currency: "EUR", Length: 1, Unit: people.PeriodMonth,
		StartDate: "2026-01-15", StartZone: "UTC",
	}); err != nil {
		t.Fatalf("AuthorCeiling: %v", err)
	}

	newest, err := reader.Newest(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	if newest.Caller != policy.CallerPeople {
		t.Errorf("the version's caller = %q, want %q", newest.Caller, policy.CallerPeople)
	}
	var lent *policy.PersonDeclaration
	for n, p := range newest.Declaration.People {
		if p.CredentialName == "model.anthropic" {
			lent = &newest.Declaration.People[n]
		}
	}
	if lent == nil {
		t.Fatalf("the newest version's declaration = %+v, want the lent credential on it", newest.Declaration.People)
	}
	if lent.Key != "hk_alice" || lent.SpendCeiling != 250 || len(lent.Rates) != 1 {
		t.Errorf("the version names %+v, want the lender, the ceiling and the rate authored on it", *lent)
	}

	// Taking the credential back keeps the row and leaves it off the newest
	// version's declaration.
	taken, err := w.TakeBack(ctx, owner, "model.anthropic")
	if err != nil {
		t.Fatalf("TakeBack: %v", err)
	}
	if taken.Lent() || taken.WithdrawnAt == "" {
		t.Errorf("TakeBack left %+v, want the row kept and marked", taken)
	}
	newest, err = reader.Newest(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Newest after TakeBack: %v", err)
	}
	for _, p := range newest.Declaration.People {
		if p.CredentialName == "model.anthropic" {
			t.Errorf("the version after TakeBack still names %+v", p)
		}
	}
	if _, err := w.TakeBack(ctx, owner, "model.anthropic"); err != nil {
		t.Errorf("TakeBack of a credential already taken back = %v, want it to stay taken back", err)
	}
}

// TestDDLListsEveryAccountKind keeps the CHECK constraint and
// people.AccountKinds from disagreeing: every kind lends cleanly through the
// writer, and one outside the two is refused around it.
func TestDDLListsEveryAccountKind(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	for _, kind := range people.AccountKinds {
		if _, err := w.Lend(ctx, owner, "hk_alice", "model."+string(kind), kind); err != nil {
			t.Errorf("Lend with account kind %q, one of people.AccountKinds, was refused: %v", kind, err)
		}
	}
	if _, err := pool.Exec(ctx, `insert into `+people.CredentialTable+`
		(id, format_version, actor_kind, actor_key, actor_key_basis, at, person_key, credential_name,
		 account_kind, currency, ceiling_amount, period_length, period_unit, period_start_date,
		 period_start_zone, withdrawn_at)
		values ($1, $2, 'human', 'person:owner', 'claimed', $3, 'hk_alice', 'model.borrowed',
		 'borrowed', '', null, 0, '', '', '', '')`,
		record.NewID(people.CredentialIDPrefix), people.CredentialFormatVersion, record.Now()); err == nil {
		t.Error("the store accepted an account kind outside people.AccountKinds")
	}
}

// TestDDLListsEveryPeriodUnit is the same for people.PeriodUnits: every unit
// authors cleanly through the writer, and one outside the two is refused
// around it.
func TestDDLListsEveryPeriodUnit(t *testing.T) {
	ctx, pool, _, w := newTable(t)

	if _, err := w.Lend(ctx, owner, "hk_alice", "model.anthropic", people.AccountPerson); err != nil {
		t.Fatalf("Lend: %v", err)
	}
	for _, unit := range people.PeriodUnits {
		if _, err := w.AuthorCeiling(ctx, owner, "model.anthropic", people.Ceiling{
			Amount: 250, Currency: "EUR", Length: 1, Unit: unit,
			StartDate: "2026-01-15", StartZone: "UTC",
		}); err != nil {
			t.Errorf("AuthorCeiling with period unit %q, one of people.PeriodUnits, was refused: %v", unit, err)
		}
	}
	if _, err := pool.Exec(ctx, `update `+people.CredentialTable+`
		set period_unit = 'fortnight' where credential_name = 'model.anthropic'`); err == nil {
		t.Error("the store accepted a period unit outside people.PeriodUnits")
	}
}
