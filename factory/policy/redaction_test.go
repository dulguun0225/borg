package policy_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/redaction"
)

// erasureListAppender is a working [redaction.ErasureAppender] over a fresh
// file, the same call reportstore.Store.AppendErasure makes over its own:
// [Factory.WriteRedaction] only hands the function on, so these tests build
// one the plain way to write a redaction that needs one.
func erasureListAppender(t *testing.T) redaction.ErasureAppender {
	t.Helper()
	list := filepath.Join(t.TempDir(), "erasure-list")
	return func(kind, key, removed string) error {
		return erasurelist.Append(list, key, kind, removed)
	}
}

// TestARedactionIsOneWriteByOneWriter: the record and the policy version that
// names it are written together, the way a legal hold's are, so the auditor
// shown an erasure is shown one record and the version that put it there.
func TestARedactionIsOneWriteByOneWriter(t *testing.T) {
	ctx, in := newFactory(t)
	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "a person named in the words a report carried",
		Spans:  []redaction.Span{{Start: 3, End: 11}},
	}

	written, version, err := in.factory.WriteRedaction(ctx, owner, writing, nil, erasureListAppender(t))
	if err != nil {
		t.Fatalf("WriteRedaction: %v", err)
	}
	if version.Action != policy.ActionRedacted || version.RedactionID != written.ID {
		t.Errorf("the redaction's version says %q of redaction %q", version.Action, version.RedactionID)
	}
	if version.Scope.Kind != string(writing.Target.Kind) || version.Scope.ID != writing.Target.ID {
		t.Errorf("the version's scope is %s, want the target %s", version.Scope, writing.Target)
	}
	if written.ErasureKey != redaction.Key(owner, writing) {
		t.Errorf("the record names erasure key %s, the row was appended under %s",
			written.ErasureKey, redaction.Key(owner, writing))
	}

	read, err := in.reader.Version(ctx, ownerReading, version.ID)
	if err != nil {
		t.Fatalf("reading the version back: %v", err)
	}
	if read.RedactionID != written.ID {
		t.Errorf("the version read back names redaction %q, want %q", read.RedactionID, written.ID)
	}
}

// TestTheSameErasurePerformedAgainWritesNothing: the erasure-list row is
// keyed, and so is the record that follows it — the second performance finds
// the first one's record and appends no second version.
func TestTheSameErasurePerformedAgainWritesNothing(t *testing.T) {
	ctx, in := newFactory(t)
	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindReport, ID: "rep_1"},
		Reason: "the words name a person",
		Spans:  []redaction.Span{{Start: 0, End: 4}},
	}

	first, version, err := in.factory.WriteRedaction(ctx, owner, writing, nil, erasureListAppender(t))
	if err != nil {
		t.Fatalf("WriteRedaction: %v", err)
	}
	again, second, err := in.factory.WriteRedaction(ctx, owner, writing, nil, erasureListAppender(t))
	if err != nil {
		t.Fatalf("WriteRedaction a second time: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("the erasure performed again wrote %s beside %s", again.ID, first.ID)
	}
	if second.ID != version.ID {
		t.Errorf("the erasure performed again appended version %s beside %s", second.ID, version.ID)
	}
}

// TestARedactionIsRefusedWhileALegalHoldStands: both readings refuse it, a
// refused erasure writes neither a record nor a redaction, and the refusal
// itself is recorded — [Factory.WriteRedaction] writes it in the same call
// that refuses, so a caller need not remember a second one.
func TestARedactionIsRefusedWhileALegalHoldStands(t *testing.T) {
	ctx, in := newFactory(t)
	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindArtifactVersion, ID: "art_1"},
		Reason: "words quoted into a spec",
		Spans:  []redaction.Span{{Start: 1, End: 2}},
	}

	reaches := func(context.Context) (bool, error) { return true, nil }
	if _, _, err := in.factory.WriteRedaction(ctx, owner, writing, reaches, nil); !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Errorf("a redaction the caller's check refuses = %v, want ErrLegalHoldReaches", err)
	}
	assertTheRefusalIsOnRecord(t, ctx, in, writing)

	if _, _, err := in.factory.SetLegalHold(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectFactory}, "counsel asked"); err != nil {
		t.Fatalf("SetLegalHold: %v", err)
	}
	if _, _, err := in.factory.WriteRedaction(ctx, owner, writing, nil, nil); !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Errorf("a redaction under a hold on the whole install = %v, want ErrLegalHoldReaches", err)
	}
	assertTheRefusalIsOnRecord(t, ctx, in, writing)

	over, err := redaction.OverKind(ctx, in.pool, redaction.KindArtifactVersion)
	if err != nil {
		t.Fatalf("reading the redactions over an artifact version: %v", err)
	}
	if len(over) != 0 {
		t.Errorf("a refused erasure wrote %d redactions", len(over))
	}
}

// assertTheRefusalIsOnRecord is what the newest policy version says right
// after a refused [Factory.WriteRedaction]: the write that performed nothing
// else is the one that names the refusal.
func assertTheRefusalIsOnRecord(t *testing.T, ctx context.Context, in installed, writing redaction.Writing) {
	t.Helper()
	version, err := in.reader.Newest(ctx, ownerReading)
	if err != nil {
		t.Fatalf("reading the newest version: %v", err)
	}
	if version.Action != policy.ActionRedactionRefused || version.Refusal == "" {
		t.Errorf("the newest version is %q with refusal %q, want the refusal on the record",
			version.Action, version.Refusal)
	}
	if version.Scope.Kind != string(writing.Target.Kind) || version.Scope.ID != writing.Target.ID {
		t.Errorf("the refusal's version names %s, want the target %s", version.Scope, writing.Target)
	}
}

// TestARedactionIsAuthoredByAHuman: gate policy's writer refuses a component
// here as it does everywhere else, an erasure being an owner's act.
func TestARedactionIsAuthoredByAHuman(t *testing.T) {
	ctx, in := newFactory(t)
	_, _, err := in.factory.WriteRedaction(ctx, decompositionActor, redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "why", Spans: []redaction.Span{{Start: 0, End: 1}},
	}, nil, nil)
	if !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component performing an erasure = %v, want ErrNotAnOwner", err)
	}
}
