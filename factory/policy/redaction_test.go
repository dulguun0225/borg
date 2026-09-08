package policy_test

import (
	"context"
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/redaction"
)

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

	written, version, err := in.factory.WriteRedaction(ctx, owner, writing, nil)
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

	first, version, err := in.factory.WriteRedaction(ctx, owner, writing, nil)
	if err != nil {
		t.Fatalf("WriteRedaction: %v", err)
	}
	again, second, err := in.factory.WriteRedaction(ctx, owner, writing, nil)
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

// TestARedactionIsRefusedWhileALegalHoldStands: both readings refuse it, and
// a refused erasure writes neither a record nor a version.
func TestARedactionIsRefusedWhileALegalHoldStands(t *testing.T) {
	ctx, in := newFactory(t)
	writing := redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindArtifactVersion, ID: "art_1"},
		Reason: "words quoted into a spec",
		Spans:  []redaction.Span{{Start: 1, End: 2}},
	}

	reaches := func(context.Context) (bool, error) { return true, nil }
	if _, _, err := in.factory.WriteRedaction(ctx, owner, writing, reaches); !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Errorf("a redaction the caller's check refuses = %v, want ErrLegalHoldReaches", err)
	}

	if _, _, err := in.factory.SetLegalHold(ctx, owner,
		legalhold.Subject{Kind: legalhold.SubjectFactory}, "counsel asked"); err != nil {
		t.Fatalf("SetLegalHold: %v", err)
	}
	if _, _, err := in.factory.WriteRedaction(ctx, owner, writing, nil); !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Errorf("a redaction under a hold on the whole install = %v, want ErrLegalHoldReaches", err)
	}

	over, err := redaction.OverKind(ctx, in.pool, redaction.KindArtifactVersion)
	if err != nil {
		t.Fatalf("reading the redactions over an artifact version: %v", err)
	}
	if len(over) != 0 {
		t.Errorf("a refused erasure wrote %d redactions", len(over))
	}
}

// TestARedactionIsAuthoredByAHuman: gate policy's writer refuses a component
// here as it does everywhere else, an erasure being an owner's act.
func TestARedactionIsAuthoredByAHuman(t *testing.T) {
	ctx, in := newFactory(t)
	_, _, err := in.factory.WriteRedaction(ctx, decompositionActor, redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "why", Spans: []redaction.Span{{Start: 0, End: 1}},
	}, nil)
	if !errors.Is(err, policy.ErrNotAnOwner) {
		t.Errorf("a component performing an erasure = %v, want ErrNotAnOwner", err)
	}
}
