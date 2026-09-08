package main

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/grouper"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
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

// grouperPrincipal is who the grouper's pass reads a project's reports as, and
// the name the read event that read carries.
//
// ../../../end-goal/components.md gives the grouper no row, an agent not being
// a component, so no package below coins this name: package reportstore is
// handed a principal and package grouper is handed one, and this composition is
// where the pass that made the read is named.
var grouperPrincipal = principal.OfComponent("grouper")

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
	// grouper is the pass that turns reports into intents. It is nil in every
	// composition that opened no report store, which is every subcommand but
	// the process that serves the entrance.
	grouper *grouper.Grouper
	// prompts is the role prompt version in force per role, which the first
	// start entered.
	prompts    *rolePrompts
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

	// mu guards the three fields a pass and an HTTP handler both reach —
	// byItem, serviceByID and logRead — through the accessors in shared.go,
	// which is where what it does and does not cover is stated. Every other
	// field below is the pass goroutine's alone.
	mu sync.Mutex
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
	// servicesOf is which services each intent's decomposition yields items
	// on, by intent id: what this interface is told and never what it decides.
	// An intent no caller of this process named — one a screen took in, one a
	// detector raised — is not in it, and the services are read off the
	// statement's own prefix instead.
	servicesOf map[string][]string
	// sets is the decomposition of each intent this run took in, by intent id,
	// so the pass that finds the Decomposition row decided at Work can perform
	// what that verdict causes and record it where the run reports it.
	sets map[string]*decompositionSet
	// logRead is the log as this pass read it, held for the length of one pass and
	// cleared at the start of the next: the rows pending and the decisions that
	// have closed are both reads of the whole log, and every item of a pass asks
	// the same two questions of them. It is nil outside a pass, so a caller
	// driving one item reads the log as it stands.
	logRead *read
	// moved is whether this pass performed a step: a gate row fired, a verdict
	// a human left acted on, a merge, or a deploy. [path.advance] clears it and
	// returns it, and a pass that ends with it unset is what says every live
	// item is waiting on a human, held, or done — which is where the run's loop
	// ends.
	moved bool
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
// candidate's authoring stages from wherever the records say it stands, every
// candidate's own environment, every Merge to master gate, the queue once per
// service, the production deploys in the number's order, and the watch.
//
// It returns the last production deploy it wrote, which is where the link walk
// starts, and every candidate it adopted — an item another run left queued, which
// this run finishes and has to report like any other.
func (p *path) layer(ctx context.Context, candidates []*candidate) (string, []*candidate, error) {
	// The four authoring stages per item, each with its own gate row: the spec
	// and the criteria it introduces, the implementation plan, the tasks the
	// plan divides into, and the implementation with the build and the consumer
	// contract derived from it. Each is entered where the records say the item
	// has not passed it, so a pass performs only what is left.
	for _, c := range candidates {
		_, err := p.authorFrom(ctx, c)
		if err == nil {
			continue
		}
		if !heldHere(err) {
			return "", nil, p.gaveUp(c.itemID, err)
		}
		// A condition stopped this item's dispatch, and dispatch wrote the hold
		// row before it returned. It is reported and the pass goes on to the
		// next item, for the reason ../../../end-goal/one-process.md gives
		// every component's own pass — one process is not one failure — and
		// because the condition is the credential's or the fleet's and not
		// this item's: twelve items waiting on one ceiling are twelve rows in
		// Work and a count at Factory, which a pass that ended at the first of
		// them would never write. Nothing further is performed on the item
		// this pass, a hold being what says there is no step left to take.
		fmt.Fprintln(p.d.out, describeHold(c.itemID, "", err))
		c.held = true
	}

	// Every candidate's own environment: the gate that decides its deploy creates
	// one, the build goes on it, and the criteria are decided there. The order is
	// dispatch's, because what limits how many move at once is the platform's own
	// room for candidate environments: dispatch orders admission by the tier of
	// the intent each item was decomposed from, an item's priority breaking a tie
	// within one, so what waits at the ceiling is what a tier put last.
	admitted, err := p.admissionOrder(ctx, readyFor(candidates, stepCandidateEnvironment))
	if err != nil {
		return "", nil, err
	}
	for _, c := range admitted {
		if err := p.candidateEnvironment(ctx, c); err != nil {
			return "", nil, err
		}
	}

	// Every candidate's Merge to master gate, fired again against a new build for
	// as long as it keeps rejecting mechanically: what it reads is the candidate's
	// own run — the criteria, every consumer contract, and the producer's own
	// contract diff — and the last two reject on their own terms before a verdict
	// is asked for. [path.mergeUntilQueued] is what builds the candidate again
	// rather than leaving it at Implementation for good; it ends when the row
	// approves, when a human decides it, or when the implementer's own attempt
	// limit escalates.
	for _, c := range readyFor(candidates, stepMerge) {
		if c.environmentID == "" {
			continue
		}
		if err := p.mergeUntilQueued(ctx, c); err != nil {
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
	// pass opened has closed. A window's duration is measured and never set, so what
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
		if err := p.watchWindowsTo(ctx, svc, time.Now().Add(p.d.watchFor), p.d.watchEvery); err != nil {
			return deployed, adoptedAll, err
		}
	}
	return deployed, adoptedAll, nil
}

// readyFor is the candidates this pass may perform one step on: those the
// records place at that step or above it, and no candidate waiting on a human,
// held, or done.
func readyFor(candidates []*candidate, at step) []*candidate {
	var ready []*candidate
	for _, c := range candidates {
		if c.waiting != (gate.Row{}) || c.held || c.from > at {
			continue
		}
		ready = append(ready, c)
	}
	return ready
}
