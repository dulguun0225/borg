package boundary

import "math"

// Crossing is where the log of the ratio crosses, in either direction: the log
// of the comparison count over one-minus-confidence. It is published rather than
// inlined because it is the whole of what the confidence means here.
//
// The count is in it because the confidence is held over the set and not per
// reading. A crossing on any one comparison starts the same rollback, so reading
// each of them at the authored confidence would roll a good release back about
// as many times as there are comparisons, and the figure would move every time a
// target or an operation was added without anything the owner wrote changing.
// The power takes no such adjustment and is held per quantity, which is why it
// is not here.
func (b Boundary) Crossing() float64 {
	return math.Log(float64(b.Comparisons) / (1 - b.Confidence))
}

// Evaluate reads the quantity against the boundary. It reaches no state and
// remembers nothing between calls: the guarantee is over the sequence of reads
// and needs no memory of them.
//
// The construction is a sequential likelihood ratio between two means of the
// per-interval difference between the arms — no change, and a change of the size
// — with the spread between intervals as the scale. The unit is the interval, so
// the effective sample is the count of intervals and a burst of traffic inside
// one interval buys nothing the next interval does not. The spread between
// intervals is what absorbs the correlation a platform routing by user, session
// or source puts between requests: two requests from one session are one
// interval's business.
//
// The spread is estimated and then read as known, and it is floored by the
// sampling variance the counts themselves imply. That floor is the second bound
// the design states: the volume sets the size a rate can be read at within an
// interval, both bounds stand, and the stricter governs — which is what the
// floor does, since a larger spread needs more intervals.
func (b Boundary) Evaluate(o Observed) (Reading, error) {
	if err := b.Validate(); err != nil {
		return Reading{}, err
	}
	if err := o.Validate(); err != nil {
		return Reading{}, err
	}

	reading := Reading{Crossing: b.Crossing()}
	differences, sampling, baselines, seen := b.series(o)
	reading.Intervals = len(differences)
	switch {
	case reading.Intervals == 0 && seen.release && !seen.baseline:
		// One arm was read and the other was not, which is the case a release with
		// no control is in. The request rate is what catches an arm that went
		// silent beside one that is serving; this is the arm that was never there.
		reading.Unavailable = NoBaseline
		return reading, nil
	case reading.Intervals == 0:
		reading.Unavailable = NoVolume
		return reading, nil
	case reading.Intervals < minimumIntervals:
		// The interval is the unit the variance is estimated over, and a spread
		// cannot be read from one observation. So a burst of traffic inside one
		// interval buys nothing the next interval does not, however large it was.
		reading.Unavailable = NoSpread
		return reading, nil
	}

	reading.Difference = mean(differences)
	reading.BaselineRate = mean(baselines)
	if b.Worse == WorseHigher && reading.BaselineRate+b.Size > 1 ||
		b.Worse == WorseLower && reading.BaselineRate-b.Size < 0 {
		reading.Unavailable = NoHeadroom
		return reading, nil
	}

	reading.Deviation = math.Sqrt(math.Max(variance(differences), mean(sampling)))
	if reading.Deviation <= 0 {
		reading.Unavailable = NoVolume
		return reading, nil
	}

	// The log of the likelihood ratio between a mean difference of the size and a
	// mean difference of nothing, over the intervals read: the sum of the
	// differences weighted by the size, less the half-size the alternative costs
	// per interval, over the variance.
	n := float64(reading.Intervals)
	sum := reading.Difference * n
	reading.Log = (b.Size*sum - n*b.Size*b.Size/2) / (reading.Deviation * reading.Deviation)

	switch {
	case reading.Log >= reading.Crossing:
		reading.Failed = true
	case reading.Log <= -reading.Crossing:
		reading.Passed = true
	}
	return reading, nil
}

// series reduces the intervals to the three things the statistic is computed
// from: the difference between the arms per interval signed so that positive is
// worse, the sampling variance of that difference the counts imply, and the
// other arm's share. An interval either arm has no units in is not an
// observation of the difference — a read whose period the store does not cover
// is read as no volume and never as a low one — and seen is which arms appeared
// at all, which is what tells a release with no control from one nobody called.
func (b Boundary) series(o Observed) (differences, sampling, baselines []float64, seen arms) {
	for _, c := range o.Intervals {
		release, hasRelease := c.Rate()
		baseline, hasBaseline := c.BaselineRate()
		seen.release = seen.release || hasRelease
		seen.baseline = seen.baseline || hasBaseline
		if !hasRelease || !hasBaseline {
			continue
		}
		difference := release - baseline
		if b.Worse == WorseLower {
			difference = -difference
		}
		differences = append(differences, difference)
		baselines = append(baselines, baseline)
		sampling = append(sampling, sampled(c.Count, c.Units)+sampled(c.BaselineCount, c.BaselineUnits))
	}
	return differences, sampling, baselines, seen
}

// arms is which arms were read at all over the whole series, which is what tells
// a release with no control from a service nobody called.
type arms struct{ release, baseline bool }

// sampled is the variance of one arm's share inside one interval, smoothed by
// half an occurrence in one unit so that an arm which counted nothing still has
// one. It is the floor under the spread between intervals, and it is what makes
// the volume inside an interval bound the size that interval can be read at.
func sampled(count, units int64) float64 {
	if units <= 0 {
		return 0
	}
	rate := (float64(count) + smoothing) / (float64(units) + 2*smoothing)
	return rate * (1 - rate) / float64(units)
}

func mean(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	var sum float64
	for _, v := range values {
		sum += v
	}
	return sum / float64(len(values))
}

// variance is the spread between intervals, and nothing where there is one
// interval: with a single observation there is no spread to read and the
// sampling floor is the whole of the scale.
func variance(values []float64) float64 {
	if len(values) < 2 {
		return 0
	}
	average := mean(values)
	var sum float64
	for _, v := range values {
		sum += (v - average) * (v - average)
	}
	return sum / float64(len(values)-1)
}
