package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
)

// fired is one gate firing as the path saw it. Every fact in it is on the opening
// row too; they are here for the end-to-end test to assert over without parsing a
// payload.
type fired struct {
	opening      string
	closing      string
	humanDecided bool
	// marks is what put a human at the row, in the order gate.Marks lists them,
	// and empty where none is. mismatch is what the drift detector found
	// disagreeing, which puts a human there without being a mark.
	marks         []gate.Mark
	mismatch      string
	number        float64
	threshold     float64
	thresholdFrom string
	scoreVersion  string
	policyVersion string
	safeguards    []string
	// heldOut is whether the score's own sample selected this item, and whyHeldOut
	// which of the two ways. Both are on the open event too; a row that reads held
	// out with the number under the threshold is an item selected at an earlier gate.
	heldOut    bool
	whyHeldOut string
	// row is which gate row fired, so a test asserting over three firings can say
	// which one it means.
	row gate.Row
}

// report prints one firing as a human at the row would read it: the number
// beside the threshold it was compared against and where that threshold came
// from, every factor with the quantity it was read from, every unavailable
// factor with its reason, and every criterion result. It prints the same lines
// whether or not a human decides, because an auto-pass an owner cannot read is
// an auto-pass they cannot argue with.
//
// The results are empty at the candidate deploy row, where the run that decides
// them is what the deploy is for, and the line that says so is printed instead —
// a row that showed nothing about the criteria would read the same as a row where
// they all passed.
func report(out io.Writer, opened gate.Opened, results []gate.CriterionResult) {
	a, applied := opened.Assessment, opened.Applied
	fmt.Fprintf(out, "Gate %s fired; decision %s\n", opened.Gate, opened.Row.ID)
	fmt.Fprintf(out, "  number %.3f against threshold %.3f (%s), likelihood %.3f, impact %.3f discounted to %.3f\n",
		a.Number, applied.Threshold, applied.ThresholdFrom, a.Likelihood, a.Impact, a.DiscountedImpact)
	fmt.Fprintf(out, "  factor set %s, score version %s (formula %s), policy version %s\n",
		a.FactorSet, a.Version, a.FormulaVersion, applied.PolicyVersion)
	for _, f := range a.Vector {
		if f.Resolved != "" {
			fmt.Fprintf(out, "  factor %s: resolved rather than valued — %s\n", f.Name, f.Resolved)
			continue
		}
		fmt.Fprintf(out, "  factor %s: %.2f (%s, weight %.2f) — %s\n",
			f.Name, f.Level, f.Term, f.Weight, f.Reading)
	}
	if len(results) == 0 && opened.Gate == gate.DeployToCandidateEnvironment {
		fmt.Fprintln(out, "  no criterion is decided yet: this deploy is what the run that decides them happens on")
	}
	for _, r := range results {
		fmt.Fprintf(out, "  criterion %s: %s\n", r.CriterionID, r.Outcome)
	}
	if applied.Supplied.Why != "" {
		fmt.Fprintf(out, "  the score supplies that threshold: %s\n", applied.Supplied.Why)
	}
	for _, id := range applied.Safeguards {
		fmt.Fprintf(out, "  safeguard %s applies here\n", id)
	}
	if opened.HeldOut {
		fmt.Fprintf(out, "  held out: %s\n", opened.WhyHeldOut)
	}
	if opened.Mismatch != "" {
		fmt.Fprintf(out, "  the drift detector found a record disagreeing with what runs: %s\n", opened.Mismatch)
	}
	if opened.HumanDecides {
		fmt.Fprintf(out, "  a human decides: %s; the row waits on %s\n",
			whyHumanDecides(opened), waitedOn(opened.WaitsOn))
		return
	}
	if opened.HeldOut && a.Number >= applied.Threshold {
		fmt.Fprintln(out, "  no human decides: the score held this item out of a gate it would have gated, which is the one thing in the factory that removes a human from a row")
		return
	}
	fmt.Fprintln(out, "  no human decides: the number is under the threshold and no safeguard adds one")
}

// whyHumanDecides is what put a human at the row, in words: every mark the
// firing carried, and the two conditions that put one there without being a
// mark — a mismatch the drift detector found, and a derivation that could not
// derive. A row with a human at it and nothing to say would read as a row
// nobody has to decide.
func whyHumanDecides(opened gate.Opened) string {
	reasons := make([]string, 0, len(opened.Marks)+1)
	for _, m := range opened.Marks {
		reasons = append(reasons, string(m))
	}
	if opened.Mismatch != "" {
		reasons = append(reasons, gate.HoldDriftMismatch)
	}
	if len(reasons) == 0 {
		return "the firing read something it could not value"
	}
	return strings.Join(reasons, "; ")
}

// waitedOn is who the row waits on, as the open event says it: the duty, the
// named human a record's routing gives, the holders the People declaration
// recorded, and the owner where nobody holds it.
func waitedOn(w gate.Waits) string {
	switch {
	case w.Human != "":
		return w.Human
	case len(w.Holders) > 0:
		return fmt.Sprintf("duty %d, held by %s", w.Duty, strings.Join(w.Holders, ", "))
	case w.Duty != 0:
		return fmt.Sprintf("duty %d, which nobody holds, so it widens to the owner", w.Duty)
	default:
		return "the owner, this row naming no duty"
	}
}

