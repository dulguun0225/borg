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
	// lent it, never a name, and AccountKind is [AccountPerson],
	// [AccountOrganisation], or empty where the declaration says neither.
	CredentialName     string
	ProcessingLocation string
	LenderKey          string
	AccountKind        AccountKind

	// ItemID and Stage are what the run served where it served an item; IntentID
	// where it served an intent. InputManifestID is the manifest context
	// assembly wrote before the agent started.
	ItemID          string
	Stage           string
	IntentID        string
	InputManifestID string

	// UnitsByKind is the units the provider returned, per kind it counts apart,
	// and UnitsAt the time it returned them. Sources is what was handed over.
	UnitsByKind map[string]int64
	UnitsAt     string
	Sources     []string
	// RatesByKind is the rate each kind was converted at, the owner's authored
	// price and not a fact read from the provider. ConvertedAmount is the sum
	// over the kinds at those rates, in Currency, and Priced is false where a
	// kind the run returned has no rate — which gives a spend ceiling nothing to
	// sum and is what makes a credential under one fail closed.
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

// AccountKinds is every value the account kind may have besides empty, which is
// a declaration that says neither. The CHECK in [DDL] lists the same two, and
// TestDDLListsEveryAccountKind fails if the lists stop agreeing.
var AccountKinds = []AccountKind{AccountPerson, AccountOrganisation}

// UnpricedKinds is the kinds a run returned units for that its rates do not
// cover. It is empty on a priced run, and it is what the hold a spend ceiling
// writes names beside the model version and the effort.
func (r Run) UnpricedKinds() []string {
	var missing []string
	for kind := range r.UnitsByKind {
		if _, ok := r.RatesByKind[kind]; !ok {
			missing = append(missing, kind)
		}
	}
	sort.Strings(missing)
	return missing
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
