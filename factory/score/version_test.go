package score

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"testing"
)

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the key from the name, so a
// name changed under the constant is a failure here rather than two processes
// appending a version each.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	sum := sha256.Sum256([]byte("borg/factory/score"))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if got := AdvisoryLockKey(); got != want {
		t.Errorf("AdvisoryLockKey = %d, want %d", got, want)
	}
	if AdvisoryLockKey() < 0 {
		t.Error("the key is negative, and the top bit is meant to be cleared")
	}
}

// TestTheScopeOfAPolicyVersionReadsAsTheScopeItWasWrittenOn: the confirmation
// [InForceAt] reads is keyed on the scope an owner authored on, which package
// policy writes as an object of three fields. The two spellings are held
// together from the other side by TestThePolicyVersionFieldsTheScoreReads in
// that package; this one holds the composition of the key.
func TestTheScopeOfAPolicyVersionReadsAsTheScopeItWasWrittenOn(t *testing.T) {
	const row = `{"scope":{"kind":"environment","id":"env-1","key":"merge_to_master"},
		"confirms_score_version":"dl-9"}`
	var event policyVersionEvent
	if err := json.Unmarshal([]byte(row), &event); err != nil {
		t.Fatalf("reading a policy version row: %v", err)
	}
	if got, want := event.Scope.String(), "environment:env-1:merge_to_master"; got != want {
		t.Errorf("the scope reads as %q, want %q", got, want)
	}
	if event.ConfirmsScoreVersion != "dl-9" {
		t.Errorf("the confirmed version reads as %q, want dl-9", event.ConfirmsScoreVersion)
	}

	var keyless policyVersionEvent
	if err := json.Unmarshal([]byte(`{"scope":{"kind":"service","id":"svc-1","key":""}}`), &keyless); err != nil {
		t.Fatalf("reading a policy version row with no key: %v", err)
	}
	if got, want := keyless.Scope.String(), "service:svc-1"; got != want {
		t.Errorf("a scope with no key reads as %q, want %q", got, want)
	}
}
