package main

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
	"github.com/dulguun0225/borg/factory/window"
)

// One item's candidate read back out of the records, which is what makes the
// pass resumable: a row a human decides is left pending in Work by the pass that
// fired it, so every field the stage below it reads has to come from a record
// rather than from the pass that authored it. resume.go is where the step the
// pass enters at is derived.

// rehydrate is one item's candidate read back out of the records: every field
// the steps below it read, filled from the record that holds it, and the step
// the pass enters at.
//
// Four fields are derived rather than stored and each is named where it is
// filled: the measurement, taken again from the repository the way the stage
// that built took it; the composition the run that passed at Merge to master
// ran against, read off the criterion results of that build, which is where a
// run's own composition survives; which of the item's builds the rows below the
// implementation stage decide, told from the merge queue's by the release; and
// the seven firings, read back off the log's own rows.
//
// One is deliberately empty. A compile failure is the reading of one attempt and
// the stage that re-enters takes its own.
func (p *path) rehydrate(ctx context.Context, itemID string) (*candidate, error) {
	it, err := item.Get(ctx, p.d.pool, itemID)
	if err != nil {
		return nil, err
	}
	svc, err := p.serviceOf(ctx, it.ServiceID)
	if err != nil {
		return nil, err
	}
	c := &candidate{
		itemID:         itemID,
		intentID:       it.IntentID,
		svc:            svc,
		branch:         it.Branch,
		waitsOn:        it.WaitsOn,
		requirementIDs: it.RequirementsAnswered,
		superseded:     it.Stage == item.StageSuperseded,
		// An item that merged was admitted to the queue first, so both are read
		// off the one stage the item stands at now.
		queued: it.Stage == item.StageQueued || it.Stage == item.StageMerged,
		merged: it.Stage == item.StageMerged,
	}

	// What the spec author is told, which is the intent's own and the service's
	// own: the statement, the requirements this item answers, the criteria the
	// service already promises, the constraints the request arrived with, and
	// the area where it is graded irreversible.
	in, err := intent.Get(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return nil, err
	}
	c.statement = in.Statement
	c.constraints = constraintsInForce(in)
	// What this item answers, whole or as a share: the intent's requirements in
	// force narrowed to the ids the item's own record names. A share is one of
	// them, written on the intent and pointing at the requirement it was derived
	// from, so one reading covers both.
	inForce, err := intent.Requirements(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return nil, err
	}
	for _, r := range inForce {
		if slices.Contains(it.RequirementsAnswered, r.ID) {
			c.requirements = append(c.requirements, requirementTold(r))
		}
	}
	if c.promised, err = p.inForceFor(ctx, svc, nil); err != nil {
		return nil, err
	}
	if c.hazard, err = p.hazardInForce(ctx, c.promised); err != nil {
		return nil, err
	}

	// The four versions the authoring stages wrote and the text of each, which
	// is what the stage below one is handed.
	for _, of := range []struct {
		kind    artifact.Kind
		id      *string
		content *string
	}{
		{artifact.KindSpec, &c.specArtifactID, &c.spec},
		{artifact.KindImplementationPlan, &c.planArtifactID, &c.plan},
		{artifact.KindTasks, &c.tasksArtifactID, &c.tasks},
		{artifact.KindImplementation, &c.implArtifactID, nil},
		{artifact.KindConsumerContract, &c.consumerContractArtifactID, nil},
	} {
		version, found, err := artifact.NewestOfKind(ctx, p.d.pool, itemID, of.kind)
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		*of.id = version.ID
		if of.content != nil {
			*of.content = version.Content
		}
	}
	// The screens the spec introduced, as the implementation stage is told them.
	machines, err := screenstatemachine.InForce(ctx, p.d.pool, svc.ID, []string{itemID})
	if err != nil {
		return nil, err
	}
	for _, m := range machines {
		c.screens = append(c.screens, screenTold(m))
	}

	// The release, read before the builds because it is what tells the merge
	// queue's build from the implementation stage's.
	rel, minted, err := release.ForItem(ctx, p.d.pool, itemID)
	if err != nil {
		return nil, err
	}
	if minted {
		c.releaseID = rel.ID
		c.releaseNumber = rel.Number
		c.reverifiedBuildID = rel.BuildID
		c.reverifiedCommit = rel.Commit
		// The deploys of this release and not the service's current one: a
		// release the service has since moved past was still deployed, and
		// reading the current release here would leave every release but the
		// newest looking like one nothing had deployed — which is a deploy
		// performed again at every pass.
		if c.published, err = publishedBy(ctx, p, rel.ID); err != nil {
			return nil, err
		}
		deploys, err := deploy.ByRelease(ctx, p.d.pool, p.production.ID, rel.ID)
		if err != nil {
			return nil, err
		}
		for _, one := range deploys {
			if one.Status != deploy.StatusComplete {
				continue
			}
			c.deployID = one.ID
			w, found, err := window.ForDeploy(ctx, p.d.pool, one.ID)
			if err != nil {
				return nil, err
			}
			if found {
				c.windowID = w.ID
			}
		}
	}

	// The builds: every one of them, oldest first, which is what a criterion's
	// own outcome history is read over. The one the rows below the implementation
	// stage decide is the newest the release does not name, because the release
	// names the build the merge queue verified and every other build of the item
	// is one the implementation stage made — and where the re-verification reused
	// that build the two are one, which the same reading gives.
	builds, err := build.ForItem(ctx, p.d.pool, itemID)
	if err != nil {
		return nil, err
	}
	for _, one := range builds {
		c.buildHistory = append(c.buildHistory, one.ID)
		if one.ID != c.reverifiedBuildID || len(builds) == 1 {
			c.buildID, c.commit = one.ID, one.CommitHash
		}
	}
	if c.buildID == "" && len(builds) > 0 {
		newest := builds[len(builds)-1]
		c.buildID, c.commit = newest.ID, newest.CommitHash
	}
	if c.buildID != "" {
		// The build's diff, taken again where the repository is rather than
		// stored: no record holds it, and the stage that built took it the same
		// way. What it is taken against follows from whether master exists, as
		// it did there.
		head, err := p.masterHead(ctx, svc)
		if err != nil {
			return nil, err
		}
		c.basedOnMaster = head != ""
		c.measurement = measure(svc.Repository, c.basedOnMaster)
	}
	// The build the row below the merge decides is the re-verification's where
	// there is one and the implementation stage's otherwise, which is what the
	// queue's own outcome already says.
	if c.reverifiedBuildID == "" {
		c.reverifiedBuildID = c.buildID
	}

	// The candidate's own environment, what it was composed from, and what the
	// runs on it decided.
	env, found, err := environment.ForItem(ctx, p.d.pool, itemID)
	if err != nil {
		return nil, err
	}
	if found && len(env.Targets) > 0 {
		c.environmentID = env.ID
		c.environmentDir = env.Targets[0].Address
		c.tornDown = !env.Live()
		c.composedFrom = env.Composition.From
		// The deploys into this environment and not the release running on it:
		// a deploy onto a candidate environment names a build and no release,
		// the number being minted one gate below that row.
		onto, err := deploy.ForEnvironment(ctx, p.d.pool, env.ID)
		if err != nil {
			return nil, err
		}
		for _, one := range onto {
			if one.Status != deploy.StatusComplete {
				continue
			}
			c.candidateDeployID = one.ID
			c.candidateDeployBuild = one.BuildID
		}
	}
	if c.buildID != "" {
		if c.criteria, err = p.criteriaOf(ctx, c); err != nil {
			return nil, err
		}
		// What the environment was composed from at the run, which the merge
		// queue compares its own re-verification against. The record's
		// composed-from field is rewritten at every recomposition, so what a run
		// ran against survives on the build: the composition is copied onto each
		// criterion result row at the run.
		if c.approvedComposition, err = ranAgainst(ctx, p, c.buildID); err != nil {
			return nil, err
		}
	}

	// Where the pass enters, what a rejection it has not yet acted on carries
	// into the stage that re-authors, and the seven firings as the log's own rows
	// hold them.
	held, err := p.readLog(ctx)
	if err != nil {
		return nil, err
	}
	rows, waiting := held.on(itemID)
	c.rows = rows
	// The merge queue's own rejection after the Merge to master row that
	// approved this build: the row is closed as an approval and the queue sent
	// the item back anyway, so the approval is spent.
	if merged, closed := rows[gate.KindMergeToMaster]; closed && merged.verdict == gate.VerdictApprove {
		if row, why, sent := queueSentBack(held.rows, itemID, merged.closing.ID); sent {
			c.queueRejected, c.queueWhy, c.queueWaitRow = true, why, row
		}
	}
	c.from = p.stepFrom(it, c, rows, waiting)
	if waiting != nil {
		c.waiting = waiting.Gate
		c.pending = waiting
	}
	c.recordFirings(rows, waiting)
	// A hold a human set at either deploy row is a stop on the event: the row is
	// closed, nothing is deployed, and the pass performs nothing until a holder
	// releases it at Work.
	for kind, subject := range map[gate.Kind]string{
		gate.KindDeployToCandidateEnvironment: c.buildID,
		gate.KindDeployToProduction:           c.reverifiedBuildID,
	} {
		if heldOver(rows, kind, subject) {
			c.held = true
			c.heldAt = rows[kind].opened.Gate
		}
	}
	return c, nil
}

