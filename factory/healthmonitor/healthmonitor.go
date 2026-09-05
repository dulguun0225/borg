package healthmonitor

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/window"
)

// Actor is who the health monitor's writes are made as: the analysis window it
// opens and closes, the incident it raises, the revert intent it takes in
// through intake, and its own last check per service.
var Actor = record.Actor{Kind: record.KindComponent, Key: "health_monitor"}

// Watching is the service one call is about: the record's id, the name — which
// the revert intent's statement is written with, a statement naming an id being
// one no human reads — and the production environment the health monitor exists
// in and nowhere else.
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

// Arm is one side of a comparison: the build a set of instances runs and the
// deploy that placed them. The deploy is what tells the arms apart — the control
// and the long-lived instances of the build it runs are two sets of instances of
// one build on one target, and the build alone cannot separate them.
type Arm struct {
	BuildID  string
	DeployID string
}

// Named reports whether the arm names anything at all. An unnamed baseline arm
// is a release with no control and no release below it on the target.
func (a Arm) Named() bool { return a.BuildID != "" }

// Reading is which series one call asks the emission for: the two arms on one
// target, for one service. What comes back is per operation and per quantity.
type Reading struct {
	ServiceName string
	Target      string
	Release     Arm
	// Baseline is the control the deployer started beside the release under this
	// same deploy, or — where the strategy kept no control — the release below
	// this one on the target, which is the weak fallback. It is unnamed where
	// there is neither.
	Baseline Arm
}

// History is which series the reading against a service's own recent history
// asks for: one arm, measured against what that service was doing before that
// deploy rather than against instances running beside it. It runs whether or not
// a window is open and nothing tears it down.
type History struct {
	ServiceName string
	Target      string
	Of          Arm
}

// Spend is what one service consumed of its objective over one period: the work
// counted and how much of it was good. The error budget is what is left of the
// objective over that, and the burn rate is the share of the budget spent per
// hour.
type Spend struct {
	Units int64
	Good  int64
	// Covered is whether the store covers the whole period asked for. A period
	// the store does not cover leaves the budget uncomputed, and an uncomputed
	// budget holds the way an exhausted one does.
	Covered bool
}

// FailureRecord is one kept count of failures: how often a failure class was
// raised from one point in the code, for one service, build, deploy and target.
// The health monitor copies these onto an incident at the crossing, a field of
// it rather than a link to the store.
type FailureRecord struct {
	FailureClass string `json:"failure_class"`
	CodeLocation string `json:"code_location"`
	Target       string `json:"target"`
	BuildID      string `json:"build_id"`
	DeployID     string `json:"deploy_id"`
	Count        int64  `json:"count"`
}

// Emission is what the software the factory wrote emits and the store keeps. It
// is an interface because where that lands is the platform's arrangement: a
// health monitor that read a file would be a health monitor that only works on
// one kind of target.
type Emission interface {
	// Read is the kept series for both arms of one comparison on one target, per
	// operation and per quantity, at the emission version each arm was read at.
	Read(ctx context.Context, r Reading) (Series, error)
	// History is the same shape for one arm read against the service's own recent
	// history, which is what has no second arm to move with it.
	History(ctx context.Context, h History) (Series, error)
	// FailureRecords is what the store keeps of one service's failures for one
	// release and target, which the incident carries a copy of.
	FailureRecords(ctx context.Context, r Reading) ([]FailureRecord, error)
	// Spent is what the service consumed of its objective over the period, and
	// over the last hour where that is the period asked for.
	Spent(ctx context.Context, serviceName string, period time.Duration) (Spend, error)
	// Shape is the emission version the store's records for one arm carry, and
	// empty where the store holds none for it yet — which is what a window over a
	// deploy just written reads before any record has arrived.
	Shape(ctx context.Context, a Arm) (string, error)
}

