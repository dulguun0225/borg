package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// PlatformWaitKind is what the wait row the platform's ceiling writes says it
// is, so a reader can tell it from every other kind of wait.
const PlatformWaitKind = "platform_has_no_room"

// platformWait is what that row says. It is a wait and not a decision: no gate
// fired, and the condition is not a record — the design's arrangement for a wait
// the factory could not compute at a firing.
type platformWait struct {
	Kind      string `json:"kind"`
	ItemID    string `json:"item_id"`
	Gate      string `json:"gate"`
	Condition string `json:"condition"`
	Live      int    `json:"live_candidate_environments"`
	Ceiling   int    `json:"ceiling"`
}

// candidateEnvironment is the Deploy to candidate environment row and everything
// its approval performs: the environment created from what the candidate's
// dependencies are running, the build put on it, and the criteria decided there.
//
// The factory's own holds are computed before the gate fires, because a hold of
// that kind is not a verdict — it writes no decision and is recomputed at every
// firing. One of the two writes nothing at all; the other is written into the log
// as a wait, being neither a record nor a parameter of an owner's.
func (p *path) candidateEnvironment(ctx context.Context, c *candidate) error {
	d := p.d
	it, err := item.Get(ctx, d.pool, c.itemID)
	if err != nil {
		return err
	}
	// The repository is put on this candidate's branch before anything reads it.
	// Every candidate of the run was authored before any of them reached this step,
	// so what the working tree holds is the last one's — and the encoding check and
	// the criteria run both read the tree.
	if _, err := git(c.svc.Repository, "switch", it.Branch); err != nil {
		return err
	}

	held, err := p.dependencyHold(ctx, it)
	if err != nil {
		return err
	}
	if held != "" {
		c.factoryHold = held
		fmt.Fprintf(d.out, "Item %s waits at %s: %s\n", c.itemID, gate.DeployToCandidateEnvironment, held)
		fmt.Fprintln(d.out, "  the factory set this hold over a record that already exists, so nothing is written and it is recomputed at every firing")
		return nil
	}
	live, err := environment.CountLiveCandidates(ctx, d.pool, p.production.ID)
	if err != nil {
		return err
	}
	if live >= d.candidateCeiling {
		payload, err := json.Marshal(platformWait{
			Kind:      PlatformWaitKind,
			ItemID:    c.itemID,
			Gate:      gate.DeployToCandidateEnvironment.String(),
			Condition: gate.HoldNoRoomOnThePlatform,
			Live:      live,
			Ceiling:   d.candidateCeiling,
		})
		if err != nil {
			return fmt.Errorf("factory: marshalling the platform's wait for %s: %w", c.itemID, err)
		}
		row, err := p.log.AppendWaitOpen(ctx, decisionlog.Entry{Actor: deployActor, Payload: string(payload), FormatVersion: "wait/1"})
		if err != nil {
			return err
		}
		c.factoryHold = gate.HoldNoRoomOnThePlatform
		c.holdWaitRow = row.ID
		fmt.Fprintf(d.out, "Item %s waits at %s: %s (%d live, ceiling %d); wait row %s\n",
			c.itemID, gate.DeployToCandidateEnvironment, gate.HoldNoRoomOnThePlatform, live, d.candidateCeiling, row.ID)
		return nil
	}

	// The criteria this build will be decided against. At this row none of them
	// has been decided — the run that decides them is what this deploy is for — so
	// the firing names how many there are and no outcome, and the coverage factor
	// reads the count.
	inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
	if err != nil {
		return err
	}
	reached, err := p.exposureOf(ctx, c.buildID)
	if err != nil {
		return err
	}
	firing := gate.Firing{
		Row:             gate.DeployToCandidateEnvironment,
		ItemID:          c.itemID,
		BuildID:         c.buildID,
		ServiceID:       c.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(inForce),
		Measurement:     c.measurement,
		Exposure:        reached,
	}
	opened, err := p.gate.Fire(ctx, firing)
	if err != nil {
		return err
	}
	report(d.out, opened, nil)
	verdict, feedback, closing, err := p.settle(ctx, opened, firing)
	if err != nil {
		return err
	}
	c.candidateGate = recordFiring(opened, closing)
	switch verdict {
	case gate.VerdictReject:
		c.rejected = true
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Rejected: %s\nItem %s goes back to %s with an attempt counted there\n",
			feedback, c.itemID, item.StageImplementation)
		return nil
	case gate.VerdictHold:
		c.held = true
		fmt.Fprintf(d.out, "Held by a human; item %s has no environment and nothing is deployed\n", c.itemID)
		return nil
	}

	// The environment: composed from the producers the candidate build's
	// consumer contract names, and theirs, which is none where the build
	// declares against nothing. Its target is a directory of its own under the
	// install's, which is what makes two candidates of one service not read
	// each other's.
	composed, err := p.compositionFor(ctx, it)
	if err != nil {
		return err
	}
	c.environmentDir = filepath.Join(d.dir, "candidate-"+c.itemID)
	if err := os.MkdirAll(c.environmentDir, 0o755); err != nil {
		return fmt.Errorf("factory: making the candidate environment's directory: %w", err)
	}
	env, err := p.candidates.Compose(ctx, deployActor, c.itemID, p.projectID,
		[]environment.Target{{Address: c.environmentDir}}, d.credential, environment.Composition{From: composed})
	if err != nil {
		return err
	}
	c.environmentID = env.ID
	c.composedFrom = composed
	c.approvedComposition = composed
	fmt.Fprintf(d.out, "Candidate environment %s composed for item %s at %s, from %s\n",
		env.ID, c.itemID, c.environmentDir, describeComposition(composed))

	dep, err := p.putOnCandidateEnvironment(ctx, c, c.buildID)
	if err != nil {
		return err
	}
	c.candidateDeployID = dep.ID
	fmt.Fprintf(d.out, "Deploy %s complete: build %s runs on candidate environment %s\n", dep.ID, c.buildID, env.ID)

	c.criteria, err = p.decideCriteria(ctx, c, c.buildID, inForce)
	return err
}

