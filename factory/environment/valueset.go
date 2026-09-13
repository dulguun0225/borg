package environment

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dulguun0225/borg/factory/secretref"
)

// Value is one entry in the non-production value set. An entry names either a
// service interface, an external, or a secret. Secrets carry a name only;
// deploy resolves the reference when it hands the set to a target.
type Value struct {
	Name      string
	Service   string
	Interface string
	External  string
	Address   string
	Literal   bool
	Secret    secretref.Ref
}

// ValueSet is the typed content an owner authored for a candidate. The order
// is stable so the deployer can hand the same names and values to a target.
type ValueSet struct {
	Entries []Value
}

// ParseValueSet parses and validates a value set. JSON object values may be
// strings, or objects naming service/interface, external, address, or secret.
// The line form remains accepted for existing authored versions.
func ParseValueSet(content string) (ValueSet, error) {
	content = strings.TrimSpace(content)
	if content == "" {
		return ValueSet{}, nil
	}
	var object map[string]json.RawMessage
	if json.Unmarshal([]byte(content), &object) == nil {
		return parseObject(object)
	}
	var entries []Value
	for lineNumber, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found || strings.TrimSpace(name) == "" {
			return ValueSet{}, fmt.Errorf("environment: value set line %d is not name=value", lineNumber+1)
		}
		entries = append(entries, valueOf(strings.TrimSpace(name), strings.TrimSpace(value)))
	}
	set := ValueSet{Entries: entries}
	return set, set.Validate()
}

// Validate refuses an entry that names no kind or more than one kind, and
// refuses incomplete service-interface entries.
func (s ValueSet) Validate() error {
	for _, entry := range s.Entries {
		if entry.Name == "" {
			return errors.New("environment: a value-set entry names no value")
		}
		kinds := 0
		if entry.Service != "" || entry.Interface != "" {
			kinds++
			if entry.Service == "" || entry.Interface == "" {
				return fmt.Errorf("environment: value %s names an incomplete service interface", entry.Name)
			}
		}
		if entry.External != "" {
			kinds++
		}
		if !entry.Secret.IsZero() {
			kinds++
		}
		if entry.Literal {
			kinds++
		}
		if kinds != 1 {
			return fmt.Errorf("environment: value %s names %d value kinds", entry.Name, kinds)
		}
	}
	return nil
}

func parseObject(object map[string]json.RawMessage) (ValueSet, error) {
	entries := make([]Value, 0, len(object))
	names := make([]string, 0, len(object))
	for name := range object {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		raw := object[name]
		var value string
		if json.Unmarshal(raw, &value) == nil {
			entries = append(entries, valueOf(name, value))
			continue
		}
		var typed struct {
			Service   string `json:"service"`
			Interface string `json:"interface"`
			External  string `json:"external"`
			Address   string `json:"address"`
			Secret    string `json:"secret"`
		}
		if err := json.Unmarshal(raw, &typed); err != nil {
			return ValueSet{}, fmt.Errorf("environment: value %s is not a string or typed entry: %w", name, err)
		}
		entry := Value{Name: name, Service: typed.Service, Interface: typed.Interface,
			External: typed.External, Address: typed.Address,
			Literal: typed.Address != "" && typed.Service == "" && typed.Interface == "" && typed.External == ""}
		if typed.Secret != "" {
			ref, err := secretref.New(typed.Secret)
			if err != nil {
				return ValueSet{}, fmt.Errorf("environment: value %s secret: %w", name, err)
			}
			entry.Secret = ref
		}
		entries = append(entries, entry)
	}
	set := ValueSet{Entries: entries}
	return set, set.Validate()
}

func valueOf(name, value string) Value {
	if secret, found := strings.CutPrefix(value, "secret:"); found {
		ref, err := secretref.New(secret)
		if err == nil {
			return Value{Name: name, Secret: ref}
		}
	}
	return Value{Name: name, Address: value, Literal: true}
}
