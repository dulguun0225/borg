package boundary

import (
	"errors"
	"fmt"
)

// Version names the construction [Boundary.Evaluate] applies: what turns the
// size, the confidence, the power and the run length into a boundary and
// allocates it over the set of comparisons a window makes. It is stored on the
// analysis window record at the open, for the reason the design gives — a
// reading at an exit is not interpretable against anything but the boundary it
// was actually read against, and the size and the confidence alone do not say
// what was done with them.
//
// A change to the construction changes this string, so a window closed under one
// is never read as a window closed under another.
const Version = "interval-paired-difference/v1"

// minimumIntervals is how many intervals both arms must be read over before
// either exit is reachable. The interval is the unit the variance is estimated
// over, so one observation carries no spread to read: a window closes on the
// count of intervals and never on the volume inside one, however large that
// volume was.
const minimumIntervals = 2

// smoothing is the half an occurrence in one unit added to each arm's rate
// before its sampling variance is computed. An arm that counted nothing would
// otherwise have a sampling variance of nothing, and a series of identical
// intervals would then divide by zero.
const smoothing = 0.5

// Worse is which direction of the difference between the two arms is a
// regression. It is a field of the boundary rather than something read off the
// numbers, because the two quantities that move in opposite directions — an
// error rate and a request rate — are read by one construction and a caller that
// left it implicit would pass a silent release arm as an improvement.
type Worse string

const (
	// WorseHigher is a quantity a regression raises: the error rate, the latency
	// share, and the count of a hazardous operation.
	WorseHigher Worse = "higher"
	// WorseLower is a quantity a regression lowers: the request rate, where a
	// release arm receiving materially fewer requests than its control is the
	// crossing.
	WorseLower Worse = "lower"
)

// Worses is every direction a boundary may be read in.
var Worses = []Worse{WorseHigher, WorseLower}

var (
	// ErrSizeOutOfRange is returned for a size outside nothing to one. The size is
	// the smallest change worth catching, as a share — so a size of nothing asks
	// the comparison to rule out no change at all.
	ErrSizeOutOfRange = errors.New("boundary: the size is a share above 0 and at most 1")
	// ErrConfidenceOutOfRange is returned for a confidence outside nothing to
	// one, either end excluded: at nothing every reading crosses at once, and at
	// one no reading ever crosses.
	ErrConfidenceOutOfRange = errors.New("boundary: the confidence is a share above 0 and below 1")
	// ErrPowerOutOfRange is returned for a power outside nothing to one, either
	// end excluded. A power of one is what no finite volume reaches.
	ErrPowerOutOfRange = errors.New("boundary: the power is a share above 0 and below 1")
	// ErrComparisonsNotPositive is returned for a boundary allocated over no
	// comparison at all. A window reads at least one, and the count is what the
	// authored confidence is held over.
	ErrComparisonsNotPositive = errors.New("boundary: the confidence is held over at least one comparison")
	// ErrRunLengthTooShort is returned by [AtRunLength] for a run length at or
	// below one observation, which is a reading that crosses on everything.
	ErrRunLengthTooShort = errors.New("boundary: an average run length is above one observation")
	// ErrWorseUnknown is returned for a direction outside [Worses].
	ErrWorseUnknown = errors.New("boundary: a regression raises the quantity or lowers it")
	// ErrCountsNegative is returned for a count below zero, or for more
	// occurrences than the units they were counted over.
	ErrCountsNegative = errors.New("boundary: a count of units is not negative and occurrences do not exceed units")
)

