// Tests that the Implementation gate's transition check and drivers actually
// fire over a checkout: the composition hands the build's checkout to
// [screenstatemachine.DeriveTransitions] and [screenstatemachine.DeriveDrivers]
// at the row's own firing, and [gate.ScreenRejection] rejects or resolves a
// human from what they found — never on what could not be derived.
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// screenHeader and screenTransitionLine pick the screen's id, its declared
// states and its one transition out of the implementer's own prompt, rendered
// by [agent.Implementer.Implement] as "<id>: <states>" and
// "  <from> on <event> goes to <to>".
var (
	screenHeader         = regexp.MustCompile(`(?m)^(ssm_[0-9a-f]{32}): (.+)$`)
	screenTransitionLine = regexp.MustCompile(`(?m)^  (\S+) on (\S+) goes to (\S+)$`)
)

// validScreenModel wraps a model and has the implementer write a screen's own
// transition function that admits exactly the one transition the machine
// declares, plus a driver for every state the prompt names — a clean,
// derivable checkout, which is the baseline the two tests over this row amend
// afterward. A call naming no screen is passed through unchanged, which is
// every role but the implementer's on an item with a screen.
type validScreenModel struct{ inner agent.Model }

func (m *validScreenModel) Complete(ctx context.Context, as principal.Principal, call agent.Call) (agent.Reply, error) {
	reply, err := m.inner.Complete(ctx, as, call)
	if err != nil || call.System != agent.ShippedImplementerPrompt {
		return reply, err
	}
	header := screenHeader.FindStringSubmatch(call.User)
	if header == nil {
		return reply, nil
	}
	id, states := header[1], strings.Split(header[2], ", ")
	transition := screenTransitionLine.FindStringSubmatch(call.User)
	if transition == nil {
		return reply, fmt.Errorf("fake model: the implementer's prompt names screen %s and no transition", id)
	}
	from, event, to := transition[1], transition[2], transition[3]

	reply.Text += fmt.Sprintf("\n=== FILE %s ===\npackage main\n\nfunc Transition(from, event string) string {\n"+
		"\tswitch from {\n\tcase %q:\n\t\tswitch event {\n\t\tcase %q:\n\t\t\treturn %q\n\t\t}\n\t}\n\treturn from\n}\n=== END ===\n",
		screenstatemachine.FileName(id), from, event, to)

	var drivers strings.Builder
	drivers.WriteString("package main\n\n")
	for _, state := range states {
		fmt.Fprintf(&drivers, "// drives %s:%s\n", id, state)
	}
	reply.Text += "\n=== FILE screendrivers.go ===\n" + drivers.String() + "=== END ===\n"
	return reply, nil
}

// withApprovedScreen authors one item with a user interface through
// Implementation's own approval, on a checkout the transition check and the
// drivers both derive cleanly — the baseline the two tests over this row
// amend by hand afterward, the way an agent's next attempt would change the
// checkout the row rejected. The model is wrapped before composing the path,
// which is what makes the swap reach the dispatcher: [path.d] is a copy taken
// at composition, and a swap after it would reach nothing running.
func withApprovedScreen(t *testing.T, ctx context.Context, d deps, out *bytes.Buffer) (*path, *candidate, string) {
	t.Helper()
	d.model = &validScreenModel{inner: d.model}
	path := p(ctx, t, d)
	c := authorOne(t, ctx, path, theScreenStatement, out)
	if len(c.screens) != 1 {
		t.Fatalf("the item declared %d screen(s), want the one statement introduced", len(c.screens))
	}
	return path, c, c.screens[0].ID
}

// TestAForbiddenTransitionIsRejectedAtImplementation is the transition check's
// own rejection: the implementation admits a transition from a state and an
// event the machine declares to a destination the machine does not declare
// there, which [gate.ScreenRejection] rejects mechanically, before a verdict
// is asked for — the same shape the Spec row's own mechanical rejections take.
func TestAForbiddenTransitionIsRejectedAtImplementation(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	path, c, screenID := withApprovedScreen(t, ctx, d, out)

	// The checkout an agent's next attempt would leave: the same machine, the
	// same declared transition, admitting a destination the machine does not
	// declare there.
	forbidden := fmt.Sprintf("package main\n\nfunc Transition(from, event string) string {\n"+
		"\tswitch from {\n\tcase %q:\n\t\tswitch event {\n\t\tcase %q:\n\t\t\treturn %q\n\t\t}\n\t}\n\treturn from\n}\n",
		"start", "begin", "not-active")
	writeScreenFile(t, c.svc.Repository, screenID, forbidden)

	verdict, reason, err := path.itemGate(ctx, c, gate.Implementation, c.implArtifactID, &fired{}, "", "")
	if err != nil {
		t.Fatalf("itemGate: %v\n%s", err, out)
	}
	if verdict != gate.VerdictReject {
		t.Fatalf("the verdict is %s, want reject: %s\n%s", verdict, reason, out)
	}
	if !strings.Contains(reason, "the machine declares") {
		t.Errorf("the rejection does not read as the transition check's own: %q", reason)
	}
	if !strings.Contains(out.String(), gate.AutoRejectedByForbiddenTransition) {
		t.Errorf("the run does not name the forbidden-transition check:\n%s", out)
	}
}

// TestAConstructTheExtractorCannotFollowResolvesAHuman is the could-not-derive
// outcome: a screen holding a construct the extractor cannot follow rejects
// nothing — the direction is fixed at transitions the machine could have
// declared and did not — and instead resolves a factor, which puts a human at
// the row whatever the score would have returned on its own.
func TestAConstructTheExtractorCannotFollowResolvesAHuman(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	path, c, screenID := withApprovedScreen(t, ctx, d, out)

	// A default case standing for every event the machine does not name: a
	// construct this extractor meets and cannot follow, so the whole screen is
	// could-not-derive rather than partly clean.
	notFollowed := "package main\n\nfunc Transition(from, event string) string {\n" +
		"\tswitch from {\n\tcase \"start\":\n\t\tswitch event {\n\t\tdefault:\n\t\t\treturn \"active\"\n\t\t}\n\t}\n\treturn from\n}\n"
	writeScreenFile(t, c.svc.Repository, screenID, notFollowed)

	var into fired
	verdict, _, err := path.itemGate(ctx, c, gate.Implementation, c.implArtifactID, &into, "", "")
	if err != nil {
		t.Fatalf("itemGate: %v\n%s", err, out)
	}
	if !into.humanDecided {
		t.Fatalf("a screen the extractor could not derive did not put a human at the row:\n%s", out)
	}
	found := false
	for _, m := range into.marks {
		if m == gate.MarkResolvedFactor {
			found = true
		}
	}
	if !found {
		t.Errorf("the marks are %v, want the resolved factor a could-not-derive screen puts there", into.marks)
	}
	if verdict != gate.VerdictApprove {
		t.Errorf("the scripted human's approve was not read as the verdict: %s", verdict)
	}
	if !strings.Contains(out.String(), "could not derive") {
		t.Errorf("the run does not report the screen as could not derive:\n%s", out)
	}
}

// writeScreenFile overwrites screen id's own transition function on disk —
// the checkout the derivations in [path.itemGate] read directly, the way a
// build's own diff is read from the repository and not from a record.
func writeScreenFile(t *testing.T, repo, screenID, source string) {
	t.Helper()
	path := filepath.Join(repo, screenstatemachine.FileName(screenID))
	if err := os.WriteFile(path, []byte(source), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
}
