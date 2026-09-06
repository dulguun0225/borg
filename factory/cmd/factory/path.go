package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
)

// The component actors of the path, named per the M1 convention.
var (
	scoreActor         = record.Actor{Kind: record.KindComponent, Key: "score", Basis: record.BasisClaimed}
	intakeActor        = record.Actor{Kind: record.KindComponent, Key: "intake", Basis: record.BasisClaimed}
	decompositionActor = record.Actor{Kind: record.KindComponent, Key: "decomposition", Basis: record.BasisClaimed}
	dispatchActor      = record.Actor{Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed}
	buildActor         = record.Actor{Kind: record.KindComponent, Key: "build", Basis: record.BasisClaimed}
	// installActor is the install's first-start step, which is what enters the
	// shipped role prompt versions: a call that authors nothing, with the
	// factory's own start as the actor.
	installActor = record.Actor{Kind: record.KindComponent, Key: "install", Basis: record.BasisClaimed}
	deployActor  = record.Actor{Kind: record.KindComponent, Key: "deploy", Basis: record.BasisClaimed}
)

// The four authoring roles, each an agent rather than a component: a model in
// a role dispatch put on a stage, keyed by the model version this run was
// given. All four name the same model, this interface running one model per
// run — two agents on one model are one author under two actors, which is why
// the key is the model version and not the role.
func (p *path) specAuthorActor() record.Actor {
	return record.Actor{Kind: record.KindAgent, Key: p.d.modelName, Basis: record.BasisClaimed}
}

func (p *path) plannerActor() record.Actor {
	return record.Actor{Kind: record.KindAgent, Key: p.d.modelName, Basis: record.BasisClaimed}
}

func (p *path) taskAuthorActor() record.Actor {
	return record.Actor{Kind: record.KindAgent, Key: p.d.modelName, Basis: record.BasisClaimed}
}

func (p *path) implementerActor() record.Actor {
	return record.Actor{Kind: record.KindAgent, Key: p.d.modelName, Basis: record.BasisClaimed}
}

// path is one run's collaborators, composed once. It is also the deployer: it
// implements [mergequeue.Repository] and [contractcheck.Checkout], because
// everything the queue needs done to a repository and everything enforcement needs
// read out of a checkout is the deployer's work, and neither of those
// components reaches one.
type path struct {
	d          deps
	human      record.Actor
	lines      *bufio.Scanner
	production environment.Environment
	projectID  string
	areaID     string
	// areaChain is that area and every area above it, up to the project the
	// chain ends at. It is what a fleet entry's scope is matched against, a
	// scope drawn on any area in the chain reaching the item, and it is read
	// once per run because this interface declares one area for the whole of it.
	areaChain []string

	policy        *policy.Reader
	factory       *policy.Factory
	log           *decisionlog.Writer
	gate          *gate.Gate
	store         *artifact.Store
	intake        *intent.Intake
	decomposition *item.Decomposition
	// items is the item's writer after decomposition — the stage and the count
	// beside it — and dispatch is the component that puts an agent on a stage
	// and writes the transition through it.
	items    *item.Dispatch
	dispatch *dispatch.Dispatch
	// prompts is the role prompt version in force per role, which the first
	// start entered.
	prompts    rolePrompts
	builds     *build.Writer
	deploys    *deploy.Writer
	checks     *lastcheck.Writer
	candidates *environment.Candidates
	queue      *mergequeue.Queue
	contracts  *contractcheck.Check
	// scoreVersion is the version in force for this run, held because a window
	// stores the two versions in force at its open and the health monitor does not append
	// one of its own.
	scoreVersion string
	// The three of everything downstream of a deploy: the health monitor the run
	// watches with, the notifier it tells a human through, and the reads of the
	// drift detector's own store. The notifier is nil for no install and the
	// mismatch reads are nil where no drift detector is installed, which is
	// what [gate.NoDriftDetector] answers for.
	healthMonitor *healthmonitor.HealthMonitor
	notifier      *notifier.Notifier
	// escalations is the wait an item escalated leaves, as dispatch reaches it.
	// It is held rather than composed at the call because the composition is
	// what supplies it to dispatch and what a test exercises.
	escalations dispatchNotifier

	// byItem is the candidate of each item the run has touched, so the queue's
	// re-verification can write what it produced onto the candidate the run reports.
	byItem map[string]*candidate
	// authored is the items this run decomposed. The queue's membership is the service's, so
	// an outcome for an item outside this set is one another run left queued — and
	// telling the two apart is what says which candidates the run has to add to what
	// it reports.
	authored map[string]bool
	// serviceByID is every service record this run has read, so the steps that start
	// from an item read it once.
	serviceByID map[string]service.Service
}

