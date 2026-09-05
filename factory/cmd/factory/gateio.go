package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
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

// settle closes one firing: the factory's own verdict where the firing put no
// human at the row, and the human's typed verdict where it did. What may be
// typed is what the row offers, which differs per row — the merge row rejects,
// the production deploy row holds, and the candidate deploy row does both.
func (p *path) settle(ctx context.Context, opened gate.Opened) (gate.Verdict, string, decisionlog.Row, error) {
	if !opened.HumanDecides {
		closing, err := p.gate.AutoPass(ctx, opened)
		if err != nil {
			return "", "", decisionlog.Row{}, err
		}
		by := "the threshold"
		if opened.HeldOut && opened.Assessment.Number >= opened.Applied.Threshold {
			by = "the score's held-out sample"
		}
		fmt.Fprintf(p.d.out, "Auto-passed by %s; close event %s written as the gate component\n", by, closing.ID)
		return gate.VerdictApprove, "", closing, nil
	}

	actions, err := gate.Actions(opened.Gate)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	fmt.Fprintf(p.d.out, "Verdict (%s): ", strings.Join(words(actions), ", "))
	line, err := readLine(p.lines)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	verdict, feedback, err := typed(line, actions)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	// An approve names the set of holds it goes through, each one, because a bare
	// approve while a hold stands is what the gate refuses. The set is the one
	// the firing found; the gate recomputes it at the close and refuses an
	// approve that names a hold no longer standing or leaves out one that is.
	given := gate.Given{Actor: p.human, Verdict: verdict, Reason: feedback}
	if verdict == gate.VerdictApprove {
		given.Holds = opened.Holds
	}
	closing, err := p.gate.Decide(ctx, opened, given)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	fmt.Fprintf(p.d.out, "The verdict is %s; close event %s written as %s %s\n",
		verdict, closing.ID, closing.Actor.Kind, p.d.human)
	return verdict, feedback, closing, nil
}

// typed reads a verdict the human typed. A reject carries its feedback after the
// word, which is what the action is: reject with feedback.
func typed(line string, actions []gate.Verdict) (gate.Verdict, string, error) {
	for _, action := range actions {
		rest, matched := strings.CutPrefix(line, string(action))
		if !matched {
			continue
		}
		return action, strings.TrimSpace(rest), nil
	}
	return "", "", fmt.Errorf("factory: the verdict is one of %s, not %q", strings.Join(words(actions), ", "), line)
}

// words is the actions as they are offered on the terminal, the reject carrying
// the feedback its action is named for.
func words(actions []gate.Verdict) []string {
	offered := make([]string, 0, len(actions))
	for _, a := range actions {
		if a == gate.VerdictReject {
			offered = append(offered, "reject <feedback>")
			continue
		}
		offered = append(offered, string(a))
	}
	return offered
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

// readLine is the next line the human typed, without its line ending and
// surrounding blank space.
func readLine(lines *bufio.Scanner) (string, error) {
	if !lines.Scan() {
		if err := lines.Err(); err != nil {
			return "", fmt.Errorf("factory: reading the human's input: %w", err)
		}
		return "", errors.New("factory: the human's input ended before the path was done asking")
	}
	return strings.TrimSpace(lines.Text()), nil
}
