package driftdetector

import (
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// InstanceCountMitigation is a standing instance-count mitigation identified
// by the target and deploy record it changes.
type InstanceCountMitigation struct {
	Address  string
	DeployID string
	Count    int
}

// InstanceCountExpectation returns the kept build and largest effective count
// for target among windows still open at now. A standing instance-count
// mitigation replaces the deploy record's count.
func InstanceCountExpectation(windows []OpenWindow, target string, mitigations []InstanceCountMitigation, now time.Time) (string, int) {
	var build string
	var expected int
	found := false
	for _, window := range windows {
		if !windowIsOpen(window, now) {
			continue
		}
		for _, one := range window.Targets {
			if one.Address != target || one.Complete || one.KeptBuildID == "" {
				continue
			}
			count := one.KeptInstances
			if mitigation, found := instanceCountMitigation(mitigations, one.Address, one.DeployID); found {
				count = mitigation.Count
			}
			if !found || count > expected {
				build, expected, found = one.KeptBuildID, count, true
			}
		}
	}
	return build, expected
}

func windowIsOpen(window OpenWindow, now time.Time) bool {
	opened, err := record.ParseTime(window.OpenedAt)
	return err == nil && now.Sub(opened).Seconds() < window.CapSeconds
}

func instanceCountMitigation(mitigations []InstanceCountMitigation, address, deployID string) (InstanceCountMitigation, bool) {
	for _, mitigation := range mitigations {
		if mitigation.Address == address && mitigation.DeployID == deployID {
			return mitigation, true
		}
	}
	return InstanceCountMitigation{}, false
}

func instanceCountAgrees(expected, actual int) bool {
	return expected == 0 || actual >= expected
}