// The seams this value implements. Every one of them is a thing the package
// that declares it cannot do: reaching a repository, reaching a deploy target,
// reading a checkout, observing a run, reading a candidate's own store, reading
// a backfill's completion, and computing the factory's own holds.
var (
	_ mergequeue.Repository    = (*path)(nil)
	_ healthmonitor.Deployer   = (*path)(nil)
	_ contractcheck.Checkout   = (*path)(nil)
	_ contractcheck.Exchanges  = (*path)(nil)
	_ contractcheck.StoreState = (*path)(nil)
	_ gate.Holds               = (*path)(nil)
)

// run walks the whole path once for each intent it is given, from a statement to
// a running release, stopping with the first error.
//
// Every candidate of one dependency layer reaches each step before any of them
// reaches the next, which is what makes two of them live at once — and the merge
// queue, which is the one step that is not per candidate, is where their order is
// decided. Layers exist because an item may wait on another: a consumer's candidate
// environment is composed from its producer's current release, so the producer has
// to have shipped before the consumer can be verified at all. What a layer does is
// what the hold at the candidate deploy row would otherwise make happen across two
// runs.
func run(ctx context.Context, d deps, statements []asked) (shipped, error) {
	var s shipped
	if len(statements) == 0 {
		return s, errors.New("factory: a run needs at least one intent")
	}

	p, err := compose(ctx, d)
	if err != nil {
		return s, err
	}
	s.environmentID = p.production.ID
	s.areaID = p.areaID

	// 1. Every intent taken in, refined, decomposed into its items, ratified at
	// Decomposition where it yielded more than one, and every item's spec,
	// implementation, and build authored.
	for n, one := range statements {
		set, candidates, err := p.authorIntent(ctx, one, fmt.Sprintf("%d of %d", n+1, len(statements)))
		if err != nil {
			return s, err
		}
		s.decompositions = append(s.decompositions, set)
		s.candidates = append(s.candidates, candidates...)
		for _, c := range candidates {
			p.byItem[c.itemID] = c
			p.authored[c.itemID] = true
		}
	}
	for _, name := range d.serviceNames() {
		svc, found, err := service.ByName(ctx, d.pool, name)
		if err != nil {
			return s, err
		}
		if found {
			s.serviceIDs = append(s.serviceIDs, svc.ID)
		}
	}
	if len(s.serviceIDs) > 0 {
		s.serviceID = s.serviceIDs[0]
	}

	// 2. The path below decomposition, one dependency layer at a time. A superseded
	// candidate is left out: the Decomposition row rejected the set it was part of,
	// so it has no artifact below decomposition and nothing under it fires.
	var live []*candidate
	for _, c := range s.candidates {
		if !c.superseded {
			live = append(live, c)
		}
	}
	deployed := ""
	for _, one := range layers(live) {
		last, adopted, err := p.layer(ctx, one)
		// The adopted candidates are reported whether or not the layer finished:
		// an item another run left queued is one this run touched, and a run that
		// stopped after touching it should still say so.
		s.candidates = append(s.candidates, adopted...)
		if err != nil {
			return s, err
		}
		if last != "" {
			deployed = last
		}
	}

	// 3. The detector: every deprecation-marked element whose derived consumer
	// contracts are gone gets a removal intent, so nobody has to remember step three
	// of a migration. It runs once at the end of a run rather than per layer, because
	// what empties a list is a release deploying and the layers above are where those
	// happened.
	if err := p.raiseRemovals(ctx); err != nil {
		return s, err
	}

	// 4. The walk, the demonstration's direction: from the last deploy back to
	// the intent, every step a field and none reconstructed. A run whose release was
	// failed walks the rollback's own deploy record, which is the deploy that is
	// live at the end of it.
	if deployed == "" {
		fmt.Fprintln(d.out, "Nothing reached production, so there is no deploy to walk back from")
		return s, nil
	}
	if c := p.byItem[itemOfDeploy(ctx, d.pool, deployed)]; c != nil && c.svc.ID != "" {
		if live, running, err := deploy.Current(ctx, d.pool, c.svc.ID, p.production.ID, p.productionAddresses()); err == nil && running {
			deployed = live.ID
		}
	}
	return s, walk(ctx, d.pool, d.out, d.token, asPrincipal(p.human), deployed)
}

