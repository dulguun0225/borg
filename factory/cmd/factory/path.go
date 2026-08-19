package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// deps is everything the path composes, explicit so the end-to-end test
// drives the same code the run subcommand does — a fake model, scripted
// input, temp directories — with nothing swapped anywhere but here.
type deps struct {
	pool      *pgxpool.Pool
	model     agent.Model
	modelName string // the provider's model id, which is the author an authorship prior is kept on
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
	service    string // the service's name
	area       string // the area's name, empty where the run names none
	repo       string // path of the service's git repository
	// candidateCeiling is how many candidate environments this substrate has room
	// for at once. It is the factory's own infrastructure limit and not gate
	// policy: the design says of the condition it holds on that no parameter of an
	// owner's limits it. A candidate that meets it waits, and the wait is written
	// into the log because it is not a record and no gate fired.
	candidateCeiling int
}

// targetSet is one [targetseam.Target] per environment, made on demand and kept.
// Kept, because what a local target has running is held in its own memory: asking
// twice for the target of one directory and getting two would be two views of one
// place, and the second would report nothing running.
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

// shipped is what the run did, which is the install's own records and one
// [candidate] per intent. The run subcommand discards it; the end-to-end test
// asserts over it.
type shipped struct {
	serviceID string
	areaID    string
	// environmentID is production's, which is the record every gate row reads its
	// threshold from.
	environmentID string
	candidates    []*candidate
}

// candidate is one item followed as far as the run took it, each field filled as
// the record it names was written and empty from wherever that candidate stopped.
// A candidate is an item plus its build, which is why the two builds are here
// separately: the one the implementation stage made, and the one the queue's
// re-verification made from the candidate branch with master merged into it.
type candidate struct {
	intentID       string
	itemID         string
	branch         string
	implArtifactID string
	criterionIDs   []string
	buildID        string
	commit         string
	// measurement is the build's diff, taken where the repository is and handed to
	// every firing over that build. It is re-taken after the re-verification,
	// because that produced a different build against a master that had moved.
	measurement score.Measurement

	// The candidate's own environment and what happened on it.
	environmentID     string
	environmentDir    string
	composedFrom      []environment.Composed
	candidateDeployID string
	criteria          []gate.CriterionResult
	tornDown          bool

	// The three firings, each as it was decided.
	candidateGate fired
	mergeGate     fired
	deployGate    fired

	// factoryHold is the factory's own hold where one stopped the candidate before
	// its gate could fire, and holdWaitRow is the log row where that hold is
	// written — which is the substrate's and not the dependency's.
	factoryHold string
	holdWaitRow string

	// rejected is true where the human rejected at the merge row or at the
	// candidate deploy row. That stops this candidate and is not an error: a reject
	// is the gate working.
	rejected bool
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
}

// The component actors of the path, named per the M1 convention. The two
// authoring agents are components too — an agent is a part of the factory,
// in a role.
var (
	scoreActor       = record.Actor{Kind: record.KindComponent, Name: "score"}
	intakeActor      = record.Actor{Kind: record.KindComponent, Name: "intake"}
	specAuthorActor  = record.Actor{Kind: record.KindComponent, Name: "agent.spec_author"}
	cutActor         = record.Actor{Kind: record.KindComponent, Name: "cut"}
	dispatchActor    = record.Actor{Kind: record.KindComponent, Name: "dispatch"}
	implementerActor = record.Actor{Kind: record.KindComponent, Name: "agent.implementer"}
	buildActor       = record.Actor{Kind: record.KindComponent, Name: "build"}
	deployActor      = record.Actor{Kind: record.KindComponent, Name: "deploy"}
)

// path is one run's collaborators, composed once. It is also the deploy agent:
// it implements [mergequeue.Repository], because everything the queue needs done
// to the repository and to a candidate's environment is the deploy agent's work
// and the queue does not reach a deploy target.
type path struct {
	d          deps
	human      record.Actor
	lines      *bufio.Scanner
	production environment.Environment
	areaID     string
	svc        service.Service
	subjects   policy.Subjects

	policy     *policy.Reader
	log        *decisionlog.Writer
	gate       *gate.Gate
	store      *artifact.Store
	intake     *intent.Intake
	dispatch   *item.Dispatch
	builds     *build.Writer
	deploys    *deploy.Writer
	candidates *environment.Candidates
	queue      *mergequeue.Queue

	// byItem is the candidate of each item the run has touched, so the queue's
	// re-verification can write what it produced onto the candidate the run reports.
	byItem map[string]*candidate
	// authored is the items this run cut. The queue's membership is the service's, so
	// an outcome for an item outside this set is one another run left queued — and
	// telling the two apart is what says which candidates the run has to add to what
	// it reports.
	authored map[string]bool
}

var _ mergequeue.Repository = (*path)(nil)

