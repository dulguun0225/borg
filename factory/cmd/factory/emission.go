package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/localtarget"
)

// signalFiles is [healthmonitor.Emission] on this platform: the quantity read
// out of the file each deployed process emits into. One file per build in the
// directory that build ran in, which is what makes a release's own counts
// tellable from the counts of the build that ran there before it — package
// localtarget names the file, being the thing that told the process where to
// write it.
//
// A build that emitted nothing reads as no units, which the boundary treats as a
// window with nothing to say yet. That is right for a release just deployed and
// it is also what an implementation that ignored the instruction to emit looks
// like, and nothing here can tell the two apart.
//
// Two things the interface asks for this platform cannot give, and each says so
// rather than answering as though it could. The file is keyed by build and not
// by deploy, so the two arms of a comparison that differ only in which deploy
// placed them — a control and the long-lived instances of the same build — read
// as one; this platform starts no control, so that pair never arises here. And
// it keeps no failure records and no objective, so the two reads for those
// answer with nothing.
type signalFiles struct{ dir string }

// The two shapes the factory has shipped. Every record names the version it was
// emitted at, and the health monitor reads every version the factory has
// shipped: a version adds a name beside what the version before carried, so a
// series kept at an earlier version is read as it was.
const (
	// emissionVersion is the first shape: one line per unit of work, the
	// outcome and nothing else. It carries no time, so a series at this version
	// is one interval per unit of work — the smallest thing the file
	// distinguishes — and there is no period to cut it by.
	emissionVersion = "emission/1"
	// emissionVersionTimed is the second: the time the unit of work finished, a
	// tab, and the outcome. The time is what lets a record be assigned to an
	// interval at ingest, which is what the reading against a service's own
	// recent history needs — a service read against its own past has no past
	// where the store keeps no time.
	emissionVersionTimed = "emission/2"
	// emissionTimeLayout is how a record at [emissionVersionTimed] writes its
	// time: RFC 3339 with nanoseconds, which sorts as it reads.
	emissionTimeLayout = time.RFC3339Nano
)

// intervalResolution is the width of one interval at [emissionVersionTimed]: the
// resolution the factory fixes for every service and ships with the
// instrumentation, rather than a value an owner authors. Fifty milliseconds is
// what a process exercising itself about once a millisecond fills with tens of
// units, which is the volume an interval's rate has to be read at, and it is
// short enough that a window capped at a second still holds intervals enough for
// a spread to be read between them.
const intervalResolution = 50 * time.Millisecond

// unit is one unit of work as the file records it: when it finished, whether the
// file said when, and whether it failed.
type unit struct {
	at     time.Time
	timed  bool
	failed bool
}

// emitted is what one build's file holds: its units in the order they were
// written, and the emission version its records carry. The version is empty
// where the file holds no record at all, which is what a build deployed a moment
// ago reads as.
type emitted struct {
	units   []unit
	version string
}

// Read is the comparison's two arms on one target. The target is a directory on
// this platform, which is where the two builds' files are, and the release's arm
// is the units; the baseline arm's units are the other arm of the same counts,
// which is what the boundary reads a change against.
//
// Only the error rate is answered. The other three quantities the shipped
// emission version carries need more of a record than the outcome and the time —
// a latency is a histogram over buckets and a hazardous operation is a name in
// the key — and the file holds neither, so a series for them would be a number
// the boundary would read as a reading and it is left out instead. A quantity
// the series does not carry is one the comparison skips.
func (s signalFiles) Read(_ context.Context, r healthmonitor.Reading) (healthmonitor.Series, error) {
	release, err := s.emitted(r.Target, r.Release.BuildID)
	if err != nil {
		return healthmonitor.Series{}, err
	}
	baseline, err := s.emitted(r.Target, r.Baseline.BuildID)
	if err != nil {
		return healthmonitor.Series{}, err
	}
	series := healthmonitor.Series{
		EmissionVersionRelease: release.version,
		Newest:                 release.newest(),
		Operations: []healthmonitor.OperationSeries{{
			Operation: healthmonitor.PooledOperation,
			Quantities: map[gatepolicy.Quantity]boundary.Observed{
				gatepolicy.QuantityErrorRate: {Intervals: paired(release.intervals(), baseline.intervals())},
			},
		}},
	}
	if r.Baseline.BuildID != "" {
		series.EmissionVersionBaseline = baseline.version
	}
	return series, nil
}

