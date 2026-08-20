package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/declaration"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/service"
)

// stageAttempts is one stage's bound, its remaining attempts, and what each of
// them spent. The bound is per stage and not per call, which is what the design
// compares it against: a stage that asks the model twice — the interview's
// question and then the spec — has its attempts across both calls and not that
// many of each. The spends are kept because a refused attempt cost tokens and
// dispatch is told about every one of them, so the item's stored count is the
// count the bound was applied to.
//
// The bound is read through package policy, so it is what an owner authored, or
// what the score supplies where they authored nothing, clamped by any pin.
type stageAttempts struct {
	bound  int
	left   int
	spends []int64
}

// boundFor is one stage's attempt bound as it is in force. The read happens once
// per stage rather than once per attempt: an owner re-authoring the bound while a
// stage is retrying would otherwise change the number the stage is being held to
// half way through it.
func boundFor(ctx context.Context, reader *policy.Reader, stage item.Stage, s policy.Subjects) (*stageAttempts, error) {
	s.Stage = stage
	effective, err := reader.AttemptBound(ctx, s)
	if err != nil {
		return nil, err
	}
	bound := int(effective.Number)
	if bound < 1 {
		return nil, fmt.Errorf("factory: the attempt bound in force at %s is %v, and a stage gets at least one attempt",
			stage, effective.Number)
	}
	return &stageAttempts{bound: bound, left: bound}, nil
}

// ErrOutOfAttempts is what a stage that spent its bound returns. It is a sentinel
// because the escalation page turns on it: the factory giving up on an item is what
// the design shows in Work as an escalation, and whether it also pages depends on the
// intent behind the item, which the caller reads and this generic function cannot.
var ErrOutOfAttempts = errors.New("factory: the stage used every attempt its bound allows")

// attempt runs one authoring call, retrying while the stage has attempts left
// and returning what the call produced as soon as a reply parses.
//
// What is retried is a reply the protocol refused and an answer the client could
// not read — both are the model failing to say the thing, which another sample
// may say correctly. Nothing else is: a rate-limited or unauthorised account is
// not an attempt at the work, and what the design does with an account that has
// run out is a hold — ../../../end-goal/how-humans-do-it/10-fleet.md#an-account-that-runs-out-is-a-hold
// — so those return on the first failure rather than spending the bound on a
// refusal that will not change. There is no wait between attempts, which costs
// nothing on a refused reply and would be the wrong shape for a rate limit
// anyway.
//
// A stage out of attempts is the factory saying it cannot do this one, which the
// design shows in Work as an escalation. There is no surface to show it on yet, so
// the run stops and the message says so, the human being at the terminal already.
func attempt[T any](out io.Writer, a *stageAttempts, role string, call func() (T, int64, error)) (T, error) {
	var zero T
	var last error
	for a.left > 0 {
		result, spend, err := call()
		a.left--
		a.spends = append(a.spends, spend)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, agent.ErrReply) && !errors.Is(err, agent.ErrAnswer) {
			return zero, err
		}
		last = err
		fmt.Fprintf(out, "The %s's reply was refused; %d attempt(s) left: %v\n", role, a.left, err)
	}
	return zero, fmt.Errorf("%w: the %s used all %d without a reply the protocol accepts, and the factory is stuck on this item: %w",
		ErrOutOfAttempts, role, a.bound, last)
}

// take is the intent this cut is authored from: the unrefined one already
// waiting with exactly this statement, or one intake takes in for it.
//
// The first is how a revert and a removal reach the pipeline. The comparison took a
// revert's intent in at the rollback and the detector takes a removal's in at the
// pass that finds the list empty, and this interface's run is given a statement
// rather than an intent id — so a run given one of those statements works that
// intent rather than taking in a second one saying the same thing.
//
// Matching on the statement is what an interface with no surface can do. What it
// costs is a false match where an owner types a statement character for character
// equal to one already waiting; the surface that replaces this shows an owner the
// intents that are waiting and has them pick.
func (p *path) take(ctx context.Context, statement string) (intent.Intent, error) {
	waiting, found, err := intent.Unrefined(ctx, p.d.pool, statement)
	if err != nil {
		return intent.Intent{}, err
	}
	if found {
		fmt.Fprintf(p.d.out, "Intent %s is already waiting with this statement, taken in from %s by %s %s; this run works it\n",
			waiting.ID, waiting.Source, waiting.Actor.Kind, waiting.Actor.Name)
		return waiting, nil
	}
	return p.intake.TakeIn(ctx, p.human, intent.SourceOwner, statement)
}

