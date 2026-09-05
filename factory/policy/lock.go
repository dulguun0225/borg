package policy

import (
	"crypto/sha256"
	"encoding/binary"
)

// lockName is what [AdvisoryLockKey] hashes. It names this package so that no
// other part of the factory derives the same key from a name of its own — the
// arrangement score's one lock and release's per-service ones both use.
const lockName = "borg/factory/policy"

// AdvisoryLockKey is the PostgreSQL advisory lock every write in this package
// takes for the whole of its transaction: the first eight bytes of SHA-256 of
// [lockName], big-endian, with the top bit cleared so the value is positive.
// TestAdvisoryLockKeyIsDerivedFromTheName recomputes it.
//
// One key and not one per anything. What it serialises is reading the version in
// force and appending the one that carries its state forward: two owner writes
// doing that at once would each carry forward the state the other was about to
// change. It is not the log's own lock — the log takes that for itself inside
// the same transaction — and the order between the two is always this one first.
func AdvisoryLockKey() int64 {
	sum := sha256.Sum256([]byte(lockName))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}
