package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
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
	pool       *pgxpool.Pool
	model      agent.Model
	modelName  string // the provider's model id, which is the author an authorship prior is kept on
	target     targetseam.Target
	targets    string        // where the built binary goes and the target runs releases from
	credential secretref.Ref // the deploy credential, deploy.local
	in         io.Reader     // what the human answers on
	out        io.Writer
	human      string // the deciding human's name
	service    string // the service's name
	area       string // the area's name, empty where the run names none
	repo       string // path of the service's git repository
}

// shipped is how far the path went, each field filled as the record it names
// was written and empty from wherever the path stopped. The run subcommand
// discards it; the end-to-end test asserts over it.
type shipped struct {
	intentID       string
	serviceID      string
	areaID         string
	environmentID  string
	itemID         string
	implArtifactID string
	buildID        string
	commit         string
	releaseID      string
	deployID       string
	// The two firings, each as it was decided: what put a human at the row where
	// one was put there, the score's number, and the two versions the decision
	// names. Every fact in them is on the opening row too; they are here for the
	// end-to-end test to assert over without parsing a payload.
	merge  fired
	deploy fired
	// rejected is true where the human rejected at the merge row. That stops the
	// path and is not an error: a reject is the gate working.
	rejected bool
	// held is true where the human held at the production deploy row: the
	// release is minted, nothing is deployed, and the event stays queued with the
	// change still good.
	held bool
}

// fired is one gate firing as the path saw it.
type fired struct {
	opening       string
	closing       string
	humanDecided  bool
	whyHuman      string
	number        float64
	threshold     float64
	thresholdFrom string
	scoreVersion  string
	policyVersion string
	pins          []string
}

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
// design shows in Work as an escalation. M1 has no surface to show it on, so the
// run stops and the message says so, the human being at the terminal already.
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
	mergeActor       = record.Actor{Kind: record.KindComponent, Name: "merge"}
	deployActor      = record.Actor{Kind: record.KindComponent, Name: "deploy"}
)

