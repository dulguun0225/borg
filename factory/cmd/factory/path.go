package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/checker"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
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
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
	"github.com/dulguun0225/borg/factory/window"
)

// serviceRepo is one service this install knows: its name, and the git repository
// that is it. A service is one repository with one long-lived branch and no
// repository holds two, so this pair is the whole of what the interface has to be
// told before a decomposition can write the record.
//
// It is a list on [deps] rather than one name and one path because contracts need
// a second service: an interface has consumers, and the consumers are other
// services in the same factory.
type serviceRepo struct {
	name string
	repo string
}

// deps is everything the path composes, explicit so the end-to-end test
// drives the same code the run subcommand does — a fake model, scripted
// input, temp directories — with nothing swapped anywhere but here.
type deps struct {
	pool      *pgxpool.Pool
	model     agent.Model
	modelName string // the provider's model id, which is the author a per-author prior is kept on
	// targets is one target per environment. There is one per environment and not
	// one per install, because a candidate's environment is a place of its own.
	targets *targetSet
	// dir is production's target, and the directory each candidate environment's
	// own target is made under.
	dir        string
	credential secretref.Ref // the deploy credential, deploy.local
	in         io.Reader     // what the human answers on
	out        io.Writer
	human      string // the deciding human's name
	// services is every service this install knows, with the repository each is.
	// A statement names the services its intent changes, and a name not here is an
	// error rather than a service the run invents.
	services []serviceRepo
	area     string // the area's name, empty where the run names none
	// candidateCeiling is how many candidate environments this substrate has room
	// for at once. It is the factory's own infrastructure limit and not gate
	// policy: the design says of the condition it holds on that no parameter of an
	// owner's limits it. A candidate that meets it waits, and the wait is written
	// into the log because it is not a record and no gate fired.
	candidateCeiling int
	// checker is the independent checker's own store, read and never written: the gate
	// asks it for a mismatch at the production deploy row, and the notifier reads
	// it for both ends of the page about one. It is nil where no independent checker is
	// installed, which is a factory whose every check reads a record the factory
	// wrote — the state the independent checker exists to remove.
	checker *pgxpool.Pool
	// watchFor is how long a run watches its own windows before it leaves them open,
	// and watchEvery is how often it reads the quantity while it does. A window's
	// duration is measured and never set, so a run cannot know in advance how long to
	// wait: what it gives up on, `factory watch` continues.
	watchFor   time.Duration
	watchEvery time.Duration
	// draw is what the score's held-out sample is drawn from. It is nil in a run,
	// which is the runtime's own generator: the sample is one in ten of the firings
	// the score would have gated, and a run that composed a fixed draw would either
	// sample nothing or sample everything. A test composes one that answers a fixed
	// sequence, which is the only way a selection is assertable.
	draw score.Draw
}

// repoOf is the repository of the named service, and an error for a name this
// install was not told about. A run that could invent one would write a service
// record naming a directory nobody chose.
func (d deps) repoOf(name string) (string, error) {
	for _, s := range d.services {
		if s.name == name {
			return s.repo, nil
		}
	}
	return "", fmt.Errorf("factory: no service named %q is configured; this install knows %s",
		name, strings.Join(d.serviceNames(), ", "))
}

// serviceNames is every service this install knows, in the order it was told them.
// It is the order a run's queues are run in and the order its watches are, so two
// services' merges are ordered by configuration rather than by whichever map
// iteration came first.
func (d deps) serviceNames() []string {
	names := make([]string, 0, len(d.services))
	for _, s := range d.services {
		names = append(names, s.name)
	}
	return names
}

// targetSet is one [targetseam.Target] per environment, made on demand and kept.
// It was kept because what a local target had running was in its own memory, and
// that is no longer so — the target records what runs in its own directory, which is
// what let a second process read it. What the set is now is the set of directories
// this run has deployed into, which is what its caller stops in cleanup.
type targetSet struct {
	make func(dir string) targetseam.Target
	made map[string]targetseam.Target
}

// newTargetSet returns a set whose targets are made by make.
func newTargetSet(make func(dir string) targetseam.Target) *targetSet {
	return &targetSet{make: make, made: map[string]targetseam.Target{}}
}

