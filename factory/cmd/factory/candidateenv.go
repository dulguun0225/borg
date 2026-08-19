package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// SubstrateWaitKind is what the wait row the substrate's ceiling writes says it
// is, so a reader can tell it from every other kind of wait.
const SubstrateWaitKind = "substrate_has_no_room"

// substrateWait is what that row says. It is a wait and not a decision: no gate
// fired, and the condition is not a record — the design's arrangement for a wait
// the factory could not compute at a firing.
type substrateWait struct {
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
	if _, err := git(d.repo, "switch", it.Branch); err != nil {
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
	live, err := environment.CountLiveCandidates(ctx, d.pool)
	if err != nil {
		return err
	}
	if live >= d.candidateCeiling {
		payload, err := json.Marshal(substrateWait{
			Kind:      SubstrateWaitKind,
			ItemID:    c.itemID,
			Gate:      string(gate.DeployToCandidateEnvironment),
			Condition: gate.HoldNoRoomForAnotherEnvironment,
			Live:      live,
			Ceiling:   d.candidateCeiling,
		})
		if err != nil {
			return fmt.Errorf("factory: marshalling the substrate's wait for %s: %w", c.itemID, err)
		}
		row, err := p.log.AppendWait(ctx, decisionlog.Entry{Actor: deployActor, Payload: string(payload)})
		if err != nil {
			return err
		}
		c.factoryHold = gate.HoldNoRoomForAnotherEnvironment
		c.holdWaitRow = row.ID
		fmt.Fprintf(d.out, "Item %s waits at %s: %s (%d live, ceiling %d); wait row %s\n",
			c.itemID, gate.DeployToCandidateEnvironment, gate.HoldNoRoomForAnotherEnvironment, live, d.candidateCeiling, row.ID)
		return nil
	}

	// The criteria this build will be decided against. At this row none of them
	// has been decided — the run that decides them is what this deploy is for — so
	// the firing names how many there are and no outcome, and the coverage factor
	// reads the count.
	inForce, err := p.inForceFor(ctx, c.itemID)
	if err != nil {
		return err
	}
	opened, err := p.gate.Fire(ctx, gate.Firing{
		Row:             gate.DeployToCandidateEnvironment,
		ItemID:          c.itemID,
		BuildID:         c.buildID,
		ServiceID:       p.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(inForce),
		Measurement:     c.measurement,
	})
	if err != nil {
		return err
	}
	report(d.out, opened, nil)
	verdict, feedback, closing, err := p.settle(ctx, opened)
	if err != nil {
		return err
	}
	c.candidateGate = recordFiring(opened, closing)
	switch verdict {
	case gate.VerdictReject:
		c.rejected = true
		if _, err := p.dispatch.SendBack(ctx, p.human, c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Rejected: %s\nItem %s goes back to %s with an attempt counted there\n",
			feedback, c.itemID, gate.ReturnsTo)
		return nil
	case gate.VerdictHold:
		c.held = true
		fmt.Fprintf(d.out, "Held by a human; item %s has no environment and nothing is deployed\n", c.itemID)
		return nil
	}

	// The environment: composed from the current release of each of the
	// candidate's dependencies, which is none on a path where the cut declares
	// none. Its target is a directory of its own under the install's, which is what
	// makes two candidates of one service not read each other's.
	composed, err := p.compositionFor(ctx, it)
	if err != nil {
		return err
	}
	c.environmentDir = filepath.Join(d.dir, "candidate-"+c.itemID)
	if err := os.MkdirAll(c.environmentDir, 0o755); err != nil {
		return fmt.Errorf("factory: making the candidate environment's directory: %w", err)
	}
	env, err := p.candidates.Compose(ctx, deployActor, c.itemID,
		[]string{c.environmentDir}, d.credential, composed)
	if err != nil {
		return err
	}
	c.environmentID = env.ID
	c.composedFrom = composed
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
	if err := buildInto(p.d.repo, c.environmentDir, buildID); err != nil {
		return deploy.Deploy{}, err
	}
	return deploy.Straight(ctx, p.deploys, p.d.targets.at(c.environmentDir), deployActor,
		p.svc.ID, p.svc.Name, c.environmentID, deploy.OfBuild(buildID), p.d.credential)
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
	if err := p.checkEncodings(inForce); err != nil {
		return nil, err
	}

	first, firstOutput := runEncodings(p.d.repo)
	second, secondOutput := runEncodings(p.d.repo)
	outcome := criterion.Decide(first, second)
	switch outcome {
	case criterion.OutcomePassed:
		fmt.Fprintln(p.d.out, "The encodings ran twice on the candidate environment and passed both times")
	case criterion.OutcomeFailed:
		fmt.Fprintf(p.d.out, "The encodings ran twice on the candidate environment and failed both times:\n%s\n", firstOutput)
	default:
		fmt.Fprintf(p.d.out, "The encodings disagreed between two runs, so every criterion is undecided for build %s:\n%s\n%s\n",
			buildID, firstOutput, secondOutput)
	}

	outcomes := make(map[string]criterion.Outcome, len(inForce))
	results := make([]gate.CriterionResult, 0, len(inForce))
	for _, cr := range inForce {
		outcomes[cr.ID] = outcome
		results = append(results, gate.CriterionResult{CriterionID: cr.ID, Outcome: outcome})
	}
	if err := criterion.RecordResults(ctx, p.d.pool, deployActor, buildID, outcomes); err != nil {
		return nil, err
	}
	return results, nil
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
func (p *path) checkEncodings(inForce []criterion.Criterion) error {
	err := criterion.CheckEncodings(p.d.repo, inForce)
	if err == nil {
		return nil
	}
	named, readErr := criterion.Encodings(p.d.repo)
	if readErr != nil {
		return errors.Join(err, readErr)
	}
	fmt.Fprintf(p.d.out, "The criteria in force: %s\n", strings.Join(criterionIDs(inForce), ", "))
	if len(named) == 0 {
		fmt.Fprintln(p.d.out, "The build names no criterion id in any _test.go file")
	} else {
		fmt.Fprintf(p.d.out, "The build names: %s\n", strings.Join(named, ", "))
	}
	return err
}

// compositionFor is the current release of each of the candidate's dependencies,
// which is what the environment is composed from. A dependency with nothing
// running is an error here and not a composition with a hole in it: the hold above
// is what stops a candidate whose dependency is not live, so reaching this with one
// means the two disagree.
func (p *path) compositionFor(ctx context.Context, it item.Item) ([]environment.Composed, error) {
	var composed []environment.Composed
	for _, waitsOn := range it.WaitsOn {
		dependency, err := item.Get(ctx, p.d.pool, waitsOn)
		if err != nil {
			return nil, err
		}
		current, found, err := deploy.Current(ctx, p.d.pool, dependency.ServiceID, p.production.ID)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("factory: item %s waits on %s and %s is running nothing, which the hold at %s should have caught",
				it.ID, waitsOn, dependency.ServiceID, gate.DeployToCandidateEnvironment)
		}
		composed = append(composed, environment.Composed{
			ServiceID: dependency.ServiceID,
			ReleaseID: current.ReleaseID,
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
		current, found, err := deploy.Current(ctx, p.d.pool, dependency.ServiceID, p.production.ID)
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
// reading the run. Nothing is the honest word for a candidate whose item declared
// no dependency, and it is what every candidate on this path reads.
func describeComposition(composed []environment.Composed) string {
	if len(composed) == 0 {
		return "nothing, its item declaring no dependency"
	}
	named := make([]string, 0, len(composed))
	for _, dependency := range composed {
		named = append(named, dependency.ServiceID+" at "+dependency.ReleaseID)
	}
	return strings.Join(named, ", ")
}
