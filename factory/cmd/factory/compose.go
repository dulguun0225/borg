package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// compose is a run's collaborators, the install's two records, and the two
// versions in force — everything a run has before it takes an intent in. It is
// separate from [run] because the same value is what drives every step, and a test
// that drives them one at a time needs the one run composes rather than a second
// composition to keep in step with it.
func compose(ctx context.Context, d deps) (*path, error) {
	if d.candidateCeiling < 1 {
		return nil, fmt.Errorf("factory: the platform's room for candidate environments is %d, and a run needs one",
			d.candidateCeiling)
	}
	if len(d.services) == 0 {
		return nil, errors.New("factory: an install knows at least one service, and this one knows none")
	}

	// The score version first, because the policy reader is composed with it: the
	// supplied half of every value in force is a field of the version in force, so
	// a reader holding a different one would answer a gate firing with a number the
	// decision's own row does not name. This is also where the score learns — the
	// ensure computes the supplied table from every outcome in the store and appends
	// a version where it has moved.
	marks := marksOf(d.pool)
	scoreVersion, err := score.NewWriter(d.pool, d.token, marks).Ensure(ctx, scoreActor)
	if err != nil {
		return nil, err
	}

	human, err := humanNamed(ctx, d.pool, d.token, d.human)
	if err != nil {
		return nil, err
	}

	p := &path{
		d:             d,
		human:         human,
		lines:         bufio.NewScanner(d.in),
		policy:        policy.NewReader(d.pool, d.token, scoreVersion),
		log:           decisionlog.NewWriter(d.pool, d.token),
		store:         artifact.NewStore(d.pool, d.token),
		intake:        intent.NewIntake(d.pool, d.token),
		decomposition: item.NewDecomposition(d.pool, d.token),
		dispatch:      item.NewDispatch(d.pool, d.token),
		builds:        build.NewWriter(d.pool, d.token),
		deploys:       deploy.NewWriter(d.pool, d.token),
		checks:        lastcheck.NewWriter(d.pool, d.token),
		factory:       policy.NewFactory(d.pool, d.token),
		byItem:        map[string]*candidate{},
		authored:      map[string]bool{},
		serviceByID:   map[string]service.Service{},
	}
	p.candidates = environment.NewCandidates(d.pool, d.token)

	// The install. The factory-wide settings record exists before any project
	// does; the project and production's environment for it are created in the
	// same event, an owner not choosing production because it exists
	// everywhere. This ensures all three as the owner and takes the policy
	// version in force from it.
	installed, err := p.factory.Install(ctx, p.human, d.project, []string{d.dir}, d.credential, d.candidateCeiling)
	if err != nil {
		return nil, err
	}
	p.production = installed.Production
	p.projectID = installed.Project.ID
	p.scoreVersion = scoreVersion.ID

	// Drift detection's store, read and never written. Where none is installed the
	// gate is composed with [gate.NoDriftDetector], which answers no mismatch ever — so a
	// factory without one decides exactly as it did before this milestone, and the
	// absence shows in the line below rather than as a failure.
	var mismatches gate.DriftDetector = gate.NoDriftDetector{}
	if d.driftdetector != nil {
		mismatches = driftdetector.NewStore(d.driftdetector)
		fmt.Fprintln(d.out, "An drift detector is installed; the production deploy row reads its store at every firing")
	} else {
		fmt.Fprintln(d.out, "No drift detector is installed, so every check this factory makes reads a record it wrote itself")
	}
	// The notifier first: the gate reaches a human through it for an
	// acknowledgement and for the wait an escalation leaves, so it exists before
	// the gate does. It is composed with the owner's name because a page widens
	// to the owner and the design gives the owner no record.
	p.notifier, err = notifier.New(d.pool, p.log, d.token, terminal{out: d.out}, d.human)
	if err != nil {
		return nil, err
	}

	p.gate = gate.New(gate.Composition{
		Pool:          d.pool,
		Token:         d.token,
		Log:           p.log,
		Score:         score.New(d.pool, scoreVersion, d.draw, marks, d.token),
		Policy:        p.policy,
		Holds:         p,
		DriftDetector: mismatches,
		IntentState:   p.intentState,
		HumanName:     p.nameOfKey,
		Draw:          d.draw,
		Notifier:      gateNotifier{notifier: p.notifier},
		Dispatch:      p.dispatch,
	})

	// The queue. It reaches the repository and the candidate environments through
	// this same value, which is the deployer, and it is composed without three
	// readings the design gives it: the health monitor's store, which a mint
	// takes its second number from, the design system constraint records, and
	// what waits behind a rollback hold. Each is named with the value the package
	// exposes for a factory composed without it, so the composition says which
	// ones it is without rather than passing nothing.
	p.queue = mergequeue.New(mergequeue.Composition{
		Pool:         d.pool,
		Token:        d.token,
		Log:          p.log,
		Releases:     release.NewWriter(d.pool, d.token),
		Repository:   p,
		Numbers:      mergequeue.NoNumbersSeen{},
		DesignSystem: mergequeue.EveryMoveDiffers{},
		Backlog:      mergequeue.NoBacklog{},
	})
	fmt.Fprintf(d.out, "Policy version %s in force; score version %s (formula %s)\n",
		installed.Version.ID, scoreVersion.ID, scoreVersion.FormulaVersion)

	// The health monitor. It reads the quantity through the file each deployed
	// process emits into and reaches a target through this same value, the
	// deployer being what reaches one. The builder is nil: the search that would
	// use it is not built.
	//
	// The reading against a service's own recent past is composed with a size and
	// a run length here, the service record's fields for them not being built and
	// the score supplying no value for either: ownHistorySize and
	// ownHistoryRunLength say what each is and why it is that number. The
	// explicit threshold is still composed with no run length, so it never
	// crosses — its number is the service's objective and nothing here authors
	// one.
	p.healthMonitor, err = healthmonitor.New(d.pool, window.NewWriter(d.pool, d.token),
		incident.NewWriter(d.pool, d.token), p.checks, p.intake, p.policy, p.notifier,
		signalFiles{dir: d.dir}, p, nil, mismatches, healthmonitor.Readings{
			OwnHistorySize:      ownHistorySize(),
			OwnHistoryRunLength: ownHistoryRunLength,
			Interval:            intervalResolution,
			PassInterval:        atLeastASecond(d.watchEvery),
		})
	if err != nil {
		return nil, err
	}
	// Enforcement. The checkout, the run it observes, the candidate's own store
	// and a backfill's completion are all this same value, the deployer being
	// what reaches a repository and a target.
	p.contracts, err = contractcheck.New(d.pool, p.policy, p.intake, p, p, p, p)
	if err != nil {
		return nil, err
	}

	// The area, where the run names one. Declaring one is an owner's write and
	// the owner is the human at this terminal, so a name not yet declared is
	// declared here rather than refused. An item with no area is allowed and
	// costs the score both of its context readings, which puts a human at every
	// gate of that item.
	if d.area != "" {
		ar, found, err := area.ByName(ctx, d.pool, d.area)
		if err != nil {
			return nil, err
		}
		if !found {
			ar, err = area.NewWriter(d.pool, d.token).Declare(ctx, p.human, d.area,
				area.InsideProject(p.projectID), area.Hazard{})
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(d.out, "Area %s declared as %s\n", d.area, ar.ID)
		}
		p.areaID = ar.ID
	}
	return p, nil
}

