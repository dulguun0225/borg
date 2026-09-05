package inputmanifest

import (
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/record"
)

// Manifest is one input manifest as it is stored: what context assembly
// handed an agent before one run, named by reference and never by value. One
// is written per dispatch, before the agent starts, and nothing rewrites it.
type Manifest struct {
	ID    string
	Actor record.Actor
	At    string

	// ItemID and Stage are what the dispatch was for where it was for an item;
	// IntentID where it was for an intent — an interview round or a
	// decomposition. One of the two is always set.
	ItemID   string
	Stage    string
	IntentID string

	// Materials is everything named to the agent: the intent, each report,
	// each constraint, each artifact version, the commit and the paths of the
	// repository read, the run outputs, and the reject or rework request the
	// stage re-authors against — one entry per source, by reference.
	Materials []Material
	// ReadAtOnceBound is how much the model reads at once, a field the fleet
	// entry carries; it is nil where the entry that bound is read from is not
	// built yet.
	ReadAtOnceBound *int64
	// SelectionRuleVersion is the version of the selection rule applied, the
	// newest one its gate approved; it is empty where that record does not
	// exist yet.
	SelectionRuleVersion string
	// Excluded is what did not reach the agent and why: the rule excluding it,
	// or a class of material the fleet entry does not name, which context
	// assembly withholds before the rule selects anything.
	Excluded []Exclusion
}

// Material is one source named to the agent by reference: its class, the
// reference itself, and its size, so a bound applied against it can be
// checked against what actually reached the model.
type Material struct {
	Class     string
	Reference string
	Bytes     int64
}

// Exclusion is one source that did not reach the agent: what it was and why
// it was left out — the selection rule, or a class the fleet entry the run
// dispatched on does not name.
type Exclusion struct {
	What   string
	Reason string
}

func marshalMaterials(materials []Material) (string, error) {
	if materials == nil {
		materials = []Material{}
	}
	encoded, err := json.Marshal(materials)
	if err != nil {
		return "", fmt.Errorf("inputmanifest: encoding the materials: %w", err)
	}
	return string(encoded), nil
}

func unmarshalMaterials(stored string) ([]Material, error) {
	var materials []Material
	if stored == "" {
		return materials, nil
	}
	if err := json.Unmarshal([]byte(stored), &materials); err != nil {
		return nil, fmt.Errorf("inputmanifest: decoding the materials: %w", err)
	}
	return materials, nil
}

func marshalExcluded(excluded []Exclusion) (string, error) {
	if excluded == nil {
		excluded = []Exclusion{}
	}
	encoded, err := json.Marshal(excluded)
	if err != nil {
		return "", fmt.Errorf("inputmanifest: encoding what was excluded: %w", err)
	}
	return string(encoded), nil
}

func unmarshalExcluded(stored string) ([]Exclusion, error) {
	var excluded []Exclusion
	if stored == "" {
		return excluded, nil
	}
	if err := json.Unmarshal([]byte(stored), &excluded); err != nil {
		return nil, fmt.Errorf("inputmanifest: decoding what was excluded: %w", err)
	}
	return excluded, nil
}
