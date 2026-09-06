package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// decomposeItems is decomposition: one item per service the intent changes, in the order the
// intent named them, each declared to wait on the one before it.
//
// A service the work changes may not exist yet and nothing about decomposition changes:
// the item that creates it is decomposed first and the service record is written in the
// same step, because an item names one service and the record has to exist for the
// item's only outbound link to point at anything.
//
// The order is what decomposition records. Where one item cannot be verified until
// another has shipped — the producing release of a migration — that dependency is
// declared here, and both deploy gates hold on it. This interface declares a chain
// rather than deducing a graph: the services are given in order, and each item waits
// on the one before it.
//
// What each item answers is assigned here too. A decomposition yielding one item
// assigns every requirement of the intent to it whole, which is what a set of one
// answers by construction. One yielding several assigns none of them whole: the split
// spreads each requirement over the items, so a share per item is derived from it and
// written by intake, and the item answers the shares rather than the whole. What
// states a share is the one thing this interface cannot supply — it is told which
// services the work changes and never which part of a requirement each item answers —
// so [shareOf] restates the requirement and doc.go says what that costs.
func (p *path) decomposeItems(ctx context.Context, in intent.Intent, services []string,
	requirements []agent.Requirement) ([]*candidate, error) {
	d := p.d

	// The area's own project, read once: an item's area and its service agree
	// by construction, which [item.Decomposition.Create] enforces by comparing
	// the two. Where the run names no area there is nothing to compare.
	areaProjectID := ""
	if p.areaID != "" {
		_, projectID, err := area.Chain(ctx, d.pool, p.areaID)
		if err != nil {
			return nil, err
		}
		areaProjectID = projectID
	}

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
			svc, err = service.NewWriter(d.pool, d.token).Create(ctx, decompositionActor, name, repo, p.projectID)
			if err != nil {
				return nil, err
			}
		}
		svc, err = p.runsOnProduction(ctx, svc)
		if err != nil {
			return nil, err
		}
		svc, err = p.provisioned(ctx, svc)
		if err != nil {
			return nil, err
		}
		p.serviceByID[svc.ID] = svc

		var waitsOn []string
		if previous != "" {
			waitsOn = []string{previous}
		}
		// A requirement one item answers alone is assigned to it whole; one the
		// split spreads over several is assigned to none of them, and the item
		// answers the share derived below instead.
		var answered []string
		if len(services) == 1 {
			answered = make([]string, len(requirements))
			for n, r := range requirements {
				answered[n] = r.ID
			}
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
		it, err := p.decomposition.Create(ctx, decompositionActor, item.New{
			IntentID:             in.ID,
			ServiceID:            svc.ID,
			AreaID:               p.areaID,
			Branch:               branch,
			WaitsOn:              waitsOn,
			RequirementsAnswered: answered,
		}, areaProjectID, svc.ProjectID, nil)
		if err != nil {
			return nil, err
		}
		c := &candidate{
			intentID:       in.ID,
			itemID:         it.ID,
			svc:            svc,
			branch:         branch,
			waitsOn:        waitsOn,
			requirementIDs: it.RequirementsAnswered,
			requirements:   requirements,
		}
		if len(services) > 1 {
			shares, err := p.deriveShares(ctx, in, it.ID, requirements)
			if err != nil {
				return nil, err
			}
			c.requirements = shares
			for _, share := range shares {
				c.requirementIDs = append(c.requirementIDs, share.ID)
			}
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
		fmt.Fprintf(d.out, "Service %s %s; item %s decomposed on branch %s%s\n", svc.ID, was, it.ID, branch, waited)
		fmt.Fprintf(d.out, "  it answers %d requirement(s): %v\n", len(c.requirementIDs), c.requirementIDs)
	}
	return candidates, nil
}

// deriveShares writes this item's share of every requirement the split spreads
// over the set, one requirement record each, attached to the intent and
// pointing at the one it was derived from. Intake writes them, at
// decomposition's call, and the item answers them.
//
// The statement is [shareOf]'s.
func (p *path) deriveShares(ctx context.Context, in intent.Intent, itemID string,
	requirements []agent.Requirement) ([]agent.Requirement, error) {
	shares := make([]agent.Requirement, 0, len(requirements))
	for _, whole := range requirements {
		statement := shareOf(whole.Statement)
		escapeReason := ""
		if _, matched := criterion.Classify(statement); !matched {
			escapeReason = "not classified by the command-line interface"
		}
		written, err := p.intake.DeriveForItem(ctx, decompositionActor, intent.Derivation{
			IntentID:     in.ID,
			DerivedFrom:  whole.ID,
			ItemID:       itemID,
			Statement:    statement,
			EscapeReason: escapeReason,
		})
		if err != nil {
			return nil, err
		}
		shares = append(shares, agent.Requirement{ID: written.ID, Statement: written.Statement})
		fmt.Fprintf(p.d.out, "  requirement %s is spread over the set; share %s is item %s's\n",
			whole.ID, written.ID, itemID)
	}
	return shares, nil
}

// shareOf is what a derived requirement states. The design has decomposition
// state the item's share in the requester's terms, and a stage that decides a
// decomposition is what would author that sentence: this interface is told
// which services the work changes and nothing about which part of a requirement
// each item answers, so the share restates the requirement and the service it
// is for is read off the item.
//
// What that costs is the derivation's own cost, paid in full: a statement the
// requester never confirmed stands where a confirmed one did, and here it says
// no less than the whole rather than one item's part of it, so a criterion
// naming it is drafted against the whole request.
func shareOf(statement string) string { return statement }

