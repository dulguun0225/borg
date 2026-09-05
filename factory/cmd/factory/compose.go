package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"

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
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/incident"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
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
		return nil, fmt.Errorf("factory: the substrate's room for candidate environments is %d, and a run needs one",
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
	scoreVersion, err := score.NewWriter(d.pool).Ensure(ctx, scoreActor)
	if err != nil {
		return nil, err
	}

	p := &path{
		d:             d,
		human:         record.Actor{Kind: record.KindHuman, Name: d.human},
		lines:         bufio.NewScanner(d.in),
		policy:        policy.NewReader(d.pool, scoreVersion),
		log:           decisionlog.NewWriter(d.pool),
		store:         artifact.NewStore(d.pool),
		intake:        intent.NewIntake(d.pool),
		decomposition: item.NewDecomposition(d.pool),
		dispatch:      item.NewDispatch(d.pool),
		builds:        build.NewWriter(d.pool),
		deploys:       deploy.NewWriter(d.pool),
		byItem:        map[string]*candidate{},
		authored:      map[string]bool{},
		serviceByID:   map[string]service.Service{},
	}
	p.candidates = environment.NewCandidates(d.pool)

	// The install. The factory-wide settings record and production's environment
	// record are what an owner authors on, and they exist before a project does —
	// so this ensures both as the owner and takes the policy version in force from
	// it.
	installed, err := policy.NewFactory(d.pool).Install(ctx, p.human, []string{d.dir}, d.credential)
	if err != nil {
		return nil, err
	}
	p.production = installed.Production
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
	p.gate = gate.New(p.log, score.New(d.pool, scoreVersion, d.draw), p.policy, mismatches)
	p.queue = mergequeue.New(d.pool, p.log, release.NewWriter(d.pool), p.dispatch, p)
	fmt.Fprintf(d.out, "Policy version %s in force; score version %s (formula %s)\n",
		installed.Version.ID, scoreVersion.ID, scoreVersion.FormulaVersion)

	// The notifier and the health monitor. The notifier is composed with the owner's
	// name because a page widens to the owner and the design gives the owner no record;
	// the health monitor is composed with this same value as its rollbacker, the deploy
	// agent being what reaches a target.
	p.notifier, err = notifier.New(d.pool, p.log, terminal{out: d.out}, d.human)
	if err != nil {
		return nil, err
	}
	p.healthMonitor, err = healthmonitor.New(d.pool, window.NewWriter(d.pool), incident.NewWriter(d.pool),
		p.intake, p.policy, p.notifier, signalFiles{dir: d.dir}, p)
	if err != nil {
		return nil, err
	}
	// Enforcement. The checkout and the run it observes are both this same value,
	// the deploy agent being what reaches a repository and a target.
	p.contracts, err = contractcheck.New(d.pool, p.policy, p.intake, p, p)
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
			ar, err = area.NewWriter(d.pool).Declare(ctx, p.human, d.area, "")
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
	reverts, rest := []*candidate{}, []*candidate{}
	for _, c := range minted {
		it, err := item.Get(ctx, p.d.pool, c.itemID)
		if err != nil {
			return nil, err
		}
		if it.IntentID != "" && it.IntentID == rollback.Undoing.RevertIntentID {
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
// itemID is empty where the caller wants what the service already promises rather
// than what a build is decided against, which is what the spec author is told.
func (p *path) inForceFor(ctx context.Context, svc service.Service, itemID string) ([]criterion.Criterion, error) {
	if svc.ID == "" {
		return nil, nil
	}
	merged, err := item.AtStage(ctx, p.d.pool, svc.ID, item.StageMerged)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(merged)+1)
	for _, it := range merged {
		ids = append(ids, it.ID)
	}
	if itemID != "" {
		ids = append(ids, itemID)
	}
	return criterion.InForce(ctx, p.d.pool, svc.ID, ids)
}