// The reasons neither exit is reachable, in the words [Reading.Unavailable]
// carries.
const (
	// NoVolume is neither arm having been read over a whole interval yet. It is
	// the side an absent input lands on: a window over it cannot pass, and the
	// cap is what ends it.
	NoVolume = "no interval has been read on both arms, so there is nothing to compare"
	// NoBaseline is a release with no control beside it — a service's first
	// release, or a deploy whose strategy kept none and whose fallback found no
	// release below it. Nothing about it is ruled out by watching.
	NoBaseline = "the release has no arm to be compared against, so no change can be ruled out or found"
	// NoSpread is fewer intervals than the spread between them can be read from.
	// It is the volume bound stated the other way: a burst inside one interval
	// buys nothing the next interval does not.
	NoSpread = "fewer intervals have been read on both arms than a spread between intervals can be read from"
	// NoHeadroom is a baseline sitting so near the end of the scale that the size
	// takes the alternative past it. There is no value beyond it for the
	// alternative to name, so the comparison has nothing to detect a change in.
	NoHeadroom = "the baseline already sits so near the end of the scale that the size takes the alternative past it"
)

// Boundary is one quantity's boundary on one window: the size the comparison
// must rule out, the confidence it must do it with, the count of comparisons
// that confidence is held over, and which direction of the difference is a
// regression. All four are resolved when the window opens and stored on its
// record.
type Boundary struct {
	// Size is the smallest change worth catching, as a share: the alternative the
	// boundary tests against is the baseline plus this much, in the direction
	// [Boundary.Worse] names.
	Size float64
	// Confidence is how sure the comparison must be, as a share. It is the
	// authored value and is held over the whole set of comparisons rather than
	// over this one, which is what [Boundary.Crossing] does with [Boundary.Comparisons].
	Confidence float64
	// Comparisons is how many readings the confidence is held over: the quantities
	// on every operation read alone on every target the rollout is planned to
	// reach. A crossing on any one of them starts the same rollback, so reading
	// each at the authored confidence would roll a good release back as often as
	// there are comparisons.
	Comparisons int
	Worse       Worse
}

// Validate reports whether the boundary may be read. It is a programmer's error
// rather than a reading, so it is an error and not an [Reading.Unavailable]: the
// values come from what an owner authored or the score supplied, both of which
// are already checked where they are written.
func (b Boundary) Validate() error {
	if b.Size <= 0 || b.Size > 1 {
		return fmt.Errorf("%w: %v", ErrSizeOutOfRange, b.Size)
	}
	if b.Confidence <= 0 || b.Confidence >= 1 {
		return fmt.Errorf("%w: %v", ErrConfidenceOutOfRange, b.Confidence)
	}
	if b.Comparisons < 1 {
		return fmt.Errorf("%w: %d", ErrComparisonsNotPositive, b.Comparisons)
	}
	if b.Worse != WorseHigher && b.Worse != WorseLower {
		return fmt.Errorf("%w: %q", ErrWorseUnknown, b.Worse)
	}
	return nil
}

// AtRunLength is the boundary for a reading that never closes: the reading
// against a service's own recent history and an explicit threshold. Neither has
// a last look to spend a rate against, so what is stated instead is the average
// run length — the mean number of intervals a service whose behaviour has not
// changed runs before the reading crosses it once.
//
// A run length is a confidence here rather than a second construction, because
// the two are the same number read from opposite ends: a statistic that crosses
// at the log of one over one-minus-confidence crosses about that many
// observations apart when there is nothing to find. So a run length of a
// thousand is a confidence of all but one in a thousand, and one boundary reads
// every reading the factory takes.
//
// The design states the run length as a volume of traffic; this reads it in
// intervals, the unit the variance is estimated over and the unit
// [Boundary.IntervalsToPassed] already counts in.
func AtRunLength(size, runLength float64, comparisons int, worse Worse) (Boundary, error) {
	if runLength <= 1 {
		return Boundary{}, fmt.Errorf("%w: a run length of %v is not above one observation", ErrRunLengthTooShort, runLength)
	}
	b := Boundary{Size: size, Confidence: 1 - 1/runLength, Comparisons: comparisons, Worse: worse}
	return b, b.Validate()
}

// RunLength is the boundary's crossing read back as one: the mean number of
// intervals between crossings when there is nothing to find.
func (b Boundary) RunLength() float64 { return 1 / (1 - b.Confidence) }