// serviceOf is the service record of one id, read once per run. It is what every
// step that starts from an item uses: an item names one service, and where the work
// is is that record's own repository field rather than something this interface is
// told twice.
func (p *path) serviceOf(ctx context.Context, serviceID string) (service.Service, error) {
	if svc, found := p.serviceByID[serviceID]; found {
		return svc, nil
	}
	svc, err := service.Get(ctx, p.d.pool, serviceID)
	if err != nil {
		return service.Service{}, err
	}
	p.serviceByID[serviceID] = svc
	return svc, nil
}

// runsOnProduction authors production's targets on a service that names none,
// which is an owner's write and is made as the owner at this terminal: -targets
// is what this interface calls production's addresses, and a service installed
// here runs on all of them. It is authored once — a service already naming
// targets is left as it is, an owner having said which it runs on.
//
// It matters beyond the record saying so. What is running is read per target
// against the addresses a deploy recorded its completion on, so a service naming
// none is a service whose current deploy no reader can find: the analysis window
// stands the environment in for the whole set at the open, and the reading after
// a window has closed has no such fallback and would find nothing running.
func (p *path) runsOnProduction(ctx context.Context, svc service.Service) (service.Service, error) {
	addresses := p.production.Addresses()
	if len(svc.Targets) > 0 || len(addresses) == 0 {
		return svc, nil
	}
	if _, err := p.factory.SetServiceTargets(ctx, p.human, svc.ID, addresses, addresses); err != nil {
		return service.Service{}, err
	}
	read, found, err := service.ByName(ctx, p.d.pool, svc.Name)
	if err != nil {
		return service.Service{}, err
	}
	if !found {
		return svc, nil
	}
	return read, nil
}