// Control is one control: a set of instances running the build already in
// production, started alongside the release on one target and taking comparable
// traffic. It belongs to the deploy the rollout is part of and is not a deploy
// of the release it runs, and it mints no release number.
type Control struct {
	ServiceID     string
	ServiceName   string
	EnvironmentID string
	// DeployID is the release's own deploy, which the control is named on: the
	// build the control runs and the instances running it.
	DeployID string
	Target   string
	// BuildID is the build the control runs, which is the build of the release a
	// rollback of this one would return to and never the ordinal predecessor.
	BuildID string
}

// Rollback is what the health monitor asks the deployer for at the failed exit.
// Every field of it is what the rollback's own deploy record will name.
type Rollback struct {
	ServiceID     string
	ServiceName   string
	EnvironmentID string
	// ToReleaseID and ToBuildID are the target: the release a rollback returns to
	// and the build to put back on the target.
	ToReleaseID string
	ToBuildID   string
	// FailedReleaseID is the release the reading crossed against.
	FailedReleaseID string
	// SkippedReleaseIDs is every release above the failed one, which returning to
	// the target undoes as well. Master is linear, so this is not a choice.
	SkippedReleaseIDs []string
	// Source is what called for the rollback, which is
	// [deploy.SourceHealthMonitorAtFailed] every time this package asks.
	Source string
}

// SearchDeploy is a deploy the search calls for: a build nothing has watched,
// deployed with a control, whose record names the build and no release.
type SearchDeploy struct {
	ServiceID     string
	ServiceName   string
	EnvironmentID string
	BuildID       string
	// ControlBuildID is the build the control beside it runs, which is the
	// rollback's target — the release traffic returns to and the search never
	// tears down.
	ControlBuildID string
}

// Deployer performs what reaches a deploy target. It is an interface because
// reaching one is the deployer's and this package reaches none — the arrangement
// the merge queue already has for what it needs done to a repository.
type Deployer interface {
	// StartControl starts the control on one target, beside the release, and
	// names it on the release's own deploy record.
	StartControl(ctx context.Context, c Control) error
	// TearDownControl ends it. The health monitor calls this at passed and at
	// timed out and then closes the window; a rollback ends every control it
	// touches with the windows it closes.
	TearDownControl(ctx context.Context, c Control) error
	// RollBack puts the target's build back on the target, writes the rollback's
	// deploy record, and advances the deploy of the failed release and of every
	// release it skipped over to rolled back.
	RollBack(ctx context.Context, r Rollback) error
	// DeploySearch deploys a build the search made, with a control, and returns
	// the deploy record's id. The record names the build and no release, so the
	// service's current release stays the rollback's target throughout.
	DeploySearch(ctx context.Context, s SearchDeploy) (string, error)
}

// Builder makes the builds a search measures. It is an interface for the reason
// [Deployer] is: a build needs a repository, and this package reaches none.
type Builder interface {
	// BuildRevertOnto builds the revert's change applied onto the commit of one
	// release and returns the build's id. It returns false where the revert does
	// not apply onto that commit, which is a null result: it narrows the search no
	// further and marks nobody.
	BuildRevertOnto(ctx context.Context, serviceID, revertOfReleaseID, ontoReleaseID string) (string, bool, error)
}

// Pager is how a wait reaches a human. It is an interface rather than
// [notifier.Notifier] because what the health monitor needs of the notifier is
// one call, and a package that held the whole of it would be composed with
// everything the notifier is composed with.
type Pager interface {
	Notify(ctx context.Context, w notifier.Wait) ([]decisionlog.Row, error)
}

// Mismatches is the drift detector's own store, read through an interface
// because that store is not the factory's and no factory component may write it.
// While a mismatch stands on a service, no rollback is performed.
type Mismatches interface {
	Mismatch(ctx context.Context, serviceID string) (bool, string, error)
}

