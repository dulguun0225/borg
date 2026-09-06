// A second change on one service: shipped with a human at every row, shipped
// with nobody asked once the score reads low, and stopped by a safeguard
// putting a human back at production deploy.
package main

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// twoRunsOnOneService walks the path twice on one service and returns what each
// run shipped. The second run reads from a reader of its own because run wraps
// the reader in a bufio.Scanner, which reads further than the lines it hands
// back, so a second scanner over the same reader finds it drained — the run
// subcommand is one process per run and never meets that. The fake spec author
// asks its one question on its first call only.
//
// secondInput is what the second run's human types, and it is empty wherever the
// second run is meant to auto-pass at every row: a reader with nothing in it is
// how a test says nobody was asked anything.
func twoRunsOnOneService(t *testing.T, firstVerdicts, secondInput string) (context.Context, deps, *candidate, *candidate) {
	t.Helper()
	ctx, d, out := newPath(t, theAnswer+"\n"+firstVerdicts)

	first, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the first run stopped: %v\noutput so far:\n%s", err, out)
	}

	d.in = strings.NewReader(secondInput)
	second, err := run(ctx, d, of(theSecondStatement))
	if err != nil {
		t.Fatalf("the second run stopped: %v\noutput so far:\n%s", err, out)
	}
	return ctx, d, only(t, first), only(t, second)
}

// TestASecondChangeShips is the second change on one service: a second intent, a
// second item, a second criterion authored beside the one already in force, and a
// build encoding both — which is what the encoding check demands of a build whose
// item set holds the first item, it having merged. It ships as release number 2,
// and the walk from its deploy reaches its own intent.
func TestASecondChangeShips(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, approvals, approvals)

	if second.itemID == first.itemID {
		t.Errorf("both runs report item %s, a second change is a second item", second.itemID)
	}

	// Release number 2, the number being minted per service.
	rel, err := release.Get(ctx, d.pool, second.releaseID)
	if err != nil {
		t.Fatalf("reading the second release: %v", err)
	}
	if rel.Number != 2 {
		t.Errorf("the second release's number = %d, the second release of a service is 2", rel.Number)
	}

	// Two criteria in force for the second item's build, the second one authored
	// rather than the first restated.
	inForce, err := criterion.InForce(ctx, d.pool, rel.ServiceID, []string{first.itemID, second.itemID})
	if err != nil {
		t.Fatalf("reading the criteria in force: %v", err)
	}
	if len(inForce) != 2 {
		t.Fatalf("%d criteria are in force, two items introduced one each: %+v", len(inForce), inForce)
	}
	if inForce[0].Sentence != criterionSentence || inForce[1].Sentence != secondCriterionSentence {
		t.Errorf("the sentences in force are %q and %q, want the first item's then the second's",
			inForce[0].Sentence, inForce[1].Sentence)
	}

	// The second build encodes both, which is the check the second run had to
	// pass and is asserted here over the tree that build was made from.
	derived, err := criterion.Derive(theRepo(d))
	if err != nil {
		t.Fatalf("deriving the encodings: %v", err)
	}
	if err := criterion.CheckEncodings(derived, inForce, nil); err != nil {
		t.Errorf("the second build does not satisfy the encoding check: %v", err)
	}
	encoded, err := criterion.Encodings(theRepo(d))
	if err != nil {
		t.Fatalf("reading the encodings: %v", err)
	}
	if len(encoded) != 2 {
		t.Errorf("the build names %d criteria in its tests, both criteria in force are encoded: %v", len(encoded), encoded)
	}

	// The walk from the second deploy reaches the second intent and no other.
	var walked bytes.Buffer
	if err := walk(ctx, d.pool, &walked, d.token, asPrincipal(owner(t, ctx, d.pool, d.token, d.human)), second.deployID); err != nil {
		t.Fatalf("the walk stopped: %v\noutput so far:\n%s", err, walked.String())
	}
	if !strings.Contains(walked.String(), theSecondStatement) {
		t.Errorf("the walk from %s does not reach the statement %q:\n%s", second.deployID, theSecondStatement, walked.String())
	}
	if strings.Contains(walked.String(), theStatement) {
		t.Errorf("the walk from %s reaches the first intent's statement:\n%s", second.deployID, walked.String())
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after two changes: %v", err)
	}
}