// authorIntent takes one intent in, refines it, cuts every item it yields,
// ratifies the set at Decomposition where it yielded more than one, and authors
// each item's spec, implementation, and build.
//
// The order is the design's: intake, the interview, the cut, the cut's own gate,
// and then the stages per item. One simplification is kept from M1 and it shows
// here: the interview's own call is what authors the first item's spec, so on a set
// that spec exists before Decomposition ratified the set. What that costs is one
// spec thrown away on a rejected cut, which is what a rejected round throws away
// anyway — its items are superseded and their replacements start at nothing.
func (p *path) authorIntent(ctx context.Context, one asked, of string) (*cutSet, []*candidate, error) {
	d := p.d
	if len(one.services) == 0 {
		return nil, nil, fmt.Errorf("factory: the intent %q names no service to cut an item on", one.statement)
	}

	// 1. Intake: the intent arrives from the owner, unrefined — unless one is
	// already there for this statement, which is how a revert and a removal reach the
	// pipeline.
	in, err := p.take(ctx, one.statement)
	if err != nil {
		return nil, nil, err
	}
	set := &cutSet{intentID: in.ID}
	fmt.Fprintf(d.out, "Intent %s taken in (%s): %s\n", in.ID, of, in.Statement)
	fmt.Fprintf(d.out, "  it changes %d service(s): %v\n", len(one.services), one.services)

	// gaveUp is the escalation page. A stage that spent its bound is the factory
	// saying it cannot do this one, and whether that also reaches a human out of the
	// product turns on the intent: an owner's request has nothing live that is worse,
	// and an intent a detector wrote is a defect that is live.
	gaveUp := func(itemID string, err error) error {
		if !errors.Is(err, ErrOutOfAttempts) {
			return err
		}
		if pageErr := p.escalated(ctx, in.ID, itemID, err.Error()); pageErr != nil {
			return errors.Join(err, pageErr)
		}
		return err
	}

	// 2. The interview, one round or none, and the first item's spec with it. The
	// service records that exist already are read before it, because the spec author
	// is told which criteria that service already promises and authors one that is
	// not among them.
	first := one.services[0]
	firstSvc, _, err := service.ByName(ctx, d.pool, first)
	if err != nil {
		return nil, nil, err
	}
	promised, err := p.inForceFor(ctx, firstSvc, "")
	if err != nil {
		return nil, nil, err
	}
	specStage, err := boundFor(ctx, p.policy, item.StageSpec,
		policy.Subjects{ServiceID: firstSvc.ID, AreaID: p.areaID})
	if err != nil {
		return nil, nil, err
	}
	refined, interviewSpend, rounds, err := p.interview(ctx, in, first, promised, specStage)
	if err != nil {
		return nil, nil, gaveUp("", err)
	}
	if err := p.intake.MarkRefined(ctx, intakeActor, in.ID); err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(d.out, "Intent %s refined after %d round(s)\n", in.ID, rounds)

	// 3. The cut: one item per service the intent changes, each waiting on the one
	// before it, and the service record written where the service does not exist yet.
	candidates, err := p.cutItems(ctx, in, one.services)
	if err != nil {
		return nil, nil, err
	}
	for _, c := range candidates {
		set.itemIDs = append(set.itemIDs, c.itemID)
	}

	// 4. Decomposition, where the cut yielded more than one item. One verdict
	// covers the whole cut however many services it changes.
	if len(candidates) > 1 {
		approved, err := p.decompositionGate(ctx, in, set, candidates)
		if err != nil {
			return nil, nil, err
		}
		if !approved {
			return set, candidates, nil
		}
	}

	// 5. The stages, per item: the spec, the implementation, the declaration
	// derived from the same build, and the build record.
	for n, c := range candidates {
		authored := refined
		stage := specStage
		if n > 0 {
			// Every item after the first authors its own spec, against the criteria
			// its own service already promises. The interview is not re-run: it is
			// the intent's and it is over.
			stage, err = boundFor(ctx, p.policy, item.StageSpec, p.subjectsFor(c))
			if err != nil {
				return nil, nil, err
			}
			itsPromised, err := p.inForceFor(ctx, c.svc, "")
			if err != nil {
				return nil, nil, err
			}
			author := agent.SpecAuthor{Model: d.model}
			authored, err = attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
				r, err := author.Refine(ctx, agent.Refining{
					Statement: in.Statement, Service: c.svc.Name, InForce: briefCriteria(itsPromised),
				})
				return r, r.Tokens, err
			})
			if err != nil {
				return nil, nil, gaveUp(c.itemID, err)
			}
			if authored.Question != "" {
				return nil, nil, fmt.Errorf(
					"factory: the spec author asked a question about item %s, and the interview is the intent's and is over", c.itemID)
			}
		}
		spent := interviewSpend
		if n > 0 {
			spent = 0
		}
		if err := p.specStage(ctx, c, authored, stage, spent); err != nil {
			return nil, nil, err
		}
		if err := p.implementationStage(ctx, c, authored.Spec, gaveUp); err != nil {
			return nil, nil, err
		}
	}
	return set, candidates, nil
}