var (
	// ErrWatchingIncomplete is returned for a service the health monitor was not
	// told enough about.
	ErrWatchingIncomplete = errors.New("healthmonitor: the service is missing something every call needs")
	// ErrNoEmission is returned by [New] for a health monitor with nothing to
	// read. A health monitor that reads nothing measures nothing, and a window it
	// opened would never close except at its cap.
	ErrNoEmission = errors.New("healthmonitor: a health monitor with no emission to read measures nothing")
	// ErrScoreVersionMissing is returned by [Open] for an opening naming no score
	// version. The window stores the two versions in force at the open the way a
	// gate's open event does, and the score's is the caller's to hand over.
	ErrScoreVersionMissing = errors.New("healthmonitor: a window names the score version in force at the open")
	// ErrNoDeployer is returned by [HealthMonitor.Search] where the factory is
	// composed without one. The search deploys builds, so it cannot run without
	// the component that deploys.
	ErrNoDeployer = errors.New("healthmonitor: the search deploys builds, and this factory is composed with no deployer")
	// ErrSearchRefused is returned by [HealthMonitor.Search] where the service
	// cannot support one: a bisection over a signal that cannot conclude
	// terminates anyway and names an innocent release.
	ErrSearchRefused = errors.New("healthmonitor: this service's windows cannot close on evidence, so no search runs")
	// ErrSearchBudgetSpent is returned by [HealthMonitor.Search] where the
	// service's search budget is spent: each build the search deploys puts a build
	// that passed no gate in front of real traffic.
	ErrSearchBudgetSpent = errors.New("healthmonitor: the search budget is spent")
)

// Readings is what the two readings beside the comparison are read at, which the
// window copies at the open. The size and the run length of the own-history
// reading are authored on the service record beside the window's own parameters
// and supplied by the score where an owner authors neither; neither the field
// nor the supplied value is built, so the composition hands them over and
// doc.go says so.
type Readings struct {
	// OwnHistorySize is the smallest change in each quantity the reading against
	// the service's own recent history has to detect.
	OwnHistorySize map[gatepolicy.Quantity]float64
	// OwnHistoryRunLength is the mean number of intervals a service whose
	// behaviour has not changed runs before that reading crosses it once.
	OwnHistoryRunLength float64
	// ThresholdRunLength is the run length an explicit threshold is read at. The
	// threshold's own number is the service's objective, read off the service
	// record, and its size is that objective's distance from what the service is
	// doing.
	ThresholdRunLength float64
	// Interval is how long the store's own interval is, which is the unit the
	// boundary's variance is estimated over. It is fixed by the factory and
	// shipped with the instrumentation.
	Interval time.Duration
	// PassInterval is how often the health monitor's own pass runs, which is what
	// it writes onto its last check per service.
	PassInterval time.Duration
}

// HealthMonitor is the health monitor over one factory.
type HealthMonitor struct {
	pool       *pgxpool.Pool
	windows    *window.Writer
	incidents  *incident.Writer
	checks     *lastcheck.Writer
	intake     *intent.Intake
	policy     *policy.Reader
	pager      Pager
	emission   Emission
	deployer   Deployer
	builder    Builder
	mismatches Mismatches
	readings   Readings
}

// New returns the health monitor over pool, writing windows, incidents, its own
// last check and the revert intent through the writers it is given, reading what
// is in force through the policy, telling humans through the pager, reading the
// quantities through the emission, and reaching a target through the deployer.
//
// A nil emission is refused. A nil deployer, builder, pager or mismatches store
// is allowed: a factory whose deployer cannot roll back still has to watch — the
// window closing passed or timing out is most of what watching does — and a
// failed exit there closes the window, writes the incident, and raises the revert
// intent without a rollback, which is what the design does where a failed exit
// finds no release to return to. A nil mismatches store reads as no mismatch
// standing, which is what an install with no drift detector composed has.
func New(pool *pgxpool.Pool, windows *window.Writer, incidents *incident.Writer,
	checks *lastcheck.Writer, intake *intent.Intake, p *policy.Reader, pager Pager,
	emission Emission, deployer Deployer, builder Builder, mismatches Mismatches,
	readings Readings) (*HealthMonitor, error) {
	if emission == nil {
		return nil, ErrNoEmission
	}
	return &HealthMonitor{
		pool: pool, windows: windows, incidents: incidents, checks: checks, intake: intake,
		policy: p, pager: pager, emission: emission, deployer: deployer, builder: builder,
		mismatches: mismatches, readings: readings,
	}, nil
}