// run walks the whole path once, from a statement to a running release,
// stopping with the first error. The numbered comments are the steps of
// roadmap M1's demonstration, in order.
func run(ctx context.Context, d deps, statement string) (shipped, error) {
	var s shipped
	human := record.Actor{Kind: record.KindHuman, Name: d.human}
	lines := bufio.NewScanner(d.in)

	// 0. The install and the two versions. The factory policy record and
	// production's environment record are what an owner authors on, and they
	// exist before a project does — so the run ensures both as the owner and
	// takes the policy version in force from it. The score version is the
	// score's own: what the source publishes, appended where it has stopped
	// matching the newest stored version.
	factory := policy.NewFactory(d.pool)
	installed, err := factory.Install(ctx, human, []string{d.targets}, d.credential)
	if err != nil {
		return s, err
	}
	s.environmentID = installed.Production.ID
	scoreVersion, err := score.NewWriter(d.pool).Ensure(ctx, scoreActor)
	if err != nil {
		return s, err
	}
	assessor := score.New(d.pool, scoreVersion)
	policyReader := policy.NewReader(d.pool)
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
			return s, err
		}
		if !found {
			ar, err = area.NewWriter(d.pool).Declare(ctx, human, d.area, "")
			if err != nil {
				return s, err
			}
			fmt.Fprintf(d.out, "Area %s declared as %s\n", d.area, ar.ID)
		}
		s.areaID = ar.ID
	}

	// 1. Intake: the intent arrives from the owner, unrefined.
	intake := intent.NewIntake(d.pool)
	in, err := intake.TakeIn(ctx, human, intent.SourceOwner, statement)
	if err != nil {
		return s, err
	}
	s.intentID = in.ID
	fmt.Fprintf(d.out, "Intent %s taken in: %s\n", in.ID, in.Statement)

	// The service record where it exists already, read before the interview
	// because the spec author is told which criteria are in force for the
	// service and authors one that is not among them. This is the read; the cut
	// in step 3 is the only writer, and a first run on a service finds nothing
	// here and sends no criteria.
	svc, existing, err := service.ByName(ctx, d.pool, d.service)
	if err != nil {
		return s, err
	}
	var inForce []criterion.Criterion
	if existing {
		inForce, err = criterion.InForce(ctx, d.pool, svc.ID)
		if err != nil {
			return s, err
		}
	}

	// 2. The interview, one round or none: the spec author asks what it
	// cannot author without and proceeds on the answer. A second question is
	// an error, which is the stopping rule enforced rather than assumed.
	author := agent.SpecAuthor{Model: d.model}
	// The subjects every policy read of this run is performed against. The
	// service is empty on a first run — M1's path authors the spec before the cut
	// writes the service — so a pin on a service the factory has not seen does
	// not bound that run's spec stage, and does bound every run after it.
	subjects := policy.Subjects{ServiceID: svc.ID, AreaID: s.areaID}
	specStage, err := boundFor(ctx, policyReader, item.StageSpec, subjects)
	if err != nil {
		return s, err
	}
	refined, err := attempt(d.out, specStage, "spec author", func() (agent.Refined, int64, error) {
		r, err := author.Refine(ctx, statement, nil, briefCriteria(inForce))
		return r, r.Tokens, err
	})
	if err != nil {
		return s, err
	}
	rounds := 0
	// Everything the stage spent up to a question belongs to the interview's
	// round and not to an attempt at the spec: the design counts a round against
	// this same bound but keeps it on the intent, upstream of the item's first
	// stage, and intake is what wrote the count. An intent has no spend field in
	// M1, so the round's tokens are charged to the stage's first attempt where it
	// is reported below, and the item's total is right even though the split is
	// coarse.
	var interviewSpend int64
	if refined.Question != "" {
		rounds = 1
		for _, spend := range specStage.spends {
			interviewSpend += spend
		}
		specStage.spends = nil
		q, err := intake.Ask(ctx, specAuthorActor, in.ID, refined.Question)
		if err != nil {
			return s, err
		}
		fmt.Fprintf(d.out, "The spec author asks: %s\n", q.Question)
		// An empty line is asked again rather than sent: the answer is
		// write-once, and the interview's one round is what it is spent on, so
		// a blank one would stamp the question answered and leave the spec
		// author authoring on nothing. Input that ends instead of answering is
		// readLine's error and stops the path.
		var answer string
		for answer == "" {
			answer, err = readLine(lines)
			if err != nil {
				return s, err
			}
			if answer == "" {
				fmt.Fprint(d.out, "An answer is what the interview's one round is spent on; type one: ")
			}
		}
		q, err = intake.Answer(ctx, human, q.ID, answer)
		if err != nil {
			return s, err
		}
		refined, err = attempt(d.out, specStage, "spec author", func() (agent.Refined, int64, error) {
			r, err := author.Refine(ctx, statement,
				[]agent.QA{{Question: q.Question, Answer: q.Answer}}, briefCriteria(inForce))
			return r, r.Tokens, err
		})
		if err != nil {
			return s, err
		}
		if refined.Question != "" {
			return s, errors.New("factory: the spec author asked a second question, and the interview is one round or none")
		}
	}
	if err := intake.MarkRefined(ctx, intakeActor, in.ID); err != nil {
		return s, err
	}
	fmt.Fprintf(d.out, "Intent %s refined after %d round(s)\n", in.ID, rounds)

	// 3. The cut: one service, one item, so no Decomposition gate fires —
	// it fires only where the cut yielded more than one. The service record is
	// written where the service does not exist yet: the cut writes a service's
	// identity once, and the second item on that service — a later change, or
	// this one run again after a reject — reaches the record the first one
	// wrote, which is the record read above.
	if !existing {
		svc, err = service.NewWriter(d.pool).Create(ctx, cutActor, d.service, d.repo)
		if err != nil {
			return s, err
		}
	}
	s.serviceID = svc.ID
	subjects.ServiceID = svc.ID
	branch := "item/" + in.ID
	it, err := item.NewCut(d.pool).Create(ctx, cutActor, item.New{
		IntentID:  in.ID,
		ServiceID: svc.ID,
		AreaID:    s.areaID,
		Branch:    branch,
	})
	if err != nil {
		return s, err
	}
	s.itemID = it.ID
	if existing {
		fmt.Fprintf(d.out, "Service %s already exists; item %s cut on branch %s\n", svc.ID, it.ID, branch)
	} else {
		fmt.Fprintf(d.out, "Service %s created; item %s cut on branch %s\n", svc.ID, it.ID, branch)
	}

	// 4. The spec stage: one spec version, introducing one criterion, then
	// the stage reports its attempt and its spend to dispatch and advances.
	store := artifact.NewStore(d.pool)
	// The author is the model version and not the role: the prior is kept per
	// model, so two agents on one model share one.
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName}
	specArt, criteria, err := store.SubmitSpec(ctx, specAuthorActor, by,
		it.ID, svc.ID, refined.Spec, []artifact.Draft{{Sentence: refined.Criterion}})
	if err != nil {
		return s, err
	}
	cr := criteria[0]
	// The set in force again, now that the spec version introducing this
	// item's criterion is committed. It is read once here and used twice — the
	// implementer's brief in step 5 and the encoding check in step 7 — so the
	// build is checked against the same set the implementer was given, and an
	// encoding cannot be missing for a criterion the implementer was never
	// told about.
	inForce, err = criterion.InForce(ctx, d.pool, svc.ID)
	if err != nil {
		return s, err
	}
	// Every attempt the spec stage made, one report each, now that the item they
	// belong to exists. The reports come after the fact because M1's path authors
	// the spec before the cut writes the item, so the count the bound was applied
	// to is in memory until here and stored after — the same number either way,
	// this being the item's only writer.
	dispatch := item.NewDispatch(d.pool)
	for n, spend := range specStage.spends {
		if n == 0 {
			spend += interviewSpend
		}
		if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, spend); err != nil {
			return s, err
		}
	}
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageImplementation); err != nil {
		return s, err
	}
	fmt.Fprintf(d.out, "Spec %s submitted; criterion %s (%s): %s\n", specArt.ID, cr.ID, cr.Pattern, cr.Sentence)

	// 5. The implementation stage. The repository may not exist yet, so the
	// stage initialises it, and what the candidate branch is based on follows
	// from whether master exists. The cut
	// (../../end-goal/how-humans-do-it/02-intent-into-items.md#the-cut) says
	// master does not exist until the first release and the implementation role
	// commits the candidate branch with no base — the first item's case and no
	// later one. Every item after it is based on master, so the tree the branch
	// starts from holds what the items already merged put there: their code, and
	// the encodings the check in step 7 rejects a build without.
	if err := os.MkdirAll(d.repo, 0o755); err != nil {
		return s, fmt.Errorf("factory: creating the repository directory: %w", err)
	}
	if _, err := git(d.repo, "init"); err != nil {
		return s, err
	}
	masterCreated, err := masterExists(d.repo)
	if err != nil {
		return s, err
	}
	if masterCreated {
		if _, err := git(d.repo, "switch", "-c", branch, "master"); err != nil {
			return s, err
		}
	} else if _, err := git(d.repo, "switch", "--orphan", branch); err != nil {
		return s, err
	}
	current, err := repoFiles(d.repo)
	if err != nil {
		return s, err
	}
	// Each attempt is reported as it is made, the item being there to report it
	// against, so an item the factory gave up on carries the count in the store
	// and not only in what the run printed.
	implStage, err := boundFor(ctx, policyReader, item.StageImplementation, subjects)
	if err != nil {
		return s, err
	}
	change, err := attempt(d.out, implStage, "implementer", func() (agent.Change, int64, error) {
		c, err := agent.Implementer{Model: d.model}.Implement(ctx, agent.Brief{
			Criteria: briefCriteria(inForce),
			Spec:     refined.Spec,
			Files:    current,
		})
		if reportErr := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageImplementation, c.Tokens); reportErr != nil {
			return c, c.Tokens, reportErr
		}
		return c, c.Tokens, err
	})
	if err != nil {
		return s, err
	}
	for _, f := range change.Files {
		if !filepath.IsLocal(f.Path) {
			return s, fmt.Errorf("factory: the implementer's file path %q leaves the repository", f.Path)
		}
		path := filepath.Join(d.repo, f.Path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return s, fmt.Errorf("factory: creating the directory of %s: %w", f.Path, err)
		}
		if err := os.WriteFile(path, []byte(f.Content), 0o644); err != nil {
			return s, fmt.Errorf("factory: writing %s: %w", f.Path, err)
		}
	}
	if _, err := git(d.repo, "add", "-A"); err != nil {
		return s, err
	}
	if _, err := git(d.repo, "-c", "user.name=agent.implementer", "-c", "user.email=agent.implementer@factory.invalid",
		"commit", "-m", "item "+it.ID+": implement "+cr.ID); err != nil {
		return s, err
	}
	commit, err := git(d.repo, "rev-parse", "HEAD")
	if err != nil {
		return s, err
	}
	s.commit = commit
	implArt, err := store.SubmitImplementation(ctx, implementerActor, by, it.ID, commit)
	if err != nil {
		return s, err
	}
	s.implArtifactID = implArt.ID
	fmt.Fprintf(d.out, "Implementation %s submitted: commit %s on %s\n", implArt.ID, commit, branch)

	// 6. The build: the record first, so the binary's path can name it.
	// There is no candidate environment until M3, so the build is made here
	// and the criterion is decided wherever the build was made — here too.
	bl, err := build.NewWriter(d.pool).Create(ctx, buildActor, it.ID, commit)
	if err != nil {
		return s, err
	}
	s.buildID = bl.ID
	targets, err := filepath.Abs(d.targets)
	if err != nil {
		return s, fmt.Errorf("factory: resolving the targets directory: %w", err)
	}
	binary := filepath.Join(targets, "build-"+bl.ID)
	if _, err := inDir(d.repo, "go", "build", "-o", binary, "."); err != nil {
		return s, err
	}
	fmt.Fprintf(d.out, "Build %s made from commit %s\n", bl.ID, commit)

	// 7. The criteria, both directions: every criterion in force has an
	// encoding in the build naming it and every encoding names one in force.
	// The set is the one read in step 4 and given to the implementer. Then the
	// encodings run, and the run's result is each criterion's Passed — a failed
	// run is not an error here, because deciding over it is the gate's.
	if err := criterion.CheckEncodings(d.repo, inForce); err != nil {
		// What the build does name, printed beside what it was required to name.
		// The check's own errors say which criterion has no encoding and never
		// what the encodings are, which leaves a human reading a failure with
		// nothing to compare — and the two lists are the whole of the answer:
		// an id missing from the build, or one there under a spelling the check
		// does not recognise.
		named, readErr := criterion.Encodings(d.repo)
		if readErr != nil {
			return s, errors.Join(err, readErr)
		}
		fmt.Fprintf(d.out, "The criteria in force: %s\n", strings.Join(criterionIDs(inForce), ", "))
		if len(named) == 0 {
			fmt.Fprintln(d.out, "The build names no criterion id in any _test.go file")
		} else {
			fmt.Fprintf(d.out, "The build names: %s\n", strings.Join(named, ", "))
		}
		return s, err
	}
	testOutput, testErr := inDir(d.repo, "go", "test", "./...")
	passed := testErr == nil
	results := make([]gate.CriterionResult, 0, len(inForce))
	for _, c := range inForce {
		results = append(results, gate.CriterionResult{CriterionID: c.ID, Passed: passed})
	}
	if passed {
		fmt.Fprintln(d.out, "The encodings ran and passed")
	} else {
		fmt.Fprintf(d.out, "The encodings ran and failed:\n%s\n", testOutput)
	}

	// 8. The gate — Merge to master. The score is real from here, so what is
	// printed is what a human argues with when one is put at the row: the number
	// beside the threshold it was compared against, every factor with the
	// quantity it was read from, and every criterion result.
	//
	// The measurement is taken here, where the repository is, and against the
	// master this build was made from — before the fast-forward moves it. A
	// first item has no master and is diffed against the empty tree, which is
	// every line of it added and is the reading the design gives a first release:
	// the widest reach and nothing to return to.
	measurement := measure(d.repo, masterCreated)
	if measurement.Unavailable != "" {
		fmt.Fprintf(d.out, "The build's diff could not be measured: %s\n", measurement.Unavailable)
	}
	g := gate.New(decisionlog.NewWriter(d.pool), assessor, policyReader)
	merge := gate.Firing{
		Row:           gate.MergeToMaster,
		ItemID:        it.ID,
		BuildID:       bl.ID,
		ArtifactID:    implArt.ID,
		ServiceID:     svc.ID,
		AreaID:        s.areaID,
		EnvironmentID: installed.Production.ID,
		Criteria:      results,
		Measurement:   measurement,
	}
	opened, err := g.Fire(ctx, merge)
	if err != nil {
		return s, err
	}
	report(d.out, opened, results)
	verdict, feedback, closing, err := settle(ctx, d, g, opened, human, lines)
	if err != nil {
		return s, err
	}
	s.merge = recordFiring(opened, closing)
	if verdict == gate.VerdictReject {
		s.rejected = true
		fmt.Fprintf(d.out, "Rejected: %s\nThe path stops; item %s stays at implementation\n", feedback, it.ID)
		return s, nil
	}

	// 9. The fast-forward: one command that creates master at the candidate
	// commit or fast-forwards it to there, and refuses anything else. Then
	// the release is minted and the item is merged.
	//
	// The two are one event in the design and two statements here, so either
	// order leaves a half-write. This one: a mint that fails after the
	// fast-forward leaves master at the candidate commit with no release record
	// naming it, so the repository says the change is merged and the store has
	// nothing to deploy, and the next item's mint takes the number this one
	// would have had. The other order costs a release record numbering a commit
	// that never reached master, which is worse — a deploy could put it live.
	// What makes the two one event is the merge queue, and that is M3, so until
	// then a half-write is found by a reader noticing it rather than by the
	// factory, the way a stuck started deploy record is.
	if _, err := git(d.repo, "fetch", ".", commit+":master"); err != nil {
		return s, err
	}
	rel, err := release.NewWriter(d.pool).Mint(ctx, mergeActor, svc.ID, bl.ID, it.ID)
	if err != nil {
		return s, err
	}
	s.releaseID = rel.ID
	if _, err := dispatch.Advance(ctx, dispatchActor, it.ID, item.StageMerged); err != nil {
		return s, err
	}
	fmt.Fprintf(d.out, "Master fast-forwarded to %s; release %s minted, number %d\n", commit, rel.ID, rel.Number)

	// 10. The gate — Deploy to production, the row this milestone adds and the
	// one that offers hold. It fires after the release exists and is written
	// against the item and the build like the row above it, and it names no
	// artifact: there is none under decision at a deploy. Its vector is computed
	// again, because every firing produces one of its own — and the merge row's
	// verdict, where a human gave one, has already moved the prior it reads.
	deployRow := merge
	deployRow.Row = gate.DeployToProduction
	deployRow.ArtifactID = ""
	openedDeploy, err := g.Fire(ctx, deployRow)
	if err != nil {
		return s, err
	}
	report(d.out, openedDeploy, results)
	deployVerdict, _, deployClosing, err := settle(ctx, d, g, openedDeploy, human, lines)
	if err != nil {
		return s, err
	}
	s.deploy = recordFiring(openedDeploy, deployClosing)
	if deployVerdict == gate.VerdictHold {
		s.held = true
		fmt.Fprintf(d.out, "Held; release %s is minted and is not deployed, and the event stays queued\n", rel.ID)
		fmt.Fprintf(d.out, "No attempt is counted and the score learns nothing from a hold; item %s stays where it is\n", it.ID)
		return s, nil
	}

	// 11. The straight deploy: the local target runs targets/<release>, so
	// the built binary is copied to the release's name first. Straight because
	// this substrate moves a process rather than traffic, so the strategy that
	// keeps a control is unavailable and there is nothing to pin.
	if err := copyFile(binary, filepath.Join(targets, rel.ID)); err != nil {
		return s, err
	}
	dep, err := deploy.Straight(ctx, deploy.NewWriter(d.pool), d.target, deployActor,
		svc.ID, svc.Name, installed.Production.ID, rel.ID, d.credential)
	if err != nil {
		return s, err
	}
	s.deployID = dep.ID
	fmt.Fprintf(d.out, "Deploy %s complete: release %s runs in production\n", dep.ID, rel.ID)

	// 12. The walk, the demonstration's direction: from the deploy back to
	// the intent, every step a field and none reconstructed.
	return s, walk(ctx, d.pool, d.out, dep.ID)
}

