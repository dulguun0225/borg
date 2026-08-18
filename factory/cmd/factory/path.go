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
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
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
	target     targetseam.Target
	targets    string        // where the built binary goes and the target runs releases from
	credential secretref.Ref // the deploy credential, deploy.local
	in         io.Reader     // what the human answers on
	out        io.Writer
	human      string // the deciding human's name
	service    string // the service's name
	repo       string // path of the service's git repository
}

// shipped is how far the path went, each field filled as the record it names
// was written and empty from wherever the path stopped. The run subcommand
// discards it; the end-to-end test asserts over it.
type shipped struct {
	intentID       string
	serviceID      string
	itemID         string
	implArtifactID string
	buildID        string
	commit         string
	releaseID      string
	deployID       string
	// rejected is true where the human rejected at the gate. That stops the
	// path and is not an error: a reject is the gate working.
	rejected bool
}

// The component actors of the path, named per the M1 convention. The two
// authoring agents are components too — an agent is a part of the factory,
// in a role.
var (
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
	refined, err := author.Refine(ctx, statement, nil, briefCriteria(inForce))
	if err != nil {
		return s, err
	}
	specTokens := refined.Tokens
	rounds := 0
	if refined.Question != "" {
		rounds = 1
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
		refined, err = author.Refine(ctx, statement,
			[]agent.QA{{Question: q.Question, Answer: q.Answer}}, briefCriteria(inForce))
		if err != nil {
			return s, err
		}
		specTokens += refined.Tokens
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
	branch := "item/" + in.ID
	it, err := item.NewCut(d.pool).Create(ctx, cutActor, in.ID, svc.ID, branch)
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
	specArt, criteria, err := store.SubmitSpec(ctx, specAuthorActor, artifact.AuthorshipAgent,
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
	dispatch := item.NewDispatch(d.pool)
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageSpec, specTokens); err != nil {
		return s, err
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
	change, err := agent.Implementer{Model: d.model}.Implement(ctx, agent.Brief{
		Criteria: briefCriteria(inForce),
		Spec:     refined.Spec,
		Files:    current,
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
	implArt, err := store.SubmitImplementation(ctx, implementerActor, artifact.AuthorshipAgent, it.ID, commit)
	if err != nil {
		return s, err
	}
	s.implArtifactID = implArt.ID
	if err := dispatch.ReportAttempt(ctx, dispatchActor, it.ID, item.StageImplementation, change.Tokens); err != nil {
		return s, err
	}
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

	// 8. The gate — Merge to master, the one row M1 builds. What is printed
	// is what a human argues with: the number, each factor, each criterion
	// result.
	g := gate.New(decisionlog.NewWriter(d.pool), gate.Stub{})
	opened, err := g.Fire(ctx, gate.Firing{ItemID: it.ID, BuildID: bl.ID, ArtifactID: implArt.ID, Criteria: results})
	if err != nil {
		return s, err
	}
	fmt.Fprintf(d.out, "Gate %s fired; decision %s waits on %s\n", gate.MergeToMaster, opened.Row.ID, gate.WaitsOn)
	fmt.Fprintf(d.out, "The score's number: %s (score version %s)\n", opened.Assessment.Number, opened.Assessment.Version)
	for _, f := range opened.Assessment.Vector {
		if f.Unavailable != "" {
			fmt.Fprintf(d.out, "  factor %s: %s — unavailable: %s\n", f.Name, f.Value, f.Unavailable)
		} else {
			fmt.Fprintf(d.out, "  factor %s: %s\n", f.Name, f.Value)
		}
	}
	for _, r := range results {
		word := "failed"
		if r.Passed {
			word = "passed"
		}
		fmt.Fprintf(d.out, "  criterion %s: %s\n", r.CriterionID, word)
	}
	fmt.Fprint(d.out, "Verdict (approve, or reject <feedback>): ")
	line, err := readLine(lines)
	if err != nil {
		return s, err
	}
	verdict, feedback := gate.VerdictApprove, ""
	if line != "approve" {
		rest, isReject := strings.CutPrefix(line, "reject")
		if !isReject {
			return s, fmt.Errorf("factory: the verdict is approve or reject <feedback>, not %q", line)
		}
		verdict, feedback = gate.VerdictReject, strings.TrimSpace(rest)
	}
	closing, err := g.Decide(ctx, opened, human, verdict, feedback)
	if err != nil {
		return s, err
	}
	if verdict == gate.VerdictReject {
		s.rejected = true
		fmt.Fprintf(d.out, "Rejected: %s\nThe path stops; item %s stays at implementation\n", feedback, it.ID)
		return s, nil
	}
	fmt.Fprintf(d.out, "Approved; closing row %s\n", closing.ID)

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

	// 10. The straight deploy: the local target runs targets/<release>, so
	// the built binary is copied to the release's name first.
	if err := copyFile(binary, filepath.Join(targets, rel.ID)); err != nil {
		return s, err
	}
	dep, err := deploy.Straight(ctx, deploy.NewWriter(d.pool), d.target, deployActor,
		svc.ID, svc.Name, "production", rel.ID, d.credential)
	if err != nil {
		return s, err
	}
	s.deployID = dep.ID
	fmt.Fprintf(d.out, "Deploy %s complete: release %s runs in production\n", dep.ID, rel.ID)

	// 11. The walk, the demonstration's direction: from the deploy back to
	// the intent, every step a field and none reconstructed.
	return s, walk(ctx, d.pool, d.out, dep.ID)
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
