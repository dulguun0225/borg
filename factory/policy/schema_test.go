package policy_test

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the key from the name, so a
// change to either is caught here rather than by two processes that stopped
// serialising against each other.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	sum := sha256.Sum256([]byte("borg/factory/policy"))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if got := policy.AdvisoryLockKey(); got != want {
		t.Errorf("AdvisoryLockKey = %d, want %d", got, want)
	}
	if policy.AdvisoryLockKey() < 0 {
		t.Error("the key is negative, and the top bit is meant to be cleared")
	}
}

// TestTheSequenceCannotFork: supersedes is what makes the sequence readable
// without a column that orders it, and two versions naming one predecessor would
// make it branch. The store refuses that, and the lock is what means a second
// writer waits rather than meeting the refusal.
func TestTheSequenceCannotFork(t *testing.T) {
	ctx, in := newFactory(t)

	inForce, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}

	// A second version naming the same predecessor is refused by the store.
	_, err = in.pool.Exec(ctx, `insert into `+policy.Table+`
		(id, actor_kind, actor_name, at, action, parameter, subject_kind, subject_id, qualifier, safeguard_id, supersedes)
		values ($1, 'human', 'owner', $2, 'created', '', 'factory_settings', 'fs_x', '', '', $3)`,
		record.NewID(policy.IDPrefix), record.Now(), inForce.Supersedes)
	if err == nil {
		t.Error("the store accepted two versions naming one predecessor, and the sequence would fork")
	}

	// And a second version naming none is refused for the same reason: a sequence
	// has one beginning.
	_, err = in.pool.Exec(ctx, `insert into `+policy.Table+`
		(id, actor_kind, actor_name, at, action, parameter, subject_kind, subject_id, qualifier, safeguard_id, supersedes)
		values ($1, 'human', 'owner', $2, 'created', '', 'factory_settings', 'fs_y', '', '', '')`,
		record.NewID(policy.IDPrefix), record.Now())
	if err == nil {
		t.Error("the store accepted a second version superseding nothing, and the sequence would have two beginnings")
	}

	// Concurrent authoring serialises on the lock, so both writes land and the
	// chain each of them appended is still a chain.
	const writers = 4
	done := make(chan error, writers)
	for i := range writers {
		go func(n int) {
			_, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, float64(n+1))
			done <- err
		}(i)
	}
	for range writers {
		if err := <-done; err != nil {
			t.Errorf("a concurrent authoring failed rather than waiting: %v", err)
		}
	}

	var rows, roots, distinct int
	if err := in.pool.QueryRow(ctx, `select count(*), count(*) filter (where supersedes = ''),
		count(distinct supersedes) from `+policy.Table).Scan(&rows, &roots, &distinct); err != nil {
		t.Fatalf("counting the versions: %v", err)
	}
	if roots != 1 {
		t.Errorf("%d versions supersede nothing, want the one beginning", roots)
	}
	if distinct != rows {
		t.Errorf("%d versions name %d distinct predecessors, and a chain names one each", rows, distinct)
	}
}
