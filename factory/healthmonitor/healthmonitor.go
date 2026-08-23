package healthmonitor

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/boundary"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// Actor is who the health monitor's writes are made as: the watch window it opens and
// closes, the incident it raises, and the revert intent it takes in through intake.
var Actor = record.Actor{Kind: record.KindComponent, Name: "health_monitor"}

// Watching is the service one call is about: the record's id, the name — which the
// revert intent's statement is written with, a statement naming an id being one no
// human reads — and the production environment the health monitor exists in and nowhere
// else.
type Watching struct {
	ID            string
	Name          string
	EnvironmentID string
}

func (w Watching) validate() error {
	for _, required := range []struct{ what, value string }{
		{"id", w.ID}, {"name", w.Name}, {"production environment", w.EnvironmentID},
	} {
		if required.value == "" {
			return fmt.Errorf("%w: the service names no %s", ErrWatchingIncomplete, required.what)
		}
	}
	return nil
}

// Quantity is which build's counts a [Signal] is asked for, and which build's to
// read them against.
type Quantity struct {
	ServiceName string
	// BuildID is the build the release under watch put on the target. It is the
	// build and not the release, because a target reports the build it runs and a
	// release is the name that build has on master.
	BuildID string
	// BaselineBuildID is the build of the release a rollback would return to, and is
	// empty where there is none — which makes both exits unreachable and leaves the
	// window to end at its cap.
	BaselineBuildID string
}

// Signal is where the quantity comes from. It is an interface because what
// emits it is the software the factory wrote and where that lands is the
// substrate's arrangement: a health monitor that read a file would be a health
// monitor that only works on one kind of target.
type Signal interface {
	Read(ctx context.Context, q Quantity) (boundary.Observed, error)
}

// Rollback is what the health monitor asks for at the condemned exit. Every
// field of it is what the rollback's own deploy record will name.
type Rollback struct {
	ServiceID     string
	ServiceName   string
	EnvironmentID string
	// ToReleaseID and ToBuildID are the target: the release a rollback returns to
	// and the build to put back on the target.
	ToReleaseID string
	ToBuildID   string
	// CondemnedReleaseID is the release the comparison crossed against.
	CondemnedReleaseID string
	// SweptReleaseIDs is every release above the condemned one, which returning to
	// the target undoes as well. Master is linear, so this is not a choice.
	SweptReleaseIDs []string
	// Source is what called for the rollback, which is
	// [deploy.SourceHealthMonitorAtCondemned] every time this package asks.
	Source string
	// RevertIntentID is the intent the health monitor raised before it asked, which is
	// the one stored link from a rollback to the item that undoes it.
	RevertIntentID string
}

// Rollbacker performs one. It is an interface because reaching a deploy target is
// the deploy agent's and this package reaches none — the arrangement the merge
// queue already has for what it needs done to a repository.
type Rollbacker interface {
	// RollBack puts the target's build back on the target, writes the rollback's
	// deploy record, and advances the deploy of the condemned release and of every
	// release it swept to rolled back.
	RollBack(ctx context.Context, r Rollback) error
}

var (
	// ErrWatchingIncomplete is returned for a service the health monitor was not told
	// enough about.
	ErrWatchingIncomplete = errors.New("healthmonitor: the service is missing something every call needs")
	// ErrNoSignal is returned by [New] for a health monitor with no signal to read. A
	// health monitor that reads nothing measures nothing, and a window it opened would
	// never close except at its cap.
	ErrNoSignal = errors.New("healthmonitor: a health monitor with no signal to read measures nothing")
)

// HealthMonitor is the health monitor over one factory.
type HealthMonitor struct {
	pool       *pgxpool.Pool
	windows    *window.Writer
	incidents  *incident.Writer
	intake     *intent.Intake
	policy     *policy.Reader
	notifier   *notifier.Notifier
	signal     Signal
	rollbacker Rollbacker
}

// New returns the health monitor over pool, writing windows, incidents, and the revert
// intent through the writers it is given, reading what is in force through the
// policy, telling humans through the notifier, reading the quantity through signal,
// and asking for a rollback through rollbacker.
//
// A nil rollbacker is allowed and a nil signal is not. A factory whose deploy agent
// cannot roll back still has to watch — the window closing cleared or timing out is
// most of what watching does — and a condemned exit there closes the window, writes the
// incident, and raises the revert intent without a rollback, which is exactly what
// the design does where a condemned exit finds no release to return to.
func New(pool *pgxpool.Pool, windows *window.Writer, incidents *incident.Writer,
	intake *intent.Intake, p *policy.Reader, n *notifier.Notifier,
	signal Signal, rollbacker Rollbacker) (*HealthMonitor, error) {
	if signal == nil {
		return nil, ErrNoSignal
	}
	return &HealthMonitor{
		pool: pool, windows: windows, incidents: incidents, intake: intake,
		policy: p, notifier: n, signal: signal, rollbacker: rollbacker,
	}, nil
}
