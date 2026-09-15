package healthmonitor

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// SignalPath names the file a build emits on one target. The target owns this
// path convention; the health monitor owns what the file contains.
type SignalPath func(target, build string) string

// FileEmission reads the versioned emission from files supplied by a target.
// dir is used when a Reading has no target, as Shape does.
type FileEmission struct {
	dir  string
	path SignalPath
}

// NewFileEmission returns an emission reader over the target files in dir.
func NewFileEmission(dir string, path SignalPath) *FileEmission {
	return &FileEmission{dir: dir, path: path}
}

// EmissionRecord is one record in emission/3. The same fields identify an
// arrival and its completion; the completion adds Outcome and Duration.
type EmissionRecord struct {
	Version        string        `json:"version"`
	Kind           string        `json:"kind"`
	Time           time.Time     `json:"time"`
	Service        string        `json:"service"`
	Build          string        `json:"build"`
	Deploy         string        `json:"deploy"`
	Target         string        `json:"target"`
	Operation      string        `json:"operation"`
	Outcome        string        `json:"outcome,omitempty"`
	Duration       time.Duration `json:"duration,omitempty"`
	Deadline       time.Duration `json:"deadline,omitempty"`
	FailureClass   string        `json:"failure_class,omitempty"`
	CodeLocation   string        `json:"code_location,omitempty"`
	HazardousCount int64         `json:"hazardous_count,omitempty"`
	acceptedAt     time.Time     `json:"-"`
}

// RecordKinds are the records emission/3 accepts.
const (
	RecordArrival            = "arrival"
	RecordCompletion         = "completion"
	RecordHazardousOperation = "hazardous_operation"
)

const (
	emissionVersion       = "emission/1"
	emissionVersionTimed  = "emission/2"
	emissionVersionRecord = "emission/3"
	emissionTimeLayout    = time.RFC3339Nano
)

// intervalResolution is shipped with emission/3. The unfinished deadline is a
// service value carried on each arrival record.
const (
	intervalResolution = 50 * time.Millisecond
)

var histogramBoundaries = []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10}

const histogramQuantile = 0.99

type unit struct {
	at     time.Time
	timed  bool
	failed bool
	deploy string
}

type emitted struct {
	units   []unit
	records []EmissionRecord
	version string
}

// Read returns the two arms grouped by operation and quantity. Records are
// selected by build and deploy, so one build file can retain records from more
// than one placement without joining an arm by time.
func (f *FileEmission) Read(_ context.Context, r Reading) (Series, error) {
	release, err := f.emitted(r.Target, r.Release)
	if err != nil {
		return Series{}, err
	}
	baseline, err := f.emitted(r.Target, r.Baseline)
	if err != nil {
		return Series{}, err
	}
	series := pairSeries(release, baseline, r.OperationsReadAlone, r.ServiceName, false)
	if latest, err := f.newestServiceRecord(r.Target, r.ServiceName); err != nil {
		return Series{}, err
	} else if latest != "" {
		series.Newest = latest
	}
	return series, nil
}

// History reads one build against records from other builds before its newest
// record. Legacy lines accepted by the local target use their envelope times;
// a direct legacy file without those times has no history baseline.
func (f *FileEmission) History(_ context.Context, h History) (Series, error) {
	arm, err := f.emitted(h.Target, h.Of)
	if err != nil {
		return Series{}, err
	}
	if arm.version == "" {
		return Series{}, nil
	}
	if arm.version == emissionVersionRecord {
		current := recordIntervals(arm, nil, h.ServiceName)
		if intervalCount(current) == 0 || arrivalCount(current) == 0 {
			series := Series{EmissionVersionRelease: arm.version, Newest: newest(arm)}
			return f.withNewest(h.Target, h.ServiceName, series)
		}
		past, err := f.pastRecords(h.Target, h.Of.BuildID, arm, h.Against)
		if err != nil {
			return Series{}, err
		}
		series := pairSeries(arm, emitted{records: past, version: emissionVersionRecord}, h.OperationsReadAlone, h.ServiceName, true)
		return f.withNewest(h.Target, h.ServiceName, series)
	}
	past, err := f.before(h.Target, h.Of.BuildID, arm)
	if err != nil {
		return Series{}, err
	}
	series := pairSeries(arm, emitted{units: past, version: arm.version}, h.OperationsReadAlone, h.ServiceName, true)
	return f.withNewest(h.Target, h.ServiceName, series)
}