// ownHistoryRunLength is how many intervals a service whose behaviour has not
// changed runs before the reading against its own recent past crosses it once.
// A thousand intervals of the resolution emission.go fixes is under a minute of
// a busy service and is deliberately the loose end: this reading has no control
// moving with it, so everything that moves both arms together — a dependency
// slowing down, a host filling up — reads here as the release having changed,
// and a rate tight enough to catch a small regression would raise an intent for
// every one of them.
//
// It is also what keeps this reading looser than the comparison beside it: the
// comparison is read at the confidence an owner authored and this at a run
// length, so a release inside its window is failed by the comparison and read
// here only where the comparison found nothing.
const ownHistoryRunLength = 1000

// ownHistorySize is the smallest change in each quantity the reading against a
// service's own recent past has to detect: the value the score supplies for the
// analysis window's size, for every quantity the shipped emission version
// carries. The two readings are about the same thing at the same scale — the
// smallest regression worth catching — and the difference between them is the
// arm each reads against, so a second number here would be a second answer to
// one question.
//
// It is the supplied value and not the value in force on the service: the value
// in force is authored per service and this is composed once, before any service
// exists. What that costs is a service whose owner authored a coarser window size
// being read here at the finer one.
func ownHistorySize() map[gatepolicy.Quantity]float64 {
	supplied, ok := score.Starting(gatepolicy.WindowSize)
	if !ok {
		return nil
	}
	sizes := map[gatepolicy.Quantity]float64{}
	for _, quantity := range gatepolicy.Quantities {
		sizes[quantity] = supplied.Value
	}
	return sizes
}

// atLeastASecond is an interval a last check can carry. The record stores whole
// seconds — an interval shorter than one is refused rather than rounded to
// nothing — and this interface reads as often as -watch-every says, which a test
// sets below a second. Rounding up is the honest direction: the interval is what
// a reader holds a check's age against, and a longer one says stopped later
// rather than sooner.
func atLeastASecond(every time.Duration) time.Duration {
	if every < time.Second {
		return time.Second
	}
	return every
}

// productionAddresses is the addresses of production's targets, in the
// environment's order. It is what every read of what is running is performed
// against: a release is current when its deploy is marked complete on every one
// of them, so a producer deployed to three targets of four is not current and
// holds its consumer until the fourth lands.
func (p *path) productionAddresses() []string {
	addresses := make([]string, 0, len(p.production.Targets))
	for _, target := range p.production.Targets {
		addresses = append(addresses, target.Address)
	}
	return addresses
}