// run walks the whole path once for each intent it is given, from a statement to
// a running release, stopping with the first error. Every candidate reaches each
// step before any of them reaches the next, which is what makes two of them live
// at once — and the merge queue, which is the one step that is not per candidate,
// is where their order is decided.
func run(ctx context.Context, d deps, statements []string) (shipped, error) {
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

	// 1. Every candidate authored and built. The service record is read once and
	// written by the first cut that finds it missing, so the second intent of one
	// run reaches the record the first one wrote.
	for n, statement := range statements {
		c, err := p.author(ctx, statement, fmt.Sprintf("%d of %d", n+1, len(statements)))
		if err != nil {
			return s, err
		}
		s.candidates = append(s.candidates, c)
		p.byItem[c.itemID] = c
		p.authored[c.itemID] = true
	}
	s.serviceID = p.svc.ID

	// 2. Every candidate's own environment: the gate that decides its deploy
	// creates one, the build goes on it, and the criteria are decided there.
	for _, c := range s.candidates {
		if err := p.candidateEnvironment(ctx, c); err != nil {
			return s, err
		}
	}

	// 3. Every candidate's merge gate. Approving admits it to the queue, which is
	// the stage the item advances to; rejecting sends the item back.
	for _, c := range s.candidates {
		if err := p.mergeGate(ctx, c); err != nil {
			return s, err
		}
	}

	// 4. The queue, once for the service: it orders the members, re-verifies each
	// against the master that actually resulted, fast-forwards and mints, or
	// rejects on the candidate's own merits.
	adopted, err := p.runQueue(ctx)
	s.candidates = append(s.candidates, adopted...)
	if err != nil {
		return s, err
	}

	// 5. The production deploys, in the order the numbers were minted — a
	// numbered release waiting to deploy is ordered by its number and by nothing
	// else, so an owner's priority reaches every queue before this one and none
	// after it.
	deployed := ""
	for _, c := range inNumberOrder(s.candidates) {
		if err := p.productionDeploy(ctx, c); err != nil {
			return s, err
		}
		if c.deployID != "" {
			deployed = c.deployID
		}
	}

	// 6. The walk, the demonstration's direction: from the last deploy back to
	// the intent, every step a field and none reconstructed.
	if deployed == "" {
		fmt.Fprintln(d.out, "Nothing reached production, so there is no deploy to walk back from")
		return s, nil
	}
	return s, walk(ctx, d.pool, d.out, deployed)
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

	p := &path{
		d:        d,
		human:    record.Actor{Kind: record.KindHuman, Name: d.human},
		lines:    bufio.NewScanner(d.in),
		policy:   policy.NewReader(d.pool),
		log:      decisionlog.NewWriter(d.pool),
		store:    artifact.NewStore(d.pool),
		intake:   intent.NewIntake(d.pool),
		dispatch: item.NewDispatch(d.pool),
		builds:   build.NewWriter(d.pool),
		deploys:  deploy.NewWriter(d.pool),
		byItem:   map[string]*candidate{},
		authored: map[string]bool{},
	}
	p.candidates = environment.NewCandidates(d.pool)

	// The install and the two versions. The factory policy record and production's
	// environment record are what an owner authors on, and they exist before a
	// project does — so this ensures both as the owner and takes the policy version
	// in force from it. The score version is the score's own: what the source
	// publishes, appended where it has stopped matching the newest stored version.
	installed, err := policy.NewFactory(d.pool).Install(ctx, p.human, []string{d.dir}, d.credential)
	if err != nil {
		return nil, err
	}
	p.production = installed.Production
	scoreVersion, err := score.NewWriter(d.pool).Ensure(ctx, scoreActor)
	if err != nil {
		return nil, err
	}
	p.gate = gate.New(p.log, score.New(d.pool, scoreVersion), p.policy)
	p.queue = mergequeue.New(d.pool, p.log, release.NewWriter(d.pool), p.dispatch, p)
	fmt.Fprintf(d.out, "Policy version %s in force; score version %s (formula %s)\n",
		installed.Version.ID, scoreVersion.ID, scoreVersion.FormulaVersion)

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

	// The service, where the cut has already written one. It is read here so a
	// caller that drives the steps has it before the first item is authored;
	// author reads it again, being the step a first run creates it in.
	svc, _, err := service.ByName(ctx, d.pool, d.service)
	if err != nil {
		return nil, err
	}
	p.svc = svc
	return p, nil
}

// inNumberOrder is the candidates that were minted a release, lowest number
// first. A candidate with no release is left out: there is nothing to deploy.
func inNumberOrder(candidates []*candidate) []*candidate {
	var minted []*candidate
	for _, c := range candidates {
		if c.releaseID != "" {
			minted = append(minted, c)
		}
	}
	for a := 1; a < len(minted); a++ {
		for b := a; b > 0 && minted[b].releaseNumber < minted[b-1].releaseNumber; b-- {
			minted[b], minted[b-1] = minted[b-1], minted[b]
		}
	}
	return minted
}

// inForceFor is the criteria in force for one build of this service: the ones
// introduced by an item already merged, plus the ones this item's own spec
// versions introduced. A build is a set of items and this is that set — an item
// whose branch predates a sibling's spec version is not deciding that sibling's
// promise, and holding it in force would reject every candidate cut in parallel
// with the one that introduced it.
//
// itemID is empty where the caller wants what the service already promises rather
// than what a build is decided against, which is what the spec author is told.
func (p *path) inForceFor(ctx context.Context, itemID string) ([]criterion.Criterion, error) {
	if p.svc.ID == "" {
		return nil, nil
	}
	merged, err := item.AtStage(ctx, p.d.pool, p.svc.ID, item.StageMerged)
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
	return criterion.InForce(ctx, p.d.pool, p.svc.ID, ids)
}