// putOnCandidateEnvironment builds the binary into the environment's directory
// and deploys it there. The deploy record names the build and no release: the
// number is minted one gate below this one.
func (p *path) putOnCandidateEnvironment(ctx context.Context, c *candidate, buildID string) (deploy.Deploy, error) {
	if err := buildInto(c.svc.Repository, c.environmentDir, buildID); err != nil {
		return deploy.Deploy{}, err
	}
	return p.intoCandidate(ctx, c, buildID)
}

// decideCriteria runs the encodings on the candidate environment and records what
// each criterion's encoding produced against this build.
//
// It runs them twice. An encoding that produced a failure and a pass over the same
// build decided nothing, so that criterion is undecided for the build — and a
// second run is the only thing that can produce that verdict. What it costs is
// double the time a verification takes, to catch a class of defect a deterministic
// suite does not have.
//
// The run's result is one exit status for the whole suite, so every criterion in
// force takes the same outcome from one run. What that costs is that a suite where
// one encoding fails reads as every criterion failing, which is the coarseness of
// running the suite rather than each encoding — the encoding is code picked out by
// the criterion id it names, and nothing here runs one of them alone.
func (p *path) decideCriteria(ctx context.Context, c *candidate, buildID string,
	inForce []criterion.Criterion) ([]gate.CriterionResult, error) {
	if err := p.checkEncodings(ctx, c.svc.Repository, c.svc.ID, []string{c.itemID}, inForce); err != nil {
		return nil, err
	}

	// The composition is copied onto each run's rows, which is what
	// [criterion.Undecided] groups two runs by: two runs against compositions
	// that differ are two answers to two questions and not a disagreement.
	composition, err := json.Marshal(environment.Composition{From: c.composedFrom})
	if err != nil {
		return nil, fmt.Errorf("factory: marshalling the composition for the criterion run: %w", err)
	}

	// The run number continues from whatever this build already has, rather
	// than restarting at 1: a re-verification that changed nothing reuses the
	// build the implementation stage made, per doc.go, and a second decision
	// over that same build is the deployer's next run on it and not its first.
	nextRun, err := nextCriterionRun(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}

	first, firstOutput := runEncodings(c.svc.Repository)
	if err := p.recordCriterionRun(ctx, buildID, nextRun, string(composition), inForce, first); err != nil {
		return nil, err
	}
	second, secondOutput := runEncodings(c.svc.Repository)
	if err := p.recordCriterionRun(ctx, buildID, nextRun+1, string(composition), inForce, second); err != nil {
		return nil, err
	}
	switch {
	case first && second:
		fmt.Fprintln(p.d.out, "The encodings ran twice on the candidate environment and passed both times")
	case !first && !second:
		fmt.Fprintf(p.d.out, "The encodings ran twice on the candidate environment and failed both times:\n%s\n", firstOutput)
	default:
		fmt.Fprintf(p.d.out, "The encodings disagreed between two runs, so every criterion is undecided for build %s:\n%s\n%s\n",
			buildID, firstOutput, secondOutput)
	}

	undecided, err := criterion.Undecided(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	isUndecided := make(map[string]bool, len(undecided))
	for _, id := range undecided {
		isUndecided[id] = true
	}
	latest, err := criterion.Latest(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]criterion.Outcome, len(latest))
	for _, r := range latest {
		byID[r.CriterionID] = r.Outcome
	}
	results := make([]gate.CriterionResult, 0, len(inForce))
	for _, cr := range inForce {
		outcome := byID[cr.ID]
		if isUndecided[cr.ID] {
			outcome = criterion.OutcomeUndecided
		}
		results = append(results, gate.CriterionResult{CriterionID: cr.ID, Outcome: outcome})
	}
	return results, nil
}

