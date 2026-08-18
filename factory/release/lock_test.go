package release

import (
	"crypto/sha256"
	"encoding/binary"
	"testing"
)

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes a key from the name
// schema.go says it comes from, so the derivation cannot drift from what the
// comment claims.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	const serviceID = "svc_00112233445566778899aabbccddeeff"
	sum := sha256.Sum256([]byte("borg/factory/release/" + serviceID))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if got := AdvisoryLockKey(serviceID); got != want {
		t.Fatalf("AdvisoryLockKey = %#x, the name hashes to %#x", got, want)
	}
	if AdvisoryLockKey(serviceID) <= 0 {
		t.Fatalf("AdvisoryLockKey = %d, want a positive value", AdvisoryLockKey(serviceID))
	}
	if AdvisoryLockKey("svc_one") == AdvisoryLockKey("svc_two") {
		t.Fatal("two services derive one key, so their mints would serialise against each other")
	}
}