// at is the target for one environment's directory.
func (s *targetSet) at(dir string) targetseam.Target {
	if made, ok := s.made[dir]; ok {
		return made
	}
	made := s.make(dir)
	s.made[dir] = made
	return made
}

// asked is one intent a run is given: the statement, and the services it changes
// in the order decomposition declares them, each item waiting on the one before it. One
// service is the ordinary case and several is what a contract migration is.
type asked struct {
	statement string
	services  []string
}

// shipped is what the run did, which is the install's own records, one [decompositionSet]
// per intent, and one [candidate] per item. The run subcommand discards it; the
// end-to-end test asserts over it.
type shipped struct {
	// serviceID is the first service the run named, which is every single-service
	// run's only one; serviceIDs is all of them in that order.
	serviceID  string
	serviceIDs []string
	areaID     string
	// environmentID is production's, which is the record every gate row reads its
	// threshold from.
	environmentID  string
	decompositions []*decompositionSet
	candidates     []*candidate
}

// decompositionSet is one intent's set as decomposition produced it and the Decomposition row
// decided it. A decomposition that yielded one item has no firing here: the row fires where
// there is a set to ratify and nowhere else.
type decompositionSet struct {
	intentID string
	itemIDs  []string
	// fired is the Decomposition row's firing, and is empty where decomposition yielded
	// one item.
	fired fired
	// decided is whether the row fired at all, and approved whether it approved.
	decided  bool
	approved bool
	// reDecompositions is the intent's re-decomposition count after a rejection, which is what the
	// attempt limit is compared against.
	reDecompositions int
}

// candidate is one item followed as far as the run took it, each field filled as
// the record it names was written and empty from wherever that candidate stopped.
// A candidate is an item plus its build, which is why the two builds are here
// separately: the one the implementation stage made, and the one the queue's
// re-verification made from the candidate branch with master merged into it.
type candidate struct {
	intentID string
	itemID   string
	// svc is the service record this item changes, and repo is that service's
	// repository — the record's own field, so the run reads where the work is
	// rather than being told twice.
	svc            service.Service
	branch         string
	implArtifactID string
	// consumerContractArtifactID is the consumer contract version derived from the
	// same build, and is empty where the build declares nothing about another
	// service.
	consumerContractArtifactID string
	criterionIDs               []string
	buildID                    string
	commit                     string
	// measurement is the build's diff, taken where the repository is and handed to
	// every firing over that build. It is re-taken after the re-verification,
	// because that produced a different build against a master that had moved.
	measurement score.Measurement
	// waitsOn is the items decomposition declared this one waiting on, which is how a
	// consumer's item is ordered behind its producer's.
	waitsOn []string

	// The candidate's own environment and what happened on it.
	environmentID     string
	environmentDir    string
	composedFrom      []environment.Composed
	candidateDeployID string
	criteria          []gate.CriterionResult
	tornDown          bool

	// The three firings, each as it was decided. The Decomposition row is not
	// among them: it decides a set and is on the [decompositionSet].
	candidateGate fired
	mergeGate     fired
	deployGate    fired

	// checked is what enforcement found about this candidate's contracts at its
	// merge row, and published is what the fast-forward wrote for each contract its
	// build declares.
	checked   *contractcheck.Checked
	published []contract.Published

	// factoryHold is the factory's own hold where one stopped the candidate before
	// its gate could fire, and holdWaitRow is the log row where that hold is
	// written — which is the substrate's and not the dependency's.
	factoryHold string
	holdWaitRow string

	// rejected is true where the human rejected at the merge row or at the
	// candidate deploy row. That stops this candidate and is not an error: a reject
	// is the gate working.
	rejected bool
	// autoRejected is true where a mechanical check rejected at the merge row
	// before a verdict was asked for, and autoRejectedBy names which.
	autoRejected   bool
	autoRejectedBy string
	// superseded is true where the Decomposition row rejected the set this item
	// was part of.
	superseded bool
	// held is true where the human held at a deploy row.
	held bool

	// What the queue did.
	queued            bool
	merged            bool
	reverifiedBuildID string
	reverifiedCommit  string
	releaseID         string
	releaseNumber     int64
	queueRejected     bool
	queueWhy          string
	queueWaitRow      string

	deployID string
	// windowID is the watch window opened over the production deploy, and is empty
	// where none was — a rollback opens none, and neither does a redeploy of a release
	// already watched.
	windowID string
}