// publishedBy is the contract versions one release published, read back the way
// [contract.Publish] wrote them: the version rows the release names, the
// contract each is of, and the change each published — diffed against the
// version in force below that release's number, which is the diff Publish itself
// made.
//
// A publication that minted nothing has no version row, so it is not here: what
// a release publishes is the versions it minted, and a form identical to the one
// below it publishes none.
func publishedBy(ctx context.Context, p *path, releaseID string) ([]contract.Published, error) {
	versions, err := contract.VersionsForRelease(ctx, p.d.pool, releaseID)
	if err != nil {
		return nil, err
	}
	var all []contract.Published
	for _, v := range versions {
		of, err := contract.Get(ctx, p.d.pool, v.ContractID)
		if err != nil {
			return nil, err
		}
		after, err := contract.FormOf(ctx, p.d.pool, of, v.ID)
		if err != nil {
			return nil, err
		}
		before := contract.Form{}
		below, hadOne, err := contract.VersionAt(ctx, p.d.pool, v.ContractID, v.ReleaseNumber-1)
		if err != nil {
			return nil, err
		}
		if hadOne {
			if before, err = contract.FormOf(ctx, p.d.pool, of, below.ID); err != nil {
				return nil, err
			}
		}
		all = append(all, contract.Published{
			Contract: of,
			Version:  v,
			Change:   contract.Diff(before, after),
			Moved:    true,
			// The contract exists from the release that published its first
			// version, so no version below this release's number is what says
			// this publication created it.
			Created: !hadOne,
		})
	}
	return all, nil
}