// report prints one firing as a human at the row would read it: the number
// beside the threshold it was compared against and where that threshold came
// from, every factor with the quantity it was read from, every unavailable
// factor with its reason, and every criterion result. It prints the same lines
// whether or not a human decides, because an auto-pass an owner cannot read is
// an auto-pass they cannot argue with.
func report(out io.Writer, opened gate.Opened, results []gate.CriterionResult) {
	a, applied := opened.Assessment, opened.Applied
	fmt.Fprintf(out, "Gate %s fired; decision %s\n", opened.Gate, opened.Row.ID)
	fmt.Fprintf(out, "  number %.3f against threshold %.3f (%s), likelihood %.3f, impact %.3f, exposure %.3f\n",
		a.Number, applied.Threshold, applied.ThresholdFrom, a.Likelihood, a.Impact, a.Exposure)
	fmt.Fprintf(out, "  score version %s (formula %s), policy version %s\n",
		a.Version, a.FormulaVersion, applied.PolicyVersion)
	for _, f := range a.Vector {
		if f.Unavailable != "" {
			fmt.Fprintf(out, "  factor %s: unavailable — %s\n", f.Name, f.Unavailable)
			continue
		}
		fmt.Fprintf(out, "  factor %s: %.2f (%s, weight %.2f) — %s\n",
			f.Name, f.Level, f.Half, f.Weight, f.Reading)
	}
	for _, r := range results {
		word := "failed"
		if r.Passed {
			word = "passed"
		}
		fmt.Fprintf(out, "  criterion %s: %s\n", r.CriterionID, word)
	}
	for _, id := range applied.Pins {
		fmt.Fprintf(out, "  pin %s applies here\n", id)
	}
	if opened.HumanDecides {
		fmt.Fprintf(out, "  a human decides: %s; the row waits on %s\n", opened.WhyHuman, gate.WaitsOn(opened.Gate))
		return
	}
	fmt.Fprintln(out, "  no human decides: the number is under the threshold and no pin adds one")
}

