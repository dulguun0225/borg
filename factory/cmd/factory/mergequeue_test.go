// Candidate environments and the merge queue: an environment of its own per
// candidate, two proceeding at once, a candidate whose reverification fails,
// the priority that reorders the queue, and the substrate with no room left.
package main

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/release"
)

// TestACandidateGetsAnEnvironmentOfItsOwn is M3's first claim: the gate that
// decides the candidate's deploy creates an environment named for the item, the
// build goes on it and the deploy record names that build and no release, the
// criteria are decided there, and the environment is torn down at the merge with
// its record kept.
func TestACandidateGetsAnEnvironmentOfItsOwn(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if env.ID != c.environmentID {
		t.Errorf("the item's environment is %s, the run composed %s", env.ID, c.environmentID)
	}
	if env.Kind != environment.KindCandidate {
		t.Errorf("the environment's kind is %s, want a candidate's", env.Kind)
	}
	if env.Name != environment.NameForItem(c.itemID) {
		t.Errorf("the environment is named %q, want %q", env.Name, environment.NameForItem(c.itemID))
	}
	if env.ItemID != c.itemID {
		t.Errorf("the environment names item %s, want %s", env.ItemID, c.itemID)
	}
	if !slices.Equal(env.Targets, []string{c.environmentDir}) {
		t.Errorf("the environment's targets are %v, want the directory of its own %q", env.Targets, c.environmentDir)
	}
	if len(env.ComposedFrom) != 0 {
		t.Errorf("the environment was composed from %+v, and decomposition declared no dependency", env.ComposedFrom)
	}
	if env.Live() {
		t.Error("the environment is still live, and the item merged")
	}
	if !c.tornDown {
		t.Error("the run does not report the environment torn down")
	}
	// The record is kept rather than deleted, because the deploy records naming it
	// would otherwise point at nothing.
	if _, err := environment.Get(ctx, d.pool, env.ID); err != nil {
		t.Errorf("the torn-down environment's record does not read back: %v", err)
	}

	// The candidate deploy record names the build and no release: the number is
	// minted one gate below it.
	candidateDeploy, err := deploy.Get(ctx, d.pool, c.candidateDeployID)
	if err != nil {
		t.Fatalf("reading the candidate deploy: %v", err)
	}
	if candidateDeploy.EnvironmentID != env.ID {
		t.Errorf("the candidate deploy names environment %s, want %s", candidateDeploy.EnvironmentID, env.ID)
	}
	if candidateDeploy.ReleaseID != "" {
		t.Errorf("the candidate deploy names release %q, and no number exists at that row", candidateDeploy.ReleaseID)
	}
	if candidateDeploy.BuildID == "" {
		t.Error("the candidate deploy names no build, and the build is what it put there")
	}
	// Nothing is current on a candidate environment: Current reads the records
	// that name a release.
	if _, running, err := deploy.Current(ctx, d.pool, res.serviceID, env.ID); err != nil || running {
		t.Errorf("Current on a candidate environment = running %v, %v", running, err)
	}

	// The criteria were decided against the build, on that environment, by the
	// deploy agent.
	results, err := criterion.ResultsForBuild(ctx, d.pool, candidateDeploy.BuildID)
	if err != nil {
		t.Fatalf("reading what was decided over the build: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("%d criteria were decided over the build, one was in force: %+v", len(results), results)
	}
	if results[0].Outcome != criterion.OutcomePassed {
		t.Errorf("the criterion is %s over the build, want passed", results[0].Outcome)
	}
	if results[0].Actor.Key != "deploy" {
		t.Errorf("the result was written by %q, want the deploy agent", results[0].Actor.Key)
	}
	if !strings.Contains(out.String(), "ran twice on the candidate environment") {
		t.Errorf("the run does not report the encodings running twice:\n%s", out)
	}
}

// TestTwoCandidatesProceedAtOnce is M3's demonstration: two intents in one run,
// two items decomposed on the same master, two candidate environments live at once with
// different targets and different deploy records, and the queue merging them in
// its order — the second re-verifying against the master the first created, which
// is a build the implementation stage never made.
func TestTwoCandidatesProceedAtOnce(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	// The window limit at two, so both releases may hold a window open and both may
	// deploy. At the limit of one the score supplies, the second candidate's own
	// environment and its merge are unaffected and its production deploy waits behind
	// the first release's window — which is
	// [TestTheWindowLimitHoldsTheNextProductionDeploy], and is the serial factory the
	// design says a limit of one is.
	installWindow(t, ctx, d, 2)

	// One change first, so master exists and the two candidates below are both
	// based on it. Two candidates decomposed before any release have no common commit, and
	// what that costs is stated where the queue merges them.
	first, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("")
	res, err := run(ctx, d, of(theSecondStatement, theThirdStatement))
	if err != nil {
		t.Fatalf("the two-candidate run stopped: %v\noutput so far:\n%s", err, out)
	}
	if len(res.candidates) != 2 {
		t.Fatalf("the run has %d candidates, two intents are two", len(res.candidates))
	}
	a, b := res.candidates[0], res.candidates[1]

	// Two environments, neither reading the other's: different records, different
	// items, different directories, different deploy records.
	if a.environmentID == b.environmentID || a.environmentDir == b.environmentDir {
		t.Fatalf("the two candidates share environment %s at %s", a.environmentID, a.environmentDir)
	}
	if a.candidateDeployID == b.candidateDeployID {
		t.Error("the two candidate deploys are one record")
	}
	for _, c := range res.candidates {
		env, found, err := environment.ForItem(ctx, d.pool, c.itemID)
		if err != nil || !found {
			t.Fatalf("ForItem(%s) = found %v, %v", c.itemID, found, err)
		}
		if env.ItemID != c.itemID {
			t.Errorf("environment %s names item %s, want %s", env.ID, env.ItemID, c.itemID)
		}
		if !c.merged {
			t.Errorf("item %s did not merge:\n%s", c.itemID, out)
		}
		if !c.tornDown {
			t.Errorf("item %s merged and its environment was not torn down", c.itemID)
		}
	}

	// Each candidate's criteria attach to its own build, so what one run produced
	// is not read as the other's.
	for _, c := range res.candidates {
		results, err := criterion.ResultsForBuild(ctx, d.pool, c.reverifiedBuildID)
		if err != nil {
			t.Fatalf("reading what was decided over build %s: %v", c.reverifiedBuildID, err)
		}
		if len(results) == 0 {
			t.Errorf("nothing was decided over build %s", c.reverifiedBuildID)
		}
		for _, result := range results {
			if result.BuildID != c.reverifiedBuildID {
				t.Errorf("a result of build %s names %s", c.reverifiedBuildID, result.BuildID)
			}
		}
	}

	// The queue merged them in its order: numbers 2 and 3 after the first run's 1.
	numbers := map[int64]string{}
	for _, c := range res.candidates {
		rel, err := release.Get(ctx, d.pool, c.releaseID)
		if err != nil {
			t.Fatalf("reading release %s: %v", c.releaseID, err)
		}
		numbers[rel.Number] = c.itemID
	}
	if numbers[2] != a.itemID || numbers[3] != b.itemID {
		t.Errorf("the numbers went %+v, want 2 to the first-approved item %s and 3 to %s",
			numbers, a.itemID, b.itemID)
	}

	// The second candidate's release names a build the implementation stage never
	// made: it is the re-verification's, made from the candidate branch with the
	// master the first one created merged into it.
	if b.reverifiedBuildID == b.buildID {
		t.Error("the second candidate's re-verification reused its own build, and master had moved under it")
	}
	if _, err := git(theRepo(d), "merge-base", "--is-ancestor", first.candidates[0].reverifiedCommit,
		b.reverifiedCommit); err != nil {
		t.Errorf("the first release's commit is not an ancestor of the second candidate's re-verified commit: %v", err)
	}

	// Master is at the last commit the queue fast-forwarded to.
	master, err := git(theRepo(d), "rev-parse", "master")
	if err != nil {
		t.Fatalf("reading master: %v", err)
	}
	if master != b.reverifiedCommit {
		t.Errorf("master is at %s, the last fast-forward targeted %s", master, b.reverifiedCommit)
	}

	// Both deployed, and production runs the last one.
	for _, c := range res.candidates {
		if c.deployID == "" {
			t.Errorf("item %s minted release %s and deployed nothing", c.itemID, c.releaseID)
		}
	}
	running, err := d.targets.at(d.dir).ReadRunning(ctx, "demo", d.credential)
	if err != nil {
		t.Fatalf("reading what production runs: %v", err)
	}
	if running.Build != b.reverifiedBuildID {
		t.Errorf("production runs %q, the last deploy put build %s there", running.Build, b.reverifiedBuildID)
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after two candidates at once: %v", err)
	}
}

// TestTheQueueRejectsACandidateThatFailedItsOwnReverification is the queue's
// rejection: two candidates change one file differently, so the second one's
// re-verification against the master the first created is a merge that conflicts.
// The item goes back to Implementation with an attempt counted there, no release
// is minted for it, its environment stays its own, and the log holds a wait row
// naming the queue as its actor.
func TestTheQueueRejectsACandidateThatFailedItsOwnReverification(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &conflictingModel{inner: &fakeModel{}}

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader("")
	res, err := run(ctx, d, of(theSecondStatement, theThirdStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a queue rejection is not an error: %v\noutput so far:\n%s", err, out)
	}
	a, b := res.candidates[0], res.candidates[1]

	if !a.merged {
		t.Fatalf("the first candidate in the queue did not merge:\n%s", out)
	}
	if b.merged || !b.queueRejected {
		t.Fatalf("the second candidate merged=%v rejected=%v, want the queue rejecting it:\n%s",
			b.merged, b.queueRejected, out)
	}
	if b.releaseID != "" {
		t.Errorf("the rejected candidate minted release %s", b.releaseID)
	}
	if !strings.Contains(b.queueWhy, "merging master") {
		t.Errorf("the queue rejected it because %q, want the merge that conflicted", b.queueWhy)
	}

	// The item is back at Implementation with an attempt counted there — the
	// rework booked against the thing that was wrong.
	it, err := item.Get(ctx, d.pool, b.itemID)
	if err != nil {
		t.Fatalf("reading the rejected item: %v", err)
	}
	if it.Stage != item.StageImplementation {
		t.Errorf("the rejected item is at %s, want implementation", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, b.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	var attempts int
	for _, st := range stages {
		if st.Stage == item.StageImplementation {
			attempts = st.Attempts
		}
	}
	if attempts != 2 {
		t.Errorf("the implementation stage records %d attempts, want the authoring one and the queue's rejection", attempts)
	}

	// Its environment is still its own: nothing waits on the environment a
	// rejected candidate used, and it stays the item's until it merges or is
	// dropped.
	env, found, err := environment.ForItem(ctx, d.pool, b.itemID)
	if err != nil || !found {
		t.Fatalf("ForItem = found %v, %v", found, err)
	}
	if !env.Live() {
		t.Error("the rejected candidate's environment was torn down, and it stays the item's")
	}

	// The rejection is a queue_rejection row the log wrote with the queue as
	// caller and actor: no gate fired, the Merge to master gate's own having
	// closed as an approval.
	rows := readLog(t, ctx, d)
	var waits int
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeQueueRejection {
			continue
		}
		waits++
		if row.ID != b.queueWaitRow {
			t.Errorf("the log's wait row is %s, the run reported %s", row.ID, b.queueWaitRow)
		}
		if row.Actor != mergequeue.Actor {
			t.Errorf("the wait row's actor is %+v, want the queue %+v", row.Actor, mergequeue.Actor)
		}
		var payload mergequeue.RejectionPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the rejection payload: %v", err)
		}
		if payload.Kind != mergequeue.RejectionKind || payload.ItemID != b.itemID {
			t.Errorf("the rejection payload is %+v, want kind %q for item %s",
				payload, mergequeue.RejectionKind, b.itemID)
		}
		if payload.ReturnsTo != gate.ReturnsTo || !payload.CountsAnAttempt {
			t.Errorf("the rejection returns the item to %q and counts an attempt %v",
				payload.ReturnsTo, payload.CountsAnAttempt)
		}
	}
	if waits != 1 {
		t.Errorf("the log holds %d wait rows, one candidate was rejected", waits)
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after a queue rejection: %v", err)
	}
}

// TestThePriorityReordersTheQueue is the settable order: an owner writes a
// priority through dispatch and the queue takes that candidate first. What it
// changes is when a candidate re-verifies and never what it has to pass — so the
// one that goes second is the one whose merge conflicts, which is the opposite of
// what happens with the priorities left where decomposition wrote them.
func TestThePriorityReordersTheQueue(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.model = &conflictingModel{inner: &fakeModel{}}

	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	// The priority is written between the Merge to master gate and the queue, which is what
	// a screen would do: the run does both in one call, so this test drives the
	// steps rather than run itself.
	d.in = strings.NewReader("")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	var candidates []*candidate
	for _, statement := range []string{theSecondStatement, theThirdStatement} {
		candidates = append(candidates, authorOne(t, ctx, p, statement, out))
	}
	for _, c := range candidates {
		if err := p.candidateEnvironment(ctx, c); err != nil {
			t.Fatalf("the candidate environment of %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
		if err := p.mergeGate(ctx, c); err != nil {
			t.Fatalf("the Merge to master gate of %s: %v\noutput so far:\n%s", c.itemID, err, out)
		}
	}

	// The second-approved candidate is pushed to the front.
	if _, err := item.NewDispatch(d.pool, d.token).SetPriority(ctx, owner(d.human), candidates[1].itemID, 5); err != nil {
		t.Fatalf("setting the priority: %v", err)
	}
	members, err := p.queue.Members(ctx, theServiceRecord(t, ctx, p).ID)
	if err != nil {
		t.Fatalf("reading the queue's members: %v", err)
	}
	if len(members) != 2 || members[0].ID != candidates[1].itemID {
		t.Fatalf("the queue's order is %+v, want the pushed candidate %s first", members, candidates[1].itemID)
	}

	if _, err := p.runQueue(ctx, theServiceRecord(t, ctx, p)); err != nil {
		t.Fatalf("the queue stopped: %v\noutput so far:\n%s", err, out)
	}
	if !candidates[1].merged {
		t.Errorf("the candidate an owner pushed to the front did not merge:\n%s", out)
	}
	if candidates[0].merged || !candidates[0].queueRejected {
		t.Errorf("the candidate behind it merged=%v rejected=%v, and its merge is the one that conflicts",
			candidates[0].merged, candidates[0].queueRejected)
	}
}

// TestTheSubstrateWithNoRoomWaits is the other hold at the candidate deploy row,
// and the one that writes: the substrate has no room for another environment, that
// condition is not a record, and no parameter of an owner's limits it — so it goes
// into the log as a wait with the deploy agent as caller and actor.
func TestTheSubstrateWithNoRoomWaits(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	d.candidateCeiling = 1

	res, err := run(ctx, d, of(theStatement, theSecondStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a wait is not an error: %v\noutput so far:\n%s", err, out)
	}
	a, b := res.candidates[0], res.candidates[1]

	if a.environmentID == "" {
		t.Fatalf("the first candidate got no environment with room for one:\n%s", out)
	}
	if b.environmentID != "" {
		t.Errorf("the second candidate got environment %s with the ceiling at one", b.environmentID)
	}
	if b.factoryHold != gate.HoldNoRoomForAnotherEnvironment {
		t.Errorf("the second candidate's hold is %q, want the substrate's", b.factoryHold)
	}
	if b.candidateGate.opening != "" {
		t.Error("the candidate deploy row fired for the held candidate, and a factory hold is not a verdict")
	}
	if b.releaseID != "" || b.deployID != "" {
		t.Errorf("the held candidate minted release %q and deployed %q", b.releaseID, b.deployID)
	}

	rows := readLog(t, ctx, d)
	var waits int
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeWait {
			continue
		}
		waits++
		if row.ID != b.holdWaitRow {
			t.Errorf("the wait row is %s, the run reported %s", row.ID, b.holdWaitRow)
		}
		if row.Actor.Key != "deploy" {
			t.Errorf("the wait row's actor is %q, want the deploy agent that met the condition", row.Actor.Key)
		}
		var payload substrateWait
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the wait payload: %v", err)
		}
		if payload.Kind != SubstrateWaitKind || payload.ItemID != b.itemID {
			t.Errorf("the wait payload is %+v, want kind %q for item %s", payload, SubstrateWaitKind, b.itemID)
		}
		if payload.Ceiling != 1 || payload.Live != 1 {
			t.Errorf("the wait payload says %d live against a ceiling of %d, want 1 and 1", payload.Live, payload.Ceiling)
		}
	}
	if waits != 1 {
		t.Errorf("the log holds %d wait rows, one candidate met the ceiling", waits)
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after a wait: %v", err)
	}
}
