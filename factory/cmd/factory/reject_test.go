// A reject at the merge row: what it costs before the attempt limit is spent
// (rebuilt against the feedback, on the same environment, no release minted),
// and the second item a service reaches once the first item's attempts are
// spent and it is escalated rather than merged.
package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// rejectUntilEscalated is enough verdicts to reject the merge row [attemptLimit]
// times running, on a first release where every one of Spec, Implementation
// plan, Tasks, Implementation and Deploy to candidate environment is a human's
// decision too: [approvalsBeforeMerge] once for the first attempt, then one
// reject at the merge row per attempt, each rebuild after the first needing
// only Implementation and Deploy to candidate environment approved again — the
// item goes back to Implementation and Spec, the plan and the tasks are not
// re-authored, so their rows do not fire again.
var rejectUntilEscalated = approvalsBeforeMerge + "reject not what I asked for\n" +
	strings.Repeat("approve\napprove\nreject not what I asked for\n", attemptLimit-1)

// TestARejectStopsThePath scripts a reject at the merge row, repeated until the
// stage's own attempt limit is spent: [path.mergeUntilQueued] sends the item back
// to Implementation with an attempt counted there and builds it again against the
// feedback, and only running out of attempts stops it — a single reject no longer
// does. No release exists, master is never created, and the item's environment
// stays live throughout, keeping it ready for whoever clears the escalation.
func TestARejectStopsThePath(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+rejectUntilEscalated)
	// A human who keeps rejecting the same thing is not something a rebuild
	// fixes on its own; see
	// [TestAStoresForwardPromiseRefusesAnAlwaysPopulatedColumn] for why this
	// uses [retriedWithNoFix] — the fake's reply would otherwise be byte-identical
	// across attempts and git would refuse the rebuild's own commit.
	d.model = &retriedWithNoFix{inner: d.model}

	res, err := run(ctx, d, of(theStatement))
	if err == nil {
		t.Fatalf("a reject repeated until the attempt limit is spent finished without escalating:\n%s", out)
	}
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Errorf("the error is %v, want a stage out of attempts", err)
	}
	c := only(t, res)
	if c.releaseID != "" || c.deployID != "" {
		t.Errorf("the run names release %q and deploy %q, a reject ships nothing", c.releaseID, c.deployID)
	}

	// No release exists.
	var releases int
	if err := d.pool.QueryRow(ctx, `select count(*) from `+release.Table).Scan(&releases); err != nil {
		t.Fatalf("counting releases: %v", err)
	}
	if releases != 0 {
		t.Errorf("%d releases exist, a reject mints none", releases)
	}

	// The item is escalated: the stage's own attempt limit is spent rebuilding
	// against feedback a rebuild here never satisfies.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageEscalated {
		t.Errorf("item stage = %s, an item that spent its attempts is escalated", it.Stage)
	}

	// Implementation carries every rebuild's own entry; Spec, Implementation
	// plan and Tasks are not re-authored by a return to Implementation, so
	// they stand at their first and only entry.
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		want := 1
		if st.Stage == item.StageImplementation {
			want = attemptLimit + 1
		}
		if st.Attempts != want {
			t.Errorf("stage %s attempts = %d, want %d", st.Stage, st.Attempts, want)
		}
	}

	// Master was never created.
	if _, err := git(theRepo(d), "rev-parse", "--verify", "master"); err == nil {
		t.Error("master exists, and the fast-forward runs only after the queue passes a candidate")
	}

	// The environment stays the item's: nothing waits on the environment a
	// rejected candidate used, and every rebuild ran on it again rather than a
	// second one.
	env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if !env.Live() {
		t.Error("the escalated item's environment was torn down, and it stays the item's until it merges or is dropped")
	}

	// Every reject's close event carries the feedback and sends the item back
	// to Implementation.
	rejects := 0
	for _, row := range decisionRows(readLog(t, ctx, d)) {
		payload := closingPayload(t, row)
		if payload.Verdict != string(gate.VerdictReject) {
			continue
		}
		rejects++
		if payload.Reason != "not what I asked for" {
			t.Errorf("a reject's close event carries the reason %q, the human typed %q", payload.Reason, "not what I asked for")
		}
		if payload.ReturnsTo != gate.ReturnsToImplementation {
			t.Errorf("a reject's close event returns the item to %q, want %q", payload.ReturnsTo, gate.ReturnsToImplementation)
		}
	}
	if rejects != attemptLimit {
		t.Errorf("the log holds %d reject close event(s), want %d — one per attempt the merge row rejected", rejects, attemptLimit)
	}
	if !strings.Contains(out.String(), "goes back to implementation against what the Merge to master row found wrong") {
		t.Errorf("the run does not print the re-entry line:\n%s", out)
	}
}

// TestARejectThenASecondRunShips is the other way a service reaches a second
// item: the first item's attempts are spent rejecting at the merge row and it
// is escalated rather than merged, so master does not exist and the second
// branch is committed with no base too. The escalated item's criterion is not
// in force for the second item's build — a build is a set of items and the
// escalated one is not in it, which is what lets a candidate decomposed in
// parallel with another one build at all. What it ships is release number 1,
// the first item having minted none.
func TestARejectThenASecondRunShips(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+rejectUntilEscalated)
	d.model = &retriedWithNoFix{inner: d.model}
	firstRes, err := run(ctx, d, of(theStatement))
	if err == nil {
		t.Fatalf("a reject repeated until the attempt limit is spent finished without escalating:\n%s", out)
	}
	if !errors.Is(err, dispatch.ErrOutOfAttempts) {
		t.Errorf("the error is %v, want a stage out of attempts", err)
	}
	first := only(t, firstRes)

	d.in = strings.NewReader(approvals)
	d.model = &fakeModel{}
	secondRes, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	second := only(t, secondRes)
	if second.itemID == first.itemID {
		t.Fatal("both runs report the same item, a second change is a second item")
	}
	if second.rejected {
		t.Fatal("the second run reports rejected, and its scripted verdict was approve")
	}

	rel, err := release.Get(ctx, d.pool, second.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("the release's number = %d, the first item minted none so this is the service's first", rel.Number)
	}

	// One criterion in force for the second item's build: its own. The escalated
	// item's is a promise the service records and this build's tree could not keep.
	inForce, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria in force: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ItemID != second.itemID {
		t.Fatalf("%d criteria are in force for the second item's build: %+v", len(inForce), inForce)
	}
	derived, err := criterion.Derive(theRepo(d))
	if err != nil {
		t.Fatalf("deriving the encodings: %v", err)
	}
	if err := criterion.CheckEncodings(derived, inForce, nil); err != nil {
		t.Errorf("the second build does not satisfy the encoding check: %v", err)
	}

	// Both criteria are records of the service all the same, nothing here
	// withdrawing one.
	both, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{first.itemID, second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria of both items: %v", err)
	}
	if len(both) != 2 {
		t.Errorf("%d criteria belong to the two items, each introduced one", len(both))
	}

	// The second branch had no base either: the first item's attempts were
	// spent without minting a release, so nothing had created master by the
	// time it was decomposed.
	depth, err := git(theRepo(d), "rev-list", "--count", second.branch)
	if err != nil {
		t.Fatalf("counting the second branch's commits: %v", err)
	}
	if depth != "1" {
		t.Errorf("the second branch is %s commits deep, and with master absent it is committed with no base", depth)
	}
}