// decompositionGate is the stage's own gate: the one row where approving admits
// several timelines at once. It fires over the set that already exists — how many
// items, which service each changes, and what waits on what — and one verdict covers
// the whole decomposition however many services it changes.
//
// A rejection supersedes every item of the set and counts a re-decomposition on the intent.
// It does not re-decompose: that needs a stage which decides the decomposition
// rather than one told what to produce, and this interface is told. What that leaves is a gate that
// can stop a bad decomposition and cannot repair one.
func (p *path) decompositionGate(ctx context.Context, in intent.Intent, set *decompositionSet, candidates []*candidate) (bool, error) {
	members := make([]gate.SetMember, 0, len(candidates))
	for _, c := range candidates {
		members = append(members, gate.SetMember{
			ItemID: c.itemID, ServiceID: c.svc.ID, AreaID: p.areaID,
			// How many of the intent's requirements this item answers, which
			// is what the change group is computed from at this row: there is
			// no build and no diff, so the set's own size is the reading.
			Requirements: len(c.requirementIDs),
			WaitsOn:      c.waitsOn,
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
	fmt.Fprintln(p.d.out, "  the diff factors are unavailable here, decomposition happening before anything is built, so this row is scored on a vector with holes in it")

	// The firing a refer would re-fire with is the set's, and [gate.Gate.Refer]
	// re-fires through [gate.Gate.Fire], which refuses the Decomposition row
	// because that row decides a set. So a refer here is refused by the gate
	// and the human is asked again — the one row of the four this interface
	// fires where the action the design puts on every row is not reachable.
	verdict, feedback, closing, err := p.settle(ctx, opened, gate.Firing{Row: gate.Decomposition})
	if err != nil {
		return false, err
	}
	set.fired = recordFiring(opened, closing)
	if verdict != gate.VerdictReject {
		set.approved = true
		fmt.Fprintf(p.d.out, "Approved; decomposition of intent %s stands\n", in.ID)
		return true, nil
	}

	// Marking the intent re-decomposing is what stops every unmerged item of it
	// while this Decomposition firing is open, and it is what advances the
	// count the attempt limit is compared against — decomposition's own budget,
	// a field beside the interview's rounds and never the same one.
	reDecompositions, err := p.intake.MarkReDecomposing(ctx, decompositionActor, in.ID)
	if err != nil {
		return false, err
	}
	set.reDecompositions = reDecompositions
	for _, c := range candidates {
		// Every item of the set is superseded and points at nothing, because no
		// re-decomposition replaced it. What says why is the superseded stage beside the
		// decision that rejected the set.
		if _, err := p.decomposition.Supersede(ctx, decompositionActor, c.itemID, nil); err != nil {
			return false, err
		}
		// The shares that item carried go with it, pointing at nothing for the
		// same reason: a derived requirement is superseded with the item that
		// carried it, and the two records have two writers.
		superseded, err := p.intake.SupersedeDerived(ctx, decompositionActor, c.itemID, nil)
		if err != nil {
			return false, err
		}
		if len(superseded) > 0 {
			fmt.Fprintf(p.d.out, "  the %d share(s) item %s carried are superseded with it\n",
				len(superseded), c.itemID)
		}
		c.superseded = true
	}
	fmt.Fprintf(p.d.out, "Rejected: %s\n", feedback)
	fmt.Fprintf(p.d.out, "  every item of the set is superseded and re-decomposition %d is counted on intent %s\n", reDecompositions, in.ID)
	fmt.Fprintln(p.d.out, "  the re-decomposition itself is not built: this interface is told what to decompose, so a bad decomposition is stopped here and not repaired")

	limit, err := decompositionAttemptLimit(ctx, p.d.pool)
	if err != nil {
		return false, err
	}
	if reDecompositions > limit {
		if _, err := p.intake.Escalate(ctx, decompositionActor, in.ID, limit); err != nil {
			return false, err
		}
		fmt.Fprintf(p.d.out, "  re-decomposition %d exceeds the limit of %d; intent %s is escalated\n", reDecompositions, limit, in.ID)
		return false, nil
	}
	// Nothing here re-decomposes, so the Decomposition firing that stopped
	// unmerged items closes with nothing having replaced them.
	if err := p.intake.ClearReDecomposing(ctx, decompositionActor, in.ID); err != nil {
		return false, err
	}
	return false, nil
}

// decompositionAttemptLimit is the attempt limit in force for decomposition's
// re-decompositions. Package policy's reader answers an item's stage alone —
// [factorysettings.OfStage] refuses "decomposition", which is the intent's and
// not an item's — so this reads the authored value directly and falls back to
// what the score supplies where an owner authored none, which is the number
// [policy.Reader] would resolve to with no safeguard clamping it.
func decompositionAttemptLimit(ctx context.Context, pool *pgxpool.Pool) (int, error) {
	settings, err := factorysettings.Get(ctx, pool)
	if err != nil {
		return 0, err
	}
	authored, err := factorysettings.AttemptLimit(ctx, pool, settings.ID, factorysettings.SubjectDecomposition)
	if err != nil {
		return 0, err
	}
	if authored.Present {
		return int(authored.Number), nil
	}
	starting, _ := score.Starting(gatepolicy.AttemptLimit)
	return int(starting.Value), nil
}