// The component actors of the path, named per the M1 convention. The two
// authoring agents are components too — an agent is a part of the factory,
// in a role.
var (
	scoreActor         = record.Actor{Kind: record.KindComponent, Name: "score"}
	intakeActor        = record.Actor{Kind: record.KindComponent, Name: "intake"}
	specAuthorActor    = record.Actor{Kind: record.KindComponent, Name: "agent.spec_author"}
	decompositionActor = record.Actor{Kind: record.KindComponent, Name: "decomposition"}
	dispatchActor      = record.Actor{Kind: record.KindComponent, Name: "dispatch"}
	implementerActor   = record.Actor{Kind: record.KindComponent, Name: "agent.implementer"}
	buildActor         = record.Actor{Kind: record.KindComponent, Name: "build"}
	deployActor        = record.Actor{Kind: record.KindComponent, Name: "deploy"}
)

// path is one run's collaborators, composed once. It is also the deploy agent: it
// implements [mergequeue.Repository] and [contractcheck.Checkout], because
// everything the queue needs done to a repository and everything enforcement needs
// read out of a checkout is the deploy agent's work, and neither of those
// components reaches one.
type path struct {
	d          deps
	human      record.Actor
	lines      *bufio.Scanner
	production environment.Environment
	areaID     string

	policy        *policy.Reader
	log           *decisionlog.Writer
	gate          *gate.Gate
	store         *artifact.Store
	intake        *intent.Intake
	decomposition *item.Decomposition
	dispatch      *item.Dispatch
	builds        *build.Writer
	deploys       *deploy.Writer
	candidates    *environment.Candidates
	queue         *mergequeue.Queue
	contracts     *contractcheck.Check
	// scoreVersion is the version in force for this run, held because a window
	// stores the two versions in force at its open and the health monitor does not append
	// one of its own.
	scoreVersion string
	// The three of everything downstream of a deploy: the health monitor the run
	// watches with, the notifier it tells a human through, and the reads of the
	// independent checker's own store. The notifier is nil for no install and the
	// mismatch reads are nil where no independent checker is installed, which is
	// what [gate.NoChecker] answers for.
	healthMonitor *healthmonitor.HealthMonitor
	notifier      *notifier.Notifier

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

var (
	_ mergequeue.Repository    = (*path)(nil)
	_ healthmonitor.Rollbacker = (*path)(nil)
	_ contractcheck.Checkout   = (*path)(nil)
	_ contractcheck.Exchanges  = (*path)(nil)
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
	// condemned walks the rollback's own deploy record, which is the deploy that is
	// live at the end of it.
	if deployed == "" {
		fmt.Fprintln(d.out, "Nothing reached production, so there is no deploy to walk back from")
		return s, nil
	}
	if c := p.byItem[itemOfDeploy(ctx, d.pool, deployed)]; c != nil && c.svc.ID != "" {
		if live, running, err := deploy.Current(ctx, d.pool, c.svc.ID, p.production.ID); err == nil && running {
			deployed = live.ID
		}
	}
	return s, walk(ctx, d.pool, d.out, deployed)
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
	// one, the build goes on it, and the criteria are decided there.
	for _, c := range candidates {
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

	// The independent checker's store, read and never written. Where none is installed the
	// gate is composed with [gate.NoChecker], which answers no mismatch ever — so a
	// factory without one decides exactly as it did before this milestone, and the
	// absence shows in the line below rather than as a failure.
	var mismatches gate.Checker = gate.NoChecker{}
	if d.checker != nil {
		mismatches = checker.NewStore(d.checker)
		fmt.Fprintln(d.out, "An independent checker is installed; the production deploy row reads its store at every firing")
	} else {
		fmt.Fprintln(d.out, "No independent checker is installed, so every check this factory makes reads a record it wrote itself")
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