// settle closes one firing: the factory's own verdict where the firing put no
// human at the row, and the human's typed verdict where it did. What may be
// typed is what the row offers, which differs per row — the merge row rejects
// and the deploy row holds.
func settle(ctx context.Context, d deps, g *gate.Gate, opened gate.Opened,
	human record.Actor, lines *bufio.Scanner) (gate.Verdict, string, decisionlog.Row, error) {
	if !opened.HumanDecides {
		closing, err := g.AutoPass(ctx, opened)
		if err != nil {
			return "", "", decisionlog.Row{}, err
		}
		fmt.Fprintf(d.out, "Auto-passed by the threshold; closing row %s written as the gate component\n", closing.ID)
		return gate.VerdictApprove, "", closing, nil
	}

	actions, err := gate.Actions(opened.Gate)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	fmt.Fprintf(d.out, "Verdict (%s): ", strings.Join(words(actions), ", "))
	line, err := readLine(lines)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	verdict, feedback, err := typed(line, actions)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	closing, err := g.Decide(ctx, opened, human, verdict, feedback)
	if err != nil {
		return "", "", decisionlog.Row{}, err
	}
	fmt.Fprintf(d.out, "The verdict is %s; closing row %s written as %s %s\n",
		verdict, closing.ID, closing.Actor.Kind, closing.Actor.Name)
	return verdict, feedback, closing, nil
}