// TestTheSecondChangeShipsWithNoHumanAtAnyGate is M2's demonstration: the second
// item on the service reads under the threshold at every row that decides over a
// build, so the factory gives those verdicts itself.
//
// What made the difference is the first run: a human approved its implementation,
// which narrowed the prior on the model that wrote it and the history of the area
// it was in, and its release gave the service something to return to. The factory
// earns the autonomy rather than starting with it.
//
// The four rows above a build are not among them, and cannot be: the factor set
// those rows read holds the change's reach, which is computed from a diff, and
// nothing is built when Spec, Implementation plan and Tasks fire. A factor that
// cannot be computed is resolved and a human decides whatever the formula
// returns, so a human is at those three rows on every item. Which factors a row
// above a build reads is the factor set's own question and not this
// interface's.
func TestTheSecondChangeShipsWithNoHumanAtAnyGate(t *testing.T) {
	ctx, d, first, second := twoRunsOnOneService(t, approvals, approvals)

	for name, firing := range map[string]fired{
		"candidate deploy": first.candidateGate,
		"merge":            first.mergeGate,
		"production":       first.deployGate,
	} {
		if !firing.humanDecided {
			t.Fatalf("the first item's %s row auto-passed, and on a fresh factory a human decides every one", name)
		}
	}
	for name, firing := range map[string]fired{
		"candidate deploy": second.candidateGate,
		"merge":            second.mergeGate,
		"production":       second.deployGate,
	} {
		if firing.humanDecided {
			t.Fatalf("the second item's %s row put a human there because %v", name, firing.marks)
		}
	}
	if second.deployID == "" {
		t.Fatal("the second item did not deploy")
	}
	if second.mergeGate.number >= second.mergeGate.threshold {
		t.Errorf("the second item's merge number is %v against a threshold of %v",
			second.mergeGate.number, second.mergeGate.threshold)
	}
	if !(second.mergeGate.number < first.mergeGate.number) {
		t.Errorf("the second item reads %v and the first read %v, and the evidence the first left narrows the second",
			second.mergeGate.number, first.mergeGate.number)
	}

	// Every one of the second run's decisions over a build was closed by the gate
	// component and says what auto-passed it, and every open event of an
	// auto-pass waits on nobody — which is how a reader of the log tells a
	// decision nobody was asked to make from a pending one. The four rows the
	// second run opened above a build are skipped: a human decided each, for the
	// reason this test's own comment gives.
	rows := decisionRows(readLog(t, ctx, d))
	// Seven decisions per run — the four rows of the item's own artifacts and
	// the three event rows — and two rows per decision.
	if len(rows) != 28 {
		t.Fatalf("the log holds %d decision rows, two runs of seven decisions are twenty-eight", len(rows))
	}
	for _, row := range rows[14+8:] {
		if row.Part == decisionlog.PartOpen {
			payload := openingPayload(t, row)
			if payload.WaitsOn.Duty != 0 || payload.WaitsOn.Human != "" || len(payload.WaitsOn.Holders) > 0 {
				t.Errorf("an auto-passed firing waits on %+v", payload.WaitsOn)
			}
			continue
		}
		if row.Actor.Kind != record.KindComponent {
			t.Errorf("the second run's closing was written by %+v, want the gate component", row.Actor)
		}
		payload := closingPayload(t, row)
		if payload.Verdict != string(gate.VerdictApprove) || payload.WhyItAutoPassed != score.AutoPassThreshold {
			t.Errorf("the closing says %+v, want an approve auto-passed by the threshold", payload)
		}
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after two runs: %v", err)
	}
}

