package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/window"
)

// The milestone's demonstration through the crude interface: an item held out of a
// gate the score would have gated, and a supplied value moving because the
// outcomes of an earlier run moved it. What the rules are is score's own
// demonstration; this is that the path obeys them.

// alwaysDraw selects every firing the score would have gated. A run composed with
// the runtime's own generator holds one firing in ten out, which a test could not
// assert either way.
type alwaysDraw struct{}

func (alwaysDraw) Fraction() float64 { return 0 }

// TestTheScoreHoldsAnItemOutOfTheGateItWouldHaveGated is the sample end to end: no
// human at a row the number gated, the close event saying the sample and not the
// threshold, and the window over its release running to the cap because the
// factory is measuring what it guessed at.
func TestTheScoreHoldsAnItemOutOfTheGateItWouldHaveGated(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer)
	d.draw = alwaysDraw{}

	res, err := run(ctx, d, of(theStatement))
	if err != nil {
		t.Fatalf("the run stopped, and a held-out item needs no verdict typed: %v\noutput so far:\n%s", err, out)
	}
	c := only(t, res)

	// Every row of the item is held out, and the row that put no human there is the
	// one the number would have gated.
	gated := 0
	for _, fired := range []fired{c.candidateGate, c.mergeGate, c.deployGate} {
		if !fired.heldOut {
			t.Errorf("the %s row does not say the item is held out", fired.row)
			continue
		}
		if fired.humanDecided {
			t.Errorf("a human decided the %s row of a held-out item", fired.row)
		}
		if fired.number >= fired.threshold {
			gated++
		}
	}
	if gated == 0 {
		t.Fatalf("no row of this item read over its threshold, so nothing was held out of anything:\n%s", out)
	}

	// The first row was selected here and the ones below it were selected earlier:
	// the sample selects an item and not a firing.
	if c.candidateGate.whyHeldOut != score.SelectedHere {
		t.Errorf("the first row reads held out because %q", c.candidateGate.whyHeldOut)
	}
	if c.mergeGate.whyHeldOut != score.SelectedEarlier {
		t.Errorf("the merge row reads held out because %q, want the earlier selection", c.mergeGate.whyHeldOut)
	}
	if !strings.Contains(out.String(), "Auto-passed by the score's held-out sample") {
		t.Errorf("no row reports the sample as what passed it:\n%s", out)
	}

	// The window over its release runs to the cap: auto-passing a change the score
	// wanted gated is where the factory is most openly guessing, so it takes the
	// longest watch available.
	if c.windowID == "" {
		t.Fatalf("no window opened over the release of a held-out item:\n%s", out)
	}
	w, err := window.Get(ctx, d.pool, c.windowID)
	if err != nil {
		t.Fatalf("reading the window: %v", err)
	}
	if !w.HeldOut {
		t.Error("the window does not say the release was held out")
	}
	if w.PassedAvailable {
		t.Error("the passed exit is available to a held-out release's window, and the sample runs it to the cap")
	}

	// And the selection is readable off the decisions, which is where the design
	// puts it rather than in a set of its own.
	held, err := score.HeldOutItems(ctx, d.pool, d.token)
	if err != nil {
		t.Fatalf("HeldOutItems: %v", err)
	}
	if len(held) != 1 || held[0] != c.itemID {
		t.Errorf("the log says the score held out %v, want the one item %s", held, c.itemID)
	}
}