// typed reads a verdict the human typed. A reject carries its feedback after the
// word, which is what the action is: reject with feedback.
func typed(line string, actions []gate.Verdict) (gate.Verdict, string, error) {
	for _, action := range actions {
		rest, matched := strings.CutPrefix(line, string(action))
		if !matched {
			continue
		}
		return action, strings.TrimSpace(rest), nil
	}
	return "", "", fmt.Errorf("factory: the verdict is one of %s, not %q", strings.Join(words(actions), ", "), line)
}

// words is the actions as they are offered on the terminal, the reject carrying
// the feedback its action is named for.
func words(actions []gate.Verdict) []string {
	offered := make([]string, 0, len(actions))
	for _, a := range actions {
		if a == gate.VerdictReject {
			offered = append(offered, "reject <feedback>")
			continue
		}
		offered = append(offered, string(a))
	}
	return offered
}

// recordFiring is one firing as the end-to-end test reads it. Every field is on
// the opening row as well; this saves the test a payload to unmarshal.
func recordFiring(opened gate.Opened, closing decisionlog.Row) fired {
	return fired{
		opening:       opened.Row.ID,
		closing:       closing.ID,
		humanDecided:  opened.HumanDecides,
		whyHuman:      opened.WhyHuman,
		number:        opened.Assessment.Number,
		threshold:     opened.Applied.Threshold,
		thresholdFrom: string(opened.Applied.ThresholdFrom),
		scoreVersion:  opened.Assessment.Version,
		policyVersion: opened.Applied.PolicyVersion,
		pins:          opened.Applied.Pins,
	}
}