// Counts is one arm's count of the quantity and the units it was counted over,
// for both arms. It is one interval's observation inside [Observed], and the sum
// of them is what an exit is stored against.
//
// What Count is depends on the quantity, and the boundary is told none of them:
// the failures for an error rate, the completions at or past the bucket the
// latency quantile falls in, the times a hazardous operation was performed, and
// the release arm's own arrivals out of both arms' arrivals for a request rate.
// Units is what that count was taken over on the same arm.
type Counts struct {
	Units         int64
	Count         int64
	BaselineUnits int64
	BaselineCount int64
}

// Validate refuses counts that are not counts.
func (c Counts) Validate() error {
	for _, pair := range [][2]int64{{c.Units, c.Count}, {c.BaselineUnits, c.BaselineCount}} {
		if pair[0] < 0 || pair[1] < 0 || pair[1] > pair[0] {
			return fmt.Errorf("%w: %d units, %d counted", ErrCountsNegative, pair[0], pair[1])
		}
	}
	return nil
}

// Empty reports whether nothing was counted on either arm.
func (c Counts) Empty() bool { return c == Counts{} }

// Add is the two sets of counts summed, which is what the window stores over the
// whole of its life: a count is a count, so counts add across intervals, across
// instances, and across the operations a series is kept per.
func (c Counts) Add(o Counts) Counts {
	return Counts{
		Units:         c.Units + o.Units,
		Count:         c.Count + o.Count,
		BaselineUnits: c.BaselineUnits + o.BaselineUnits,
		BaselineCount: c.BaselineCount + o.BaselineCount,
	}
}

// Rate is the release arm's share, and false where that arm counted no units.
func (c Counts) Rate() (float64, bool) {
	if c.Units <= 0 {
		return 0, false
	}
	return float64(c.Count) / float64(c.Units), true
}

// BaselineRate is the other arm's share, and false where it counted no units.
func (c Counts) BaselineRate() (float64, bool) {
	if c.BaselineUnits <= 0 {
		return 0, false
	}
	return float64(c.BaselineCount) / float64(c.BaselineUnits), true
}

// Observed is one quantity read over one window: what each arm counted in each
// interval, in the order the store keeps them. The interval is the unit the
// variance is estimated over and the request is not, so each interval's counts
// per arm are one observation and the spread the boundary reads is the spread
// between intervals.
type Observed struct {
	Intervals []Counts
}

// Totals is the four counts summed over every interval, which is what a window
// stores its exit against.
func (o Observed) Totals() Counts {
	var total Counts
	for _, c := range o.Intervals {
		total = total.Add(c)
	}
	return total
}

// Validate refuses an interval whose counts are not counts.
func (o Observed) Validate() error {
	for i, c := range o.Intervals {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("boundary: interval %d: %w", i, err)
		}
	}
	return nil
}

// Reading is what one read of the quantity says against the boundary. Every
// number the verdict was reached from is on it, because a boundary nobody can
// recompute is one nobody can argue with — the same rule the score's vector
// keeps.
type Reading struct {
	// Failed is the statistic having crossed against the release: a change at
	// least as large as the size, in the direction the boundary reads as worse,
	// at the confidence held over the set.
	Failed bool
	// Passed is the statistic having crossed the other way: a change of that size
	// ruled out, at the same confidence.
	Passed bool
	// Unavailable is why neither exit is reachable, and is empty where both are.
	Unavailable string
	// Intervals is how many intervals were read on both arms, which is the
	// effective sample and not the count of requests.
	Intervals int
	// Difference is the mean of the per-interval differences between the arms,
	// signed so that a positive number is worse.
	Difference float64
	// BaselineRate is the mean of the other arm's per-interval shares, which is
	// what the alternative was raised from.
	BaselineRate float64
	// Deviation is the standard deviation between intervals the statistic was
	// scaled by, floored by the sampling variance the counts themselves imply.
	Deviation float64
	// Log is the log of the likelihood ratio between the alternative and no
	// change over the intervals observed. Failed is this at or above the crossing
	// and passed is this at or below its negative, so one number is both exits.
	Log float64
	// Crossing is [Boundary.Crossing] as it was applied, so the reading carries
	// the line it was read against.
	Crossing float64
}
