package score

import (
	"fmt"
	"testing"

	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// heldOutFiring is one held-out firing on a set, with the vector it was decided
// on and the exit its release's window then took.
func heldOutFiring(itemID string, set FactorSet, number float64, author string, vector []Factor) Firing {
	return Firing{
		OpenEvent: OpenEvent{
			ItemID: itemID, Gate: "merge_to_master", FactorSet: set, Number: number,
			HeldOut: true, HeldOutRate: StartingHeldOutSampleRate, AuthorKey: author, Vector: vector,
		},
		CloseEvent: CloseEvent{Verdict: VerdictApproved, WhyItAutoPassed: AutoPassSample},
		At:         "2026-08-20T00:00:00Z",
	}
}

// heldOutEvidence is a graph of held-out firings whose releases' windows closed
// at the exits given, which is the only population the bands, the prior's own
// drift reading and the fit are computed over.
func heldOutEvidence(firings []Firing, exits []window.Exit) *Evidence {
	e := newEvidence()
	e.firings = firings
	for i, f := range firings {
		releaseID := "rel_" + f.OpenEvent.ItemID
		e.items = append(e.items, item.Item{ID: f.OpenEvent.ItemID, ServiceID: "svc_a", Stage: item.StageMerged})
		e.releases = append(e.releases, release.Release{
			ID: releaseID, ItemID: f.OpenEvent.ItemID, ServiceID: "svc_a", Number: int64(i + 1),
		})
		e.windows = append(e.windows, window.Window{
			ID: "win_" + f.OpenEvent.ItemID, ServiceID: "svc_a", ReleaseID: releaseID, Exit: exits[i],
			At: "2026-08-20T00:00:00Z", ClosedAt: "2026-08-20T01:00:00Z",
		})
	}
	e.index()
	return e
}

// TestTheBandsArePublishedPerSetAndPerServiceWithTheirCounts is the one reading
// over the number rather than over a factor: whether the number ranks anything
// at all. Bands whose failure shares do not rise with the number say the formula
// is worth nothing rather than mis-weighted.
func TestTheBandsArePublishedPerSetAndPerServiceWithTheirCounts(t *testing.T) {
	e := heldOutEvidence([]Firing{
		heldOutFiring("it_a", SetWithABuild, 0.15, "model-1", nil),
		heldOutFiring("it_b", SetWithABuild, 0.15, "model-1", nil),
		heldOutFiring("it_c", SetWithABuild, 0.85, "model-1", nil),
	}, []window.Exit{window.ExitPassed, window.ExitFailed, window.ExitFailed})

	bands := e.bands(ShippedBandWidth)
	low, high, factoryWide := 0, 0, 0
	for _, b := range bands {
		if b.Service == "" {
			factoryWide++
		}
		if b.Service != "svc_a" {
			continue
		}
		switch {
		case near(b.From, 0.1):
			low = b.Windows
			if !near(b.FailedShare, 0.5) {
				t.Errorf("the 0.1 band's failure share reads %v, want half of two", b.FailedShare)
			}
		case near(b.From, 0.8):
			high = b.Windows
			if !near(b.FailedShare, 1) {
				t.Errorf("the 0.8 band's failure share reads %v, want one of one", b.FailedShare)
			}
		}
	}
	if low != 2 || high != 1 {
		t.Errorf("the bands hold %d and %d windows, want two and one", low, high)
	}
	if factoryWide == 0 {
		t.Error("the pass published no factory-wide band")
	}

	// A window that timed out is in no band: the exit that reports no outcome
	// reports none here either.
	quiet := heldOutEvidence([]Firing{heldOutFiring("it_a", SetWithABuild, 0.15, "model-1", nil)},
		[]window.Exit{window.ExitTimedOut})
	if len(quiet.bands(ShippedBandWidth)) != 0 {
		t.Errorf("a held-out release whose window timed out was published in %d band(s)", len(quiet.bands(ShippedBandWidth)))
	}
}

// TestAFactorWhoseDistributionMovedIsFoundDrifted: a factor whose distribution
// has moved is measuring something other than what the formula was calibrated
// on, and it takes the treatment an unavailable factor takes.
func TestAFactorWhoseDistributionMovedIsFoundDrifted(t *testing.T) {
	var firings []Firing
	for i := range 16 {
		level := 0.1
		if i >= 8 {
			level = 0.9
		}
		firings = append(firings, Firing{
			OpenEvent: OpenEvent{
				ItemID: fmt.Sprintf("it_%d", i), Gate: "merge_to_master", FactorSet: SetWithABuild,
				Vector: []Factor{{Name: changeSize.name, Term: TermLikelihood, Level: level}},
			},
			CloseEvent: CloseEvent{Verdict: VerdictApproved, WhyItAutoPassed: AutoPassThreshold},
			At:         fmt.Sprintf("2026-08-20T%02d:00:00Z", i),
		})
	}
	e := newEvidence()
	e.firings = firings
	e.index()

	drifted := e.drift("")
	if len(drifted) != 1 || drifted[0].Factor != changeSize.name {
		t.Fatalf("the pass found %+v drifted, want %s", drifted, changeSize.name)
	}
	version := Version{Drift: drifted}
	if !version.Drifted(changeSize.name) {
		t.Error("a version carrying the drift does not report the factor drifted")
	}

	// The same sixteen decisions over four items are four readings and not
	// sixteen: every row over one item weighs one vector, so the pass reads
	// nothing.
	fourItems := newEvidence()
	for i, f := range firings {
		f.OpenEvent.ItemID = fmt.Sprintf("it_%d", i/4)
		fourItems.firings = append(fourItems.firings, f)
	}
	fourItems.index()
	if found := fourItems.drift(""); len(found) != 0 {
		t.Errorf("the pass found %+v drifted over four items, and sixteen decisions of four items are four readings", found)
	}

	// Two items read at an extreme among sixteen move the mean and not the
	// median, and a factor is drifted on a level that moved and stayed.
	outliers := newEvidence()
	for i, f := range firings {
		level := 0.4
		if i < 2 {
			level = 1.0
		}
		f.OpenEvent.Vector = []Factor{{Name: changeSize.name, Term: TermLikelihood, Level: level}}
		outliers.firings = append(outliers.firings, f)
	}
	outliers.index()
	if found := outliers.drift(""); len(found) != 0 {
		t.Errorf("the pass found %+v drifted on two items read at an extreme, and a shift is one that stayed", found)
	}

	// A factor that held still is not drifted, and the prior is exempt from this
	// reading: its distribution moving is what it working looks like.
	steady := newEvidence()
	for _, f := range firings {
		f.OpenEvent.Vector = []Factor{
			{Name: changeSize.name, Term: TermLikelihood, Level: 0.4},
			{Name: authorPrior.name, Term: TermLikelihood, Level: f.OpenEvent.Vector[0].Level},
		}
		steady.firings = append(steady.firings, f)
	}
	steady.index()
	if found := steady.drift(""); len(found) != 0 {
		t.Errorf("the pass found %+v drifted, and neither a steady factor nor the prior is", found)
	}
}

// TestAPriorThatNoLongerSeparatesIsFoundDrifted: the prior's own reading is the
// one a learned value can fail — the prior each held-out decision was taken on,
// against what that release's window then closed.
func TestAPriorThatNoLongerSeparatesIsFoundDrifted(t *testing.T) {
	prior := func(level float64) []Factor {
		return []Factor{{Name: authorPrior.name, Term: TermLikelihood, Level: level}}
	}
	// The prior reads low on every release whose window failed and high on every
	// one that passed, which is the wrong way round.
	e := heldOutEvidence([]Firing{
		heldOutFiring("it_a", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_b", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_c", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_d", SetWithABuild, 0.2, "model-1", prior(0.9)),
		heldOutFiring("it_e", SetWithABuild, 0.2, "model-1", prior(0.9)),
		heldOutFiring("it_f", SetWithABuild, 0.2, "model-1", prior(0.9)),
	}, []window.Exit{
		window.ExitFailed, window.ExitFailed, window.ExitFailed,
		window.ExitPassed, window.ExitPassed, window.ExitPassed,
	})

	drifted := e.drift("")
	if len(drifted) != 1 || drifted[0].Author != "model-1" {
		t.Fatalf("the pass found %+v drifted, want the prior on model-1", drifted)
	}
	if !(Version{Drift: drifted}).PriorDrifted("model-1") {
		t.Error("a version carrying the drift does not report the prior drifted")
	}

	// A truncation that removed every held-out decision on the author leaves no
	// reading, so the prior restarts as an unseen author's rather than standing
	// drifted on evidence that can no longer arrive.
	truncated := newEvidence()
	truncated.index()
	if found := truncated.drift(""); len(found) != 0 {
		t.Errorf("a log with no held-out decision on the author still reports %+v", found)
	}
}

// TestTheWeightsAreFittedPerSetAndFallBackToWhatTheProductShipped: a set whose
// held-out decisions are too few to fit keeps the shipped weights, and the
// counts on its bands say so.
func TestTheWeightsAreFittedPerSetAndFallBackToWhatTheProductShipped(t *testing.T) {
	thin := heldOutEvidence([]Firing{
		heldOutFiring("it_a", SetWithABuild, 0.2, "model-1", []Factor{{Name: changeSize.name, Term: TermLikelihood, Level: 0.9}}),
	}, []window.Exit{window.ExitFailed})
	if got := Fit(thin)[SetWithABuild].Text(); got != ShippedWeights(SetWithABuild).Text() {
		t.Errorf("one held-out decision refitted the weights to %q", got)
	}

	var firings []Firing
	var exits []window.Exit
	for i := range fitEvidence {
		failed := i%2 == 0
		size, churn := 0.2, 0.5
		if failed {
			size = 0.9
		}
		firings = append(firings, heldOutFiring(fmt.Sprintf("it_%d", i), SetWithABuild, 0.5, "model-1", []Factor{
			{Name: changeSize.name, Term: TermLikelihood, Level: size},
			{Name: changeAreaChurn.name, Term: TermLikelihood, Level: churn},
		}))
		if failed {
			exits = append(exits, window.ExitFailed)
		} else {
			exits = append(exits, window.ExitPassed)
		}
	}
	fitted := Fit(heldOutEvidence(firings, exits))[SetWithABuild]
	if fitted.Of(changeSize.name) <= fitted.Of(changeAreaChurn.name) {
		t.Errorf("the fit gave the separating factor %v and the flat one %v",
			fitted.Of(changeSize.name), fitted.Of(changeAreaChurn.name))
	}
	if !near(fitted.Of(changeAreaChurn.name), 0) {
		t.Errorf("a factor that separates nothing was given a weight of %v", fitted.Of(changeAreaChurn.name))
	}
}

// TestTheNumberIsFittedToTheShareOfHeldOutWindowsThatFailed: the weights are
// the fit's shape and the scale is the rest of it. Every term of the formula is
// a weighted mean, so the weights decide which change ranks above which and
// nothing about where that ranking sits against a failure share — and the one
// scale the three sets share is the number estimating the share of held-out
// windows that failed among decisions taken at it.
func TestTheNumberIsFittedToTheShareOfHeldOutWindowsThatFailed(t *testing.T) {
	// Ten held-out decisions on one set: the low half passed and the high half
	// failed, so half of them failed and the number ranks them.
	var firings []Firing
	var exits []window.Exit
	for i := range fitEvidence {
		failed := i >= fitEvidence/2
		level := 0.2
		if failed {
			level = 0.8
		}
		firings = append(firings, heldOutFiring(fmt.Sprintf("it_%d", i), SetWithABuild, 0.5, "model-1", []Factor{
			{Name: changeSize.name, Group: GroupChange, Term: TermLikelihood, Level: level},
			{Name: changeReach.name, Group: GroupChange, Term: TermImpact, Level: level},
			{Name: changeReversibility.name, Group: GroupChange, Term: TermReversibility, Level: 1},
		}))
		if failed {
			exits = append(exits, window.ExitFailed)
		} else {
			exits = append(exits, window.ExitPassed)
		}
	}
	e := heldOutEvidence(firings, exits)
	weights := Fit(e)
	scale := FitScale(e, weights)[SetWithABuild]
	if !scale.Fitted() {
		t.Fatalf("ten held-out decisions the number ranked fitted no scale: %+v", scale)
	}

	// The two populations' own numbers, under the fitted weights, now read as
	// the share of each that failed: none of the low ones and all of the high
	// ones. Unscaled they are two numbers on the formula's own range, which is
	// the scale the threshold would otherwise be read against.
	low := numberUnder(weights[SetWithABuild], scale, 0.2)
	high := numberUnder(weights[SetWithABuild], scale, 0.8)
	if !near(low, 0) || !near(high, 1) {
		t.Errorf("the fitted number reads %v where nothing failed and %v where everything did, want 0 and 1", low, high)
	}
	if raw := numberUnder(weights[SetWithABuild], Scale{}, 0.8); near(raw, high) {
		t.Error("the scale moved nothing, and the weighted means alone cannot reach the failure share")
	}

	// A set with too few held-out decisions keeps the identity, and so does one
	// whose number did not rank the failures at all: a scale fitted through a
	// number that ranked nothing would move every number in the factory on
	// nothing.
	thin := heldOutEvidence(firings[:1], exits[:1])
	if got := FitScale(thin, weights)[SetWithABuild]; got.Fitted() {
		t.Errorf("one held-out decision fitted a scale of %+v", got)
	}
	var flatFirings []Firing
	for i := range fitEvidence {
		flatFirings = append(flatFirings, heldOutFiring(fmt.Sprintf("it_%d", i), SetWithABuild, 0.5, "model-1", []Factor{
			{Name: changeSize.name, Group: GroupChange, Term: TermLikelihood, Level: 0.5},
			{Name: changeReach.name, Group: GroupChange, Term: TermImpact, Level: 0.5},
			{Name: changeReversibility.name, Group: GroupChange, Term: TermReversibility, Level: 1},
		}))
	}
	if got := FitScale(heldOutEvidence(flatFirings, exits), weights)[SetWithABuild]; got.Fitted() {
		t.Errorf("a number that ranked nothing fitted a scale of %+v", got)
	}
}

// numberUnder is what the formula returns for a vector reading one level on
// every factor, under one set's weights and one scale.
func numberUnder(weights Weights, scale Scale, level float64) float64 {
	vector := []Factor{
		{Name: changeSize.name, Group: GroupChange, Term: TermLikelihood,
			Weight: weights.Of(changeSize.name), Level: level},
		{Name: changeReach.name, Group: GroupChange, Term: TermImpact,
			Weight: weights.Of(changeReach.name), Level: level},
		{Name: changeReversibility.name, Group: GroupChange, Term: TermReversibility,
			Weight: weights.Of(changeReversibility.name), Level: 1},
	}
	_, _, _, number := reduce(vector, scale)
	return number
}

// TestARecalibrationIsTheExitFromADriftedPrior: a prior standing drifted stops
// the sample selecting on that author, so no newer held-out decision about it
// can arrive, and a reading taken over every decision would find the same drift
// at every pass afterwards — the resolution would never end. The readings are
// taken over the decisions closed after the newest one the last recalibration
// read, and that is what makes the recalibration the exit the design names.
func TestARecalibrationIsTheExitFromADriftedPrior(t *testing.T) {
	prior := func(level float64) []Factor {
		return []Factor{{Name: authorPrior.name, Term: TermLikelihood, Level: level}}
	}
	e := heldOutEvidence([]Firing{
		heldOutFiring("it_a", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_b", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_c", SetWithABuild, 0.2, "model-1", prior(0.1)),
		heldOutFiring("it_d", SetWithABuild, 0.2, "model-1", prior(0.9)),
		heldOutFiring("it_e", SetWithABuild, 0.2, "model-1", prior(0.9)),
		heldOutFiring("it_f", SetWithABuild, 0.2, "model-1", prior(0.9)),
	}, []window.Exit{
		window.ExitFailed, window.ExitFailed, window.ExitFailed,
		window.ExitPassed, window.ExitPassed, window.ExitPassed,
	})
	if found := e.drift(""); len(found) != 1 {
		t.Fatalf("the pass found %+v, want the prior on model-1 drifted", found)
	}
	read := e.newestClose()
	if read == "" {
		t.Fatal("the evidence names no decision for a recalibration to have read to")
	}
	if found := e.drift(read); len(found) != 0 {
		t.Errorf("the same decisions read again after a recalibration found %+v, and a recalibration is the exit", found)
	}
}