// History is the reading against the service's own recent history, which has no
// second arm running beside it. The other arm here is what this service emitted
// on this target before the deploy under reading placed what is running: every
// other build's file in the directory, in time order, up to the first record the
// arm carries.
//
// It needs a time per record, so it is a reading at [emissionVersionTimed] and
// at no earlier version. A build emitting at [emissionVersion] has a file with
// no period to cut, so nothing in it can be ordered against another build's and
// the arm is read against no baseline — which the boundary reads as a comparison
// with nothing on the other side, and which is what this platform answered
// before the second version shipped.
func (s signalFiles) History(_ context.Context, h healthmonitor.History) (healthmonitor.Series, error) {
	arm, err := s.emitted(h.Target, h.Of.BuildID)
	if err != nil {
		return healthmonitor.Series{}, err
	}
	past, err := s.before(h.Target, h.Of.BuildID, arm)
	if err != nil {
		return healthmonitor.Series{}, err
	}
	return healthmonitor.Series{
		EmissionVersionRelease: arm.version,
		Newest:                 arm.newest(),
		Operations: []healthmonitor.OperationSeries{{
			Operation: healthmonitor.PooledOperation,
			Quantities: map[gatepolicy.Quantity]boundary.Observed{
				gatepolicy.QuantityErrorRate: {Intervals: paired(arm.intervals(), past)},
			},
		}},
	}, nil
}

// FailureRecords is what the store keeps of one service's failures, which this
// platform keeps none of: the emitted line says a unit of work was not ok and
// says nothing about which failure class or which line of code it was. An
// incident here carries no copy of them, and doc.go for package healthmonitor
// says what a caller that supplies none loses.
func (signalFiles) FailureRecords(context.Context, healthmonitor.Reading) ([]healthmonitor.FailureRecord, error) {
	return nil, nil
}

// Spent is what the service consumed of its objective over a period, which this
// platform cannot answer: nothing here keeps a series per service across builds,
// so no period can be cut out of one. Covered is false, which is what leaves the
// error budget uncomputed — package healthmonitor says an uncomputed budget holds
// the way an exhausted one does.
func (signalFiles) Spent(context.Context, string, time.Duration) (healthmonitor.Spend, error) {
	return healthmonitor.Spend{}, nil
}

// Shape is the emission version the store's records for one arm carry, and empty
// where it holds none for it yet — which is what a window over a deploy just
// written reads before any record has arrived. On this platform it is the
// version the file's own records are written at.
func (s signalFiles) Shape(_ context.Context, a healthmonitor.Arm) (string, error) {
	read, err := s.emitted("", a.BuildID)
	if err != nil {
		return "", err
	}
	return read.version, nil
}

// emitted is what one build wrote on one target, and nothing for a build this
// call names none of.
func (s signalFiles) emitted(target, build string) (emitted, error) {
	if build == "" {
		return emitted{}, nil
	}
	return readSignal(localtarget.SignalFile(s.at(target), build))
}

// before is what this service emitted on this target before the deploy that
// placed what the arm is reading: every other build's file in the directory, up
// to the newest record the arm carries, in time order and cut into intervals. It
// is the other arm of the reading against a service's own recent history.
//
// A build's own earlier records are not among them. The arm is one build's file
// and the reading is against what ran before it, so a file read against its own
// beginning would compare a release to itself. On this platform a build's file
// grows only while that build is the process running for the service, so another
// build's records are records from before this one was deployed; the bound at
// the arm's newest record is what keeps a build deployed after it — a rollback's,
// or the search's — out of what is called the past.
func (s signalFiles) before(target, build string, arm emitted) ([]boundary.Counts, error) {
	if arm.version != emissionVersionTimed || len(arm.units) == 0 {
		return nil, nil
	}
	last := arm.units[len(arm.units)-1].at
	dir := s.at(target)
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("factory: reading what %s emitted before %s: %w", dir, build, err)
	}

	var past []unit
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".signal") || name == filepath.Base(localtarget.SignalFile(dir, build)) {
			continue
		}
		read, err := readSignal(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		if read.version != emissionVersionTimed {
			continue
		}
		for _, u := range read.units {
			if !u.at.After(last) {
				past = append(past, u)
			}
		}
	}
	slices.SortStableFunc(past, func(a, b unit) int { return a.at.Compare(b.at) })
	return emitted{units: past, version: emissionVersionTimed}.intervals(), nil
}

// intervals is one arm cut into the intervals its records were assigned to at
// ingest: the records at [emissionVersionTimed] grouped by the resolution the
// factory fixes, and one unit of work per interval at [emissionVersion], which
// is the smallest thing a file with no time in it distinguishes.
//
// What the earlier version costs is that the spread the boundary estimates is
// the spread between single units of work, which is the coarsest reading there
// is: each interval's rate is nought or one. That is the reading this platform
// gave before a time was emitted, and it is what a service still emitting at
// that version is read at.
func (e emitted) intervals() []boundary.Counts {
	if e.version != emissionVersionTimed {
		counted := make([]boundary.Counts, 0, len(e.units))
		for _, u := range e.units {
			counted = append(counted, boundary.Counts{Units: 1, Count: failedCount(u.failed)})
		}
		return counted
	}

	var counted []boundary.Counts
	var current time.Time
	for _, u := range e.units {
		at := u.at.Truncate(intervalResolution)
		if len(counted) == 0 || !at.Equal(current) {
			counted = append(counted, boundary.Counts{})
			current = at
		}
		counted[len(counted)-1].Units++
		counted[len(counted)-1].Count += failedCount(u.failed)
	}
	return counted
}

