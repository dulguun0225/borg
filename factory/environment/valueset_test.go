package environment_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/environment"
)

func TestParseValueSetValidatesTypedEntries(t *testing.T) {
	set, err := environment.ParseValueSet(`{"api":{"service":"billing","interface":"public","address":"https://billing"},"token":{"secret":"billing.token"},"search":{"external":"search"}}`)
	if err != nil {
		t.Fatalf("ParseValueSet: %v", err)
	}
	if len(set.Entries) != 3 || set.Entries[0].Name != "api" || set.Entries[0].Service != "billing" || set.Entries[0].Interface != "public" {
		t.Fatalf("ValueSet = %+v, want typed entries in stable order", set)
	}
	if set.Entries[1].External != "search" || set.Entries[2].Secret.Name() != "billing.token" {
		t.Fatalf("ValueSet = %+v, want secret and external entries", set)
	}
}

func TestParseValueSetRejectsAnIncompleteServiceInterface(t *testing.T) {
	if _, err := environment.ParseValueSet(`{"api":{"service":"billing"}}`); err == nil {
		t.Fatal("ParseValueSet accepted a service without an interface")
	}
}