// settled is what one firing came to on this pass: the verdict where the pass
// itself gave one, the reason a rejection carries, the close event, and waiting
// where the firing put a human at the row.
//
// Waiting carries no verdict and no close event. Nothing here decides a row a
// human decides: the row is pending in Work, the item stops at it, and the next
// pass reads the verdict a human left through the gate component and continues
// from there.
type settled struct {
	verdict gate.Verdict
	reason  string
	closing decisionlog.Row
	waiting bool
}

// settle closes one firing where the factory decides it, and leaves it pending
// where a human does.
//
// The factory's own verdict is the auto-pass, which is what a firing that put no
// human at the row closes with. Where the firing put one there, this writes
// nothing and says where the row waits: the verdict is given at Work, through
// [gate.Gate.Decide], [gate.Gate.Refer], [gate.Gate.EditInPlace] and
// [gate.Gate.Acknowledge], by a caller this pass does not have.
//
// A firing that pages sends it here, on the row this call is about, so that the
// page goes out on the same pass that leaves the row waiting.
func (p *path) settle(ctx context.Context, opened gate.Opened) (settled, error) {
	if err := p.pagedFiring(ctx, opened); err != nil {
		return settled{}, err
	}
	if !opened.HumanDecides {
		closing, err := p.gate.AutoPass(ctx, opened)
		if err != nil {
			return settled{}, err
		}
		by := "the threshold"
		if opened.HeldOut && opened.Assessment.Number >= opened.Applied.Threshold {
			by = "the score's held-out sample"
		}
		fmt.Fprintf(p.d.out, "Auto-passed by %s; close event %s written as the gate component\n", by, closing.ID)
		return settled{verdict: gate.VerdictApprove, closing: closing}, nil
	}
	p.reportWaiting(opened)
	return settled{waiting: true}, nil
}

// reportWaiting is what the pass says where it asked for a verdict before: the
// row is in Work, on whoever it waits on, and the pass goes on to the next item
// rather than stopping.
func (p *path) reportWaiting(opened gate.Opened) {
	subject := opened.Subject.ItemID
	switch {
	case subject == "" && opened.Subject.IntentID != "":
		subject = opened.Subject.IntentID
	case subject == "" && opened.Subject.RecordID != "":
		subject = opened.Subject.RecordID
	case subject == "":
		subject = opened.ArtifactID
	}
	fmt.Fprintf(p.d.out, "Waiting in Work at %s on %s: row %s waits on %s\n",
		opened.Gate, subject, opened.Row.ID, waitedOn(opened.WaitsOn))
}

// editInPlace is the action a human takes at a document gate instead of
// rejecting: they author the version themselves. The text is the version, the
// artifact store writes it with the gate component as the authorship — the
// version's author is the human at the gate and its writer is still the store —
// and the row fires again over the new version, the one it supersedes
// abandoned.
//
// again is the firing the superseded row was fired with, which the re-firing
// needs so that the vector is recomputed over what is now under decision.
//
// It is refused at the Implementation row, which is the one artifact gate the
// design gives no Edit in place, and at every event gate, which the gate
// package refuses for itself.
func (p *path) editInPlace(ctx context.Context, opened gate.Opened, again gate.Firing,
	by record.Actor, text string) (settled, error) {
	if opened.Gate.Kind == gate.KindImplementation {
		return settled{}, fmt.Errorf("%w: a human does not author a build at the row that decides it",
			gate.ErrEditInPlaceRefused)
	}
	if !opened.Gate.ArtifactGate() {
		return settled{}, fmt.Errorf("%w: %s decides no document", gate.ErrEditInPlaceRefused, opened.Gate)
	}
	if strings.TrimSpace(text) == "" {
		return settled{}, errors.New("an edit in place authors a version, and this one is empty")
	}

	authored := p.authoredAtTheGate(by)
	var version artifact.Artifact
	var err error
	switch opened.Gate.Kind {
	case gate.KindSpec:
		// A human authoring at the gate read no manifest: context assembly
		// selects what an agent reads, and this version was typed.
		version, _, _, err = p.store.SubmitSpec(ctx, gate.Component(opened.Gate), authored,
			opened.Subject.ItemID, opened.Subject.ServiceID, text, nil, nil, nil, "")
	case gate.KindImplementationPlan:
		version, err = p.store.SubmitPlan(ctx, gate.Component(opened.Gate), authored, opened.Subject.ItemID, text, "")
	case gate.KindTasks:
		version, err = p.store.SubmitTasks(ctx, gate.Component(opened.Gate), authored, opened.Subject.ItemID, text, "")
	default:
		err = fmt.Errorf("%w: %s", gate.ErrEditInPlaceRefused, opened.Gate)
	}
	if err != nil {
		return settled{}, err
	}
	again.ArtifactID = version.ID
	reopened, err := p.gate.EditInPlace(ctx, opened, again)
	if err != nil {
		return settled{}, err
	}
	fmt.Fprintf(p.d.out, "Edited in place: version %s authored at the gate; row %s supersedes %s\n",
		version.ID, reopened.Row.ID, opened.Row.ID)
	report(p.d.out, reopened, nil)
	return p.settle(ctx, reopened)
}

