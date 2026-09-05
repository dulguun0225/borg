package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

// specStage is one item's spec version and the criteria it introduces, and then
// the attempt reports and the advance.
//
// The reports come after the fact because the spec is authored before decomposition
// writes the item, so the count the limit was applied to is in memory until here and
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
	specArt, introduced, err := p.store.SubmitSpec(ctx, p.specAuthorActor(), by,
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
	inForce, err := p.inForceFor(ctx, c.svc, c.itemID)
	if err != nil {
		return err
	}

	// Each attempt is reported as it is made, the item being there to report it
	// against, so an item the factory gave up on carries the count in the store
	// and not only in what the run printed.
	implStage, err := limitFor(ctx, p.policy, item.StageImplementation, p.subjectsFor(c))
	if err != nil {
		return err
	}
	change, err := attempt(d.out, implStage, "implementer", func() (agent.Change, int64, error) {
		ch, err := agent.Implementer{Model: d.model}.Implement(ctx, agent.Implementing{
			Criteria: rolePromptCriteria(inForce),
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
	drafts, err := consumercontract.Derive(c.svc.Repository, allowed.List)
	if err != nil {
		return err
	}
	if len(drafts) == 0 {
		return nil
	}
	// The producer's name resolved to a record, where there is one. A consumer may
	// declare against an interface no release has published yet, and the empty id is
	// that answer: the consumer contract still says which service the build named.
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
	art, written, err := p.store.SubmitConsumerContract(ctx, p.implementerActor(), by, c.itemID, c.svc.ID,
		fmt.Sprintf("%d predicate(s) derived from the build of item %s", len(drafts), c.itemID), drafts)
	if err != nil {
		return err
	}
	c.consumerContractArtifactID = art.ID
	fmt.Fprintf(p.d.out, "Consumer contract %s derived from the build: %d predicate(s) — %v\n",
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
// what it reads, drawn from the allowed predicate kinds in force.
func (p *path) Declares(ctx context.Context, c contractcheck.Candidate, allowed []string) ([]consumercontract.Draft, error) {
	repo, err := p.repoOfItem(ctx, c)
	if err != nil {
		return nil, err
	}
	drafts, err := consumercontract.Derive(repo, allowed)
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
