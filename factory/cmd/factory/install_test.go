// What a composition that is not run's does with the install's records, and who
// a version authored at a gate row names as its author.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/project"
)

// TestOnlyRunInstallsAndEveryOtherCompositionReadsTheProject: run creates the
// project and production's environment for it in the same event where they do
// not exist; every other subcommand reads the project it names and refuses where
// it does not exist, so none of them leaves a second project behind under a name
// run never used.
func TestOnlyRunInstallsAndEveryOtherCompositionReadsTheProject(t *testing.T) {
	ctx, d, _ := newPath(t, "")

	reading := d
	reading.install = false
	p, err := compose(ctx, reading)
	if err != nil {
		t.Fatalf("composing over the project the install created: %v", err)
	}
	if p.projectID == "" || p.production.ID == "" {
		t.Fatalf("the composition read project %q and production %q", p.projectID, p.production.ID)
	}

	other := reading
	other.project = "alpha"
	if _, err := compose(ctx, other); err == nil || !strings.Contains(err.Error(), "alpha") {
		t.Fatalf("composing over a project run never installed = %v, want a refusal naming it", err)
	}
	if _, found, err := project.ByName(ctx, d.pool, "alpha"); err != nil || found {
		t.Errorf("a composition that does not install created project alpha: found %v, %v", found, err)
	}
}

// TestAVersionAuthoredAtAGateNamesTheKeyAndNotTheName: a human typing a version
// at a gate row is recorded the way every record of the graph records a human —
// by the per-person key the People mapping gives the name — so the name is
// reachable through that mapping alone and an erasure of it reaches every record
// at once.
func TestAVersionAuthoredAtAGateNamesTheKeyAndNotTheName(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}

	by := p.authoredAtTheGate()
	if by.Authorship != artifact.AuthorshipGate {
		t.Errorf("the authorship is %q, want the gate component's", by.Authorship)
	}
	if by.Author == d.human {
		t.Fatalf("the author is the typed name %q, want the per-person key", by.Author)
	}
	name, err := people.NameOf(ctx, d.pool, by.Author)
	if err != nil {
		t.Fatalf("resolving the author's key through the People mapping: %v", err)
	}
	if name != d.human {
		t.Errorf("the key %q maps to %q, want the name at this terminal, %q", by.Author, name, d.human)
	}
}
