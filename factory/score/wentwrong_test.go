package score

import (
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/window"
)

// TestWentWrongOnlyCountsARollbackTraceableToTheHealthMonitor: the window's own
// size and power learn from a rollback the health monitor called for and from a
// human's undo of a release whose window the health monitor closed failed —
// the trail to the monitor — and from nothing else. A human's undo of a release
// for a reason of their own, with no failed window behind it, says nothing
// about what the monitor should have caught, and a marked rollback never
// counts, whatever its source.
func TestWentWrongOnlyCountsARollbackTraceableToTheHealthMonitor(t *testing.T) {
	windowOf := func(releaseID string, exit window.Exit) window.Window {
		return window.Window{ID: "win_" + releaseID, ServiceID: "svc_a", ReleaseID: releaseID, Exit: exit}
	}

	for _, c := range []struct {
		what     string
		windows  []window.Window
		rollback deploy.Deploy
		marked   bool
		want     bool
	}{
		{
			what:     "the health monitor's own rollback at the failed exit",
			windows:  []window.Window{windowOf("rel_1", window.ExitFailed)},
			rollback: deploy.Deploy{Undoing: deploy.Undoing{FailedReleaseID: "rel_1", Source: deploy.SourceHealthMonitorAtFailed}},
			want:     true,
		},
		{
			what:     "a human's undo of a release whose window the health monitor closed failed",
			windows:  []window.Window{windowOf("rel_1", window.ExitFailed)},
			rollback: deploy.Deploy{Undoing: deploy.Undoing{FailedReleaseID: "rel_1", Source: deploy.SourceOfHuman("person:ops", "confirming the monitor's own finding")}},
			want:     true,
		},
		{
			what:     "a human's undo for a reason of their own, with no failed window behind it",
			windows:  []window.Window{windowOf("rel_1", window.ExitPassed)},
			rollback: deploy.Deploy{Undoing: deploy.Undoing{FailedReleaseID: "rel_1", Source: deploy.SourceOfHuman("person:ops", "the feature was wrong")}},
			want:     false,
		},
		{
			what:     "a human's undo of a release nothing ever watched",
			windows:  nil,
			rollback: deploy.Deploy{Undoing: deploy.Undoing{FailedReleaseID: "rel_1", Source: deploy.SourceOfHuman("person:ops", "the feature was wrong")}},
			want:     false,
		},
		{
			what:     "the health monitor's own rollback, marked as not caused by the release",
			windows:  []window.Window{windowOf("rel_1", window.ExitFailed)},
			rollback: deploy.Deploy{Undoing: deploy.Undoing{FailedReleaseID: "rel_1", Source: deploy.SourceHealthMonitorAtFailed}},
			marked:   true,
			want:     false,
		},
	} {
		e := newEvidence()
		e.windows = c.windows
		e.rollbacks = []deploy.Deploy{c.rollback}
		if c.marked {
			e.marked["rel_1"] = true
		}
		e.index()
		if got := e.wentWrongOn("rel_1"); got != c.want {
			t.Errorf("%s: wentWrongOn = %v, want %v", c.what, got, c.want)
		}
	}
}