func (f *FileEmission) withNewest(target, service string, series Series) (Series, error) {
	latest, err := f.newestServiceRecord(target, service)
	if err != nil {
		return Series{}, err
	}
	if latest != "" {
		series.Newest = latest
	}
	return series, nil
}

func intervalCount(values map[string][]intervalData) int {
	var count int
	for _, intervals := range values {
		count += len(intervals)
	}
	return count
}

func arrivalCount(values map[string][]intervalData) int64 {
	var count int64
	for _, intervals := range values {
		for _, value := range intervals {
			count += value.arrivals
		}
	}
	return count
}

// FailureRecords returns the failure counts for one arm, retaining the full
// key needed by an incident copy.
func (f *FileEmission) FailureRecords(_ context.Context, r Reading) ([]FailureRecord, error) {
	read, err := f.emitted(r.Target, r.Release)
	if err != nil {
		return nil, err
	}
	if read.version != emissionVersionRecord {
		return nil, nil
	}
	type key struct {
		version, interval, service, class, location, target, build, deploy string
	}
	counts := map[key]int64{}
	for _, one := range read.records {
		if one.Kind != RecordCompletion || !completionFailed(one) || !matches(one, r.Release, r.Target, r.ServiceName) || one.acceptedAt.IsZero() {
			continue
		}
		arrivalAt := one.acceptedAt.Add(-one.Duration)
		k := key{one.Version, record.FormatTime(arrivalAt.Truncate(intervalResolution)), one.Service,
			one.FailureClass, one.CodeLocation, one.Target, one.Build, one.Deploy}
		counts[k]++
	}
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return strings.Join([]string{keys[i].version, keys[i].interval, keys[i].service, keys[i].class, keys[i].location,
			keys[i].target, keys[i].build, keys[i].deploy}, "\x00") <
			strings.Join([]string{keys[j].version, keys[j].interval, keys[j].service, keys[j].class, keys[j].location,
				keys[j].target, keys[j].build, keys[j].deploy}, "\x00")
	})
	result := make([]FailureRecord, 0, len(keys))
	for _, k := range keys {
		result = append(result, FailureRecord{Version: k.version, Interval: k.interval, ServiceName: k.service,
			FailureClass: k.class, CodeLocation: k.location, Target: k.target,
			BuildID: k.build, DeployID: k.deploy, Count: counts[k]})
	}
	return result, nil
}

func (f *FileEmission) emitted(target string, arm Arm) (emitted, error) {
	if arm.BuildID == "" {
		return emitted{}, nil
	}
	path := f.path
	if path == nil {
		return emitted{}, errors.New("healthmonitor: file emission has no signal path")
	}
	if target == "" {
		target = f.dir
	}
	read, err := readEmission(path(target, arm.BuildID))
	if err != nil {
		return emitted{}, err
	}
	if read.version == emissionVersionRecord {
		filtered := read.records[:0]
		for _, one := range read.records {
			if matches(one, arm, target, "") {
				filtered = append(filtered, one)
			}
		}
		read.records = filtered
	}
	return read, nil
}

func matches(one EmissionRecord, arm Arm, target, service string) bool {
	if service != "" && one.Service != service {
		return false
	}
	if one.Build != arm.BuildID {
		return false
	}
	if one.Deploy != arm.DeployID {
		return false
	}
	return target != "" && one.Target == target
}

// completionFailed is the emission rule shared by the kept failure count and
// the error-rate reading: a completion names a failure class when it failed.
func completionFailed(one EmissionRecord) bool {
	return one.Kind == RecordCompletion && one.FailureClass != ""
}

type intervalData struct {
	version             string
	at                  time.Time
	arrivals            int64
	completions         int64
	completionByOutcome map[string]int64
	deadline            time.Duration
	errors              int64
	errorsReady         bool
	hazardous           int64
	histogram           map[string][]int64
}

type intervalKey struct {
	version string
	at      time.Time
}