// recordFirings fills the seven firings from the log's own rows, so a candidate
// read back reports what each of its rows read whoever performed the firing and
// on whichever pass. A row still pending is recorded with no close event, which
// is what it is.
func (c *candidate) recordFirings(rows map[gate.Kind]decided, waiting *gate.Opened) {
	into := map[gate.Kind]*fired{
		gate.KindSpec:                         &c.specGate,
		gate.KindImplementationPlan:           &c.planGate,
		gate.KindTasks:                        &c.tasksGate,
		gate.KindImplementation:               &c.implementationGate,
		gate.KindDeployToCandidateEnvironment: &c.candidateGate,
		gate.KindMergeToMaster:                &c.mergeGate,
		gate.KindDeployToProduction:           &c.deployGate,
	}
	for kind, row := range rows {
		if where, held := into[kind]; held {
			*where = recordFiring(row.opened, row.closing)
		}
	}
	if waiting == nil {
		return
	}
	if where, held := into[waiting.Gate.Kind]; held {
		*where = recordFiring(*waiting, decisionlog.Row{})
	}
}

// criteriaOf is what deciding this build's criteria produced, read back the way
// [path.decideCriteria] computed it: the latest outcome per criterion, undecided
// where two runs against one composition disagreed, and the service's own
// unreliable bound applied over the builds this item has been decided on.
func (p *path) criteriaOf(ctx context.Context, c *candidate) ([]gate.CriterionResult, error) {
	inForce, err := p.inForceFor(ctx, c.svc, []string{c.itemID})
	if err != nil {
		return nil, err
	}
	if len(inForce) == 0 {
		return nil, nil
	}
	latest, err := criterion.Latest(ctx, p.d.pool, c.buildID)
	if err != nil {
		return nil, err
	}
	if len(latest) == 0 {
		return nil, nil
	}
	byID := make(map[string]criterion.Outcome, len(latest))
	for _, r := range latest {
		byID[r.CriterionID] = r.Outcome
	}
	undecided, err := criterion.Undecided(ctx, p.d.pool, c.buildID)
	if err != nil {
		return nil, err
	}
	isUndecided := make(map[string]bool, len(undecided))
	for _, id := range undecided {
		isUndecided[id] = true
	}
	results := make([]gate.CriterionResult, 0, len(inForce))
	for _, cr := range inForce {
		outcome := byID[cr.ID]
		if isUndecided[cr.ID] {
			outcome = criterion.OutcomeUndecided
		}
		results = append(results, gate.CriterionResult{CriterionID: cr.ID, Outcome: outcome})
	}
	for i, result := range results {
		reliability, err := criterion.Unreliable(ctx, p.d.pool, result.CriterionID, c.buildHistory, c.svc.UnreliableBound)
		if err != nil {
			return nil, err
		}
		results[i].Unreliable = reliability.Unreliable
	}
	return results, nil
}

