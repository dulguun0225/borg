package healthmonitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/record"
)

func (f *FileEmission) before(target, build string, arm emitted) ([]unit, error) {
	if arm.version != emissionVersionTimed || len(arm.units) == 0 {
		return nil, nil
	}
	last := arm.units[len(arm.units)-1].at
	entries, err := os.ReadDir(f.target(target))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("healthmonitor: reading %s: %w", f.target(target), err)
	}
	var past []unit
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".signal") || entry.Name() == filepath.Base(f.path(f.target(target), build)) {
			continue
		}
		read, err := readEmission(filepath.Join(f.target(target), entry.Name()))
		if err != nil {
			return nil, err
		}
		if read.version != emissionVersionTimed {
			continue
		}
		for _, one := range read.units {
			if (one.deploy == "" || one.deploy != armDeploy(arm)) && !one.at.After(last) {
				past = append(past, one)
			}
		}
	}
	slices.SortFunc(past, func(a, b unit) int { return a.at.Compare(b.at) })
	return past, nil
}

func (f *FileEmission) pastRecords(target, build string, arm emitted, against Arm) ([]EmissionRecord, error) {
	if against.BuildID != "" {
		read, err := f.emitted(target, against)
		if err != nil {
			return nil, err
		}
		last := newestTime(arm.records)
		past := make([]EmissionRecord, 0, len(read.records))
		for _, one := range read.records {
			if one.Version == emissionVersionRecord && !one.acceptedAt.After(last) {
				past = append(past, one)
			}
		}
		return past, nil
	}
	entries, err := os.ReadDir(f.target(target))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	last := newestTime(arm.records)
	deploy := armDeploy(arm)
	var past []EmissionRecord
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".signal") || entry.Name() == filepath.Base(f.path(f.target(target), build)) {
			continue
		}
		read, err := readEmission(filepath.Join(f.target(target), entry.Name()))
		if err != nil {
			return nil, err
		}
		for _, one := range read.records {
			if one.Version == emissionVersionRecord && (one.Deploy == "" || one.Deploy != deploy) && !one.acceptedAt.After(last) {
				past = append(past, one)
			}
		}
	}
	return past, nil
}

func (f *FileEmission) target(target string) string {
	if target == "" {
		return f.dir
	}
	return target
}

func readEmission(path string) (emitted, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return emitted{}, nil
	}
	if err != nil {
		return emitted{}, fmt.Errorf("healthmonitor: reading %s: %w", path, err)
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return emitted{}, fmt.Errorf("healthmonitor: reading emission %s: %w", path, err)
	}
	var result emitted
	lines := strings.Split(string(data), "\n")
	for n, raw := range lines {
		if n == len(lines)-1 && strings.TrimSpace(raw) != "" {
			break
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "{") {
			one, acceptedAt, legacy, deploy, err := decodeStoredRecord(line)
			if err != nil {
				return emitted{}, fmt.Errorf("healthmonitor: reading emission %s: %w", path, err)
			}
			if legacy != "" {
				parsed := parseLegacy(legacy, acceptedAt)
				parsed.deploy = deploy
				result.units = append(result.units, parsed)
				if strings.Contains(legacy, "\t") {
					result.version = emissionVersionTimed
				} else if result.version == "" {
					result.version = emissionVersion
				}
			} else {
				one.acceptedAt = acceptedAt
				if deploy != "" {
					one.Deploy = deploy
				}
				result.records = append(result.records, one)
				result.version = emissionVersionRecord
			}
			continue
		}
		one := parseLegacy(line, time.Time{})
		result.units = append(result.units, one)
		if result.version != emissionVersionRecord {
			if strings.Contains(line, "\t") {
				result.version = emissionVersionTimed
			} else if result.version == "" {
				result.version = emissionVersion
			}
		}
	}
	return result, nil
}

type wireRecord struct {
	EmissionRecord
	DurationValue json.RawMessage `json:"duration"`
}

type storedRecord struct {
	AcceptedAt time.Time       `json:"accepted_at"`
	Deploy     string          `json:"deploy"`
	Record     json.RawMessage `json:"record"`
}

func decodeStoredRecord(line string) (EmissionRecord, time.Time, string, string, error) {
	var stored storedRecord
	if err := json.Unmarshal([]byte(line), &stored); err != nil {
		return EmissionRecord{}, time.Time{}, "", "", err
	}
	if len(stored.Record) == 0 {
		one, err := decodeRecord(line)
		return one, time.Time{}, "", one.Deploy, err
	}
	if stored.AcceptedAt.IsZero() {
		return EmissionRecord{}, time.Time{}, "", "", errors.New("stored record needs an acceptance time")
	}
	if strings.HasPrefix(string(stored.Record), "{") {
		one, err := decodeRecord(string(stored.Record))
		if stored.Deploy != "" {
			one.Deploy = stored.Deploy
		}
		return one, stored.AcceptedAt, "", stored.Deploy, err
	}
	var legacy string
	if err := json.Unmarshal(stored.Record, &legacy); err != nil {
		return EmissionRecord{}, time.Time{}, "", "", fmt.Errorf("stored legacy record: %w", err)
	}
	return EmissionRecord{}, stored.AcceptedAt, legacy, stored.Deploy, nil
}