// interview is the intent's one round or none, and the spec the same call
// produces. The spec author asks what it cannot author without and proceeds on the
// answer; a second question is an error, which is the stopping rule enforced rather
// than assumed.
//
// It returns what the interview spent apart from the spec, because the design
// counts a round against the same bound and keeps it on the intent, upstream of the
// item's first stage — and an intent has no spend field, so the round's tokens are
// charged to the item's first attempt where that is reported.
func (p *path) interview(ctx context.Context, in intent.Intent, serviceName string,
	promised []criterion.Criterion, stage *stageAttempts) (agent.Refined, int64, int, error) {
	d := p.d
	author := agent.SpecAuthor{Model: d.model}
	refined, err := attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, agent.Refining{
			Statement: in.Statement, Service: serviceName, InForce: briefCriteria(promised),
		})
		return r, r.Tokens, err
	})
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	if refined.Question == "" {
		return refined, 0, 0, nil
	}

	var spend int64
	for _, s := range stage.spends {
		spend += s
	}
	stage.spends = nil
	q, err := p.intake.Ask(ctx, specAuthorActor, in.ID, refined.Question)
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	fmt.Fprintf(d.out, "The spec author asks: %s\n", q.Question)
	// An empty line is asked again rather than sent: the answer is write-once, and
	// the interview's one round is what it is spent on, so a blank one would stamp
	// the question answered and leave the spec author authoring on nothing. Input
	// that ends instead of answering is readLine's error and stops the path.
	answer := ""
	for answer == "" {
		answer, err = readLine(p.lines)
		if err != nil {
			return agent.Refined{}, 0, 0, err
		}
		if answer == "" {
			fmt.Fprint(d.out, "An answer is what the interview's one round is spent on; type one: ")
		}
	}
	q, err = p.intake.Answer(ctx, p.human, q.ID, answer)
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	refined, err = attempt(d.out, stage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, agent.Refining{
			Statement: in.Statement, Service: serviceName,
			Answered: []agent.QA{{Question: q.Question, Answer: q.Answer}},
			InForce:  briefCriteria(promised),
		})
		return r, r.Tokens, err
	})
	if err != nil {
		return agent.Refined{}, 0, 0, err
	}
	if refined.Question != "" {
		return agent.Refined{}, 0, 0,
			errors.New("factory: the spec author asked a second question, and the interview is one round or none")
	}
	return refined, spend, 1, nil
}

