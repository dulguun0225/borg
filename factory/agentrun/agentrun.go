package agentrun

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/dulguun0225/borg/factory/record"
)

// Run is one agent run record as it is stored: what ran, what it ran on, what
// it served, and what it spent. One is written per run of any agent, by the
// component that performed it, and nothing rewrites it.
type Run struct {
	ID    string
	Actor record.Actor
	At    string

	// Role is what the agent was put on — one stage, or one of the roles put on
	// an intent. RolePromptVersionID is the role prompt version in force at the
	// run and SkillVersionIDs the skill versions it matched; both are empty
	// until those records exist.
	Role                string
	RolePromptVersionID string
	SkillVersionIDs     []string
	// ModelVersion is the model the entry named, its version and not its name:
	// the per-author prior is kept per version. Effort is how long the model
	// worked before it answered, and is empty where the provider offers none.
	ModelVersion string
	Effort       string

	// CredentialName is the reference to the provider account, never a
	// credential. ProcessingLocation is the provider and region it resolves to.
	// LenderKey is the per-person key the People declaration maps to whoever
	// lent it, never a name, and AccountKind is [AccountPerson] or
	// [AccountOrganisation]. The last two are read off that declaration at the
	// run and the processing location off the fleet entry, and a run records all
	// three.
	CredentialName     string
	ProcessingLocation string
	LenderKey          string
	AccountKind        AccountKind

	// ItemID and Stage are what the run served where it served an item; IntentID
	// where it served an intent; ProjectID where the role was put on a project
	// and so served neither. InputManifestID is the manifest context assembly
	// wrote before the agent started.
	ItemID          string
	Stage           string
	IntentID        string
	ProjectID       string
	InputManifestID string

	// UnitsByKind is the units the provider returned, per kind it counts apart,
	// and UnitsAt the time it returned them. Sources is what was handed over.
	UnitsByKind map[string]int64
	UnitsAt     string
	Sources     []string
	// RatesByKind is the rate each kind was converted at, the owner's authored
	// price and not a fact read from the provider. ConvertedAmount is the sum
	// over the kinds at those rates, in Currency, computed by [Writer.Record]
	// from the two fields above; Priced is false where a kind the run returned
	// has no rate — which gives a spend ceiling nothing to sum and is what makes
	// a credential under one fail closed.
	RatesByKind     map[string]float64
	ConvertedAmount float64
	Priced          bool
	Currency        string

	StartedAt  string
	FinishedAt string
	// Outcome is what the run came to. The design names no vocabulary for it, so
	// it is the caller's words and the store requires only that there are some.
	Outcome string
}

// AccountKind is whether the account behind the credential is a person's own or
// an organisation's. The factory takes either, a reference resolving the same
// way whichever it is, and the value is read off the People declaration at the
// run.
type AccountKind string

const (
	// AccountPerson is a person's own account.
	AccountPerson AccountKind = "person"
	// AccountOrganisation is an organisation's.
	AccountOrganisation AccountKind = "organisation"
)

// AccountKinds is every value the account kind may have. The account is an
// organisation's or a person's own and the factory takes either, so there are
// two and no third. The CHECK in [DDL] lists the same two, and
// TestDDLListsEveryAccountKind fails if the lists stop agreeing.
var AccountKinds = []AccountKind{AccountPerson, AccountOrganisation}

// UnpricedKinds is the kinds a run returned units for that its rates do not
// cover. It is empty on a priced run, and it is what the hold a spend ceiling
// writes names beside the model version and the effort.
func (r Run) UnpricedKinds() []string {
	_, unpriced := convert(r.UnitsByKind, r.RatesByKind)
	return unpriced
}

// convert is the converted amount a run's units come to at the rates the record
// stores: each kind's units at that kind's rate, summed. The second answer is
// the kinds the rates do not cover, sorted; where it is not empty there is no
// amount, which is the absent converted amount a spend ceiling fails closed on.
//
// The kinds are summed in one order however the map was built, because a sum of
// float64 products depends on the order it is added in and the amount the writer
// computes is compared against the one its caller computed.
//
// It is the same conversion package people performs against the rates on the
// declaration; here it runs against the rates the record itself stores, so a
// rate corrected later does not reprice what the record already wrote.
func convert(units map[string]int64, rates map[string]float64) (float64, []string) {
	kinds := make([]string, 0, len(units))
	for kind := range units {
		kinds = append(kinds, kind)
	}
	sort.Strings(kinds)

	var amount float64
	var unpriced []string
	for _, kind := range kinds {
		rate, priced := rates[kind]
		if !priced {
			unpriced = append(unpriced, kind)
			continue
		}
		amount += float64(units[kind]) * rate
	}
	if len(unpriced) > 0 {
		return 0, unpriced
	}
	return amount, nil
}

// The unit counts and the rates are stored as JSON objects keyed by kind, and
// the sources and skill version ids one per line. An id is [record.NewID]'s
// alphabet, which holds no line ending, so the separator needs no escaping.

func joinLines(values []string) string { return strings.Join(values, "\n") }

func splitLines(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}

func marshalUnits(units map[string]int64) (string, error) {
	if units == nil {
		units = map[string]int64{}
	}
	encoded, err := json.Marshal(units)
	if err != nil {
		return "", fmt.Errorf("agentrun: encoding the units: %w", err)
	}
	return string(encoded), nil
}

func marshalRates(rates map[string]float64) (string, error) {
	if rates == nil {
		rates = map[string]float64{}
	}
	encoded, err := json.Marshal(rates)
	if err != nil {
		return "", fmt.Errorf("agentrun: encoding the rates: %w", err)
	}
	return string(encoded), nil
}