// nextCriterionRun is 1 for a build with no result recorded on the candidate
// environment yet, and one past the highest run number already recorded
// otherwise — [criterion.Run.Number] being "given by the deployer in the
// order it performed them" and not reset by which call made it.
func nextCriterionRun(ctx context.Context, pool *pgxpool.Pool, buildID string) (int, error) {
	results, err := criterion.ResultsForBuild(ctx, pool, buildID)
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, r := range results {
		if r.Run > highest {
			highest = r.Run
		}
	}
	return highest + 1, nil
}

// recordCriterionRun writes what one run of the encodings on the candidate
// environment decided, one row per criterion in force, at the run number the
// deployer assigns — 1, 2, and so on across a build's runs on that
// environment.
func (p *path) recordCriterionRun(ctx context.Context, buildID string, run int, composition string,
	inForce []criterion.Criterion, passed bool) error {
	outcome := criterion.OutcomeFailed
	if passed {
		outcome = criterion.OutcomePassed
	}
	outcomes := make(map[string]criterion.Outcome, len(inForce))
	for _, cr := range inForce {
		outcomes[cr.ID] = outcome
	}
	return criterion.RecordResults(ctx, p.d.pool, p.d.token, deployActor,
		criterion.Run{BuildID: buildID, Number: run, Place: criterion.PlaceCandidateEnvironment, Composition: composition},
		outcomes)
}

// checkEncodings rejects in both directions — a criterion in force with no
// encoding in the build naming it, and an encoding naming a criterion not in
// force — and prints both lists on a failure. The check's own errors say which
// criterion has no encoding and never what the encodings are, which leaves a
// human reading a failure with nothing to compare, and the two lists are the whole
// of the answer: an id missing from the build, or one there under a spelling the
// check does not recognise.
//
// It is the Implementation gate's rejection and that gate is not built, so a
// failure here stops the run rather than sending the item back.
func (p *path) checkEncodings(ctx context.Context, repo, serviceID string, of []string, inForce []criterion.Criterion) error {
	ids, err := p.itemsInBuild(ctx, serviceID, of)
	if err != nil {
		return err
	}
	withdrawn, err := criterion.Withdrawn(ctx, p.d.pool, ids)
	if err != nil {
		return err
	}
	derived, err := criterion.Derive(repo)
	if err != nil {
		return err
	}
	err = criterion.CheckEncodings(derived, inForce, withdrawn)
	if err == nil {
		return nil
	}
	named, readErr := criterion.Encodings(repo)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	fmt.Fprintf(p.d.out, "The criteria in force: %s\n", strings.Join(criterionIDs(inForce), ", "))
	if len(named) == 0 {
		fmt.Fprintln(p.d.out, "The build names no criterion id in any _test.go file")
	} else {
		ids := make([]string, len(named))
		for n, e := range named {
			ids[n] = e.CriterionID
		}
		fmt.Fprintf(p.d.out, "The build names: %s\n", strings.Join(ids, ", "))
	}
	return err
}

