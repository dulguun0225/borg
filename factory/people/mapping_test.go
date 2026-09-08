// mapping_test.go is the key-to-name mapping: the round trip, the deletion an
// erasure makes and a legal hold refuses, the erasure-list row that lands
// before it, and the replay after a restore. It shares db_test.go's newTable
// fixture and the owner it writes as.
package people_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/people"
)

// appending stands in for the erasure-list appender the composition supplies,
// which reaches the report store, the list's one writer. It writes the row of
// kind mapping this package may not name, keyed by the key so the same
// deletion made again appends nothing; list is the file on the host.
func appending(list string) func(context.Context, string) error {
	return func(_ context.Context, key string) error {
		return erasurelist.Append(list, key, erasurelist.KindMapping, key)
	}
}

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

	list := filepath.Join(t.TempDir(), "erasure-list")
	held := func(context.Context) (bool, error) { return true, nil }
	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", held,
		appending(list)); !errors.Is(err, people.ErrLegalHoldReaches) {
		t.Errorf("DeleteMapping under a hold = %v, want ErrLegalHoldReaches", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); err != nil {
		t.Errorf("NameOf after a refused deletion: %v, want the mapping still standing", err)
	}
	rows, err := erasurelist.ReadKind(list, erasurelist.KindMapping)
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("a refused deletion appended %d erasure-list rows, want none", len(rows))
	}

	clear := func(context.Context) (bool, error) { return false, nil }
	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", clear, appending(list)); err != nil {
		t.Fatalf("DeleteMapping with no hold: %v", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); !errors.Is(err, people.ErrMappingNotFound) {
		t.Errorf("NameOf after deletion = %v, want ErrMappingNotFound", err)
	}
	rows, err = erasurelist.ReadKind(list, erasurelist.KindMapping)
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "hk_alice" {
		t.Errorf("the deletion left the erasure list at %+v, want the one row naming the key", rows)
	}

	// The same deletion made again appends nothing, the row being keyed.
	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", clear, appending(list)); err != nil {
		t.Fatalf("DeleteMapping a second time: %v", err)
	}
	rows, err = erasurelist.ReadKind(list, erasurelist.KindMapping)
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("the deletion made again left %d rows, want the 1 already there", len(rows))
	}

	if err := people.DeleteMapping(ctx, pool, token, "hk_bob", clear, nil); !errors.Is(err, people.ErrNoErasureList) {
		t.Errorf("DeleteMapping with no appender = %v, want ErrNoErasureList", err)
	}
}

// TestReplayDeletesAgainWhatTheListSaysWasErased: the erasure list is never
// rolled back, so a restore that brought the name back is served through this
// before People serves anything.
func TestReplayDeletesAgainWhatTheListSaysWasErased(t *testing.T) {
	ctx, pool, token, _ := newTable(t)
	if _, err := people.WriteMapping(ctx, pool, token, owner, "hk_alice", "Alice"); err != nil {
		t.Fatalf("WriteMapping: %v", err)
	}
	if _, err := people.WriteMapping(ctx, pool, token, owner, "hk_bob", "Bob"); err != nil {
		t.Fatalf("WriteMapping: %v", err)
	}

	deleted, err := people.Replay(ctx, pool, token, []string{"hk_alice", "hk_nobody"})
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if deleted != 1 {
		t.Errorf("the replay deleted %d mappings, want the 1 a restore brought back", deleted)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); !errors.Is(err, people.ErrMappingNotFound) {
		t.Errorf("NameOf after the replay = %v, want ErrMappingNotFound", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_bob"); err != nil {
		t.Errorf("the replay reached a mapping the list does not name: %v", err)
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

	list := filepath.Join(t.TempDir(), "erasure-list")
	if err := people.DeleteMapping(ctx, pool, token, "hk_alice", nil,
		appending(list)); !errors.Is(err, people.ErrLegalHoldReaches) {
		t.Errorf("DeleteMapping under a hold on the whole install = %v, want ErrLegalHoldReaches", err)
	}
	if _, err := people.NameOf(ctx, pool, "hk_alice"); err != nil {
		t.Errorf("NameOf after a refused deletion: %v, want the mapping still standing", err)
	}
}