func decodeRecord(line string) (EmissionRecord, error) {
	var wire wireRecord
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		return EmissionRecord{}, err
	}
	if wire.Version != emissionVersionRecord {
		return EmissionRecord{}, fmt.Errorf("emission version %q is not readable", wire.Version)
	}
	if wire.Kind == "" || wire.Time.IsZero() {
		return EmissionRecord{}, errors.New("record needs a kind and time")
	}
	if len(wire.DurationValue) > 0 && string(wire.DurationValue) != "null" {
		var text string
		if json.Unmarshal(wire.DurationValue, &text) == nil {
			duration, err := time.ParseDuration(text)
			if err != nil {
				return EmissionRecord{}, fmt.Errorf("duration %q: %w", text, err)
			}
			wire.Duration = duration
		} else if err := json.Unmarshal(wire.DurationValue, &wire.Duration); err != nil {
			return EmissionRecord{}, fmt.Errorf("duration: %w", err)
		}
	}
	return wire.EmissionRecord, nil
}

func parseLegacy(line string, acceptedAt time.Time) unit {
	outcome := line
	if when, rest, tabbed := strings.Cut(line, "\t"); tabbed {
		if _, err := time.Parse(emissionTimeLayout, when); err == nil {
			outcome = rest
		}
	}
	return unit{at: acceptedAt, timed: !acceptedAt.IsZero(), failed: outcome == "error" || outcome == "failure"}
}

func armDeploy(arm emitted) string {
	for _, one := range arm.records {
		if one.Deploy != "" {
			return one.Deploy
		}
	}
	for _, one := range arm.units {
		if one.deploy != "" {
			return one.deploy
		}
	}
	return ""
}

func legacyIntervals(read emitted) []boundary.Counts {
	if read.version != emissionVersionTimed {
		counted := make([]boundary.Counts, 0, len(read.units))
		for _, one := range read.units {
			counted = append(counted, boundary.Counts{Units: 1, Count: failedCount(one.failed)})
		}
		return counted
	}
	var counted []boundary.Counts
	var current time.Time
	for _, one := range read.units {
		at := one.at.Truncate(intervalResolution)
		if len(counted) == 0 || !at.Equal(current) {
			counted = append(counted, boundary.Counts{})
			current = at
		}
		counted[len(counted)-1].Units++
		counted[len(counted)-1].Count += failedCount(one.failed)
	}
	return counted
}

func paired(arm, other []boundary.Counts) []boundary.Counts {
	if len(other) == 0 {
		return arm
	}
	counted := make([]boundary.Counts, 0, min(len(arm), len(other)))
	for n := range min(len(arm), len(other)) {
		counted = append(counted, boundary.Counts{Units: arm[n].Units, Count: arm[n].Count,
			BaselineUnits: other[n].Units, BaselineCount: other[n].Count})
	}
	return counted
}

func failedCount(failed bool) int64 {
	if failed {
		return 1
	}
	return 0
}

func newest(read emitted) string {
	if read.version == emissionVersionRecord {
		at := newestTime(read.records)
		if at.IsZero() {
			return ""
		}
		return record.FormatTime(at)
	}
	if read.version != emissionVersionTimed || len(read.units) == 0 {
		return ""
	}
	return record.FormatTime(read.units[len(read.units)-1].at)
}

func newestTime(records []EmissionRecord) time.Time {
	var latest time.Time
	for _, one := range records {
		if one.acceptedAt.After(latest) {
			latest = one.acceptedAt
		}
	}
	return latest
}

// newestServiceRecord is the newest accepted record the store holds for the
// service, not merely the newest record in the selected arm. A stopped arm in a
// no-control history comparison is still a current read while another release
// of the service is emitting, and that service-wide record is what the
// staleness rule on the health monitor's last check is about.
func (f *FileEmission) newestServiceRecord(target, service string) (string, error) {
	entries, err := os.ReadDir(f.target(target))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("healthmonitor: reading %s: %w", f.target(target), err)
	}
	var latest time.Time
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".signal") {
			continue
		}
		read, err := readEmission(filepath.Join(f.target(target), entry.Name()))
		if err != nil {
			return "", err
		}
		if read.version != emissionVersionRecord {
			continue
		}
		for _, one := range read.records {
			if service != "" && one.Service != service {
				continue
			}
			if one.acceptedAt.After(latest) {
				latest = one.acceptedAt
			}
		}
	}
	if latest.IsZero() {
		return "", nil
	}
	return record.FormatTime(latest), nil
}

func maxInt64(left, right int64) int64 {
	if left > right {
		return left
	}
	return right
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