// newest is the newest time this arm holds a record for, which the health
// monitor writes onto its last check. A file at [emissionVersion] carries no
// time per record, so nothing here can say when its newest record arrived and it
// answers empty — which is read as no volume rather than as a low one.
func (e emitted) newest() string {
	if e.version != emissionVersionTimed || len(e.units) == 0 {
		return ""
	}
	return e.units[len(e.units)-1].at.UTC().Format(time.RFC3339Nano)
}

// paired is the nth interval of one arm beside the nth of the other, ending at
// the shorter of the two. An interval read on one arm and not the other is not a
// comparison, and the boundary passes over it — so pairing beyond the shorter
// arm would add intervals that say nothing.
//
// The nth and not the concurrent one: this platform starts no control, so the
// two arms of every comparison it makes ran one after the other and never at the
// same time. Pairing by the clock would pair nothing at all. What it costs is
// that an arm's nth interval is compared with an interval of the other arm that
// covers a different stretch of time, which is the weak fallback the design
// already names for a comparison with no control.
func paired(arm, other []boundary.Counts) []boundary.Counts {
	if len(other) == 0 {
		return arm
	}
	counted := make([]boundary.Counts, 0, min(len(arm), len(other)))
	for n := range min(len(arm), len(other)) {
		counted = append(counted, boundary.Counts{
			Units: arm[n].Units, Count: arm[n].Count,
			BaselineUnits: other[n].Units, BaselineCount: other[n].Count,
		})
	}
	return counted
}

// failedCount is one unit of work as a count: one where it failed and nought
// where it did not.
func failedCount(failed bool) int64 {
	if failed {
		return 1
	}
	return 0
}

// at is the directory a target names. An address is a directory on this
// platform, so the target is the directory — except where the health monitor
// had no target to read and stood the environment in for the whole set, which
// is not a directory at all and reads as production's own.
func (s signalFiles) at(target string) string {
	if target == "" || !strings.ContainsRune(target, os.PathSeparator) {
		return s.dir
	}
	return target
}

// readSignal is what one build emitted, one entry per unit of work, with the
// emission version its records are written at. A file that is not there is
// nothing emitted rather than an error: a build deployed a moment ago has
// emitted nothing yet, and so has one that was never started.
//
// A record is at [emissionVersionTimed] where it carries a time and a tab before
// its outcome, and at [emissionVersion] where it is the outcome alone; the file
// reads at the version its newest record carries. A record with no time in a
// file that reads timed takes the time of the record before it, which is where
// the writer that emitted it was — a mixed file is a build replaced mid-write,
// and dropping those records would drop a reading rather than place it.
//
// Any outcome that is not exactly "ok" counts as a failure, rather than only the
// word the instruction names. That is the lenient direction and it is the safe
// one here: a program emitting something the factory cannot read is not a
// program the factory should read as healthy.
func readSignal(path string) (emitted, error) {
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return emitted{}, nil
	} else if err != nil {
		return emitted{}, fmt.Errorf("factory: reading the quantity at %s: %w", path, err)
	}

	var read emitted
	var last time.Time
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		u := unit{failed: line != "ok"}
		if when, outcome, tabbed := strings.Cut(line, "\t"); tabbed {
			if at, err := time.Parse(emissionTimeLayout, when); err == nil {
				u = unit{at: at, timed: true, failed: outcome != "ok"}
				last = at
			}
		}
		if !u.timed {
			u.at = last
		}
		read.units = append(read.units, u)
	}
	if len(read.units) > 0 {
		read.version = emissionVersion
		if read.units[len(read.units)-1].timed {
			read.version = emissionVersionTimed
		}
	}
	return read, nil
}

// countSignal is how many units of work one build emitted and how many of them
// failed, which is what the deployer's read of a service's reachability asks
// for: whether the service emits what the health monitor reads at all. It counts
// records at either version, a service's reachability being about whether it
// emits and not about which shape it emits.
func countSignal(path string) (int64, int64, error) {
	read, err := readSignal(path)
	if err != nil {
		return 0, 0, err
	}
	var failures int64
	for _, u := range read.units {
		failures += failedCount(u.failed)
	}
	return int64(len(read.units)), failures, nil
}
