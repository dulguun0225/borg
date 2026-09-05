package policy_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/policy"
)

// TestInstallIsTheTwoRecordsAnOwnerAuthorsOnAndIsIdempotent: the factory-wide
// settings record exists before any project does and production's environment is one an
// owner does not choose, so both are created here — and creating them is an
// authoring write, so the factory has a policy version in force with nothing
// authored.
func TestInstallIsTheTwoRecordsAnOwnerAuthorsOnAndIsIdempotent(t *testing.T) {
	ctx, in := newFactory(t)

	version, err := policy.InForce(ctx, in.pool)
	if err != nil {
		t.Fatalf("InForce: %v", err)
	}
	if version.Action != policy.ActionCreated {
		t.Errorf("the version in force is %q, want a creation", version.Action)
	}
	if version.Parameter != "" {
		t.Errorf("a creation names parameter %q, and it authors none", version.Parameter)
	}

	all, err := policy.All(ctx, in.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("the install left %d versions, want one per record created", len(all))
	}
	if all[0].Supersedes != "" || all[1].Supersedes != all[0].ID {
		t.Errorf("the versions do not chain: %q then %q superseding %q",
			all[0].Supersedes, all[1].ID, all[1].Supersedes)
	}

	// Running it again writes nothing: the crude interface calls it at every
	// start, and a version per start would be a sequence that says nothing.
	again, err := in.factory.Install(ctx, owner, []string{"/srv/targets"}, credential)
	if err != nil {
		t.Fatalf("Install again: %v", err)
	}
	if again.Settings.ID != in.settings.ID || again.Production.ID != in.prod.ID {
		t.Errorf("a second install created new records: %+v", again)
	}
	if again.Version.ID != version.ID {
		t.Errorf("a second install moved the version to %s", again.Version.ID)
	}
}