// cutItems is the cut: one item per service the intent changes, in the order the
// intent named them, each declared to wait on the one before it.
//
// A service the work changes may not exist yet and nothing about the cut changes:
// the item that creates it is cut first and the service record is written in the
// same step, because an item names one service and the record has to exist for the
// item's only outbound link to point at anything.
//
// The order is what the cut records. Where one item cannot be verified until
// another has shipped — the producing release of a migration — that dependency is
// declared here, and both deploy gates hold on it. This interface declares a chain
// rather than deducing a graph: the services are given in order, and each item waits
// on the one before it.
func (p *path) cutItems(ctx context.Context, in intent.Intent, services []string) ([]*candidate, error) {
	d := p.d
	candidates := make([]*candidate, 0, len(services))
	previous := ""
	for n, name := range services {
		svc, existing, err := service.ByName(ctx, d.pool, name)
		if err != nil {
			return nil, err
		}
		if !existing {
			repo, err := d.repoOf(name)
			if err != nil {
				return nil, err
			}
			svc, err = service.NewWriter(d.pool).Create(ctx, cutActor, name, repo)
			if err != nil {
				return nil, err
			}
		}
		p.serviceByID[svc.ID] = svc

		var waitsOn []string
		if previous != "" {
			waitsOn = []string{previous}
		}
		// The branch is the intent's for the first item and the intent's plus the
		// service's for the rest. Two items of one intent are on two repositories, so
		// the names could not collide — but a name that says which service it is is
		// what a human reading a repository needs, and the first keeps M1's name so
		// nothing about a single-service run changes.
		branch := "item/" + in.ID
		if n > 0 {
			branch = "item/" + in.ID + "/" + name
		}
		it, err := p.cut.Create(ctx, cutActor, item.New{
			IntentID:  in.ID,
			ServiceID: svc.ID,
			AreaID:    p.areaID,
			Branch:    branch,
			WaitsOn:   waitsOn,
		})
		if err != nil {
			return nil, err
		}
		c := &candidate{
			intentID: in.ID,
			itemID:   it.ID,
			svc:      svc,
			branch:   branch,
			waitsOn:  waitsOn,
		}
		candidates = append(candidates, c)
		previous = it.ID

		was := "already exists"
		if !existing {
			was = "created"
		}
		waited := ""
		if len(waitsOn) > 0 {
			waited = fmt.Sprintf(", waiting on item %s", waitsOn[0])
		}
		fmt.Fprintf(d.out, "Service %s %s; item %s cut on branch %s%s\n", svc.ID, was, it.ID, branch, waited)
	}
	return candidates, nil
}