// subjectsFor is what a policy read about one candidate is performed against: the item's
// service and the area of this run. The service is empty on a first item of a service —
// the spec is authored before decomposition writes the record — so a safeguard on a
// service the factory has not seen does not bound that run's spec stage, and does bound
// every run after it.
func (p *path) subjectsFor(c *candidate) policy.Subjects {
	return policy.Subjects{ServiceID: c.svc.ID, AreaID: p.areaID}
}

// deployOrder is the candidates of one service that were minted a release, in the
// order they deploy: the revert of an outstanding rollback first, and then the rest
// by number, lowest first. A candidate with no release is left out — there is
// nothing to deploy — and so is one of another service.
//
// The number orders deploys and a revert is the one exception the design makes to
// that. Every release the rollback's hold is holding cannot deploy until the revert
// ships, so making the revert wait behind them by number would be the same deadlock
// one step further out.
func (p *path) deployOrder(ctx context.Context, svc service.Service, candidates []*candidate) ([]*candidate, error) {
	var minted []*candidate
	for _, c := range candidates {
		if c.releaseID != "" && c.svc.ID == svc.ID && c.deployID == "" && !c.held && c.factoryHold == "" {
			minted = append(minted, c)
		}
	}
	for a := 1; a < len(minted); a++ {
		for b := a; b > 0 && minted[b].releaseNumber < minted[b-1].releaseNumber; b-- {
			minted[b], minted[b-1] = minted[b-1], minted[b]
		}
	}

	rollback, found, err := deploy.NewestRollback(ctx, p.d.pool, svc.ID, p.production.ID)
	if err != nil || !found {
		return minted, err
	}
	revertIntentID, err := revertIntentOf(ctx, p, svc, rollback)
	if err != nil {
		return nil, err
	}
	reverts, rest := []*candidate{}, []*candidate{}
	for _, c := range minted {
		it, err := item.Get(ctx, p.d.pool, c.itemID)
		if err != nil {
			return nil, err
		}
		if it.IntentID != "" && it.IntentID == revertIntentID {
			reverts = append(reverts, c)
			continue
		}
		rest = append(rest, c)
	}
	if len(reverts) > 0 {
		fmt.Fprintf(p.d.out, "A revert of rollback %s deploys ahead of %d release(s) its hold is holding\n",
			rollback.ID, len(rest))
	}
	return append(reverts, rest...), nil
}

// inForceFor is the criteria in force for one build of one service: the ones
// introduced by an item already merged, plus the ones this item's own spec
// versions introduced. A build is a set of items and this is that set — an item
// whose branch predates a sibling's spec version is not deciding that sibling's
// promise, and holding it in force would reject every candidate decomposed in parallel
// with the one that introduced it.
//
// of is the items whose branches the build carries, and is empty where the caller
// wants what the service already promises rather than what a build is decided
// against, which is what the spec author is told. It is a list and not one id
// because a re-verification builds the candidate onto every candidate ahead of
// it in the queue: what that tree carries is those items' work too, so their
// criteria are in force for it.
func (p *path) inForceFor(ctx context.Context, svc service.Service, of []string) ([]criterion.Criterion, error) {
	if svc.ID == "" {
		return nil, nil
	}
	ids, err := p.itemsInBuild(ctx, svc.ID, of)
	if err != nil {
		return nil, err
	}
	return criterion.InForce(ctx, p.d.pool, svc.ID, ids)
}

// itemsInBuild is a build's set of items: the ones merged into the repository
// it was made from, plus the items whose branches it carries — empty where
// the caller wants what the service already promises rather than what a
// build is decided against. It is what [path.inForceFor] and the encoding
// check's withdrawn-criteria read both filter by, so the two agree on which
// build they mean.
func (p *path) itemsInBuild(ctx context.Context, serviceID string, of []string) ([]string, error) {
	merged, err := item.AtStage(ctx, p.d.pool, serviceID, item.StageMerged)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(merged)+1)
	for _, it := range merged {
		ids = append(ids, it.ID)
	}
	for _, itemID := range of {
		if itemID != "" && !slices.Contains(ids, itemID) {
			ids = append(ids, itemID)
		}
	}
	return ids, nil
}
