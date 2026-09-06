package healthmonitor

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// SearchBudget is what a search may spend before it stops: a maximum count of
// builds and a maximum total time production spends on them. Each build the
// search deploys puts a build that passed no gate in front of real traffic,
// which is what the budget bounds.
//
// It is authored on the service record beside the window's cap and supplied
// where an owner authors none, at what bisecting a batch of the backlog cap's
// size needs. Reading it off the record is [HealthMonitor.Search]'s own; a
// budget with no count and no time is one nothing bounds, which this package
// refuses rather than treating as unlimited.
type SearchBudget struct {
	Builds  int
	Seconds float64
}

// Search is one step of the search, and the whole of what one call does: the
// batch's failure is attributed to nothing, so a build at a time is made, put in
// front of traffic with a control, and measured by a window of its own.
//
// Named is the release the search has settled on, empty while the range is
// wider than one. NullResult is a build that could not be made because the
// revert does not apply onto that commit, which narrows the search no further
// and marks nobody. Opened is the window this step opened, and false where the
// step opened none.
type Search struct {
	OntoRelease release.Release
	BuildID     string
	DeployID    string
	Opened      window.Window
	OpenedOne   bool
	NullResult  bool
	Named       string
	// Remaining is how many releases of the batch the search has still to tell
	// apart, which is what says whether another step follows.
	Remaining int
}

// Search runs one step of the search over the batch one revert's deploy
// delivered, and returns what that step did. The batch is named by its deploy
// record: the releases it delivered are on that record, and the revert whose
// change each build applies is the release that deploy carried.
//
// The three limits are here and not in the caller. The search runs only where
// each of its windows can close on evidence — a bisection over a signal that
// cannot conclude terminates anyway and names an innocent release, which is
// [ErrSearchRefused]. It runs only while the budget holds, which is
// [ErrSearchBudgetSpent]. And it needs a deployer to put a build in front of
// traffic and a builder to make one, which is [ErrNoDeployer].
//
// The first step applies the revert onto the rollback's target and answers
// whether the revert itself is at fault; the rest bisect the batch by release
// number, the range narrowed by how each earlier step's window closed. A search
// deploy's record names the build and no release, so the service's current
// release stays the rollback's target throughout.
func (h *HealthMonitor) Search(ctx context.Context, w Watching, batchDeployID string, scoreVersion string) (Search, error) {
	if err := w.validate(); err != nil {
		return Search{}, err
	}
	if h.deployer == nil || h.builder == nil {
		return Search{}, ErrNoDeployer
	}
	batch, err := deploy.Get(ctx, h.pool, batchDeployID)
	if err != nil {
		return Search{}, err
	}
	svc, err := service.Get(ctx, h.pool, w.ID)
	if err != nil {
		return Search{}, err
	}
	if err := h.withinSearchBudget(ctx, w, svc); err != nil {
		return Search{}, err
	}

	target, hasTarget, err := h.searchTarget(ctx, w, batch)
	if err != nil || !hasTarget {
		return Search{}, err
	}
	onto, remaining, err := h.nextOnto(ctx, w, batch, target)
	if err != nil {
		return Search{}, err
	}
	found := Search{OntoRelease: onto, Remaining: remaining}
	if remaining <= 1 {
		found.Named = onto.ID
		return found, nil
	}

	built, applies, err := h.builder.BuildRevertOnto(ctx, w.ID, batch.ReleaseID, onto.ID)
	if err != nil {
		return found, fmt.Errorf("healthmonitor: building the revert onto %s: %w", onto.ID, err)
	}
	if !applies {
		// A null result is a result: it narrows the search no further and marks
		// nobody, a missing label slowing the score's learning where a wrong one
		// corrupts it.
		found.NullResult = true
		return found, nil
	}
	found.BuildID = built

	deployID, err := h.deployer.DeploySearch(ctx, SearchDeploy{
		ServiceID: w.ID, ServiceName: w.Name, EnvironmentID: w.EnvironmentID,
		BuildID: built, ControlBuildID: target.BuildID, OntoReleaseID: onto.ID,
	})
	if err != nil {
		return found, fmt.Errorf("healthmonitor: deploying the search's build of %s: %w", w.Name, err)
	}
	found.DeployID = deployID

	opened, err := h.openSearchWindow(ctx, w, svc, built, deployID, target, scoreVersion)
	if err != nil {
		return found, err
	}
	found.Opened, found.OpenedOne = opened, true
	return found, nil
}