// authoredAtTheGate is who a version a human typed at a gate row is recorded
// as: the gate component's authorship, and the per-person key of the human who
// typed it — the principal at the screen, or the human this terminal was
// started as. It is the key and never the name: every record of the graph names
// a key, and the mapping from key to name is kept outside the chain so an
// erasure can delete it alone.
func (p *path) authoredAtTheGate(by record.Actor) artifact.By {
	return artifact.By{Authorship: artifact.AuthorshipGate, Author: by.Key}
}

// recordFiring is one firing as the end-to-end test reads it. Every field is on
// the open event as well; this saves the test a payload to unmarshal.
func recordFiring(opened gate.Opened, closing decisionlog.Row) fired {
	return fired{
		opening:       opened.Row.ID,
		closing:       closing.ID,
		humanDecided:  opened.HumanDecides,
		marks:         opened.Marks,
		mismatch:      opened.Mismatch,
		number:        opened.Assessment.Number,
		threshold:     opened.Applied.Threshold,
		thresholdFrom: string(opened.Applied.ThresholdFrom),
		scoreVersion:  opened.Assessment.Version,
		policyVersion: opened.Applied.PolicyVersion,
		safeguards:    opened.Applied.Safeguards,
		heldOut:       opened.HeldOut,
		whyHeldOut:    opened.WhyHeldOut,
		row:           opened.Gate,
	}
}

// decideOrResume is one row's verdict: the row fired and settled where nothing
// has decided it over this subject, and the verdict already on it where the pass
// that fired it left the row for Work.
//
// It is what makes every row below the authoring stages resumable without a
// second copy of what a verdict causes: the verdict a human gave between two
// passes reaches the same code the factory's own auto-pass reached.
func (p *path) decideOrResume(ctx context.Context, f gate.Firing,
	c *candidate, results []gate.CriterionResult) (settled, gate.Opened, error) {
	subject := f.ArtifactID
	if subject == "" {
		subject = f.BuildID
	}
	if already, closed := c.rows[f.Row.Kind]; closed && already.subject() == subject {
		fmt.Fprintf(p.d.out, "%s of item %s was decided at Work as %s; row %s closed by %s %s\n",
			f.Row, c.itemID, already.verdict, already.opened.Row.ID,
			already.closing.Actor.Kind, already.closing.Actor.Key)
		p.moved = true
		return settled{verdict: already.verdict, reason: already.reason, closing: already.closing}, already.opened, nil
	}
	p.moved = true
	opened, err := p.gate.Fire(ctx, f)
	if err != nil {
		return settled{}, gate.Opened{}, err
	}
	report(p.d.out, opened, results)
	done, err := p.settle(ctx, opened)
	return done, opened, err
}

// firingFor is the firing one row was fired with, rebuilt from the row and the
// records. A refer and an Edit in place both end the row and fire another over
// what is now under decision, and the vector of that firing is recomputed over
// the same readings the first one was.
//
// It is the composition's answer and not the gate's: the readings the score
// needs — the build's diff, what the criteria produced, what the change reaches
// — are taken where the repository and the records are, which is here. A row
// that decides no item is rebuilt from the row alone, which is the whole of what
// the firing carried.
func (p *path) firingFor(ctx context.Context, opened gate.Opened) (gate.Firing, error) {
	f := gate.Firing{
		Row:                      opened.Gate,
		RecordID:                 opened.Subject.RecordID,
		ItemID:                   opened.Subject.ItemID,
		BuildID:                  opened.Subject.BuildID,
		ArtifactID:               opened.ArtifactID,
		ServiceID:                opened.Subject.ServiceID,
		AreaID:                   opened.Subject.AreaID,
		EnvironmentID:            opened.Subject.EnvironmentID,
		ReleaseID:                opened.Subject.ReleaseID,
		RevertWhileRollbackHolds: opened.RevertWhileRollbackHolds,
	}
	if f.ItemID == "" {
		return f, nil
	}
	c, err := p.rehydrate(ctx, f.ItemID)
	if err != nil {
		return gate.Firing{}, err
	}
	inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
	if err != nil {
		return gate.Firing{}, err
	}
	reached, err := p.exposureOf(ctx, f.BuildID)
	if err != nil {
		return gate.Firing{}, err
	}
	f.CriteriaInForce = len(inForce)
	f.Measurement = c.measurement
	f.Exposure = reached
	// The candidate deploy row names how many criteria there are and no
	// outcome: the run that decides them is what that deploy is for.
	if opened.Gate.Kind != gate.KindDeployToCandidateEnvironment {
		f.Criteria = c.criteria
	}
	return f, nil
}
