package policy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/service"
)

// TestRederiveWritesBackWhatTheNewestVersionNames: the factory's start
// re-derives every authored field from the newest version naming its scope,
// which is what finishes a write a stop interrupted.
func TestRederiveWritesBackWhatTheNewestVersionNames(t *testing.T) {
	ctx, in := newFactory(t)

	if _, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3); err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}

	// A field moved out from under the version is what a stop between the two
	// writes leaves, and the store is put in that state directly here because
	// nothing in the factory can write one without the other.
	if _, err := in.pool.Exec(ctx, `update `+service.Table+` set window_limit = null where id = $1`,
		in.service.ID); err != nil {
		t.Fatalf("clearing the field: %v", err)
	}

	rewritten, err := in.factory.Rederive(ctx, owner)
	if err != nil {
		t.Fatalf("Rederive: %v", err)
	}
	if len(rewritten) != 1 {
		t.Fatalf("the re-derivation rewrote %d fields, want the one that lost its value", len(rewritten))
	}
	if rewritten[0].Value.Parameter != gatepolicy.WindowLimit || rewritten[0].Held.Present {
		t.Errorf("the re-derivation reports %+v", rewritten[0])
	}
	svc, err := service.Get(ctx, in.pool, in.service.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !svc.Parameters.WindowLimit.Present || svc.Parameters.WindowLimit.Number != 3 {
		t.Errorf("the field reads %+v after the re-derivation, want the authored 3", svc.Parameters.WindowLimit)
	}

	// A re-derivation that finds the two already agreeing writes nothing, and
	// it appends no version either way.
	before := newestVersion(t, ctx, in)
	again, err := in.factory.Rederive(ctx, owner)
	if err != nil {
		t.Fatalf("Rederive again: %v", err)
	}
	if len(again) != 0 {
		t.Errorf("a re-derivation over fields that agree rewrote %v", again)
	}
	if after := newestVersion(t, ctx, in); after.ID != before.ID {
		t.Errorf("the re-derivation appended a version: %s over %s", after.ID, before.ID)
	}
}