// ranAgainst is what the environment was composed from at the newest run on one
// build, read off that run's own criterion result rows. The composition is
// copied onto them at the run for exactly this reason: the environment record's
// own field is rewritten at every recomposition, and what a run ran against has
// to outlive the next one.
func ranAgainst(ctx context.Context, p *path, buildID string) ([]environment.Composed, error) {
	results, err := criterion.ResultsForBuild(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	highest, of := 0, ""
	for _, r := range results {
		if r.Place == criterion.PlaceCandidateEnvironment && r.Run >= highest && r.Composition != "" {
			highest, of = r.Run, r.Composition
		}
	}
	if of == "" {
		return nil, nil
	}
	var composition environment.Composition
	if err := json.Unmarshal([]byte(of), &composition); err != nil {
		// A composition this pass cannot read is no composition: the queue
		// compares it against what it recomposes and a difference is what it
		// reads as a release the author's work never saw.
		return nil, nil
	}
	return composition.From, nil
}

// requirementTold is one requirement as an authoring role is told it: the id a
// criterion names and the sentence it answers, and no other field of the record.
func requirementTold(r intent.Requirement) agent.Requirement {
	return agent.Requirement{ID: r.ID, Statement: r.Statement}
}

// screenTold is one machine in force as the implementation stage is told it,
// which is [path.submitSpec]'s own reading of what the store wrote.
func screenTold(m screenstatemachine.Machine) agent.ScreenInForce {
	screen := agent.ScreenInForce{ID: m.Screen, States: m.States}
	for _, t := range m.Transitions {
		screen.Transitions = append(screen.Transitions, agent.ScreenTransition{
			From: t.From, Event: t.Event, To: t.To, Screen: t.Screen,
		})
	}
	return screen
}
