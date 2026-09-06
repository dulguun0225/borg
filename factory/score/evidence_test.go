// The evidence these tests are written over. Each is a graph assembled here and
// indexed the way ReadEvidence indexes one.
package score

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// closes is n windows of one service, closed at one exit, one second apart.
func closes(n int, exit window.Exit) []window.Window {
	var windows []window.Window
	for i := range n {
		windows = append(windows, window.Window{
			ID:        fmt.Sprintf("win_%d", i),
			ServiceID: "svc_a",
			ReleaseID: fmt.Sprintf("rel_svc_a_%d", i),
			Exit:      exit,
			At:        record.FormatTime(time.Date(2026, 8, 20, 0, 0, i, 0, time.UTC)),
			ClosedAt:  record.FormatTime(time.Date(2026, 8, 20, 0, 0, i, 0, time.UTC)),
		})
	}
	return windows
}

// evidenceFor is one service's windows and extra history. The extra events are
// rollbacks, which are the only history a rule folds beside a window.
func evidenceFor(serviceID string, windows []window.Window, extra []serviceEvent) *Evidence {
	e := newEvidence()
	e.windows = windows
	for i, event := range extra {
		if !event.undidMoreThanItsTarget {
			continue
		}
		e.rollbacks = append(e.rollbacks, deploy.Deploy{
			ID: fmt.Sprintf("dep_%d", i), ServiceID: serviceID, At: event.at,
			Undoing: deploy.Undoing{
				FailedReleaseID:   fmt.Sprintf("rel_%s_failed_%d", serviceID, i),
				SkippedReleaseIDs: []string{fmt.Sprintf("rel_%s_skipped_%d", serviceID, i)},
				Source:            deploy.SourceHealthMonitorAtFailed,
			},
		})
	}
	e.index()
	return e
}

// undidMoreThanItsTarget is one rollback event at a time the fold reads after
// the windows above it.
func undidMoreThanItsTarget() []serviceEvent {
	return []serviceEvent{{
		at:                     record.FormatTime(time.Date(2026, 8, 20, 0, 0, 9, 0, time.UTC)),
		undidMoreThanItsTarget: true,
	}}
}

// withIncident raises an incident against one release, which is what makes a
// window that closed over it a miss.
func withIncident(e *Evidence, releaseID string) *Evidence {
	e.incidents = append(e.incidents, incident.Incident{ID: "inc_a", ReleaseID: releaseID, ServiceID: "svc_a"})
	e.index()
	return e
}

// withIncidentOn raises an incident against one release that names the quantity
// its reading crossed on, which is what makes a miss move that quantity's size
// and no other's.
func withIncidentOn(e *Evidence, releaseID string, quantity gatepolicy.Quantity) *Evidence {
	e.incidents = append(e.incidents, incident.Incident{
		ID: "inc_" + string(quantity), ReleaseID: releaseID, ServiceID: "svc_a",
		Quantity: string(quantity),
	})
	e.index()
	return e
}

// withFinestSizePerWindow puts a finest size per quantity on the window at each
// index, newest last, which is what a run read per quantity is held against.
func withFinestSizePerWindow(e *Evidence, reached []map[gatepolicy.Quantity]float64) *Evidence {
	for i := range e.windows {
		if i < len(reached) {
			e.windows[i].FinestSizeReached = reached[i]
		}
	}
	e.index()
	return e
}

// withFinestSize puts on every window of the evidence the finest size the
// window reports this service's traffic reached, which is what the size in force
// is held against.
func withFinestSize(e *Evidence, reached float64) *Evidence {
	for i := range e.windows {
		e.windows[i].FinestSizeReached = map[gatepolicy.Quantity]float64{}
		for _, quantity := range gatepolicy.Quantities {
			e.windows[i].FinestSizeReached[quantity] = reached
		}
	}
	e.index()
	return e
}

// withMark marks the rollback of one release as not caused by the release,
// which excludes it from every rule that learns.
func withMark(e *Evidence, releaseID string) *Evidence {
	e.marked[releaseID] = true
	e.index()
	return e
}

// autoPassed is one firing the factory closed itself, at a number, by one of the
// two things an auto-pass comes from.
func autoPassed(row string, number float64, by, itemID string) Firing {
	return Firing{
		OpenEvent: OpenEvent{
			ItemID: itemID, Gate: row, FactorSet: SetWithABuild, Number: number, Threshold: 0.3,
			HeldOut: by == AutoPassSample, HeldOutRate: StartingHeldOutSampleRate,
		},
		CloseEvent: CloseEvent{Verdict: VerdictApproved, WhyItAutoPassed: by},
	}
}

// humanApproved is one firing a human approved at a gate the number gated.
func humanApproved(row string, number float64, itemID string) Firing {
	return Firing{
		OpenEvent:   OpenEvent{ItemID: itemID, Gate: row, FactorSet: SetWithABuild, Number: number, Threshold: 0.3},
		CloseEvent:  CloseEvent{Verdict: VerdictApproved},
		HumanClosed: true,
	}
}

