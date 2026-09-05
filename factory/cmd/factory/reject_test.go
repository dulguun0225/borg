// A reject stopping the path before any release exists, on the first item
// and on the second one a rejected first leaves behind.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// TestARejectStopsThePath scripts a reject at the merge row: the path stops
// before any release exists, the item goes back to implementation with an attempt
// counted there, master is never created, and the close event carries the
// feedback.
func TestARejectStopsThePath(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\napprove\nreject not what I asked for\n")

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped with an error, and a reject is not one: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if !c.rejected {
		t.Fatal("the verdict was reject and the run does not say so")
	}
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

	// The item is at implementation with two attempts there: the authoring one,
	// and the one the reject booked against the stage it was sent to.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("item stage = %s, a rejected item goes back to implementation", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		want := 1
		if st.Stage == item.StageImplementation {
			want = 2
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
	// rejected candidate used.
	env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if !env.Live() {
		t.Error("the rejected item's environment was torn down, and it stays the item's until it merges or is dropped")
	}

	// The close event carries the feedback.
	rows := decisionRows(readLog(t, ctx, d))
	if len(rows) != 4 {
		t.Fatalf("the log holds %d decision rows, and a reject at the merge row is two decisions: the production row never fires", len(rows))
	}
	payload := closingPayload(t, rows[3])
	if payload.Verdict != string(gate.VerdictReject) {
		t.Errorf("the closing carries verdict %q, the human rejected", payload.Verdict)
	}
	if payload.Feedback != "not what I asked for" {
		t.Errorf("the closing carries feedback %q, the human typed %q", payload.Feedback, "not what I asked for")
	}
	if payload.ReturnsTo != gate.ReturnsTo {
		t.Errorf("the closing returns the item to %q, want %q", payload.ReturnsTo, gate.ReturnsTo)
	}
}

// TestARejectThenASecondRunShips is the other way a service reaches a second
// item: the first was rejected, so master does not exist and the second branch is
// committed with no base too. The rejected item's criterion is not in force for
// the second item's build — a build is a set of items and the rejected one is not
// in it, which is what lets a candidate decomposed in parallel with another one build at
// all. What it ships is release number 1, the reject having minted none.
func TestARejectThenASecondRunShips(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, "approve\nreject not what I asked for\n", approvals)

	if !first.rejected {
		t.Fatal("the first run's scripted verdict was a reject and the run does not say so")
	}
	if second.rejected {
		t.Fatal("the second run reports rejected, and its scripted verdict was approve")
	}

	rel, err := release.Get(ctx, d.pool, second.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("the release's number = %d, the rejected item minted none so this is the service's first", rel.Number)
	}

	// One criterion in force for the second item's build: its own. The rejected
	// item's is a promise the service records and this build's tree could not keep.
	inForce, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria in force: %v", err)
	}
	if len(inForce) != 1 || inForce[0].ItemID != second.itemID {
		t.Fatalf("%d criteria are in force for the second item's build: %+v", len(inForce), inForce)
	}
	if err := criterion.CheckEncodings(theRepo(d), inForce); err != nil {
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

	// The second branch had no base either: the reject minted no release, so
	// nothing had created master by the time it was decomposed.
	depth, err := git(theRepo(d), "rev-list", "--count", second.branch)
	if err != nil {
		t.Fatalf("counting the second branch's commits: %v", err)
	}
	if depth != "1" {
		t.Errorf("the second branch is %s commits deep, and with master absent it is committed with no base", depth)
	}
}
