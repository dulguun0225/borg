package healthmonitor

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Spent reads arrivals and completions from the same files as Read and computes
// the objective's good work over the requested acceptance-time period.
func (f *FileEmission) Spent(_ context.Context, service string, period time.Duration) ([]Spend, error) {
	files, err := signalFiles(f.dir)
	if err != nil {
		return nil, err
	}
	type totals struct {
		units, good    int64
		reachesCutoff  bool
		oldest, newest time.Time
	}
	byOperation := map[string]*totals{}
	cutoff := time.Now().Add(-period)
	for _, path := range files {
		read, err := readEmission(path)
		if err != nil {
			return nil, err
		}
		for _, one := range read.records {
			if one.Service != service {
				continue
			}
			op := one.Operation
			if op == "" {
				op = PooledOperation
			}
			value := byOperation[op]
			if value == nil {
				value = &totals{}
				byOperation[op] = value
			}
			if !one.acceptedAt.After(cutoff) {
				value.reachesCutoff = true
				continue
			}
			if value.oldest.IsZero() || one.acceptedAt.Before(value.oldest) {
				value.oldest = one.acceptedAt
			}
			if one.acceptedAt.After(value.newest) {
				value.newest = one.acceptedAt
			}
			switch one.Kind {
			case RecordArrival:
				value.units++
			case RecordCompletion:
				if !completionFailed(one) {
					value.good++
				}
			}
		}
	}
	result := make([]Spend, 0, len(byOperation))
	for operation, value := range byOperation {
		result = append(result, Spend{Operation: operation, Units: value.units,
			Good: value.good, Covered: value.reachesCutoff})
	}
	slices.SortFunc(result, func(a, b Spend) int { return strings.Compare(a.Operation, b.Operation) })
	return result, nil
}

func signalFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, entry := range entries {
		if entry.IsDir() {
			nested, err := signalFiles(filepath.Join(dir, entry.Name()))
			if err == nil {
				files = append(files, nested...)
			}
			continue
		}
		if filepath.Ext(entry.Name()) == ".signal" {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}
