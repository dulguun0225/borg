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

// platformWaitRow is the wait row already open for this item at the candidate
// deploy row, and empty where none is. The platform's ceiling is a condition and
// not a record, so the only place a wait about it is recorded is the log, and
// this is the read that keeps one wait to one row.
func (p *path) platformWaitRow(ctx context.Context, itemID string) (string, error) {
	held, err := p.readLog(ctx)
	if err != nil {
		return "", err
	}
	for _, row := range held.rows {
		if row.Shape != decisionlog.ShapeWait {
			continue
		}
		var payload platformWait
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			continue
		}
		if payload.Kind == PlatformWaitKind && payload.ItemID == itemID {
			return row.ID, nil
		}
	}
	return "", nil
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
	if c.candidateDeployID != "" && c.candidateDeployBuild == c.buildID && c.runWaitRow == "" {
		return nil
	}
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
	ceiling := d.candidateCeiling
	if p.production.MaxConcurrentCandidateEnvironments > 0 &&
		(ceiling <= 0 || p.production.MaxConcurrentCandidateEnvironments < ceiling) {
		ceiling = p.production.MaxConcurrentCandidateEnvironments
	}
	if ceiling > 0 && live >= ceiling {
		// The condition is recomputed at every firing, so a pass that meets it
		// again writes no second row about one wait: what a reader of the log
		// needs is one row per wait, and the row already there is that one.
		waitRow, err := p.platformWaitRow(ctx, c.itemID)
		if err != nil {
			return err
		}
		if waitRow == "" {
			payload, err := json.Marshal(platformWait{
				Kind:      PlatformWaitKind,
				ItemID:    c.itemID,
				Gate:      gate.DeployToCandidateEnvironment.String(),
				Condition: gate.HoldNoRoomOnThePlatform,
				Live:      live,
				Ceiling:   ceiling,
			})
			if err != nil {
				return fmt.Errorf("factory: marshalling the platform's wait for %s: %w", c.itemID, err)
			}
			row, err := p.log.AppendWaitOpen(ctx, decisionlog.Entry{Actor: deployActor, Payload: string(payload), FormatVersion: "wait/1"})
			if err != nil {
				return err
			}
			waitRow = row.ID
		}
		c.factoryHold = gate.HoldNoRoomOnThePlatform
		c.holdWaitRow = waitRow
		fmt.Fprintf(d.out, "Item %s waits at %s: %s (%d live, ceiling %d); wait row %s\n",
			c.itemID, gate.DeployToCandidateEnvironment, gate.HoldNoRoomOnThePlatform, live, ceiling, waitRow)
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
	done, opened, err := p.decideOrResume(ctx, firing, c, nil)
	if err != nil {
		return err
	}
	c.candidateGate = recordFiring(opened, done.closing)
	if done.waiting {
		c.waiting = gate.DeployToCandidateEnvironment
		return nil
	}
	switch done.verdict {
	case gate.VerdictReject:
		c.rejected = true
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Rejected: %s\nItem %s goes back to %s with an attempt counted there\n",
			done.reason, c.itemID, item.StageImplementation)
		return nil
	case gate.VerdictHold:
		c.held = true
		fmt.Fprintf(d.out, "Held by a human; item %s has no environment and nothing is deployed\n", c.itemID)
		return nil
	}

	// Compose a new candidate environment or recompose the item's existing one.
	composition, err := deploy.CompositionForCandidateRun(ctx, p, deploy.Candidate{
		ItemID: it.ID, ServiceID: it.ServiceID, ServiceName: c.svc.Name,
		ProductionID: p.production.ID, Principal: deployerPrincipal, Credential: p.d.credential,
	}, c.runWaitRow != "")
	if err != nil {
		if errors.Is(err, deploy.ErrCandidateCompositionUnavailable) {
			return p.candidateCompositionUnavailable(ctx, c, err)
		}
		return err
	}

	composed := composition.From
	if c.environmentID != "" {
		if err := deploy.RecomposeCandidateForRun(ctx, p.candidates, deployActor, c.environmentID, composition, c.runWaitRow != ""); err != nil {
			return p.candidateCompositionUnavailable(ctx, c, err)
		}
		c.composedFrom = composed
		c.composition = composition
		c.approvedComposition = composed
		c.approvedFullComposition = composition
		fmt.Fprintf(d.out, "Candidate environment %s recomposed for item %s at %s, from %s\n",
			c.environmentID, c.itemID, c.environmentDir, describeComposition(composed))
	} else {
		c.environmentDir = filepath.Join(d.dir, "candidate-"+c.itemID)
		if err := os.MkdirAll(c.environmentDir, 0o755); err != nil {
			return fmt.Errorf("factory: making the candidate environment's directory: %w", err)
		}
		env, err := deploy.ComposeCandidateForRun(ctx, p.candidates, deployActor, c.itemID, p.projectID,
			[]environment.Target{{Address: c.environmentDir}}, d.credential, composition, c.runWaitRow != "")
		if err != nil {
			return p.candidateCompositionUnavailable(ctx, c, err)
		}
		c.environmentID = env.ID
		c.composedFrom = composed
		c.composition = composition
		c.approvedComposition = composed
		c.approvedFullComposition = composition
		fmt.Fprintf(d.out, "Candidate environment %s composed for item %s at %s, from %s\n",
			env.ID, c.itemID, c.environmentDir, describeComposition(composed))
	}

	if c.candidateDeployID == "" || c.candidateDeployBuild != c.buildID {
		dep, err := p.putOnCandidateEnvironment(ctx, c, c.buildID)
		if err != nil {
			return err
		}
		c.candidateDeployID = dep.ID
		fmt.Fprintf(d.out, "Deploy %s complete: build %s runs on candidate environment %s\n", dep.ID, c.buildID, c.environmentID)
	} else if c.runWaitRow != "" {
		configuration, unavailable, err := deploy.CandidateConfiguration(ctx, p, c.svc.ID, c.composition.ValueSetVersion, p.d.secrets)
		c.configuration = configuration
		c.configurationUnavailable = unavailable
		if err != nil {
			return err
		}
	}

	if c.configurationUnavailable != "" {
		if c.runWaitRow == "" {
			configuration, unavailable, err := deploy.CandidateConfiguration(ctx, p, c.svc.ID, c.composition.ValueSetVersion, p.d.secrets)
			c.configuration = configuration
			if err != nil {
				return err
			}
			c.configurationUnavailable = unavailable
			if c.configurationUnavailable == "" {
				goto criteria
			}
		}
		row, err := deploy.OpenCandidateRunWait(ctx, candidateWaitLog{p}, deployActor, c.itemID, c.configurationUnavailable)
		if err == nil {
			c.runWaitRow = row
			c.factoryHold = c.configurationUnavailable
			c.waiting = gate.DeployToCandidateEnvironment
		}
		if err != nil {
			return err
		}
		fmt.Fprintf(d.out, "Candidate run for item %s was unavailable twice: %s; wait row %s\n",
			c.itemID, c.configurationUnavailable, c.runWaitRow)
		return nil
	}

