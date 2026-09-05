package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// specStage is one item's spec version and the criteria it introduces, and
// then the advance through the stages between spec and implementation. It
// makes no model call of its own on the item's first item — that call is the
// interview's, recorded there against the intent — so the only agentrun this
// writes is for an item after the first, whose spec author call already ran
// in the caller's own loop and recorded itself.
//
// [item.Dispatch.Advance] counts the item's own attempt at entering
// implementation_plan, tasks and implementation in turn: this milestone's
// command-line interface authors nothing at the first two, so they are passed
// through rather than skipped, the stage order admitting no other way there.
func (p *path) specStage(ctx context.Context, c *candidate, refined agent.Refined) error {
	d := p.d
	// The author is the model version and not the role: the prior is kept per
	// model, so two agents on one model share one.
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName}
	requirementID := ""
	if len(c.requirementIDs) > 0 {
		requirementID = c.requirementIDs[0]
	}
	var drafts []criterion.Draft
	if refined.Criterion != "" {
		reason := ""
		if _, matched := criterion.Classify(refined.Criterion); !matched {
			reason = "not classified by the command-line interface"
		}
		drafts = append(drafts, criterion.Draft{
			Sentence:        refined.Criterion,
			NoPatternReason: reason,
			RequirementID:   requirementID,
		})
	}
	specArt, introduced, _, err := p.store.SubmitSpec(ctx, p.specAuthorActor(), by,
		c.itemID, c.svc.ID, refined.Spec, drafts, nil, nil)
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

	for _, next := range []item.Stage{item.StageImplementationPlan, item.StageTasks, item.StageImplementation} {
		if _, err := p.dispatch.Advance(ctx, dispatchActor, c.itemID, next); err != nil {
			return err
		}
	}
	return nil
}