// TestASafeguardPutsAHumanBackAtAGateAndTheHoldStopsTheDeploy is the other half
// of M2's demonstration. An owner adds a safeguard at the production deploy row,
// so the row the score would have passed puts a human there; the human holds;
// and the run stops with the release minted, nothing deployed, no attempt
// counted, and the item where it was.
func TestASafeguardPutsAHumanBackAtAGateAndTheHoldStopsTheDeploy(t *testing.T) {
	ctx, d, _, _ := twoRunsOnOneService(t, approvals, approvals)

	// The risk threshold's subject is a row-scoped safeguard drawn on the
	// service the row fires for — package policy's own [Reader] reads it that
	// way, effective.go's safeguardsOn keying a row-scoped safeguard on the
	// service subject with the row as its key.
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service: found %v, %v", found, err)
	}
	placed, version, err := policy.NewFactory(d.pool, d.token).AddSafeguard(ctx,
		owner(t, ctx, d.pool, d.token, d.human), gatepolicy.RiskThreshold,
		safeguard.Subject{Kind: safeguard.SubjectService, ID: svc.ID, Key: gate.DeployToProduction.String()}, safeguard.Bound{Number: 0}, safeguard.Routing{})
	if err != nil {
		t.Fatalf("placing the safeguard: %v", err)
	}

	// Six approvals and then the hold: every row of this item's path puts a human
	// there — the three above a build because the change's reach cannot be
	// computed before anything is built, and the three over a build because the
	// score's calibration found two factors drifted and resolves them until a
	// recalibration is in force — and the seventh row, production, is where the
	// safeguard puts one and where the verdict is hold.
	d.in = strings.NewReader(strings.Repeat("approve\n", 6) + "hold the window before this one is still open\n")
	res, err := run(ctx, d, of(theThirdStatement))
	if err != nil {
		t.Fatalf("the third run stopped, and a hold is not an error: %v", err)
	}
	third := only(t, res)

	if !third.held {
		t.Fatal("the verdict was hold and the run does not say so")
	}
	// The safeguard's own mark is at the deploy row and at no other: it names that
	// row, and no other row carries a mark at all.
	if slices.Contains(third.mergeGate.marks, gate.MarkSafeguard) {
		t.Errorf("the merge row carries the safeguard's mark %v, and the safeguard names the deploy row alone",
			third.mergeGate.marks)
	}
	if !third.deployGate.humanDecided || !slices.Contains(third.deployGate.marks, gate.MarkSafeguard) {
		t.Errorf("the deploy row says human %v because %v, want the safeguard among the marks",
			third.deployGate.humanDecided, third.deployGate.marks)
	}
	if third.deployGate.number >= third.deployGate.threshold {
		t.Errorf("the deploy number is %v against a threshold of %v, and the safeguard is what put a human there rather than the number",
			third.deployGate.number, third.deployGate.threshold)
	}
	if !slices.Contains(third.deployGate.safeguards, placed.ID) {
		t.Errorf("the firing names safeguards %v, want the one placed", third.deployGate.safeguards)
	}
	if third.deployGate.policyVersion != version.ID {
		t.Errorf("the firing names policy version %q, want the one the safeguard appended %q",
			third.deployGate.policyVersion, version.ID)
	}

	// The release is minted and nothing is deployed: a hold is a stop and not an
	// undo, and the change is still good.
	if third.releaseID == "" {
		t.Error("the run minted no release, and the hold is after the merge")
	}
	if third.deployID != "" {
		t.Errorf("the run deployed %s, and a hold stops the event", third.deployID)
	}
	current, found, err := deploy.Current(ctx, d.pool, res.serviceID, res.environmentID, []string{d.dir})
	if err != nil {
		t.Fatalf("reading the current deploy: %v", err)
	}
	if !found || current.ReleaseID == third.releaseID {
		t.Errorf("what runs in production is %q of release %q, and the held release is not deployed",
			current.ID, current.ReleaseID)
	}

	// The item is merged and stays there, and no attempt was counted for the
	// hold: a hold is not a failed attempt.
	it, err := item.Get(ctx, d.pool, third.itemID)
	if err != nil {
		t.Fatalf("reading the item: %v", err)
	}
	if it.Stage != item.StageMerged {
		t.Errorf("the held item is at %s, want merged", it.Stage)
	}
	stages, err := item.Stages(ctx, d.pool, third.itemID)
	if err != nil {
		t.Fatalf("reading the item's stages: %v", err)
	}
	for _, st := range stages {
		if st.Attempts != 1 {
			t.Errorf("stage %s records %d attempts, and a hold counts none", st.Stage, st.Attempts)
		}
	}

	// The hold is the verdict of that firing's decision, with the human as its
	// actor.
	rows := decisionRows(readLog(t, ctx, d))
	closing := rows[len(rows)-1]
	if closing.Actor.Kind != record.KindHuman {
		t.Errorf("the hold was written by %+v, want the human who set it", closing.Actor)
	}
	payload := closingPayload(t, closing)
	if payload.Verdict != string(gate.VerdictHold) {
		t.Errorf("the closing says verdict %q, want a hold", payload.Verdict)
	}
	if payload.ReturnsTo != "" {
		t.Errorf("the hold sends the item to %q, and a hold sends nothing back", payload.ReturnsTo)
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after a hold: %v", err)
	}

	// Withdrawing the safeguard leaves the row the score's again, which is what a
	// safeguard being a bound rather than a precedence means at this row: nothing
	// else moved.
	written, _, err := policy.NewFactory(d.pool, d.token).WriteSafeguardWithdrawal(ctx,
		owner(t, ctx, d.pool, d.token, d.human), placed.ID)
	if err != nil {
		t.Fatalf("writing the withdrawal: %v", err)
	}
	// The safeguard leaves force at the row that decides the withdrawal, closed by
	// a human other than the one who wrote it — the row is routed away from them.
	if _, err := policy.NewFactory(d.pool, d.token).ApproveSafeguardWithdrawal(ctx,
		owner(t, ctx, d.pool, d.token, "reviewer"), written.ID); err != nil {
		t.Fatalf("approving the withdrawal: %v", err)
	}
	applied, err := policy.NewReader(d.pool, d.token, score.Version{}).AtGate(ctx,
		gate.ComponentPrincipal(gate.DeployToProduction), policy.Subjects{
			GateRow:       gate.DeployToProduction.String(),
			EnvironmentID: res.environmentID,
			ServiceID:     res.serviceID,
			AreaID:        res.areaID,
		})
	if err != nil {
		t.Fatalf("AtGate: %v", err)
	}
	if applied.HumanBySafeguard {
		t.Error("the withdrawn safeguard still adds a human at the row")
	}
}

// TestTheSecondCandidateBranchIsBasedOnMaster is why the first item's encoding
// is in the second item's build. Decomposition has the implementation role commit the
// candidate branch with no base, and that is the case for every candidate decomposed
// before the first release: the first branch is one commit deep with no ancestor,
// and the second is based on master, so what the first item merged is in the tree
// the second one starts from.
func TestTheSecondCandidateBranchIsBasedOnMaster(t *testing.T) {
	_, d, first, second := twoRunsOnOneService(t, approvals, approvals)

	if _, err := git(theRepo(d), "merge-base", "--is-ancestor", "master", second.branch); err != nil {
		t.Errorf("master is not an ancestor of %s, and every candidate after the first release is based on master: %v",
			second.branch, err)
	}
	depth, err := git(theRepo(d), "rev-list", "--count", first.branch)
	if err != nil {
		t.Fatalf("counting the first branch's commits: %v", err)
	}
	if depth != "1" {
		t.Errorf("%s is %s commits deep, the first item's branch is committed with no base", first.branch, depth)
	}
}