// readLine is the next line the human typed, without its line ending and
// surrounding blank space.
func readLine(lines *bufio.Scanner) (string, error) {
	if !lines.Scan() {
		if err := lines.Err(); err != nil {
			return "", fmt.Errorf("factory: reading the human's input: %w", err)
		}
		return "", errors.New("factory: the human's input ended before the path was done asking")
	}
	return strings.TrimSpace(lines.Text()), nil
}

// inDir runs one command in dir and returns its combined output. On an error
// the output is part of the message, because the command's own words are what
// a human fixes the failure by.
func inDir(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("factory: %s %s in %s: %w: %s",
			name, strings.Join(args, " "), dir, err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// git runs one git command in repo and returns its output trimmed, which for
// rev-parse is the value asked for.
func git(repo string, args ...string) (string, error) {
	out, err := inDir(repo, "git", args...)
	return strings.TrimSpace(out), err
}

// masterExists says whether the repository has master yet, which is what
// decides the candidate branch's base. git rev-parse --verify --quiet exits 1
// for a ref that is not there, and that one code is read as absent while every
// other failure is returned — a broken repository reported here rather than a
// branch quietly committed with no base, which is what would drop the tree the
// items already merged left.
func masterExists(repo string) (bool, error) {
	_, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/heads/master")
	if err == nil {
		return true, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) && exit.ExitCode() == 1 {
		return false, nil
	}
	return false, err
}

// criterionIDs is the ids of a criterion set, for a message that names which
// ones the build was required to encode.
func criterionIDs(inForce []criterion.Criterion) []string {
	ids := make([]string, 0, len(inForce))
	for _, c := range inForce {
		ids = append(ids, c.ID)
	}
	return ids
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

// repoFiles is the repository's current files, whole, for the implementer's
// brief — none on the first item, whose branch has no base, and the tree master
// points at for every item after it. The .git directory is the repository's
// bookkeeping, not part of the change, and is skipped.
func repoFiles(repo string) ([]agent.File, error) {
	var files []agent.File
	err := filepath.WalkDir(repo, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(repo, path)
		if err != nil {
			return err
		}
		files = append(files, agent.File{Path: rel, Content: string(content)})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("factory: reading the repository's files: %w", err)
	}
	return files, nil
}

// copyFile copies the built binary to the release's name, executable, because
// the local target starts exactly targets/<release>.
func copyFile(from, to string) error {
	content, err := os.ReadFile(from)
	if err != nil {
		return fmt.Errorf("factory: reading %s: %w", from, err)
	}
	if err := os.WriteFile(to, content, 0o755); err != nil {
		return fmt.Errorf("factory: writing %s: %w", to, err)
	}
	return nil
}
