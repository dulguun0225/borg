package policy_test

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
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

// TestConcurrentAuthoringKeepsOneChain: what the lock serialises is reading the
// version in force and appending the one that carries its state forward. Two
// owners authoring at once wait for each other rather than each carrying
// forward a state the other was about to change, and the log they append to is
// still one chain.
func TestConcurrentAuthoringKeepsOneChain(t *testing.T) {
	ctx, in := newFactory(t)

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

	if err := decisionlog.NewReader(in.pool, in.token).Verify(ctx, ownerReading); err != nil {
		t.Errorf("the chain broke under concurrent authoring: %v", err)
	}
	newest := newestVersion(t, ctx, in)
	held := 0
	for _, value := range newest.Authored {
		if value.Scope.ID == in.service.ID {
			held++
		}
	}
	if held != 1 {
		t.Errorf("the newest version names %d window limits on one service, want the one in force", held)
	}
}
