// exemption_test.go is one of the two files in this package that touch no
// database at all, stale_test.go beside it: [driftdetector.Excused] is pure
// code over plain inputs, so its bounds are tested as arithmetic, the way
// package boundary's are.
package driftdetector_test

import (
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/record"
)

func TestExcused(t *testing.T) {
	now := time.Now()
	opened := record.FormatTime(now.Add(-time.Minute))

	notStale := &lastcheck.LastCheck{CheckedAt: record.FormatTime(now), Interval: time.Hour, LastPass: false}
	stale := &lastcheck.LastCheck{CheckedAt: record.FormatTime(now.Add(-2 * time.Hour)), Interval: time.Hour, LastPass: false}

	windowOver := func(target, releaseBuild, controlBuild string, complete bool) []driftdetector.OpenWindow {
		return []driftdetector.OpenWindow{{
			OpenedAt:   opened,
			CapSeconds: 3600,
			Builds:     []string{releaseBuild},
			Targets: []driftdetector.WindowTarget{{
				Address:        target,
				Complete:       complete,
				ControlBuildID: controlBuild,
			}},
		}}
	}

	windowOverKept := func(target, releaseBuild, keptBuild string, complete bool) []driftdetector.OpenWindow {
		return []driftdetector.OpenWindow{{
			OpenedAt:   opened,
			CapSeconds: 3600,
			Builds:     []string{releaseBuild},
			Targets: []driftdetector.WindowTarget{{
				Address:     target,
				Complete:    complete,
				KeptBuildID: keptBuild,
			}},
		}}
	}

	tests := []struct {
		name         string
		windows      []driftdetector.OpenWindow
		target       string
		runningBuild string
		deployer     *lastcheck.LastCheck
		want         bool
	}{
		{
			name:         "excused via the release build the window watches",
			windows:      windowOver("t1", "bl_release", "", false),
			target:       "t1",
			runningBuild: "bl_release",
			want:         true,
		},
		{
			name:         "excused via the control build the target's own row names",
			windows:      windowOver("t1", "bl_release", "bl_control", false),
			target:       "t1",
			runningBuild: "bl_control",
			want:         true,
		},
		{
			name:         "a target the deploy record marks complete is never excused",
			windows:      windowOver("t1", "bl_release", "", true),
			target:       "t1",
			runningBuild: "bl_release",
			want:         false,
		},
		{
			name: "a window open past its cap excuses nothing",
			windows: []driftdetector.OpenWindow{{
				OpenedAt:   record.FormatTime(now.Add(-2 * time.Hour)),
				CapSeconds: 3600,
				Builds:     []string{"bl_release"},
				Targets:    []driftdetector.WindowTarget{{Address: "t1"}},
			}},
			target:       "t1",
			runningBuild: "bl_release",
			want:         false,
		},
		{
			name:         "a stale deployer last check excuses nothing",
			windows:      windowOver("t1", "bl_release", "", false),
			target:       "t1",
			runningBuild: "bl_release",
			deployer:     stale,
			want:         false,
		},
		{
			name:         "a not-stale deployer last check excuses as before",
			windows:      windowOver("t1", "bl_release", "", false),
			target:       "t1",
			runningBuild: "bl_release",
			deployer:     notStale,
			want:         true,
		},
		{
			name:         "a nil deployer check, meaning none recorded yet, is not staleness",
			windows:      windowOver("t1", "bl_release", "", false),
			target:       "t1",
			runningBuild: "bl_release",
			deployer:     nil,
			want:         true,
		},
		{
			name:         "an unrelated build is not excused",
			windows:      windowOver("t1", "bl_release", "bl_control", false),
			target:       "t1",
			runningBuild: "bl_somebodyelses",
			want:         false,
		},
		{
			name:         "excused via the kept build the target's own row names — the release a rollback would return to",
			windows:      windowOverKept("t1", "bl_release", "bl_kept", false),
			target:       "t1",
			runningBuild: "bl_kept",
			want:         true,
		},
		{
			name:         "a target with no kept build to fall back on excuses nothing beyond the release and control",
			windows:      windowOverKept("t1", "bl_release", "", false),
			target:       "t1",
			runningBuild: "bl_kept",
			want:         false,
		},
		{
			name:         "a target the deploy record marks complete is never excused via the kept build either",
			windows:      windowOverKept("t1", "bl_release", "bl_kept", true),
			target:       "t1",
			runningBuild: "bl_kept",
			want:         false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := driftdetector.Excused(tt.windows, tt.target, tt.runningBuild, tt.deployer, now)
			if got != tt.want {
				t.Errorf("Excused() = %v, want %v", got, tt.want)
			}
		})
	}
}