// decompositionGate is the cut's own gate: the one row where approving admits
// several threads at once. It fires over the set that already exists — how many
// items, which service each changes, and what waits on what — and one verdict covers
// the whole cut however many services it changes.
//
// A rejection supersedes every item of the set and counts a re-cut on the intent.
// It does not re-cut: that needs a cut which decides a decomposition rather than one
// told what to produce, and this interface is told. What that leaves is a gate that
// can stop a bad cut and cannot repair one.
func (p *path) decompositionGate(ctx context.Context, in intent.Intent, set *cutSet, candidates []*candidate) (bool, error) {
	members := make([]gate.SetMember, 0, len(candidates))
	for _, c := range candidates {
		members = append(members, gate.SetMember{
			ItemID: c.itemID, ServiceID: c.svc.ID, AreaID: p.areaID, WaitsOn: c.waitsOn,
		})
	}
	opened, err := p.gate.FireSet(ctx, gate.SetFiring{
		IntentID: in.ID, EnvironmentID: p.production.ID, Members: members,
	})
	if err != nil {
		return false, err
	}
	set.decided = true
	report(p.d.out, opened, nil)
	fmt.Fprintf(p.d.out, "  the set is %d item(s): %v\n", len(set.itemIDs), set.itemIDs)
	fmt.Fprintln(p.d.out, "  the diff factors are unavailable here, the cut happening before anything is built, so this row is scored on a vector with holes in it")

	verdict, feedback, closing, err := p.settle(ctx, opened)
	if err != nil {
		return false, err
	}
	set.fired = recordFiring(opened, closing)
	if verdict != gate.VerdictReject {
		set.approved = true
		fmt.Fprintf(p.d.out, "Approved; the cut of intent %s stands\n", in.ID)
		return true, nil
	}

	recuts, err := p.intake.CountRecut(ctx, cutActor, in.ID)
	if err != nil {
		return false, err
	}
	set.recuts = recuts
	for _, c := range candidates {
		// Every item of the set is superseded and points at nothing, because no
		// re-cut replaced it. What says why is the superseded stage beside the
		// decision that rejected the set.
		if _, err := p.cut.Supersede(ctx, cutActor, c.itemID, nil); err != nil {
			return false, err
		}
		c.superseded = true
	}
	fmt.Fprintf(p.d.out, "Rejected: %s\n", feedback)
	fmt.Fprintf(p.d.out, "  every item of the set is superseded and re-cut %d is counted on intent %s\n", recuts, in.ID)
	fmt.Fprintln(p.d.out, "  the re-cut itself is not built: this interface is told what to cut, so a bad cut is stopped here and not repaired")
	return false, nil
}

// specStage is one item's spec version and the criteria it introduces, and then
// the attempt reports and the advance.
//
// The reports come after the fact because the spec is authored before the cut
// writes the item, so the count the bound was applied to is in memory until here and
// stored after — the same number either way, this being the item's only writer.
// interviewSpend is charged to the first attempt, an intent having no spend field.
func (p *path) specStage(ctx context.Context, c *candidate, refined agent.Refined,
	stage *stageAttempts, interviewSpend int64) error {
	d := p.d
	// The author is the model version and not the role: the prior is kept per
	// model, so two agents on one model share one.
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName}
	var drafts []artifact.Draft
	if refined.Criterion != "" {
		drafts = append(drafts, artifact.Draft{Sentence: refined.Criterion})
	}
	specArt, introduced, err := p.store.SubmitSpec(ctx, specAuthorActor, by,
		c.itemID, c.svc.ID, refined.Spec, drafts)
	if err != nil {
		return err
	}
	for _, cr := range introduced {
		c.criterionIDs = append(c.criterionIDs, cr.ID)
		fmt.Fprintf(d.out, "Spec %s submitted; criterion %s (%s): %s\n", specArt.ID, cr.ID, cr.Pattern, cr.Sentence)
	}
	if len(introduced) == 0 {
		fmt.Fprintf(d.out, "Spec %s submitted, introducing no criterion\n", specArt.ID)
	}

	for at, spend := range stage.spends {
		if at == 0 {
			spend += interviewSpend
		}
		if err := p.dispatch.ReportAttempt(ctx, dispatchActor, c.itemID, item.StageSpec, spend); err != nil {
			return err
		}
	}
	_, err = p.dispatch.Advance(ctx, dispatchActor, c.itemID, item.StageImplementation)
	return err
}