// TestTheThresholdFallsAfterTheFactoryPassedSomethingThatWentWrong is the
// milestone's own demonstration through this interface: the episode M4 drives — a
// change auto-passed on the number and failed by its window — lowers the
// threshold the score supplies at the rows that passed it, so the run after it is
// decided by a human where the run before it was not.
//
// What makes the movement readable is the version each decision names: the
// decisions of the first run name the version that supplied the old threshold, the
// decisions of the next name the one that supplies the new, and the new version
// names the one it superseded.
func TestTheThresholdFallsAfterTheFactoryPassedSomethingThatWentWrong(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	rollBackABadRelease(ctx, t, d, out)

	before, found, err := score.Newest(ctx, d.pool)
	if err != nil || !found {
		t.Fatalf("Newest = found %v, %v", found, err)
	}
	learned, err := score.Learn(ctx, d.pool, d.token)
	if err != nil {
		t.Fatalf("Learn: %v", err)
	}

	// The row that auto-passed the change the window failed now supplies a
	// threshold below the number it passed it at.
	start, _ := score.Starting(gatepolicy.RiskThreshold)
	moved := 0
	for _, row := range gate.Rows {
		threshold, ok := learned.Value(gatepolicy.RiskThreshold, string(row))
		if !ok {
			t.Fatalf("the score supplies no threshold for %s", row)
		}
		if !threshold.Moved() {
			continue
		}
		moved++
		if threshold.Value >= start.Value {
			t.Errorf("the threshold at %s moved to %v, which is not below the starting %v",
				row, threshold.Value, start.Value)
		}
		if !strings.Contains(threshold.Why, "turned out badly") {
			t.Errorf("the moved threshold at %s carries the evidence %q", row, threshold.Why)
		}
	}
	if moved == 0 {
		t.Fatalf("no row's threshold moved after a change the factory passed was failed:\n%s", out)
	}

	// The next run is decided under a version of its own, which names the one the
	// rolled-back episode was decided under.
	// No interview rounds, so nothing consumes an answer and every line is a
	// verdict — and a verdict is now needed at rows that auto-passed before the
	// threshold fell.
	d.in = strings.NewReader(approvals)
	d.model = interviewed(0)
	res, err := run(ctx, d, of(theThirdStatement))
	if err != nil {
		t.Fatalf("the run after the rollback stopped: %v\noutput so far:\n%s", err, out)
	}
	after := only(t, res)
	if after.mergeGate.scoreVersion == before.ID {
		t.Error("the run after the rollback was decided under the version the rollback taught, unmoved")
	}
	appended, err := score.Get(ctx, d.pool, after.mergeGate.scoreVersion)
	if err != nil {
		t.Fatalf("reading the version the run named: %v", err)
	}
	if appended.Supersedes != before.ID {
		t.Errorf("the version in force supersedes %q, want %q", appended.Supersedes, before.ID)
	}
	if was, err := score.Get(ctx, d.pool, before.ID); err != nil {
		t.Fatalf("reading the superseded version: %v", err)
	} else if k, _ := was.Value(gatepolicy.RiskThreshold, string(gate.MergeToMaster)); k.Moved() {
		t.Error("the superseded version now says a threshold moved, and an append-only record does not change")
	}

	// And the learning is a pass and not a step: two ensures with nothing written
	// between them append one version. It is asserted here and not against the
	// version the run composed, because the run shipped an item after composing —
	// so the graph moved under it and a version of its own is the right answer.
	first, err := score.NewWriter(d.pool, d.token).Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	still, err := score.NewWriter(d.pool, d.token).Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if still.ID != first.ID {
		t.Errorf("a second pass over an unchanged store appended %s beside %s", still.ID, first.ID)
	}
	if first.Supersedes == "" {
		t.Error("the pass after the run appended a version superseding nothing")
	}
}

// TestThePassPrintsWhatMovedAndWhatMovedIt: the learn command's whole reason for
// being a command of its own. A run appends the version and prints two lines about
// it; this prints the table, the movement, and the evidence.
func TestThePassPrintsWhatMovedAndWhatMovedIt(t *testing.T) {
	inForce := score.Version{Supplied: score.StartingValues()}
	moved := append(score.StartingValues(), score.Supplied{
		Parameter: gatepolicy.WindowLimit, Subject: "svc_a", Value: 2,
		Why: "3 window(s) of this service closed without failing a release and 0 rollback(s) swept a release, folded in order",
	})

	out := &bytes.Buffer{}
	printSupplied(out, moved, inForce)
	printed := out.String()
	if !strings.Contains(printed, "window_limit on svc_a = 2 — moved from 1") {
		t.Errorf("the pass does not print the movement:\n%s", printed)
	}
	if !strings.Contains(printed, "closed without failing a release") {
		t.Errorf("the pass does not print what moved it:\n%s", printed)
	}

	// And a value that has moved back is a movement a reader would otherwise never
	// see, because the learned table holds no row for it at all.
	out = &bytes.Buffer{}
	printSupplied(out, score.StartingValues(), score.Version{Supplied: moved})
	printed = out.String()
	if !strings.Contains(printed, "window_limit on svc_a = 1 — moved from 2") {
		t.Errorf("the pass does not print a value that moved back:\n%s", printed)
	}
	if !strings.Contains(printed, "the outcomes no longer move it") {
		t.Errorf("the pass does not say why it moved back:\n%s", printed)
	}
}

// TestAFactoryThatHasSampledNothingSaysSo: a threshold that can only fall is a
// state worth reading off the pass rather than deducing from an empty list. Every
// test in this package but one composes a draw that selects nothing, so it is also
// the state most of them are in.
func TestAFactoryThatHasSampledNothingSaysSo(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the run stopped: %v\noutput so far:\n%s", err, out)
	}

	printed := &bytes.Buffer{}
	if err := printHeldOut(ctx, printed, d.pool, d.token); err != nil {
		t.Fatalf("printHeldOut: %v", err)
	}
	if !strings.Contains(printed.String(), "can fall and cannot rise") {
		t.Errorf("the pass does not say that a factory with no sample has a one-way threshold:\n%s", printed)
	}
}
