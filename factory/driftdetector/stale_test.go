// stale_test.go is the third comparison's own bound, beside exemption_test.go
// the other file in this package that touches no database: [driftdetector.Holds]
// and [driftdetector.MustDeliver] are pure code over plain inputs.
package driftdetector_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/lastcheck"
)

func TestHolds(t *testing.T) {
	running := []driftdetector.ServiceOnTargets{
		{ServiceID: "sv_a", EnvironmentID: "en_1", Targets: []string{"t1", "t2"}},
		{ServiceID: "sv_b", EnvironmentID: "en_1", Targets: []string{"t2"}},
		{ServiceID: "sv_c", EnvironmentID: "en_2", Targets: []string{"t3"}},
	}

	tests := []struct {
		name string
		c    lastcheck.LastCheck
		want []driftdetector.StaleHold
	}{
		{
			name: "the health monitor holds its own subject service",
			c:    lastcheck.LastCheck{Component: lastcheck.ComponentHealthMonitor, Subject: "sv_a"},
			want: []driftdetector.StaleHold{{ServiceID: "sv_a"}},
		},
		{
			name: "the deployer holds every unretired service of the environment holding the subject target, not only the one running on it",
			c:    lastcheck.LastCheck{Component: lastcheck.ComponentDeployer, Subject: "t1"},
			want: []driftdetector.StaleHold{{ServiceID: "sv_a"}, {ServiceID: "sv_b"}},
		},
		{
			name: "the deployer over a target two services of the same environment share still holds both, once each",
			c:    lastcheck.LastCheck{Component: lastcheck.ComponentDeployer, Subject: "t2"},
			want: []driftdetector.StaleHold{{ServiceID: "sv_a"}, {ServiceID: "sv_b"}},
		},
		{
			name: "the deployer over a target of another environment holds that environment's own service and no other",
			c:    lastcheck.LastCheck{Component: lastcheck.ComponentDeployer, Subject: "t3"},
			want: []driftdetector.StaleHold{{ServiceID: "sv_c"}},
		},
		{
			name: "the deployer over a target no service runs on holds nothing, one row naming no service",
			c:    lastcheck.LastCheck{Component: lastcheck.ComponentDeployer, Subject: "t9"},
			want: []driftdetector.StaleHold{{}},
		},
		{
			name: "every other component holds nothing, one row naming no service",
			c:    lastcheck.LastCheck{Component: lastcheck.ComponentNotifier, Subject: "notifier"},
			want: []driftdetector.StaleHold{{}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := driftdetector.Holds(tt.c, running)
			if len(got) != len(tt.want) {
				t.Fatalf("Holds() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("Holds()[%d] = %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestMustDeliver(t *testing.T) {
	notifierStale := lastcheck.LastCheck{Component: lastcheck.ComponentNotifier}
	deployerStale := lastcheck.LastCheck{Component: lastcheck.ComponentDeployer}
	all := []lastcheck.LastCheck{notifierStale, deployerStale}

	tests := []struct {
		name        string
		all, stale  []lastcheck.LastCheck
		wantDeliver bool
		wantWhy     string
	}{
		{
			name:        "the notifier's own last check is stale and nothing else is",
			all:         all,
			stale:       []lastcheck.LastCheck{notifierStale},
			wantDeliver: true,
			wantWhy:     "the notifier's own last check is stale",
		},
		{
			name:        "every factory last check is stale at once",
			all:         all,
			stale:       []lastcheck.LastCheck{notifierStale, deployerStale},
			wantDeliver: true,
			wantWhy:     "every factory last check is stale at once; the whole process has stopped",
		},
		{
			name:        "a stale component that is not the notifier, with others still fresh, delivers nothing",
			all:         all,
			stale:       []lastcheck.LastCheck{deployerStale},
			wantDeliver: false,
		},
		{
			name:        "nothing stale delivers nothing",
			all:         all,
			stale:       nil,
			wantDeliver: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			why, deliver := driftdetector.MustDeliver(tt.all, tt.stale)
			if deliver != tt.wantDeliver {
				t.Errorf("MustDeliver() deliver = %v, want %v", deliver, tt.wantDeliver)
			}
			if deliver && why != tt.wantWhy {
				t.Errorf("MustDeliver() why = %q, want %q", why, tt.wantWhy)
			}
		})
	}
}