// compositionFor is what the candidate's environment is composed from: the
// producers the candidate build's consumer contract names, and theirs through
// their current releases' consumer contracts, which package contractcheck walks
// over the one field that holds the edge between two services.
//
// A producer with nothing running is an error here and not a composition with a
// hole in it: the hold above is what stops a candidate whose dependency is not
// live, so reaching this with one means the two disagree.
//
// Each entry's address for this environment is not written yet: the composition
// record names a service and a release and has no field for one, which package
// contractcheck's doc.go states.
func (p *path) compositionFor(ctx context.Context, it item.Item) ([]environment.Composed, error) {
	reaches, err := p.contracts.ComposedFrom(ctx, it.ID, it.ServiceID, p.production.ID)
	if err != nil {
		return nil, err
	}
	composed := make([]environment.Composed, 0, len(reaches))
	for _, producer := range reaches {
		if producer.ReleaseID == "" {
			return nil, fmt.Errorf("factory: item %s reaches %s through %v and %s is running nothing, which the hold at %s should have caught",
				it.ID, producer.ServiceID, producer.Addresses, producer.ServiceID, gate.DeployToCandidateEnvironment)
		}
		composed = append(composed, environment.Composed{
			ServiceID: producer.ServiceID,
			ReleaseID: producer.ReleaseID,
		})
	}
	return composed, nil
}

// dependencyHold is the factory's own hold at both deploy rows: a declared
// dependency that is not its service's current release. At the candidate deploy
// row the question is whether it is live at all, the environment being composed
// from it; at the production deploy row, whether it is live still.
//
// It returns the words the hold is reported with, and nothing where every
// dependency is live. Nothing is written either way — a hold over a record that
// already exists is recomputed at every firing.
func (p *path) dependencyHold(ctx context.Context, it item.Item) (string, error) {
	for _, waitsOn := range it.WaitsOn {
		dependency, err := item.Get(ctx, p.d.pool, waitsOn)
		if err != nil {
			return "", err
		}
		addresses, err := p.addressesOf(ctx, dependency.ServiceID)
		if err != nil {
			return "", err
		}
		current, found, err := deploy.Current(ctx, p.d.pool, dependency.ServiceID, p.production.ID, addresses)
		if err != nil {
			return "", err
		}
		if !found {
			return fmt.Sprintf("%s — %s is running nothing, so item %s is not live",
				gate.HoldDependencyNotLive, dependency.ServiceID, waitsOn), nil
		}
		rel, err := release.Get(ctx, p.d.pool, current.ReleaseID)
		if err != nil {
			return "", err
		}
		if rel.ItemID != waitsOn {
			return fmt.Sprintf("%s — %s is running release %d, which is item %s and not item %s",
				gate.HoldDependencyNotLive, dependency.ServiceID, rel.Number, rel.ItemID, waitsOn), nil
		}
	}
	return "", nil
}

// describeComposition is what an environment was composed from, for a human
// reading the run. Nothing is the honest word for a candidate whose build
// declares against no producer.
func describeComposition(composed []environment.Composed) string {
	if len(composed) == 0 {
		return "nothing, its build's consumer contract naming no producer"
	}
	named := make([]string, 0, len(composed))
	for _, dependency := range composed {
		named = append(named, dependency.ServiceID+" at "+dependency.ReleaseID)
	}
	return strings.Join(named, ", ")
}