// itemOfDeploy is the item a deploy's release was cut from, and empty where the
// deploy or the release cannot be read. It is used to find which service's current
// deploy the walk should start from, and a failure to read it leaves the walk
// starting where it already was.
func itemOfDeploy(ctx context.Context, pool *pgxpool.Pool, deployID string) string {
	dep, err := deploy.Get(ctx, pool, deployID)
	if err != nil || dep.ReleaseID == "" {
		return ""
	}
	rel, err := release.Get(ctx, pool, dep.ReleaseID)
	if err != nil {
		return ""
	}
	return rel.ItemID
}

// layers is the run's candidates grouped so that an item comes after every item of
// this run it waits on. A candidate that waits on nothing this run authored is in
// the first layer, whether or not it waits on something an earlier run shipped —
// what a dependency outside the run does is hold at the deploy rows, which is where
// the design puts it.
//
// A cycle among the run's own items would leave candidates unplaced, and the
// remainder goes into a last layer rather than being dropped: decomposition declares the
// order and a decomposition that declared a cycle is a bad decomposition, which the deploy rows' holds
// then never lift. Losing the candidates entirely would be worse — nothing would say
// they existed.
func layers(candidates []*candidate) [][]*candidate {
	placed := map[string]bool{}
	remaining := slices.Clone(candidates)
	var out [][]*candidate
	for len(remaining) > 0 {
		var layer, next []*candidate
		for _, c := range remaining {
			ready := true
			for _, on := range c.waitsOn {
				if inRun(candidates, on) && !placed[on] {
					ready = false
					break
				}
			}
			if ready {
				layer = append(layer, c)
				continue
			}
			next = append(next, c)
		}
		if len(layer) == 0 {
			// Nothing is ready and something is left, which is a cycle decomposition
			// declared. The rest goes through together.
			out = append(out, next)
			return out
		}
		for _, c := range layer {
			placed[c.itemID] = true
		}
		out = append(out, layer)
		remaining = next
	}
	return out
}

// admissionOrder is this layer's candidates in the order dispatch admits them,
// which is the tier of each item's intent first and the item's own priority
// within one tier. The item records are read here and the ordering is
// dispatch's: what a tier orders is admission, and the ceiling on candidate
// environments is what makes more ready than the platform admits.
func (p *path) admissionOrder(ctx context.Context, candidates []*candidate) ([]*candidate, error) {
	if len(candidates) < 2 {
		return candidates, nil
	}
	items := make([]item.Item, 0, len(candidates))
	byID := make(map[string]*candidate, len(candidates))
	for _, c := range candidates {
		it, err := item.Get(ctx, p.d.pool, c.itemID)
		if err != nil {
			return nil, err
		}
		items = append(items, it)
		byID[it.ID] = c
	}
	ordered, err := p.dispatch.Admit(ctx, items)
	if err != nil {
		return nil, err
	}
	admitted := make([]*candidate, 0, len(ordered))
	for _, it := range ordered {
		admitted = append(admitted, byID[it.ID])
	}
	return admitted, nil
}

