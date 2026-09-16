package driftdetector_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/record"
)

func TestKeptFleetComparison(t *testing.T) {
	now := time.Now()
	window := []driftdetector.OpenWindow{{
		OpenedAt: record.FormatTime(now.Add(-time.Minute)), CapSeconds: 3600,
		Targets: []driftdetector.WindowTarget{{
			Address: "target-a", DeployID: "dep-1", KeptBuildID: "build-kept", KeptInstances: 3,
		}},
	}}

	tests := []struct {
		name      string
		actual    int
		mitigated bool
		agrees    bool
		expected  int
	}{
		{name: "fewer instances is a mismatch", actual: 2, expected: 3},
		{name: "the kept fleet is present", actual: 3, agrees: true, expected: 3},
		{name: "the standing mitigation replaces the expected count", actual: 1, mitigated: true, agrees: true, expected: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mitigations := []driftdetector.InstanceCountMitigation{}
			if tt.mitigated {
				mitigations = append(mitigations, driftdetector.InstanceCountMitigation{
					Address: "target-a", DeployID: "dep-1", Count: 1,
				})
			}
			build, expected := driftdetector.InstanceCountExpectation(window, "target-a", mitigations, now)
			if build != "build-kept" || expected != tt.expected {
				t.Fatalf("InstanceCountExpectation() = %q, %d, want build-kept and %d", build, expected, tt.expected)
			}
			pass := driftdetector.Pass{
				Reached: true, RunningBuild: "build-served", RecordedBuildID: "build-served",
				RunningKeptInstances: tt.actual, RecordedKeptInstances: expected,
			}
			if pass.Agreed() != tt.agrees {
				t.Errorf("Agreed() = %v, want %v for %d instances against %d", pass.Agreed(), tt.agrees, tt.actual, expected)
			}
		})
	}
}

func TestInstanceCountExpectationUsesOnlyOpenIncompleteWindows(t *testing.T) {
	now := time.Now()
	windows := []driftdetector.OpenWindow{
		{OpenedAt: record.FormatTime(now.Add(-2 * time.Hour)), CapSeconds: 3600, Targets: []driftdetector.WindowTarget{
			{Address: "target-a", KeptBuildID: "expired", KeptInstances: 9},
		}},
		{OpenedAt: record.FormatTime(now.Add(-time.Minute)), CapSeconds: 3600, Targets: []driftdetector.WindowTarget{
			{Address: "target-a", Complete: true, KeptBuildID: "complete", KeptInstances: 8},
			{Address: "target-a", KeptBuildID: "open", KeptInstances: 3},
		}},
	}
	build, expected := driftdetector.InstanceCountExpectation(windows, "target-a", nil, now)
	if build != "open" || expected != 3 {
		t.Errorf("InstanceCountExpectation() = %q, %d, want open and 3", build, expected)
	}
}

func TestInstanceCountExpectationUsesTheMatchingMitigation(t *testing.T) {
	now := time.Now()
	windows := []driftdetector.OpenWindow{{
		OpenedAt: record.FormatTime(now.Add(-time.Minute)), CapSeconds: 3600,
		Targets: []driftdetector.WindowTarget{{
			Address: "target-a", DeployID: "dep-1", KeptBuildID: "build-kept", KeptInstances: 3,
		}},
	}}
	build, expected := driftdetector.InstanceCountExpectation(windows, "target-a", []driftdetector.InstanceCountMitigation{
		{Address: "target-a", DeployID: "dep-2", Count: 1},
	}, now)
	if build != "build-kept" || expected != 3 {
		t.Errorf("InstanceCountExpectation() = %q, %d, want build-kept and the deploy count 3", build, expected)
	}
}