// withinSearchBudget is the two limits that are not about the signal: the budget
// the service record carries, and whether this service's windows can close on
// evidence at all. Both are read before a build is made, because a build the
// search cannot measure is a build in front of production for nothing.
func (h *HealthMonitor) withinSearchBudget(ctx context.Context, w Watching, svc service.Service) error {
	previous, err := h.previousRead(ctx, w.ID)
	if err != nil {
		return err
	}
	if !previous.ClosedOn.Empty() && !previous.PassedAvailable {
		return fmt.Errorf("%w: %s", ErrSearchRefused, w.Name)
	}

	budget := SearchBudget{
		Builds:  int(svc.SearchBudgetBuilds.Number),
		Seconds: svc.SearchBudgetSeconds.Number,
	}
	if !svc.SearchBudgetBuilds.Present || !svc.SearchBudgetSeconds.Present {
		// Where an owner authors nothing the budget is what bisecting a batch of
		// the backlog cap's size needs, each build watched for one window: the
		// cap's own log, and the cap's own count of window caps.
		budget = suppliedSearchBudget(svc)
	}
	spent, seconds, err := h.searchSpent(ctx, w)
	if err != nil {
		return err
	}
	if spent >= budget.Builds || seconds >= budget.Seconds {
		return fmt.Errorf("%w: %d build(s) and %.0f second(s) of production against a budget of %d and %.0f",
			ErrSearchBudgetSpent, spent, seconds, budget.Builds, budget.Seconds)
	}
	return nil
}

// suppliedSearchBudget is what the search may spend where an owner authored
// nothing: bisecting a batch of the backlog cap's size, each build watched for
// one window. The backlog cap is the window limit where an owner separated
// neither, which is what makes both halves of the exception one number.
func suppliedSearchBudget(svc service.Service) SearchBudget {
	backlog := svc.BacklogCap.Number
	if !svc.BacklogCap.Present {
		backlog = svc.Parameters.WindowLimit.Number
	}
	if backlog < 2 {
		backlog = 2
	}
	// A bisection over n releases takes about log2(n) steps, and one more for the
	// first build, which tests the revert itself rather than narrowing.
	steps := 1
	for n := backlog; n > 1; n /= 2 {
		steps++
	}
	windowCap := svc.Parameters.WindowCapSeconds.Number
	return SearchBudget{Builds: steps, Seconds: float64(steps) * windowCap}
}

// searchSpent is what this service's search has already spent: how many builds
// it has deployed, and how long production has carried them. A search window
// names a build and no release, which is what tells one from every other window.
func (h *HealthMonitor) searchSpent(ctx context.Context, w Watching) (int, float64, error) {
	all, err := window.All(ctx, h.pool, w.ID)
	if err != nil {
		return 0, 0, err
	}
	builds, seconds := 0, 0.0
	for _, win := range all {
		if win.ReleaseID != "" {
			continue
		}
		builds++
		opened, err := record.ParseTime(win.At)
		if err != nil {
			return 0, 0, fmt.Errorf("healthmonitor: reading when the search's window %s opened: %w", win.ID, err)
		}
		ended := time.Now()
		if !win.Open() {
			if ended, err = record.ParseTime(win.ClosedAt); err != nil {
				return 0, 0, fmt.Errorf("healthmonitor: reading when the search's window %s closed: %w", win.ID, err)
			}
		}
		seconds += ended.Sub(opened).Seconds()
	}
	return builds, seconds, nil
}

// searchTarget is the release the search's control runs and traffic returns to:
// the rollback's target for the batch's own release, which the search never
// tears down. Where there is none there is nothing to compare a search build
// against and no search runs.
func (h *HealthMonitor) searchTarget(ctx context.Context, w Watching, batch deploy.Deploy) (release.Release, bool, error) {
	if batch.ReleaseID == "" {
		return release.Release{}, false, nil
	}
	rel, err := release.Get(ctx, h.pool, batch.ReleaseID)
	if err != nil {
		return release.Release{}, false, err
	}
	return h.TargetBelow(ctx, w, rel.Number)
}

