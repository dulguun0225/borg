package driftdetector

import (
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/record"
)

// OpenWindow is one analysis window's own account of what it excuses, as
// [Excused] reads it. The caller assembles one per window an
// [window.AllOpen] read of the service returns.
type OpenWindow struct {
	// OpenedAt is the window's own At, in [record.TimeLayout] — the format
	// every record in the graph carries its timestamp in, which is why the
	// caller hands over the stored string rather than a parsed time.
	OpenedAt   string
	CapSeconds float64
	// Builds is the build under watch: the release's build, or the search's
	// build where the window names a build and no release.
	Builds []string
	// Targets is the window's own deploy's targets.
	Targets []WindowTarget
}

// WindowTarget is one target of an [OpenWindow]'s own deploy.
type WindowTarget struct {
	Address string
	// Complete is whether the deploy record marks this target complete.
	Complete bool
	// ControlBuildID is the build the control release on this target runs,
	// and empty where the target runs no control.
	ControlBuildID string
	// KeptBuildID is the build of the release a rollback of the window's own
	// release would return to on this target — the control release where a
	// control ran, otherwise the release current before the deploy — and
	// empty where the target's kept instances are none or torn down, which
	// the caller assembling it guards: a rollback needs instances there to
	// return to, and a target with none is no different from one running
	// nothing.
	KeptBuildID string
}

// Excused reports whether runningBuild on target is a build one of windows
// accounts for, at now: a window past its cap ([OpenWindow.CapSeconds])
// excuses nothing; a target [WindowTarget.Complete] is never excused; on an
// uncompleted target, runningBuild is excused where it is one of the
// window's own [OpenWindow.Builds], that target's
// [WindowTarget.ControlBuildID], or that target's [WindowTarget.KeptBuildID]
// — the release a rollback of the watched release would return to; and
// where deployer is not nil and reports stale at now, nothing is excused —
// a nil deployer, meaning no last check recorded yet, is not staleness.
func Excused(windows []OpenWindow, target, runningBuild string, deployer *lastcheck.LastCheck, now time.Time) bool {
	if deployer != nil {
		if stale, err := deployer.Stale(now); err == nil && stale {
			return false
		}
	}
	for _, w := range windows {
		opened, err := record.ParseTime(w.OpenedAt)
		if err != nil || now.Sub(opened).Seconds() >= w.CapSeconds {
			continue
		}
		for _, t := range w.Targets {
			if t.Address != target || t.Complete {
				continue
			}
			if slices.Contains(w.Builds, runningBuild) {
				return true
			}
			if t.ControlBuildID != "" && t.ControlBuildID == runningBuild {
				return true
			}
			if t.KeptBuildID != "" && t.KeptBuildID == runningBuild {
				return true
			}
		}
	}
	return false
}
