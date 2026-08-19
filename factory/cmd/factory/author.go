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
	"github.com/dulguun0225/borg/factory/criterion"
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

// attempt runs one authoring call, retrying while the stage has attempts left
// and returning what the call produced as soon as a reply parses.
//
// What is retried is a reply the protocol refused and an answer the client could
// not read — both are the model failing to say the thing, which another sample
// may say correctly. Nothing else is: a rate-limited or unauthorised account is
// not an attempt at the work, and what the design does with an account that has
// run out is a hold — ../../end-goal/how-humans-do-it/10-fleet.md#an-account-that-runs-out-is-a-hold
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
	return zero, fmt.Errorf("factory: the %s used all %d attempts without a reply the protocol accepts, and the factory is stuck on this item: %w",
		role, a.bound, last)
}

// author takes one intent in, refines it, cuts an item, authors the spec and the
// implementation, and writes the build record — everything up to the gate that
// decides the candidate's deploy. of is the intent's position in the run, and
// reaches nothing but what is printed.
func (p *path) author(ctx context.Context, statement, of string) (*candidate, error) {
	d := p.d
	c := &candidate{}

	// 1. Intake: the intent arrives from the owner, unrefined.
	in, err := p.intake.TakeIn(ctx, p.human, intent.SourceOwner, statement)
	if err != nil {
		return nil, err
	}
	c.intentID = in.ID
	fmt.Fprintf(d.out, "Intent %s taken in (%s): %s\n", in.ID, of, in.Statement)

	// The service record where it exists already, read before the interview
	// because the spec author is told which criteria the service already
	// promises and authors one that is not among them. This is the read; the cut
	// below is the only writer, and a first run on a service finds nothing here
	// and sends no criteria.
	svc, existing, err := service.ByName(ctx, d.pool, d.service)
	if err != nil {
		return nil, err
	}
	p.svc = svc
	promised, err := p.inForceFor(ctx, "")
	if err != nil {
		return nil, err
	}

	// 2. The interview, one round or none: the spec author asks what it
	// cannot author without and proceeds on the answer. A second question is
	// an error, which is the stopping rule enforced rather than assumed.
	author := agent.SpecAuthor{Model: d.model}
	// The subjects every policy read of this run is performed against. The
	// service is empty on a first run — the spec is authored before the cut writes
	// the service — so a pin on a service the factory has not seen does not bound
	// that run's spec stage, and does bound every run after it.
	p.subjects = policy.Subjects{ServiceID: svc.ID, AreaID: p.areaID}
	specStage, err := boundFor(ctx, p.policy, item.StageSpec, p.subjects)
	if err != nil {
		return nil, err
	}
	refined, err := attempt(d.out, specStage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, statement, nil, briefCriteria(promised))
		return r, r.Tokens, err
	})
	if err != nil {
		return nil, err
	}
	rounds := 0
	// Everything the stage spent up to a question belongs to the interview's
	// round and not to an attempt at the spec: the design counts a round against
	// this same bound but keeps it on the intent, upstream of the item's first
	// stage, and intake is what wrote the count. An intent has no spend field, so
	// the round's tokens are charged to the stage's first attempt where it is
	// reported below, and the item's total is right even though the split is
	// coarse.
	var interviewSpend int64
	if refined.Question != "" {
		rounds = 1
		for _, spend := range specStage.spends {
			interviewSpend += spend
		}
		specStage.spends = nil
		q, err := p.intake.Ask(ctx, specAuthorActor, in.ID, refined.Question)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(d.out, "The spec author asks: %s\n", q.Question)
		// An empty line is asked again rather than sent: the answer is
		// write-once, and the interview's one round is what it is spent on, so
		// a blank one would stamp the question answered and leave the spec
		// author authoring on nothing. Input that ends instead of answering is
		// readLine's error and stops the path.
		var answer string
		for answer == "" {
			answer, err = readLine(p.lines)
			if err != nil {
				return nil, err
			}
			if answer == "" {
				fmt.Fprint(d.out, "An answer is what the interview's one round is spent on; type one: ")
			}
		}
		q, err = p.intake.Answer(ctx, p.human, q.ID, answer)
		if err != nil {
			return nil, err
		}
		refined, err = attempt(d.out, specStage, "spec author", func() (agent.Refined, int64, error) {
			r, err := author.Refine(ctx, statement,
				[]agent.QA{{Question: q.Question, Answer: q.Answer}}, briefCriteria(promised))
			return r, r.Tokens, err
		})
		if err != nil {
			return nil, err
		}
		if refined.Question != "" {
			return nil, errors.New("factory: the spec author asked a second question, and the interview is one round or none")
		}
	}
	if err := p.intake.MarkRefined(ctx, intakeActor, in.ID); err != nil {
		return nil, err
	}
	fmt.Fprintf(d.out, "Intent %s refined after %d round(s)\n", in.ID, rounds)

	// 3. The cut: one service, one item, so no Decomposition gate fires —
	// it fires only where the cut yielded more than one. The service record is
	// written where the service does not exist yet: the cut writes a service's
	// identity once, and every item after the first reaches the record the first
	// one wrote. Nothing here declares what the item waits on, the cut yielding one
	// item per intent, so no run of this interface produces a dependency.
	if !existing {
		svc, err = service.NewWriter(d.pool).Create(ctx, cutActor, d.service, d.repo)
		if err != nil {
			return nil, err
		}
		p.svc = svc
	}
	p.subjects.ServiceID = svc.ID
	c.branch = "item/" + in.ID
	it, err := item.NewCut(d.pool).Create(ctx, cutActor, item.New{
		IntentID:  in.ID,
		ServiceID: svc.ID,
		AreaID:    p.areaID,
		Branch:    c.branch,
	})
	if err != nil {
		return nil, err
	}
	c.itemID = it.ID
	if existing {
		fmt.Fprintf(d.out, "Service %s already exists; item %s cut on branch %s\n", svc.ID, it.ID, c.branch)
	} else {
		fmt.Fprintf(d.out, "Service %s created; item %s cut on branch %s\n", svc.ID, it.ID, c.branch)
	}

	// 4. The spec stage: one spec version, introducing whatever criteria the
	// author drafted, then the stage reports its attempt and its spend to dispatch
	// and advances.
	//
	// The author is the model version and not the role: the prior is kept per
	// model, so two agents on one model share one.
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName}
	var drafts []artifact.Draft
	if refined.Criterion != "" {
		drafts = append(drafts, artifact.Draft{Sentence: refined.Criterion})
	}
	specArt, introduced, err := p.store.SubmitSpec(ctx, specAuthorActor, by,
		it.ID, svc.ID, refined.Spec, drafts)
	if err != nil {
		return nil, err
	}
	for _, cr := range introduced {
		c.criterionIDs = append(c.criterionIDs, cr.ID)
		fmt.Fprintf(d.out, "Spec %s submitted; criterion %s (%s): %s\n", specArt.ID, cr.ID, cr.Pattern, cr.Sentence)
	}
	if len(introduced) == 0 {
		fmt.Fprintf(d.out, "Spec %s submitted, introducing no criterion\n", specArt.ID)
	}
	// The set in force for this item's build, now that its own spec version is
	// committed. It is read here and used twice — the implementer's brief below and
	// the encoding check on the candidate environment — so the build is checked
	// against the same set the implementer was given, and an encoding cannot be
	// missing for a criterion the implementer was never told about.
	inForce, err := p.inForceFor(ctx, it.ID)
	if err != nil {
		return nil, err
	}
	// Every attempt the spec stage made, one report each, now that the item they
	// belong to exists. The reports come after the fact because the spec is
	// authored before the cut writes the item, so the count the bound was applied
	// to is in memory until here and stored after — the same number either way,
	// this being the item's only writer.
	for at, spend := range specStage.spends {
		if at == 0 {
			spend += interviewSpend
		}
		if err := p.dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, spend); err != nil {
			return nil, err
		}
	}
	if _, err := p.dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		return nil, err
	}

	// 5. The implementation stage. The repository may not exist yet, so the
	// stage initialises it, and what the candidate branch is based on follows from
	// whether master exists. The cut
	// (../../end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) says master
	// does not exist until the first release and the implementation role commits
	// the candidate branch with no base — which is every candidate cut before the
	// first one merges, not only the very first. Every candidate after that is
	// based on master, so the tree the branch starts from holds what the items
	// already merged put there: their code, and the encodings the check on the
	// candidate environment rejects a build without.
	if err := os.MkdirAll(d.repo, 0o755); err != nil {
		return nil, fmt.Errorf("factory: creating the repository directory: %w", err)
	}
	if _, err := git(d.repo, "init"); err != nil {
		return nil, err
	}
	head, err := p.masterHead(ctx)
	if err != nil {
		return nil, err
	}
	if head != "" {
		if _, err := git(d.repo, "switch", "-c", c.branch, "master"); err != nil {
			return nil, err
		}
	} else if _, err := git(d.repo, "switch", "--orphan", c.branch); err != nil {
		return nil, err
	}
	current, err := repoFiles(d.repo)
	if err != nil {
		return nil, err
	}
	// Each attempt is reported as it is made, the item being there to report it
	// against, so an item the factory gave up on carries the count in the store
	// and not only in what the run printed.
	implStage, err := boundFor(ctx, p.policy, item.StageImplementation, p.subjects)
	if err != nil {
		return nil, err
	}
	change, err := attempt(d.out, implStage, "implementer", func() (agent.Change, int64, error) {
		ch, err := agent.Implementer{Model: d.model}.Implement(ctx, agent.Brief{
			Criteria: briefCriteria(inForce),
			Spec:     refined.Spec,
			Files:    current,
		})
		if reportErr := p.dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageImplementation, ch.Tokens); reportErr != nil {
			return ch, ch.Tokens, reportErr
		}
		return ch, ch.Tokens, err
	})
	if err != nil {
		return nil, err
	}
	if err := writeFiles(d.repo, change.Files); err != nil {
		return nil, err
	}
	if _, err := git(d.repo, "add", "-A"); err != nil {
		return nil, err
	}
	if _, err := git(d.repo, "-c", "user.name=agent.implementer", "-c", "user.email=agent.implementer@factory.invalid",
		"commit", "-m", "item "+it.ID+": implement"); err != nil {
		return nil, err
	}
	commit, err := git(d.repo, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	c.commit = commit
	implArt, err := p.store.SubmitImplementation(ctx, implementerActor, by, it.ID, commit)
	if err != nil {
		return nil, err
	}
	c.implArtifactID = implArt.ID
	fmt.Fprintf(d.out, "Implementation %s submitted: commit %s on %s\n", implArt.ID, commit, c.branch)

	// 6. The build: the record, and a compile to see that there is something to
	// run. The binary that runs is produced where it will run, which is the
	// candidate's own environment, one step down — so what is checked here is only
	// that the build compiles, which is what the Implementation gate would reject a
	// build for and that gate is not built.
	bl, err := p.builds.Create(ctx, buildActor, it.ID, commit)
	if err != nil {
		return nil, err
	}
	c.buildID = bl.ID
	if err := compiles(d.repo); err != nil {
		return nil, err
	}
	fmt.Fprintf(d.out, "Build %s made from commit %s\n", bl.ID, commit)

	// The measurement is taken here, where the repository is, and against the
	// master this build was made from — before any fast-forward moves it. A
	// candidate with no master is diffed against the empty tree, which is every
	// line of it added and is the reading the design gives a first release: the
	// widest reach and nothing to return to.
	c.measurement = measure(d.repo, head != "")
	if c.measurement.Unavailable != "" {
		fmt.Fprintf(d.out, "The build's diff could not be measured: %s\n", c.measurement.Unavailable)
	}
	return c, nil
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
