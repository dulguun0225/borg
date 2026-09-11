package driftdetector

import (
	"slices"

	"github.com/dulguun0225/borg/factory/lastcheck"
)

// ServiceOnTargets is one unretired service, the production environment it
// runs in, and the targets of that environment it runs on, as [Holds] reads
// it. The caller assembles the whole set — every unretired service, its
// production environment's id, and the targets it runs on there — because
// which services exist and where they run is the factory's own record and
// not this package's.
type ServiceOnTargets struct {
	ServiceID     string
	EnvironmentID string
	Targets       []string
}

// StaleHold is one service a stopped component's mismatch holds. A StaleHold
// naming no service is the mismatch that holds nothing, and its page is the
// whole of it.
type StaleHold struct {
	ServiceID string
}

// Holds is what a stopped component's mismatch holds, over c — the stale last
// check that raised it — and running, the services and their production
// environments the caller assembles. The health monitor keeps one last check
// per service, so its subject is the service and it holds that service's
// production deploys. The deployer keeps one per persistent target, so its
// subject is a target address, and what it holds is every unretired service
// of the environment that target belongs to — not only the service running on
// that one target — because the deployer having stopped is a fact about the
// whole environment it deploys into and not about one target of it alone.
// Every other component reaches no deploy, so its mismatch holds nothing —
// one [StaleHold] naming no service, and the page is the whole of it.
func Holds(c lastcheck.LastCheck, running []ServiceOnTargets) []StaleHold {
	if c.Component == lastcheck.ComponentHealthMonitor {
		return []StaleHold{{ServiceID: c.Subject}}
	}
	if c.Component != lastcheck.ComponentDeployer {
		return []StaleHold{{}}
	}
	var environmentID string
	for _, s := range running {
		if slices.Contains(s.Targets, c.Subject) {
			environmentID = s.EnvironmentID
			break
		}
	}
	if environmentID == "" {
		// A target no service runs on: the deployer stopped all the same, so
		// the mismatch stands and pages, holding nothing.
		return []StaleHold{{}}
	}
	var holding []StaleHold
	for _, s := range running {
		if s.EnvironmentID == environmentID {
			holding = append(holding, StaleHold{ServiceID: s.ServiceID})
		}
	}
	return holding
}

// MustDeliver is the third comparison's own delivery decision: the notifier's
// own staleness, and every factory last check stale at once, have no carrier
// inside the factory — a mismatch about the notifier would be delivered by the
// notifier — so for those two the detector delivers to its own address
// instead. deliver is false, and why empty, for every other case: those are
// raised as a mismatch by [Holds] and [Writer.RaiseStaleComponent] instead.
func MustDeliver(all, stale []lastcheck.LastCheck) (why string, deliver bool) {
	if len(all) > 0 && len(stale) == len(all) {
		return "every factory last check is stale at once; the whole process has stopped", true
	}
	for _, c := range stale {
		if c.Component == lastcheck.ComponentNotifier {
			return "the notifier's own last check is stale", true
		}
	}
	return "", false
}