// implementationStage is one item's implementation version, the consumer
// contract derived from the same build, the build record, and the measurement.
//
// The repository may not exist yet, so the stage initialises it, and what the
// candidate branch is based on follows from whether master exists. Decomposition says
// master does not exist until the first release and the implementation role commits
// the candidate branch with no base — which is every candidate decomposed before the first
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
	inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
	if err != nil {
		return err
	}

	// The input manifest: what was handed to the implementer before the run,
	// named by reference — context assembly is not built, so this stage writes
	// it itself, as doc.go for package inputmanifest says its caller does until
	// then.
	manifestID, err := p.writeManifest(ctx, c.itemID, item.StageImplementation, "",
		[]inputmanifest.Material{
			{Class: "spec", Reference: c.itemID, Bytes: int64(len(spec))},
			{Class: "repository_files", Reference: repo, Bytes: filesSize(current)},
		})
	if err != nil {
		return err
	}

	implStage, err := limitFor(ctx, p.policy, item.StageImplementation, p.subjectsFor(c))
	if err != nil {
		return err
	}
	change, callErr := attempt(d.out, implStage, "implementer", func() (agent.Change, int64, error) {
		ch, err := agent.Implementer{Model: d.model}.Implement(ctx, agent.Implementing{
			Criteria: rolePromptCriteria(inForce),
			Spec:     spec,
			Files:    current,
		})
		return ch, ch.Tokens, err
	})
	if recErr := p.recordAgentRun(ctx, "implementer", c.itemID, item.StageImplementation,
		manifestID, implStage.spends, outcomeOf(callErr)); recErr != nil {
		return recErr
	}
	if callErr != nil {
		return gaveUp(c.itemID, callErr)
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
	implArt, err := p.store.SubmitImplementation(ctx, p.implementerActor(), by, c.itemID, commit)
	if err != nil {
		return err
	}
	c.implArtifactID = implArt.ID
	fmt.Fprintf(d.out, "Implementation %s submitted: commit %s on %s\n", implArt.ID, commit, c.branch)

	// The consumer contract, derived from the same build at the same stage and
	// written through the same store. It is derived and never typed, so what the
	// record says is what the code reads — and an item that reads nothing of another
	// service declares nothing, which is not a missing consumer contract.
	if err := p.consumerContractStage(ctx, c); err != nil {
		return err
	}

	// The build: the record, and a compile to see that there is something to run.
	// The binary that runs is produced where it will run, which is the candidate's
	// own environment, one step down — so what is checked here is only that the build
	// compiles, which is what the Implementation gate would reject a build for and
	// that gate is not built.
	bl, err := p.createBuild(ctx, repo, c.itemID, c.svc.ID, commit)
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

// consumerContractStage writes the consumer contract version this build derives,
// where it derives anything. It is authored at the implementation stage from that
// item's build, by whoever authored the stage, and derived by the factory either way
// rather than typed.
//
// A build that declares nothing about another service submits no version. That is
// not a version introducing nothing: a version with no predicate would say the
// factory looked and found nothing, and what the records should say is that this
// build reads nothing of anyone.
func (p *path) consumerContractStage(ctx context.Context, c *candidate) error {
	allowed, err := p.policy.AllowedPredicateKinds(ctx)
	if err != nil {
		return err
	}
	derived, err := p.declaresIn(ctx, c.svc.Repository, allowed.List)
	if err != nil {
		return err
	}
	// A build that declares nothing and derived completely submits no version.
	// A run that could not derive, or one that met a construct it could not
	// follow, submits one whatever it found: "no consumer reads this" and "no
	// consumer's read was visible" call for opposite responses, and the record
	// is where the two are told apart.
	if len(derived.Drafts) == 0 && !derived.CouldNotDerive() && !derived.Partial() {
		return nil
	}
	said := make([]string, 0, len(derived.Drafts))
	for _, draft := range derived.Drafts {
		said = append(said, fmt.Sprintf("%s.%s.%s %s", draft.ProducerService,
			draft.Interface, draft.Element, draft.Kind))
	}
	by := artifact.By{Authorship: artifact.AuthorshipAgent, Author: p.d.modelName}
	art, derivation, written, err := p.store.SubmitConsumerContract(ctx, p.implementerActor(), by,
		c.itemID, c.svc.ID,
		fmt.Sprintf("%d predicate(s) derived from the build of item %s", len(derived.Drafts), c.itemID),
		derived)
	if err != nil {
		return err
	}
	c.consumerContractArtifactID = art.ID
	fmt.Fprintf(p.d.out, "Consumer contract %s derived from the build: %d predicate(s) — %v\n",
		art.ID, len(written), said)
	fmt.Fprintf(p.d.out, "  the derivation is %s\n", derivation.Describe())
	return nil
}

// DeclaresSchemaChange is [contractcheck.Checkout]: whether the candidate's
// build declares a schema change, read off the build record the build runner
// wrote it on. It is a reading of the checkout, taken where the repository was
// and recorded rather than re-taken here — the build's own process is what
// declared it, and a re-reading later would be over a repository other items
// have merged into.
func (p *path) DeclaresSchemaChange(ctx context.Context, c contractcheck.Candidate) (bool, error) {
	bl, err := build.Get(ctx, p.d.pool, c.BuildID)
	if err != nil {
		return false, err
	}
	return bl.DeclaresSchemaChange, nil
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

// Declares is [contractcheck.Checkout]: what the extractor made of the
// candidate's build — the predicates it found, the constructs it could not
// follow, or the cause it could not derive at all — drawn from the allowed
// predicate kinds in force.
func (p *path) Declares(ctx context.Context, c contractcheck.Candidate, allowed []string) (consumercontract.Derived, error) {
	repo, err := p.repoOfItem(ctx, c)
	if err != nil {
		return consumercontract.Derived{}, err
	}
	return p.declaresIn(ctx, repo, allowed)
}

// declaresIn is one run of the extractor over one checkout, with each producer's
// name resolved to its record where there is one. A consumer may declare against
// an interface no release has published yet, and the empty id is that answer: the
// consumer contract still says which service the build named.
//
// It is the one place the extractor is named, so the version the implementation
// stage submits and the version enforcement reads are derived by the same
// extractor at the same factory version — which is what makes a re-derivation
// after an upgrade a comparison and not a guess.
func (p *path) declaresIn(ctx context.Context, repo string, allowed []string) (consumercontract.Derived, error) {
	derived, err := consumercontract.Derive(repo, allowed, consumercontract.GoExtractor(factoryVersion))
	if err != nil {
		return consumercontract.Derived{}, err
	}
	for n := range derived.Drafts {
		producer, found, err := service.ByName(ctx, p.d.pool, derived.Drafts[n].ProducerService)
		if err != nil {
			return consumercontract.Derived{}, err
		}
		if found {
			derived.Drafts[n].ProducerServiceID = producer.ID
		}
	}
	return derived, nil
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

// writeManifest is [inputmanifest.Writer.Write] on this run's writer,
// returning the id a run's agentrun record names. Context assembly is not
// built, so the caller dispatching the agent writes it, as package
// inputmanifest's doc.go says.
func (p *path) writeManifest(ctx context.Context, itemID string, stage item.Stage, intentID string,
	materials []inputmanifest.Material) (string, error) {
	m, err := inputmanifest.NewWriter(p.d.pool, p.d.token).Write(ctx, dispatchActor, inputmanifest.New{
		ItemID: itemID, Stage: string(stage), IntentID: intentID, Materials: materials,
	})
	if err != nil {
		return "", err
	}
	return m.ID, nil
}

// filesSize is the total bytes of a set of files, for the material entry an
// input manifest names by reference and by size.
func filesSize(files []agent.File) int64 {
	var total int64
	for _, f := range files {
		total += int64(len(f.Content))
	}
	return total
}

// rolePromptCriteria is the criteria in force as the two authoring roles are told
// them: the id an encoding names and the sentence an encoding is derived from,
// and no other field of the stored record.
func rolePromptCriteria(inForce []criterion.Criterion) []agent.Criterion {
	told := make([]agent.Criterion, 0, len(inForce))
	for _, c := range inForce {
		told = append(told, agent.Criterion{ID: c.ID, Sentence: c.Sentence})
	}
	return told
}