// implementationStage is one item's implementation version, the declaration
// derived from the same build, the build record, and the measurement.
//
// The repository may not exist yet, so the stage initialises it, and what the
// candidate branch is based on follows from whether master exists. The cut says
// master does not exist until the first release and the implementation role commits
// the candidate branch with no base — which is every candidate cut before the first
// one merges, not only the very first. Every candidate after that is based on
// master, so the tree the branch starts from holds what the items already merged put
// there: their code, and the encodings the check on the candidate environment
// rejects a build without.
func (p *path) implementationStage(ctx context.Context, c *candidate, spec string,
	gaveUp func(string, error) error) error {
	d := p.d
	repo := c.svc.Repository
	if err := os.MkdirAll(repo, 0o755); err != nil {
		return fmt.Errorf("factory: creating the repository directory: %w", err)
	}
	if _, err := git(repo, "init"); err != nil {
		return err
	}
	head, err := p.masterHead(ctx, c.svc)
	if err != nil {
		return err
	}
	if head != "" {
		if _, err := git(repo, "switch", "-c", c.branch, "master"); err != nil {
			return err
		}
	} else if _, err := git(repo, "switch", "--orphan", c.branch); err != nil {
		return err
	}
	current, err := repoFiles(repo)
	if err != nil {
		return err
	}
	inForce, err := p.inForceFor(ctx, c.svc, c.itemID)
	if err != nil {
		return err
	}

	// Each attempt is reported as it is made, the item being there to report it
	// against, so an item the factory gave up on carries the count in the store
	// and not only in what the run printed.
	implStage, err := boundFor(ctx, p.policy, item.StageImplementation, p.subjectsFor(c))
	if err != nil {
		return err
	}
	change, err := attempt(d.out, implStage, "implementer", func() (agent.Change, int64, error) {
		ch, err := agent.Implementer{Model: d.model}.Implement(ctx, agent.Brief{
			Criteria: briefCriteria(inForce),
			Spec:     spec,
			Files:    current,
		})
		if reportErr := p.dispatch.ReportAttempt(ctx, dispatchActor, c.itemID, item.StageImplementation, ch.Tokens); reportErr != nil {
			return ch, ch.Tokens, reportErr
		}
		return ch, ch.Tokens, err
	})
	if err != nil {
		return gaveUp(c.itemID, err)
	}
	if err := writeFiles(repo, change.Files); err != nil {
		return err
	}
	if _, err := git(repo, "add", "-A"); err != nil {
		return err
	}
	if _, err := git(repo, "-c", "user.name=agent.implementer", "-c", "user.email=agent.implementer@factory.invalid",
		"commit", "-m", "item "+c.itemID+": implement"); err != nil {
		return err
	}
	commit, err := git(repo, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	c.commit = commit
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName}
	implArt, err := p.store.SubmitImplementation(ctx, implementerActor, by, c.itemID, commit)
	if err != nil {
		return err
	}
	c.implArtifactID = implArt.ID
	fmt.Fprintf(d.out, "Implementation %s submitted: commit %s on %s\n", implArt.ID, commit, c.branch)

	// The declaration, derived from the same build at the same stage and written
	// through the same store. It is derived and never typed, so what the record says
	// is what the code reads — and an item that reads nothing of another service
	// declares nothing, which is not a missing declaration.
	if err := p.declarationStage(ctx, c); err != nil {
		return err
	}

	// The build: the record, and a compile to see that there is something to run.
	// The binary that runs is produced where it will run, which is the candidate's
	// own environment, one step down — so what is checked here is only that the build
	// compiles, which is what the Implementation gate would reject a build for and
	// that gate is not built.
	bl, err := p.builds.Create(ctx, buildActor, c.itemID, commit)
	if err != nil {
		return err
	}
	c.buildID = bl.ID
	if err := compiles(repo); err != nil {
		return err
	}
	fmt.Fprintf(d.out, "Build %s made from commit %s\n", bl.ID, commit)

	// The measurement is taken here, where the repository is, and against the
	// master this build was made from — before any fast-forward moves it. A
	// candidate with no master is diffed against the empty tree, which is every
	// line of it added and is the reading the design gives a first release: the
	// widest reach and nothing to return to.
	c.measurement = measure(repo, head != "")
	if c.measurement.Unavailable != "" {
		fmt.Fprintf(d.out, "The build's diff could not be measured: %s\n", c.measurement.Unavailable)
	}
	return nil
}