func pairSeries(release, baseline emitted, alone []string, serviceName string, alignHistory bool) Series {
	result := Series{EmissionVersionRelease: release.version, Newest: newest(release)}
	if baselineNewest := newest(baseline); baselineNewest > result.Newest {
		result.Newest = baselineNewest
	}
	if baseline.version != "" {
		result.EmissionVersionBaseline = baseline.version
	}
	if release.version == emissionVersionRecord || baseline.version == emissionVersionRecord {
		left := intervalsFor(release, alone, serviceName)
		right := intervalsFor(baseline, alone, serviceName)
		// emission/1 carries no time, so its intervals pair with a timed arm by
		// rank rather than by the synthetic index time.
		result.Operations = pairRecordIntervals(left, right, alignHistory || release.version == emissionVersion || baseline.version == emissionVersion)
		for _, operation := range result.Operations {
			if operation.LatencyBucketShare > 0 {
				result.LatencyBucketShare = operation.LatencyBucketShare
				result.LatencyQuantile = operation.LatencyQuantile
				break
			}
		}
		return result
	}
	result.Operations = []OperationSeries{{Operation: PooledOperation, Quantities: map[gatepolicy.Quantity]boundary.Observed{
		gatepolicy.QuantityErrorRate: {Intervals: paired(legacyIntervals(release), legacyIntervals(baseline))},
	}}}
	return result
}

func intervalsFor(read emitted, alone []string, serviceName string) map[string][]intervalData {
	if read.version == emissionVersionRecord {
		return recordIntervals(read, alone, serviceName)
	}
	values := legacyIntervals(read)
	result := map[string][]intervalData{}
	for n, one := range values {
		at := time.Unix(int64(n), 0)
		if read.version == emissionVersionTimed && n < len(read.units) && !read.units[n].at.IsZero() {
			at = read.units[n].at.Truncate(intervalResolution)
		}
		result[PooledOperation] = append(result[PooledOperation], intervalData{
			version: read.version, at: at, arrivals: one.Units, completions: one.Units, errors: one.Count,
			errorsReady:         true,
			completionByOutcome: map[string]int64{},
			histogram:           map[string][]int64{},
		})
	}
	return result
}

func recordIntervals(read emitted, alone []string, serviceName string) map[string][]intervalData {
	byOperation := map[string]map[intervalKey]*intervalData{}
	deadline := serviceDeadline(read.records)
	for _, one := range read.records {
		if serviceName != "" && one.Service != serviceName {
			continue
		}
		operation := one.Operation
		if operation == "" {
			operation = PooledOperation
		}
		if !slices.Contains(alone, operation) {
			operation = PooledOperation
		}
		at := one.acceptedAt
		if at.IsZero() {
			continue
		}
		if one.Kind == RecordCompletion {
			at = at.Add(-one.Duration)
		}
		at = at.Truncate(intervalResolution)
		key := intervalKey{version: one.Version, at: at}
		perTime, ok := byOperation[operation]
		if !ok {
			perTime = map[intervalKey]*intervalData{}
			byOperation[operation] = perTime
		}
		data := perTime[key]
		if data == nil {
			data = &intervalData{version: one.Version, at: at, deadline: deadline,
				completionByOutcome: map[string]int64{}, histogram: map[string][]int64{}}
			perTime[key] = data
		}
		switch one.Kind {
		case RecordArrival:
			data.arrivals++
		case RecordCompletion:
			data.completionByOutcome[one.Outcome]++
			if completionFailed(one) {
				data.errors++
			}
			seconds := one.Duration.Seconds()
			bucket := sort.SearchFloat64s(histogramBoundaries, seconds)
			buckets := data.histogram[one.Outcome]
			if buckets == nil {
				buckets = make([]int64, len(histogramBoundaries)+1)
			}
			buckets[bucket]++
			data.histogram[one.Outcome] = buckets
		case RecordHazardousOperation:
			data.hazardous += one.HazardousCount
		}
	}
	result := map[string][]intervalData{}
	for operation, perTime := range byOperation {
		var values []intervalData
		for _, data := range perTime {
			for _, count := range data.completionByOutcome {
				data.completions += count
			}
			data.errorsReady = data.deadline > 0 && !data.at.Add(intervalResolution+data.deadline).After(time.Now())
			if data.errorsReady {
				if unfinished := data.arrivals - data.completions; unfinished > 0 {
					data.errors += unfinished
				}
				if data.errors < 0 {
					data.errors = 0
				}
				if data.errors > data.arrivals {
					data.errors = data.arrivals
				}
			}
			values = append(values, *data)
		}
		sort.Slice(values, func(i, j int) bool { return values[i].at.Before(values[j].at) })
		result[operation] = values
	}
	return result
}