// firingEvidence is a graph where every named item shipped and its window closed
// passed, so each turned out well unless a later call fails it.
func firingEvidence(t *testing.T, firings []Firing) *Evidence {
	t.Helper()
	e := newEvidence()
	e.firings = firings
	for i, f := range firings {
		if f.OpenEvent.ItemID == "" {
			continue
		}
		releaseID := "rel_" + f.OpenEvent.ItemID
		e.items = append(e.items, item.Item{ID: f.OpenEvent.ItemID, ServiceID: "svc_a", Stage: item.StageMerged})
		e.releases = append(e.releases, release.Release{
			ID: releaseID, ItemID: f.OpenEvent.ItemID, ServiceID: "svc_a", Number: int64(i + 1),
		})
		e.windows = append(e.windows, window.Window{
			ID: "win_" + f.OpenEvent.ItemID, ServiceID: "svc_a", ReleaseID: releaseID,
			Exit:     window.ExitPassed,
			At:       record.FormatTime(time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)),
			ClosedAt: record.FormatTime(time.Date(2026, 8, 20, 0, 0, 1, 0, time.UTC)),
		})
	}
	e.index()
	return e
}

// exitOf sets the exit of the window over one item's release, which is how a
// test says what became of a held-out release.
func exitOf(e *Evidence, itemID string, exit window.Exit) *Evidence {
	for i := range e.windows {
		if e.windows[i].ReleaseID == "rel_"+itemID {
			e.windows[i].Exit = exit
		}
	}
	e.index()
	return e
}

// fail makes one item's release the release a rollback failed, which is what
// makes that item's outcome badly.
func fail(e *Evidence, itemID string) *Evidence {
	e.rollbacks = append(e.rollbacks, deploy.Deploy{
		ID: "dep_rollback", ServiceID: "svc_a", At: record.FormatTime(time.Date(2026, 8, 20, 1, 0, 0, 0, time.UTC)),
		Undoing: deploy.Undoing{FailedReleaseID: "rel_" + itemID, Source: deploy.SourceHealthMonitorAtFailed},
	})
	e.index()
	return e
}

// someEvidence is a graph with a little of every kind of outcome in it, which is
// what the idempotence test needs: a table with rows in it says more about two
// passes agreeing than a table of starting values does.
func someEvidence(t *testing.T) *Evidence {
	t.Helper()
	e := firingEvidence(t, []Firing{
		autoPassed("merge_to_master", 0.9, AutoPassSample, "it_a"),
		autoPassed("merge_to_master", 0.9, AutoPassSample, "it_b"),
		autoPassed("merge_to_master", 0.9, AutoPassSample, "it_c"),
		autoPassed("deploy_to_production", 0.2, AutoPassThreshold, "it_bad"),
	})
	e.items = append(e.items, item.Item{ID: "it_stalled", AreaID: "ar_a", Stage: item.StageImplementation})
	e.stages = append(e.stages,
		item.StageTotals{ItemID: "it_stalled", Stage: item.StageImplementation, Attempts: 3},
		item.StageTotals{ItemID: "it_a", Stage: item.StageSpec, Attempts: 2})
	for i := range e.items {
		e.items[i].AreaID = "ar_a"
	}
	e = fail(e, "it_bad")
	e = withIncident(e, "rel_it_a")
	return e
}

// valueOf is what the pass supplies for one parameter on one subject.
func valueOf(t *testing.T, e *Evidence, parameter gatepolicy.Parameter, subject string) float64 {
	t.Helper()
	learned, err := LearnFrom(e)
	if err != nil {
		t.Fatalf("LearnFrom: %v", err)
	}
	supplied, found := learned.Supplied.Value(parameter, subject)
	if !found {
		t.Fatalf("the score supplies no %s", parameter)
	}
	return supplied.Value
}

// rowFor is the supplied row for one parameter and subject, or nil where the pass
// moved nothing for it.
func rowFor(t *testing.T, e *Evidence, parameter gatepolicy.Parameter, subject string) *Supplied {
	t.Helper()
	learned, err := LearnFrom(e)
	if err != nil {
		t.Fatalf("LearnFrom: %v", err)
	}
	for _, row := range learned.Supplied {
		if row.Parameter == parameter && row.Subject == subject {
			return &row
		}
	}
	return nil
}

func encode(t *testing.T, learned Learned) string {
	t.Helper()
	text, err := json.Marshal(learned)
	if err != nil {
		t.Fatalf("encoding the table: %v", err)
	}
	return string(text)
}

// near is float comparison for a value the rules arrive at by arithmetic on
// bands. A band below 0.20 is 0.15 and reads as 0.15000000000000002, which is
// binary floating point and not a rule that missed.
func near(got, want float64) bool { return got-want < 1e-9 && want-got < 1e-9 }