// declarationStage writes the declaration version this build derives, where it
// derives anything. It is authored at the implementation stage from that item's
// build, by whoever authored the stage, and derived by the factory either way rather
// than typed.
//
// A build that declares nothing about another service submits no version. That is
// not a version introducing nothing: a version with no predicate would say the
// factory looked and found nothing, and what the records should say is that this
// build reads nothing of anyone.
func (p *path) declarationStage(ctx context.Context, c *candidate) error {
	catalog, err := p.policy.PredicateCatalog(ctx)
	if err != nil {
		return err
	}
	drafts, err := declaration.Derive(c.svc.Repository, catalog.List)
	if err != nil {
		return err
	}
	if len(drafts) == 0 {
		return nil
	}
	// The producer's name resolved to a record, where there is one. A consumer may
	// declare against an interface no release has published yet, and the empty id is
	// that answer: the declaration still says which service the build named.
	said := make([]string, 0, len(drafts))
	for n := range drafts {
		producer, found, err := service.ByName(ctx, p.d.pool, drafts[n].ProducerService)
		if err != nil {
			return err
		}
		if found {
			drafts[n].ProducerServiceID = producer.ID
		}
		said = append(said, fmt.Sprintf("%s.%s.%s %s", drafts[n].ProducerService,
			drafts[n].Interface, drafts[n].Element, drafts[n].Kind))
	}
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: p.d.modelName}
	art, written, err := p.store.SubmitDeclaration(ctx, implementerActor, by, c.itemID, c.svc.ID,
		fmt.Sprintf("%d predicate(s) derived from the build of item %s", len(drafts), c.itemID), drafts)
	if err != nil {
		return err
	}
	c.declarationArtifactID = art.ID
	fmt.Fprintf(p.d.out, "Declaration %s derived from the build: %d predicate(s) — %v\n",
		art.ID, len(written), said)
	return nil
}

// Publishes is [contractcheck.Checkout]: what the candidate's build publishes, read
// out of the checkout the candidate's branch is on. The derivation is the deploy
// agent's because reaching a checkout is, and enforcement reaches none.
func (p *path) Publishes(ctx context.Context, c contractcheck.Candidate) ([]contract.Form, error) {
	repo, err := p.repoOfItem(ctx, c)
	if err != nil {
		return nil, err
	}
	return contract.Derive(repo)
}

// Declares is [contractcheck.Checkout]: what the candidate's build declares about
// what it reads, drawn from the catalog in force.
func (p *path) Declares(ctx context.Context, c contractcheck.Candidate, catalog []string) ([]declaration.Draft, error) {
	repo, err := p.repoOfItem(ctx, c)
	if err != nil {
		return nil, err
	}
	drafts, err := declaration.Derive(repo, catalog)
	if err != nil {
		return nil, err
	}
	for n := range drafts {
		producer, found, err := service.ByName(ctx, p.d.pool, drafts[n].ProducerService)
		if err != nil {
			return nil, err
		}
		if found {
			drafts[n].ProducerServiceID = producer.ID
		}
	}
	return drafts, nil
}

// repoOfItem is the repository a candidate's checkout is in, which is the service
// record's own field.
func (p *path) repoOfItem(ctx context.Context, c contractcheck.Candidate) (string, error) {
	svc, err := p.serviceOf(ctx, c.ServiceID)
	if err != nil {
		return "", err
	}
	return svc.Repository, nil
}

// writeFiles puts what the implementer authored into the repository, refusing a
// path that leaves it.
func writeFiles(repo string, files []agent.File) error {
	for _, f := range files {
		if !filepath.IsLocal(f.Path) {
			return fmt.Errorf("factory: the implementer's file path %q leaves the repository", f.Path)
		}
		path := filepath.Join(repo, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return fmt.Errorf("factory: creating the directory of %s: %w", f.Path, err)
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return fmt.Errorf("factory: writing %s: %w", f.Path, err)
		}
	}
	return nil
}

// briefCriteria is the criteria in force as the two authoring roles are told
// them: the id an encoding names and the sentence an encoding is derived from,
// and no other field of the stored record.
func briefCriteria(inForce []criterion.Criterion) []agent.Criterion {
	told := make([]agent.Criterion, 0, len(inForce))
	for _, c := range inForce {
		told = append(told, agent.Criterion{ID: c.ID, Sentence: c.Sentence})
	}
	return told
}
