package healthmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
)

// Shape returns the version carried by the selected build's records.
func (f *FileEmission) Shape(ctx context.Context, a Arm) (string, error) {
	version, err := f.shapeAtTarget(ctx, f.dir, a)
	if err != nil || version != "" {
		return version, err
	}
	entries, err := os.ReadDir(f.dir)
	if err != nil {
		return "", nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		version, err = f.shapeAtTarget(ctx, filepath.Join(f.dir, entry.Name()), a)
		if err != nil || version != "" {
			return version, err
		}
	}
	return "", nil
}

func (f *FileEmission) shapeAtTarget(_ context.Context, target string, a Arm) (string, error) {
	if f.path == nil {
		return "", errors.New("healthmonitor: file emission has no signal path")
	}
	path := f.path(target, a.BuildID)
	read, err := readEmission(path)
	if err != nil {
		return "", err
	}
	return read.version, nil
}

// Histogram is the fixed-boundary count kept for one interval and operation.
type Histogram struct {
	Interval        string
	Outcome         string
	Buckets         []int64
	BaselineBuckets []int64
}

func indexed(values []intervalData) map[time.Time]intervalData {
	result := map[time.Time]intervalData{}
	for _, value := range values {
		result[value.at] = value
	}
	return result
}

func alignIntervals(left, right map[time.Time]intervalData) map[time.Time]intervalData {
	// History pairs records without a timestamp by rank, retaining the newest
	// common run when the two histories have different lengths.
	if len(left) == 0 || len(right) == 0 {
		return right
	}
	leftTimes := make([]time.Time, 0, len(left))
	for at := range left {
		leftTimes = append(leftTimes, at)
	}
	slices.SortFunc(leftTimes, func(a, b time.Time) int { return a.Compare(b) })
	rightValues := make([]intervalData, 0, len(right))
	for _, value := range right {
		rightValues = append(rightValues, value)
	}
	slices.SortFunc(rightValues, func(a, b intervalData) int { return a.at.Compare(b.at) })
	if len(rightValues) > len(leftTimes) {
		rightValues = rightValues[len(rightValues)-len(leftTimes):]
	}
	if len(leftTimes) > len(rightValues) {
		leftTimes = leftTimes[len(leftTimes)-len(rightValues):]
	}
	aligned := map[time.Time]intervalData{}
	for n, value := range rightValues {
		if n >= len(leftTimes) {
			aligned[value.at] = value
			continue
		}
		value.at = leftTimes[n]
		aligned[value.at] = value
	}
	return aligned
}

func histogramOutcomes(left, right map[string][]int64) []string {
	set := map[string]bool{}
	for outcome := range left {
		set[outcome] = true
	}
	for outcome := range right {
		set[outcome] = true
	}
	result := make([]string, 0, len(set))
	for outcome := range set {
		result = append(result, outcome)
	}
	slices.Sort(result)
	return result
}

func latencyBucket(left, right intervalData) int {
	combined := make([]int64, len(histogramBoundaries)+1)
	for _, data := range []intervalData{left, right} {
		for _, buckets := range data.histogram {
			for n, count := range buckets {
				combined[n] += count
			}
		}
	}
	return latencyBucketFromCounts(combined)
}

func latencyBucketFromCounts(combined []int64) int {
	total := int64(0)
	for _, count := range combined {
		total += count
	}
	if total == 0 {
		return -1
	}
	position := int(histogramQuantile * float64(total))
	if position < 1 {
		position = 1
	}
	var cumulative int64
	for n, count := range combined {
		cumulative += count
		if cumulative >= int64(position) {
			return n
		}
	}
	return len(combined) - 1
}

func pooledLatencyBucketShare(histogram []Histogram) (float64, float64) {
	combined := make([]int64, len(histogramBoundaries)+1)
	for _, one := range histogram {
		for _, buckets := range [][]int64{one.Buckets, one.BaselineBuckets} {
			for n, count := range buckets {
				if n < len(combined) {
					combined[n] += count
				}
			}
		}
	}
	bucket := latencyBucketFromCounts(combined)
	if bucket < 0 {
		return 0, 0
	}
	var total int64
	for _, count := range combined {
		total += count
	}
	if total == 0 {
		return 0, 0
	}
	return float64(combined[bucket]) / float64(total), latencyBucketBoundary(bucket)
}

func latencyCount(data intervalData, bucket int) (boundary.Counts, float64) {
	if bucket < 0 {
		return boundary.Counts{}, 0
	}
	var total, tail int64
	for _, buckets := range data.histogram {
		for n, count := range buckets {
			total += count
			if n >= bucket {
				tail += count
			}
		}
	}
	return boundary.Counts{Units: total, Count: tail}, ownLatencyQuantile(data)
}

func ownLatencyQuantile(data intervalData) float64 {
	var total int64
	for _, buckets := range data.histogram {
		for _, count := range buckets {
			total += count
		}
	}
	if total == 0 {
		return 0
	}
	position := int(histogramQuantile * float64(total))
	if position < 1 {
		position = 1
	}
	var cumulative int64
	for bucket := 0; bucket <= len(histogramBoundaries); bucket++ {
		for _, buckets := range data.histogram {
			if bucket < len(buckets) {
				cumulative += buckets[bucket]
			}
		}
		if cumulative >= int64(position) {
			return latencyBucketBoundary(bucket)
		}
	}
	return latencyBucketBoundary(len(histogramBoundaries))
}

func latencyBucketBoundary(bucket int) float64 {
	if bucket < 0 {
		return 0
	}
	if bucket >= len(histogramBoundaries) {
		return histogramBoundaries[len(histogramBoundaries)-1]
	}
	return histogramBoundaries[bucket]
}
