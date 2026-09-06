// mapping_test.go is the key-to-name mapping: the round trip, and the
// deletion an erasure makes and a legal hold refuses. It shares db_test.go's
// newTable fixture and the owner it writes as.
package people_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/people"
)

// TestWriteMappingRoundTripsAndResolvesAName is the one place a key maps to
// a name, kept outside the chain.
func TestWriteMappingRoundTripsAndResolvesAName(t *testing.T) {
	ctx, pool, token, _ := newTable(t)

	if _, err := people.WriteMapping(ctx, pool, token, owner, "hk_alice", "Alice"); err != nil {
		t.Fatalf("WriteMapping: %v", err)
	}
	name, err := people.NameOf(ctx, pool, "hk_alice")
	if err != nil {
		t.Fatalf("NameOf: %v", err)
	}
	if name != "Alice" {
		t.Errorf("NameOf = %q, want Alice", name)
	}

	// Writing it again for the same key updates the one row.
	if _, err := people.WriteMapping(ctx, pool, token, owner, "hk_alice", "Alice Smith"); err != nil {
		t.Fatalf("WriteMapping again: %v", err)
	}
	name, err = people.NameOf(ctx, pool, "hk_alice")
	if err != nil {
		t.Fatalf("NameOf: %v", err)
	}
	if name != "Alice Smith" {
		t.Errorf("NameOf after a second write = %q, want Alice Smith", name)
	}
}

// TestDeleteMappingIsRefusedUnderALegalHoldAndDeletesOtherwise is the legal
// hold's own refusal: DeleteMapping calls the caller's check first, and
// refuses the deletion with ErrLegalHoldReaches where it reports a hold
// standing, leaving the mapping and every record the key is written on
// untouched.
func TestDeleteMappingIsRefusedUnderALegalHoldAndDeletesOtherwise(t *testing.T) {
	ctx, pool, token, _ := newTable(t)
	if _, err := people.WriteMapping(ctx, pool, token, owner, "hk_alice", "Alice"); err != nil {
		t.Fatalf("WriteMapping: %v", err)
	}

	held := func(context.Context) (bool, error) { return true, nil }
	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", held); !errors.Is(err, people.ErrLegalHoldReaches) {
		t.Errorf("DeleteMapping under a hold = %v, want ErrLegalHoldReaches", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); err != nil {
		t.Errorf("NameOf after a refused deletion: %v, want the mapping still standing", err)
	}

	clear := func(context.Context) (bool, error) { return false, nil }
	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", clear); err != nil {
		t.Fatalf("DeleteMapping with no hold: %v", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); !errors.Is(err, people.ErrMappingNotFound) {
		t.Errorf("NameOf after deletion = %v, want ErrMappingNotFound", err)
	}
}

// TestDeleteMappingIsRefusedUnderAHoldOnTheWholeInstall is the half of that
// refusal this package decides itself: a legal hold's subject is a service, a
// project or the whole install and never a person, and a hold on the whole
// install reaches every mapping there is, so the deletion is refused with no
// caller's check at all.
func TestDeleteMappingIsRefusedUnderAHoldOnTheWholeInstall(t *testing.T) {
	ctx, pool, token, _ := newTable(t)
	if _, err := people.WriteMapping(ctx, pool, token, owner, "hk_alice", "Alice"); err != nil {
		t.Fatalf("WriteMapping: %v", err)
	}
	if _, err := legalhold.NewWriter(pool, token).Insert(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectFactory}, "a regulator asked for everything"); err != nil {
		t.Fatalf("holding the whole install: %v", err)
	}

	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", nil); !errors.Is(err, people.ErrLegalHoldReaches) {
		t.Errorf("DeleteMapping under a hold on the whole install = %v, want ErrLegalHoldReaches", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); err != nil {
		t.Errorf("NameOf after a refused deletion: %v, want the mapping still standing", err)
	}
}
