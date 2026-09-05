package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
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
func (p *path) decomposeItems(ctx context.Context, in intent.Intent, services []string) ([]*candidate, error) {
	d := p.d
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
			svc, err = service.NewWriter(d.pool).Create(ctx, decompositionActor, name, repo)
			if err != nil {
				return nil, err
			}
		}
		p.serviceByID[svc.ID] = svc

		var waitsOn []string
		if previous != "" {
			waitsOn = []string{previous}
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
			IntentID:  in.ID,
			ServiceID: svc.ID,
			AreaID:    p.areaID,
			Branch:    branch,
			WaitsOn:   waitsOn,
		})
		if err != nil {
			return nil, err
		}
		c := &candidate{
			intentID: in.ID,
			itemID:   it.ID,
			svc:      svc,
			branch:   branch,
			waitsOn:  waitsOn,
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
	}
	return candidates, nil
}

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
			ItemID: c.itemID, ServiceID: c.svc.ID, AreaID: p.areaID, WaitsOn: c.waitsOn,
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

	verdict, feedback, closing, err := p.settle(ctx, opened)
	if err != nil {
		return false, err
	}
	set.fired = recordFiring(opened, closing)
	if verdict != gate.VerdictReject {
		set.approved = true
		fmt.Fprintf(p.d.out, "Approved; decomposition of intent %s stands\n", in.ID)
		return true, nil
	}

	reDecompositions, err := p.intake.CountReDecomposition(ctx, decompositionActor, in.ID)
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
		c.superseded = true
	}
	fmt.Fprintf(p.d.out, "Rejected: %s\n", feedback)
	fmt.Fprintf(p.d.out, "  every item of the set is superseded and re-decomposition %d is counted on intent %s\n", reDecompositions, in.ID)
	fmt.Fprintln(p.d.out, "  the re-decomposition itself is not built: this interface is told what to decompose, so a bad decomposition is stopped here and not repaired")
	return false, nil
}
