package policy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/policy"
)

// TestInstallIsTheRecordsAnOwnerAuthorsOnAndIsIdempotent: the factory-wide
// settings record exists before any project does, and the project and
// production's environment for it — one an owner does not choose — are created
// in the same event. Creating them is an owner write, so the factory has a
// policy version in force with nothing authored.
func TestInstallIsTheRecordsAnOwnerAuthorsOnAndIsIdempotent(t *testing.T) {
	ctx, in := newFactory(t)

	versions, err := in.reader.Versions(ctx, owner)
	if err != nil {
		t.Fatalf("Versions: %v", err)
	}
	if len(versions) == 0 {
		t.Fatal("the install appended no version, and a gate has none to name")
	}
	for _, of := range []struct {
		what  string
		found bool
	}{
		{"the factory-wide settings record", namesCreation(versions, policy.ScopeFactorySettings)},
		{"the project and production's environment", namesCreation(versions, policy.ScopeProject)},
	} {
		if !of.found {
			t.Errorf("no version names the creation of %s", of.what)
		}
	}

	// Running it again writes nothing: the command-line interface calls it at
	// every start, and a version per start would be a sequence that says
	// nothing.
	before := newestVersion(t, ctx, in)
	again, err := in.factory.Install(ctx, owner, "acme", []string{"/srv/targets"}, credential, 8)
	if err != nil {
		t.Fatalf("Install again: %v", err)
	}
	if again.Settings.ID != in.settings.ID || again.Production.ID != in.prod.ID {
		t.Errorf("a second install created new records: %+v", again)
	}
	if again.Version.ID != before.ID {
		t.Errorf("a second install moved the version to %s from %s", again.Version.ID, before.ID)
	}
}

// TestAVersionIsARowOfTheLog: the version is a row of the decision log and no
// table of package policy's, because a decision names it by identifier and a
// version rewritten outside the chain would move the meaning of every decision
// naming it while the log verified clean.
func TestAVersionIsARowOfTheLog(t *testing.T) {
	ctx, in := newFactory(t)

	version, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
	if err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}

	rows, err := decisionlog.NewReader(in.pool, in.token).ByShape(ctx, owner, decisionlog.ShapePolicyVersion)
	if err != nil {
		t.Fatalf("ByShape: %v", err)
	}
	var found bool
	for _, row := range rows {
		if row.ID != version.ID {
			continue
		}
		found = true
		if row.FormatVersion != policy.FormatVersion {
			t.Errorf("the row declares format version %q, want %q", row.FormatVersion, policy.FormatVersion)
		}
	}
	if !found {
		t.Errorf("no policy version row of the log has the id %s the write returned", version.ID)
	}
	if err := decisionlog.NewReader(in.pool, in.token).Verify(ctx, owner); err != nil {
		t.Errorf("the chain does not hold with the versions in it: %v", err)
	}
}

// TestARepeatedStepWritesNothing: the version is keyed on the write, so a step
// taken again appends no version and moves nothing.
func TestARepeatedStepWritesNothing(t *testing.T) {
	ctx, in := newFactory(t)

	first, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
	if err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	again, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
	if err != nil {
		t.Fatalf("AuthorWindowLimit again: %v", err)
	}
	if again.ID != first.ID {
		t.Errorf("the step taken again appended %s over %s", again.ID, first.ID)
	}

	// A different value is a different write and appends its own version, and
	// so is the first value authored again after it.
	moved, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 4)
	if err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	if moved.ID == first.ID {
		t.Error("authoring a second value appended no version")
	}
	back, err := in.factory.AuthorWindowLimit(ctx, owner, in.service.ID, 3)
	if err != nil {
		t.Fatalf("AuthorWindowLimit: %v", err)
	}
	if back.ID == moved.ID || back.ID == first.ID {
		t.Error("authoring the first value again wrote nothing, and the value in force moved back")
	}
}

func namesCreation(versions []policy.Version, kind string) bool {
	for _, v := range versions {
		if v.Action == policy.ActionCreated && v.Scope.Kind == kind {
			return true
		}
	}
	return false
}