criteria:
	c.criteria, err = p.decideCriteria(ctx, c, c.buildID, inForce)
	if err == nil {
		err = deploy.CloseCandidateRunWait(ctx, candidateWaitLog{p}, deployActor, c.runWaitRow)
		if err == nil {
			c.runWaitRow = ""
		}
	}
	return err
}

func (p *path) candidateCompositionUnavailable(ctx context.Context, c *candidate, err error) error {
	if !errors.Is(err, deploy.ErrCandidateCompositionUnavailable) {
		return err
	}
	row, waitErr := deploy.OpenCandidateRunWait(ctx, candidateWaitLog{p}, deployActor, c.itemID, err.Error())
	if waitErr != nil {
		return waitErr
	}
	c.runWaitRow = row
	c.factoryHold = err.Error()
	c.waiting = gate.DeployToCandidateEnvironment
	return nil
}

// putOnCandidateEnvironment builds the binary into the environment's directory
// and deploys it there. The deploy record names the build and no release: the
// number is minted one gate below this one.
func (p *path) putOnCandidateEnvironment(ctx context.Context, c *candidate, buildID string) (deploy.Deploy, error) {
	rebuilt, err := p.buildInto(ctx, c.svc.Repository, c.environmentDir, buildID, c.svc.ID)
	if err != nil {
		return deploy.Deploy{}, err
	}
	c.buildID = rebuilt.ID
	c.candidateDeployBuild = rebuilt.ID
	return p.intoCandidate(ctx, c, rebuilt.ID)
}

// checkEncodings rejects in both directions — a criterion in force with no
// encoding in the build naming it, and an encoding naming a criterion not in
// force or withdrawn — and prints both lists on a defect. The check's own
// errors say which criterion has no encoding and never what the encodings
// are, which leaves a human reading a defect with nothing to compare, and the
// two lists are the whole of the answer: an id missing from the build, or one
// there under a spelling the check does not recognise.
//
// A defect is not returned as an error: it is carried on c, as
// [candidate.encodingDefect] (the four check errors, joined onto one line) or
// [candidate.encodingCouldNotDerive] (a derivation that could not be made),
// and the run proceeds — the environment is composed and the encodings run
// as they would otherwise. It is [path.mergeGate]'s rejection, at the Merge
// to master row, which rejects on the defect's own terms before a verdict is
// asked for, the item going back to the implementation stage with an attempt
// counted there rather than the run stopping outright; a could-not-derive
// puts a human at that row instead of rejecting. What is still returned as an
// error is what checking could not even attempt: reading the store, or git.
func (p *path) checkEncodings(ctx context.Context, c *candidate, repo, serviceID string, of []string, inForce []criterion.Criterion) error {
	ids, err := p.itemsInBuild(ctx, serviceID, of)
	if err != nil {
		return err
	}
	rejected, err := p.rejectedSpecs(ctx)
	if err != nil {
		return err
	}
	withdrawn, err := criterion.Withdrawn(ctx, p.d.pool, ids, rejected...)
	if err != nil {
		return err
	}
	derived, err := criterion.Derive(repo)
	if err != nil {
		return err
	}
	defect := criterion.CheckEncodings(derived, inForce, withdrawn)
	if defect == nil {
		return nil
	}
	named, readErr := criterion.Encodings(repo)
	if readErr != nil {
		return errors.Join(defect, readErr)
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
	var couldNotDerive *criterion.CouldNotDeriveError
	if errors.As(defect, &couldNotDerive) {
		c.encodingCouldNotDerive = true
		return nil
	}
	c.encodingDefect = strings.Join(lines(defect.Error()), "; ")
	return nil
}
