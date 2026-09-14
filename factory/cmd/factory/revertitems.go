package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/service"
)

func (p *path) decomposeRevertItems(ctx context.Context, in intent.Intent,
	requirements []agent.Requirement) ([]*candidate, bool, error) {
	siblings, found, err := p.shippedRevertSiblings(ctx, in)
	if err != nil || !found {
		return nil, false, err
	}
	if len(siblings) == 0 {
		return nil, true, nil
	}

	areaProjectID := ""
	var areaChain []string
	if p.areaID != "" {
		chain, projectID, err := area.Chain(ctx, p.d.pool, p.areaID)
		if err != nil {
			return nil, true, err
		}
		areaProjectID = projectID
		for _, one := range chain {
			areaChain = append(areaChain, one.ID)
		}
	}

	items := make([]item.RevertItem, 0, len(siblings))
	services := make([]service.Service, 0, len(siblings))
	answersFor := make([][]agent.Requirement, 0, len(siblings))
	previous := ""
	for n, sibling := range siblings {
		svc, err := service.Get(ctx, p.d.pool, sibling.ServiceID)
		if err != nil {
			return nil, true, err
		}
		if err := environment.RefuseUnlessComposable(ctx, p.d.pool, svc.ProjectID); err != nil {
			return nil, true, err
		}
		svc, err = p.runsOnProduction(ctx, svc)
		if err != nil {
			return nil, true, err
		}
		svc, err = p.provisioned(ctx, svc)
		if err != nil {
			return nil, true, err
		}
		p.keepService(svc)
		services = append(services, svc)

		itemID := item.NewID()
		answers := requirements
		if len(siblings) > 1 {
			answers, err = p.deriveShares(ctx, in, itemID, requirements)
			if err != nil {
				return nil, true, err
			}
		}
		answersFor = append(answersFor, answers)
		answered := make([]string, 0, len(answers))
		for _, answer := range answers {
			answered = append(answered, answer.ID)
		}
		branch := "item/" + in.ID
		if n > 0 {
			branch += "/" + svc.Name
		}
		var waitsOn []string
		if previous != "" {
			waitsOn = []string{previous}
		}
		items = append(items, item.RevertItem{
			New: item.New{ID: itemID, IntentID: in.ID, ServiceID: svc.ID,
				AreaChain: areaChain, Branch: branch, WaitsOn: waitsOn,
				RequirementsAnswered: answered},
			AreaProjectID: areaProjectID, ServiceProjectID: svc.ProjectID,
		})
		previous = itemID
	}

	created, err := p.decomposition.CreateReverts(ctx, decompositionActor, items)
	if err != nil {
		return nil, true, err
	}
	candidates := make([]*candidate, 0, len(created))
	for n, it := range created {
		c := &candidate{intentID: in.ID, itemID: it.ID, svc: services[n],
			branch: it.Branch, waitsOn: it.WaitsOn, requirementIDs: it.RequirementsAnswered,
			requirements: answersFor[n]}
		c.adoption, err = adoptionIntent(ctx, p.d.pool, c.svc)
		if err != nil {
			return nil, true, err
		}
		candidates = append(candidates, c)
		fmt.Fprintf(p.d.out, "Service %s already exists; item %s decomposed on branch %s\n",
			c.svc.ID, c.itemID, c.branch)
		fmt.Fprintf(p.d.out, "  it answers %d requirement(s): %v\n", len(c.requirementIDs), c.requirementIDs)
	}
	return candidates, true, nil
}
