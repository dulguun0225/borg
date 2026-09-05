// The end-to-end demonstration: one change followed through the whole path,
// approved at every row that puts a human there, released, deployed, running,
// and walkable back to its intent with the decisions readable in a clean chain.
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
)

// TestOneChangeShips is the demonstration: one change followed end to end,
// approved at every row that put a human there, released as number 1, deployed
// without a control, running on the target, and walkable from the deploy back to
// the intent with the decisions readable in a clean chain.
func TestOneChangeShips(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)
	if c.rejected {
		t.Fatal("the run reports rejected, and every scripted verdict was approve")
	}

	// The intent: one round, its question answered, refined.
	in, err := intent.Get(ctx, d.pool, c.intentID)
	if err != nil {
		t.Fatalf("reading the intent: %v", err)
	}
	if in.Rounds != 1 {
		t.Errorf("intent rounds = %d, one round was asked", in.Rounds)
	}
	if in.State != intent.StateRefined {
		t.Errorf("intent state = %s, the interview marked it refined", in.State)
	}
	questions, err := intent.Questions(ctx, d.pool, c.intentID)
	if err != nil {
		t.Fatalf("reading the questions: %v", err)
	}
	if len(questions) != 1 {
		t.Fatalf("the intent has %d questions, one was asked", len(questions))
	}
	if !questions[0].Answered() || questions[0].Answer != theAnswer {
		t.Errorf("the question's answer = %q answered=%t, the human answered %q",
			questions[0].Answer, questions[0].Answered(), theAnswer)
	}

	// The item: merged, one attempt with spend on each authored stage.
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageMerged {
		t.Errorf("item stage = %s, the path ends at merged", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, c.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	if len(stages) != 2 {
		t.Fatalf("the item has %d stage rows, spec and implementation reported one each: %+v", len(stages), stages)
	}
	reported := map[item.Stage]bool{}
	for _, st := range stages {
		reported[st.Stage] = true
		if st.Attempts != 1 {
			t.Errorf("stage %s attempts = %d, each stage ran once", st.Stage, st.Attempts)
		}
		if st.SpendTokens <= 0 {
			t.Errorf("stage %s spend = %d, each stage spent tokens", st.Stage, st.SpendTokens)
		}
	}
	if !reported[item.StageSpec] || !reported[item.StageImplementation] {
		t.Errorf("the reported stages are %v, spec and implementation were expected", stages)
	}

	// The release is number 1, and it names the build the re-verification made
	// rather than the one the implementation stage did — which for a candidate with
	// no master to merge is the same build.
	rel, err := release.Get(ctx, d.pool, c.releaseID)
	if err != nil {
		t.Fatalf("reading the release: %v", err)
	}
	if rel.Number != 1 {
		t.Errorf("release number = %d, a service's first release is 1", rel.Number)
	}
	if rel.BuildID != c.reverifiedBuildID {
		t.Errorf("the release names build %s, the re-verification produced %s", rel.BuildID, c.reverifiedBuildID)
	}

	// The deploy completed and Current names it — what is running, not what
	// is newest.
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID)
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ID != c.deployID {
		t.Errorf("the current deploy is %q found=%t, the path deployed %s", current.ID, found, c.deployID)
	}
	if current.Status != deploy.StatusComplete {
		t.Errorf("deploy status = %s, the deploy without a control completes", current.Status)
	}
	if current.BuildID != rel.BuildID {
		t.Errorf("the deploy names build %s and the release names %s", current.BuildID, rel.BuildID)
	}

	// The target runs the build. What crosses the seam is the build and never the
	// release: a target runs a binary rather than a name.
	running, err := d.targets.at(d.dir).ReadRunning(ctx, "demo", d.credential)
	if err != nil {
		t.Fatalf("reading what the target runs: %v", err)
	}
	if running.Build != rel.BuildID {
		t.Errorf("the target runs %q, the deploy put build %s there", running.Build, rel.BuildID)
	}

	// Master exists in the repository at the commit the queue fast-forwarded to.
	master, err := git(theRepo(d), "rev-parse", "master")
	if err != nil {
		t.Fatalf("reading master: %v", err)
	}
	if master != c.reverifiedCommit {
		t.Errorf("master is at %s, the fast-forward targeted %s", master, c.reverifiedCommit)
	}

	// The walk alone — the walk subcommand's code — reaches the intent's
	// statement from the deploy id, and reports the chain clean.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, c.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", c.deployID, theStatement, walked.String())
	}
	if !strings.Contains(walked.String(), "the chain is clean") {
		t.Errorf("the walk does not report the chain clean:\n%s", walked.String())
	}

	// The log: three decisions, six rows — the candidate deploy row, the merge
	// row, and the production deploy row, each opened by its gate component.
	rows, err := decisionlog.Read(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	if len(rows) != 6 {
		t.Fatalf("the log holds %d rows, three decisions are six:\n%s", len(rows), out)
	}
	for n, want := range []struct {
		part  decisionlog.Part
		actor string
	}{
		{decisionlog.PartOpen, "gate.deploy_to_candidate_environment"},
		{decisionlog.PartClose, ""},
		{decisionlog.PartOpen, "gate.merge_to_master"},
		{decisionlog.PartClose, ""},
		{decisionlog.PartOpen, "gate.deploy_to_production"},
		{decisionlog.PartClose, ""},
	} {
		row := rows[n]
		if row.Shape != decisionlog.ShapeDecision || row.Part != want.part {
			t.Errorf("row %d is shape %s part %s, want a %s decision row", n+1, row.Shape, row.Part, want.part)
		}
		if want.actor != "" && row.Actor.Name != want.actor {
			t.Errorf("row %d's actor is %q, want %q", n+1, row.Actor.Name, want.actor)
		}
	}
	if rows[1].Closes != rows[0].ID || rows[3].Closes != rows[2].ID || rows[5].Closes != rows[4].ID {
		t.Error("a close event does not close the open event before it")
	}

	// Every decision names the policy version and the score version it was
	// decided under, and both are records rather than names.
	scoreVersion, found, err := score.Newest(ctx, d.pool)
	if err != nil || !found {
		t.Fatalf("reading the score version: %v", err)
	}
	policyVersion, err := policy.InForce(ctx, d.pool)
	if err != nil {
		t.Fatalf("reading the policy version: %v", err)
	}
	for _, opening := range []decisionlog.Row{rows[0], rows[2], rows[4]} {
		if opening.ScoreVersion != scoreVersion.ID {
			t.Errorf("an opening names score version %q, want the record %q", opening.ScoreVersion, scoreVersion.ID)
		}
		if opening.PolicyVersion != policyVersion.ID {
			t.Errorf("an opening names policy version %q, want the record %q", opening.PolicyVersion, policyVersion.ID)
		}
	}
	if _, err := score.Get(ctx, d.pool, rows[0].ScoreVersion); err != nil {
		t.Errorf("the score version a decision names does not read back: %v", err)
	}
	if _, err := policy.Get(ctx, d.pool, rows[0].PolicyVersion); err != nil {
		t.Errorf("the policy version a decision names does not read back: %v", err)
	}

	// The merge row's opening names the implementation version under decision and
	// the whole vector; neither deploy row's names an artifact, there being none
	// under decision at a deploy.
	mergeOpening := openingPayload(t, rows[2])
	if mergeOpening.ArtifactID != c.implArtifactID {
		t.Errorf("the merge opening names artifact %s, the decision was over implementation %s",
			mergeOpening.ArtifactID, c.implArtifactID)
	}
	if len(mergeOpening.Vector) == 0 || mergeOpening.Number <= 0 {
		t.Errorf("the merge opening carries %d factors and number %v", len(mergeOpening.Vector), mergeOpening.Number)
	}
	if len(mergeOpening.Unavailable) != 0 {
		t.Errorf("the merge opening names %v as unavailable, and every factor is computable here", mergeOpening.Unavailable)
	}
	if len(mergeOpening.Criteria) != 1 || mergeOpening.Criteria[0].Outcome != criterion.OutcomePassed {
		t.Errorf("the merge opening carries criteria %+v, want the one criterion passed", mergeOpening.Criteria)
	}
	for _, deployRow := range []decisionlog.Row{rows[0], rows[4]} {
		if payload := openingPayload(t, deployRow); payload.ArtifactID != "" {
			t.Errorf("a deploy opening names artifact %q, and nothing is under decision at a deploy", payload.ArtifactID)
		}
	}
	// The candidate deploy row's opening carries no outcome: the run that decides
	// the criteria is what that deploy is for.
	if payload := openingPayload(t, rows[0]); len(payload.Criteria) != 0 {
		t.Errorf("the candidate deploy opening carries criteria %+v, and none is decided yet", payload.Criteria)
	}

	// The first item on a fresh factory is decided by a human at every row, which
	// is the calibration the milestone states: no earlier release to return to, an
	// author nobody has approved, and an area with no history.
	for name, firing := range map[string]fired{
		"candidate deploy": c.candidateGate,
		"merge":            c.mergeGate,
		"production":       c.deployGate,
	} {
		if !firing.humanDecided {
			t.Errorf("the %s row auto-passed the first item of a fresh factory (number %v against threshold %v)",
				name, firing.number, firing.threshold)
		}
	}

	if err := decisionlog.Verify(ctx, d.pool); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}