func inRun(candidates []*candidate, itemID string) bool {
	for _, c := range candidates {
		if c.itemID == itemID {
			return true
		}
	}
	return false
}

// layer is the whole path below decomposition for one dependency layer: every
// candidate's own environment, every Merge to master gate, the queue once per service, the
// production deploys in the number's order, and the watch.
//
// It returns the last production deploy it wrote, which is where the link walk
// starts, and every candidate it adopted — an item another run left queued, which
// this run finishes and has to report like any other.
func (p *path) layer(ctx context.Context, candidates []*candidate) (string, []*candidate, error) {
	// Every candidate's own environment: the gate that decides its deploy creates
	// one, the build goes on it, and the criteria are decided there. The order is
	// dispatch's, because what limits how many move at once is the platform's own
	// room for candidate environments: dispatch orders admission by the tier of
	// the intent each item was decomposed from, an item's priority breaking a tie
	// within one, so what waits at the ceiling is what a tier put last.
	admitted, err := p.admissionOrder(ctx, candidates)
	if err != nil {
		return "", nil, err
	}
	for _, c := range admitted {
		if err := p.candidateEnvironment(ctx, c); err != nil {
			return "", nil, err
		}
	}

	// Every candidate's Merge to master gate. What it reads is the candidate's own run — the
	// criteria, every consumer contract, and the producer's own contract diff —
	// and the last two reject on their own terms before a verdict is asked for.
	for _, c := range candidates {
		if err := p.mergeGate(ctx, c); err != nil {
			return "", nil, err
		}
	}

	// The queue, once per service that has a member, in the order the install names
	// its services: two services' merges have nothing to serialise against each
	// other for, and an order read off a map would not be an order.
	all := slices.Clone(candidates)
	var adoptedAll []*candidate
	for _, name := range p.d.serviceNames() {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return "", adoptedAll, err
		}
		if !found {
			continue
		}
		adopted, err := p.runQueue(ctx, svc)
		all = append(all, adopted...)
		adoptedAll = append(adoptedAll, adopted...)
		if err != nil {
			return "", adoptedAll, err
		}
	}

	// The production deploys, per service, in the order the numbers were minted —
	// a numbered release waiting to deploy is ordered by its number and by nothing
	// else, so an owner's priority reaches every queue before this one and none after
	// it. The one exception is a revert, which deploys ahead of every release the
	// rollback's hold is holding.
	deployed := ""
	for _, name := range p.d.serviceNames() {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return "", adoptedAll, err
		}
		if !found {
			continue
		}
		ordered, err := p.deployOrder(ctx, svc, all)
		if err != nil {
			return "", adoptedAll, err
		}
		for _, c := range ordered {
			if err := p.productionDeploy(ctx, c); err != nil {
				return "", adoptedAll, err
			}
			if c.deployID != "" {
				deployed = c.deployID
			}
		}
	}

	// The watch: everything downstream of a deploy, read until every window this
	// layer opened has closed. A window's duration is measured and never set, so what
	// this gives up on is left open for `factory watch` to finish rather than waited
	// out here. It runs before the next layer, because a layer below is composed from
	// what this one is running and a consumer contract in force is read over a range
	// whose floor a closing window moves.
	for _, name := range p.d.serviceNames() {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return deployed, adoptedAll, err
		}
		if !found {
			continue
		}
		if err := p.watchTo(ctx, svc, time.Now().Add(p.d.watchFor), p.d.watchEvery); err != nil {
			return deployed, adoptedAll, err
		}
	}
	return deployed, adoptedAll, nil
}