// nextOnto is the release the next build applies the revert onto, and how many
// releases of the batch the search still has to tell apart. The first step is
// the rollback's target, which answers whether the revert itself is at fault;
// after that the range is narrowed by how each earlier step's window closed —
// failed puts the change at or below that release, and passed or timed out puts
// it above — and the next step is the middle of what is left.
func (h *HealthMonitor) nextOnto(ctx context.Context, w Watching, batch deploy.Deploy,
	target release.Release) (release.Release, int, error) {
	delivered, err := h.batchReleases(ctx, w, batch)
	if err != nil {
		return release.Release{}, 0, err
	}
	if len(delivered) == 0 {
		return target, 1, nil
	}

	low, high := target.Number, delivered[len(delivered)-1].Number
	steps, err := h.searchSteps(ctx, w)
	if err != nil {
		return release.Release{}, 0, err
	}
	for _, step := range steps {
		switch {
		case step.failed && step.onto < high:
			high = step.onto
		case !step.failed && step.onto > low:
			low = step.onto
		}
	}

	var between []release.Release
	for _, r := range delivered {
		if r.Number > low && r.Number <= high {
			between = append(between, r)
		}
	}
	if len(between) == 0 {
		return target, 1, nil
	}
	if low == target.Number && !anyStep(steps, target.Number) {
		// The first build applies the revert onto the rollback's target, which is
		// the one question a bisection cannot answer: whether the revert is at
		// fault rather than any release the batch delivered.
		return target, len(between) + 1, nil
	}
	return between[len(between)/2], len(between), nil
}

// batchReleases is the releases the batch's deploy delivered, in number order.
// Every one of them carries the failed change, which is why a build of one alone
// would measure the defect just removed.
func (h *HealthMonitor) batchReleases(ctx context.Context, w Watching, batch deploy.Deploy) ([]release.Release, error) {
	var delivered []release.Release
	for _, id := range batch.DeliveredReleaseIDs {
		r, err := release.Get(ctx, h.pool, id)
		if err != nil {
			return nil, err
		}
		delivered = append(delivered, r)
	}
	sort.Slice(delivered, func(i, j int) bool { return delivered[i].Number < delivered[j].Number })
	return delivered, nil
}

// step is one earlier step of the search read back off the records: the release
// its build applied the revert onto, and whether its window failed. The release
// is on the search deploy's own record, which names it as the one release that
// deploy delivers — a search deploy delivers no release to production, so the
// field is free to carry what the next step needs to resume.
type step struct {
	onto   int64
	failed bool
}

func (h *HealthMonitor) searchSteps(ctx context.Context, w Watching) ([]step, error) {
	all, err := window.All(ctx, h.pool, w.ID)
	if err != nil {
		return nil, err
	}
	var steps []step
	for _, win := range all {
		if win.ReleaseID != "" || win.Open() {
			continue
		}
		dep, err := deploy.Get(ctx, h.pool, win.DeployID)
		if err != nil {
			return nil, err
		}
		if len(dep.DeliveredReleaseIDs) != 1 {
			continue
		}
		onto, err := release.Get(ctx, h.pool, dep.DeliveredReleaseIDs[0])
		if err != nil {
			return nil, err
		}
		steps = append(steps, step{onto: onto.Number, failed: win.Exit == window.ExitFailed})
	}
	return steps, nil
}

// anyStep is whether the search has already measured a build applied onto that
// release, which is what keeps the first step from being taken twice.
func anyStep(steps []step, number int64) bool {
	for _, s := range steps {
		if s.onto == number {
			return true
		}
	}
	return false
}

// openSearchWindow opens the window over a search's deploy: it names the build
// and no release, and its parameters are the service's own, resolved at the open
// the way every other window's are.
func (h *HealthMonitor) openSearchWindow(ctx context.Context, w Watching, svc service.Service,
	buildID, deployID string, target release.Release, scoreVersion string) (window.Window, error) {
	if scoreVersion == "" {
		return window.Window{}, ErrScoreVersionMissing
	}
	version, err := h.policy.Newest(ctx, Actor)
	if err != nil {
		return window.Window{}, err
	}
	parameters, err := h.policy.WindowParameters(ctx, w.ID)
	if err != nil {
		return window.Window{}, err
	}
	dep, err := deploy.Get(ctx, h.pool, deployID)
	if err != nil {
		return window.Window{}, err
	}
	opening, err := h.opening(ctx, w, svc, release.Release{BuildID: buildID}, dep, parameters, target, true)
	if err != nil {
		return window.Window{}, err
	}
	opening.DeployID, opening.ScoreVersion, opening.PolicyVersion = deployID, scoreVersion, version.ID
	return h.windows.Open(ctx, Actor, opening)
}
