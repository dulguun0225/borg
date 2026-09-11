package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/environment"
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
// What each item answers is assigned here too, and it is on the item's record
// either way. A decomposition yielding one item assigns every requirement of the
// intent to it whole, which is what a set of one answers by construction. One
// yielding several assigns none of them whole: the split spreads each requirement
// over the items, so a share per item is derived from it and written by intake,
// and the item answers the shares rather than the whole. What
// states a share is the one thing this interface cannot supply — it is told which
// services the work changes and never which part of a requirement each item answers —
// so [shareOf] restates the requirement and doc.go says what that costs.
//
// The item's id is minted here, before either record is written, because the two
// name each other: a derived requirement names the item that answers it, and the
// item answers the share. Decomposition writes an item again only to supersede it
// and to repoint what waits on it, so the field cannot be filled in a second
// write.
func (p *path) decomposeItems(ctx context.Context, in intent.Intent, services []string,
	requirements []agent.Requirement) ([]*candidate, error) {
	d := p.d

	// The chain of areas covering the work and the project it ends at, read
	// once: walking the chain is package area's, and which of them the item
	// names is [item.Decomposition.Create]'s — the narrowest, which is the
	// head. An item's area and its service agree by construction, which the
	// same call enforces by comparing the two projects, and where the run names
	// no area there is no chain and nothing to compare.
	areaProjectID := ""
	var areaChain []string
	if p.areaID != "" {
		chain, projectID, err := area.Chain(ctx, d.pool, p.areaID)
		if err != nil {
			return nil, err
		}
		for _, one := range chain {
			areaChain = append(areaChain, one.ID)
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

		var waitsOn []string
		if previous != "" {
			waitsOn = []string{previous}
		}
		// A requirement one item answers alone is assigned to it whole; one the
		// split spreads over several is assigned to none of them, and the item
		// answers the share derived against its id instead. Either way the ids
		// go on the item at its one creating write, which is what the item-size
		// target's unit is read off.
		itemID := item.NewID()
		answers := requirements
		answered := make([]string, 0, len(requirements))
		if len(services) > 1 {
			shares, err := p.deriveShares(ctx, in, itemID, requirements)
			if err != nil {
				return nil, err
			}
			answers = shares
		}
		for _, r := range answers {
			answered = append(answered, r.ID)
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
		var it item.Item
		if existing {
			// An environment per candidate is the shape the design admits and
			// nothing else, so a service whose project's production environment
			// declares a platform that cannot compose one on demand is refused
			// here, at decomposition, before an item is written for it — not
			// only where that record was created.
			if err := environment.RefuseUnlessComposable(ctx, d.pool, svc.ProjectID); err != nil {
				return nil, err
			}
			svc, err = p.runsOnProduction(ctx, svc)
			if err != nil {
				return nil, err
			}
			svc, err = p.provisioned(ctx, svc)
			if err != nil {
				return nil, err
			}
			p.keepService(svc)
			it, err = p.decomposition.Create(ctx, decompositionActor, item.New{
				ID:                   itemID,
				IntentID:             in.ID,
				ServiceID:            svc.ID,
				AreaChain:            areaChain,
				Branch:               branch,
				WaitsOn:              waitsOn,
				RequirementsAnswered: answered,
			}, areaProjectID, svc.ProjectID)
			if err != nil {
				return nil, err
			}
		} else {
			// A service the work changes may not exist yet, and the record has
			// to exist for the item's only outbound link — its service id — to
			// point at anything, so the two are one write: the service record
			// commits with the item that names it or not at all, which only a
			// transaction shared between the two writers can make true.
			repo, err := d.repoOf(name)
			if err != nil {
				return nil, err
			}
			tx, err := d.pool.Begin(ctx)
			if err != nil {
				return nil, fmt.Errorf("decomposition: beginning the creation of service %q: %w", name, err)
			}
			defer func() { _ = tx.Rollback(ctx) }()

			svc, err = service.NewWriter(d.pool, d.token).CreateIn(ctx, tx, decompositionActor, name, repo, p.projectID)
			if err != nil {
				return nil, err
			}
			// The check is over the project and not the service just created on
			// tx, so it is read through the pool rather than the transaction: a
			// service whose project's production environment declares a platform
			// that cannot compose one on demand is refused here too, and refusing
			// rolls the service creation back with it.
			if err := environment.RefuseUnlessComposable(ctx, d.pool, svc.ProjectID); err != nil {
				return nil, err
			}
			it, err = p.decomposition.CreateTx(ctx, tx, decompositionActor, item.New{
				ID:                   itemID,
				IntentID:             in.ID,
				ServiceID:            svc.ID,
				AreaChain:            areaChain,
				Branch:               branch,
				WaitsOn:              waitsOn,
				RequirementsAnswered: answered,
			}, areaProjectID, svc.ProjectID)
			if err != nil {
				return nil, err
			}
			if err := tx.Commit(ctx); err != nil {
				return nil, fmt.Errorf("decomposition: committing service %q with item %s: %w", name, it.ID, err)
			}

			// runsOnProduction and provisioned each write through the pool by the
			// service's id, so they read a row that has to be committed already —
			// unlike the check above, which reads the project and needed nothing
			// of this write to have landed.
			svc, err = p.runsOnProduction(ctx, svc)
			if err != nil {
				return nil, err
			}
			svc, err = p.provisioned(ctx, svc)
			if err != nil {
				return nil, err
			}
			p.keepService(svc)
		}
		c := &candidate{
			intentID:       in.ID,
			itemID:         it.ID,
			svc:            svc,
			branch:         branch,
			waitsOn:        waitsOn,
			requirementIDs: it.RequirementsAnswered,
			requirements:   answers,
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
//
// What the set answers is decided here too, and mechanically: [setRejection]
// reads the intent's requirements in force against what the set's items answer,
// and where it finds one the row is closed as the factory's own reject before a
// human is asked, the way [specRejection] closes the Spec row. It is computed before the row fires because the composition
// holds the set: package gate imports neither the requirements nor the items,
// and it names which of [gate.DecompositionChecks] rejected so the close event
// carries it.
func (p *path) decompositionGate(ctx context.Context, in intent.Intent, set *decompositionSet, candidates []*candidate) (bool, error) {
	inForce, err := intent.Requirements(ctx, p.d.pool, in.ID)
	if err != nil {
		return false, err
	}
	answered := make([]string, 0, len(inForce))
	for _, c := range candidates {
		answered = append(answered, c.requirementIDs...)
	}
	derived := make(map[string]bool, len(inForce))
	for _, r := range inForce {
		if r.Kind == intent.KindDerived {
			derived[r.ID] = true
		}
	}
	members := make([]gate.SetMember, 0, len(candidates))
	for _, c := range candidates {
		var shares []string
		for _, id := range c.requirementIDs {
			if derived[id] {
				shares = append(shares, id)
			}
		}
		members = append(members, gate.SetMember{
			ItemID: c.itemID, ServiceID: c.svc.ID, AreaID: p.areaID,
			// How many of the intent's requirements this item answers, which
			// is what the change group is computed from at this row: there is
			// no build and no diff, so the set's own size is the reading.
			Requirements: len(c.requirementIDs),
			// The shares the split wrote for this item, which are part of the
			// set this row decides beside what waits on what.
			DerivedRequirements: shares,
			WaitsOn:             c.waitsOn,
		})
	}
	check, incomplete, rejects := setRejection(inForce, answered, members)
	// The order the set declares, checked as mechanically as what it answers:
	// two items each holding a deploy gate on the other is a wait nothing
	// lifts, and a wrong order comes back as feedback rather than as a stage
	// that could not write. It is reported after the completeness checks, in
	// the order [gate.DecompositionChecks] lists them.
	//
	// The holds are read again here rather than carried from decomposeItems'
	// own per-item reads: this is the one read of them decompositionGate
	// makes, over the set as a whole rather than per item, and it is what lets
	// [gate.SetCycleRejection] see the same rollback holds the write's own
	// check does.
	if !rejects {
		holds, err := p.rollbackHolds(ctx)
		if err != nil {
			return false, err
		}
		check, incomplete, rejects = gate.SetCycleRejection(members, holds)
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

	if rejects {
		// The factory's own reject, closed as the gate component and before a
		// human is asked, because a mechanical check rejects on its own terms.
		// It goes through [gate.Gate.AutoReject], which is what writes the check
		// onto the close event as auto_rejected_by — so a reader of the log sees
		// which of the row's two directions rejected and not only the reason.
		closing, err := p.gate.AutoReject(ctx, opened, check, incomplete)
		if err != nil {
			return false, err
		}
		fmt.Fprintf(p.d.out, "The set is incomplete, so the row rejects before a human is asked by %s: %s\n",
			check, incomplete)
		set.fired = recordFiring(opened, closing)
		return false, p.decompositionOutcome(ctx, in, set, gate.VerdictReject, incomplete)
	}
	// [gate.Gate.Refer] re-fires this row over the set its own open event
	// names. A refer at it is still refused: the design names no duty for this
	// row, so it waits on the owner from its first firing and a refer at a row
	// already there has nobody left to reach — what that human has left is a
	// reject.
	done, err := p.settle(ctx, opened)
	if err != nil {
		return false, err
	}
	if done.waiting {
		set.waiting = true
		return false, nil
	}
	set.fired = recordFiring(opened, done.closing)
	if err := p.decompositionOutcome(ctx, in, set, done.verdict, done.reason); err != nil {
		return false, err
	}
	return set.approved, nil
}

// decompositionOutcome is what a verdict at the Decomposition row causes. It is
// its own function because two callers reach it: the pass that fired the row and
// settled it, and the pass that finds the verdict a human left at Work on a row
// an earlier pass left open.
//
// A rejection supersedes every item of the set and counts a re-decomposition on
// the intent. It does not re-decompose: that needs a stage which decides the
// decomposition rather than one told what to produce, and this interface is
// told.
func (p *path) decompositionOutcome(ctx context.Context, in intent.Intent, set *decompositionSet,
	verdict gate.Verdict, feedback string) error {
	if verdict != gate.VerdictReject {
		set.approved = true
		fmt.Fprintf(p.d.out, "Approved; decomposition of intent %s stands\n", in.ID)
		return nil
	}

	// Marking the intent re-decomposing is what stops every unmerged item of it
	// while this Decomposition firing is open, and it is what advances the
	// count the attempt limit is compared against — decomposition's own budget,
	// a field beside the interview's rounds and never the same one.
	reDecompositions, err := p.intake.MarkReDecomposing(ctx, decompositionActor, in.ID)
	if err != nil {
		return err
	}
	set.reDecompositions = reDecompositions
	for _, itemID := range set.itemIDs {
		// Every item of the set is superseded and points at nothing, because no
		// re-decomposition replaced it. What says why is the superseded stage beside the
		// decision that rejected the set.
		if _, err := p.decomposition.Supersede(ctx, decompositionActor, itemID, nil); err != nil {
			return err
		}
		// The shares that item carried go with it, pointing at nothing for the
		// same reason: a derived requirement is superseded with the item that
		// carried it, and the two records have two writers.
		superseded, err := p.intake.SupersedeDerived(ctx, decompositionActor, itemID, nil)
		if err != nil {
			return err
		}
		if len(superseded) > 0 {
			fmt.Fprintf(p.d.out, "  the %d share(s) item %s carried are superseded with it\n",
				len(superseded), itemID)
		}
		if c := p.heldCandidate(itemID); c != nil {
			c.superseded = true
		}
	}
	fmt.Fprintf(p.d.out, "Rejected: %s\n", feedback)
	fmt.Fprintf(p.d.out, "  every item of the set is superseded and re-decomposition %d is counted on intent %s\n", reDecompositions, in.ID)
	fmt.Fprintln(p.d.out, "  the re-decomposition itself is not built: this interface is told what to decompose, so a bad decomposition is stopped here and not repaired")

	limit, err := intentAttemptLimit(ctx, p.d.pool, factorysettings.SubjectDecomposition)
	if err != nil {
		return err
	}
	// The count is compared against the limit by the gate, which is where an
	// item's own per-stage count is compared too: over it the intent is
	// escalated and every pending row of it is abandoned naming the limit.
	escalated, err := p.gate.EnforceDecompositionRounds(ctx, decompositionActor, in.ID, limit)
	if err != nil {
		return err
	}
	if escalated.Reached {
		fmt.Fprintf(p.d.out, "  re-decomposition %d exceeds the limit of %d; intent %s is escalated\n",
			escalated.Attempts, escalated.Limit, in.ID)
		return nil
	}
	// Nothing here re-decomposes, so the Decomposition firing that stopped
	// unmerged items closes with nothing having replaced them. Clearing the
	// state is the intent leaving what stopped it, so the holds that state
	// opened are re-matched from here.
	if err := p.intake.ClearReDecomposing(ctx, decompositionActor, in.ID); err != nil {
		return err
	}
	return p.intentLeftItsStop(ctx)
}

// intentAttemptLimit is the attempt limit in force for one of the two counts an
// intent keeps: [factorysettings.SubjectInterview] for the interview's rounds
// and [factorysettings.SubjectDecomposition] for decomposition's
// re-decompositions. Package policy's reader answers an item's stage alone —
// [factorysettings.OfStage] refuses both, which are the intent's and not an
// item's — so this reads the authored value directly and falls back to what the
// score supplies where an owner authored none, which is the number
// [policy.Reader] would resolve to with no safeguard clamping it.
func intentAttemptLimit(ctx context.Context, pool *pgxpool.Pool, subject factorysettings.AttemptLimitSubject) (int, error) {
	settings, err := factorysettings.Get(ctx, pool)
	if err != nil {
		return 0, err
	}
	authored, err := factorysettings.AttemptLimit(ctx, pool, settings.ID, subject)
	if err != nil {
		return 0, err
	}
	if authored.Present {
		return int(authored.Number), nil
	}
	starting, _ := score.Starting(gatepolicy.AttemptLimit)
	return int(starting.Value), nil
}