func serviceDeadline(records []EmissionRecord) time.Duration {
	var deadline time.Duration
	for _, one := range records {
		if one.Kind == RecordArrival && one.Deadline > deadline {
			deadline = one.Deadline
		}
	}
	return deadline
}

func pairRecordIntervals(left, right map[string][]intervalData, alignHistory bool) []OperationSeries {
	operations := map[string]bool{}
	for operation := range left {
		operations[operation] = true
	}
	for operation := range right {
		operations[operation] = true
	}
	names := make([]string, 0, len(operations))
	for operation := range operations {
		names = append(names, operation)
	}
	slices.Sort(names)
	result := make([]OperationSeries, 0, len(names))
	for _, operation := range names {
		leftByTime := indexed(left[operation])
		rightByTime := indexed(right[operation])
		if alignHistory {
			rightByTime = alignIntervals(leftByTime, rightByTime)
		}
		times := make([]time.Time, 0, len(leftByTime)+len(rightByTime))
		for at := range leftByTime {
			times = append(times, at)
		}
		for at := range rightByTime {
			if _, exists := leftByTime[at]; !exists {
				times = append(times, at)
			}
		}
		slices.SortFunc(times, func(a, b time.Time) int { return a.Compare(b) })
		var request, errorsObserved, latency, hazardous []boundary.Counts
		var histogram []Histogram
		for _, at := range times {
			l, r := leftByTime[at], rightByTime[at]
			traffic := l.arrivals + r.arrivals
			// The local target runs one instance per arm. Units are the shared
			// interval traffic, so each arm is read as its share of requests.
			request = append(request, boundary.Counts{Units: traffic, Count: l.arrivals,
				BaselineUnits: traffic, BaselineCount: r.arrivals})
			if l.errorsReady || r.errorsReady {
				var lUnits, lErrors, rUnits, rErrors int64
				if l.errorsReady {
					lUnits, lErrors = l.arrivals, l.errors
				}
				if r.errorsReady {
					rUnits, rErrors = r.arrivals, r.errors
				}
				errorsObserved = append(errorsObserved, boundary.Counts{Units: lUnits, Count: lErrors,
					BaselineUnits: rUnits, BaselineCount: rErrors})
			}
			bucket := latencyBucket(l, r)
			lc, _ := latencyCount(l, bucket)
			rc, _ := latencyCount(r, bucket)
			latency = append(latency, boundary.Counts{Units: lc.Units, Count: lc.Count,
				BaselineUnits: rc.Units, BaselineCount: rc.Count})
			// The units are arrivals plus hazardous operations: both arms take
			// the same transform, while the proportion cannot exceed one.
			hazardous = append(hazardous, boundary.Counts{Units: l.hazardous + l.arrivals, Count: l.hazardous,
				BaselineUnits: r.hazardous + r.arrivals, BaselineCount: r.hazardous})
			for _, outcome := range histogramOutcomes(l.histogram, r.histogram) {
				if buckets, exists := l.histogram[outcome]; exists {
					histogram = append(histogram, Histogram{Interval: record.FormatTime(at), Outcome: outcome, Buckets: append([]int64(nil), buckets...), BaselineBuckets: append([]int64(nil), r.histogram[outcome]...)})
				} else {
					histogram = append(histogram, Histogram{Interval: record.FormatTime(at), Outcome: outcome, BaselineBuckets: append([]int64(nil), r.histogram[outcome]...)})
				}
			}
		}
		share, quantile := pooledLatencyBucketShare(histogram)
		quantities := map[gatepolicy.Quantity]boundary.Observed{
			gatepolicy.QuantityRequestRate:        {Intervals: request},
			gatepolicy.QuantityErrorRate:          {Intervals: errorsObserved},
			gatepolicy.QuantityLatency:            {Intervals: latency},
			gatepolicy.QuantityHazardousOperation: {Intervals: hazardous},
		}
		result = append(result, OperationSeries{Operation: operation, Quantities: quantities,
			LatencyBucketShare: share, LatencyQuantile: quantile,
			Histogram: histogram})
	}
	return result
}
