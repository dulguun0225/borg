package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/driftdetector"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/service"
)

type productionQueueWindow struct{ p *path }

func (r productionQueueWindow) ReadWindow(ctx context.Context, serviceID string) (deploy.WindowReading, error) {
	_, open, limit, err := r.p.healthMonitor.Room(ctx, serviceID)
	return deploy.WindowReading{Open: open, Limit: limit}, err
}

type productionQueueBudget struct {
	p       *path
	service service.Service
}

func (r productionQueueBudget) ReadBudget(ctx context.Context, serviceID string) (bool, error) {
	budget, err := r.p.healthMonitor.ErrorBudget(ctx, healthmonitor.Watching{
		ID: serviceID, Name: r.service.Name, EnvironmentID: r.p.production.ID,
	})
	return budget.Holds(), err
}

type productionQueueRollback struct{ p *path }

func (r productionQueueRollback) ReadRollback(ctx context.Context, queued deploy.QueueCandidate) (deploy.RollbackReading, error) {
	svc := service.Service{ID: queued.ServiceID}
	_, intentID, holding, err := r.p.outstandingRevert(ctx, svc)
	return deploy.RollbackReading{Holding: holding, RevertIntentID: intentID}, err
}

type productionQueueDependency struct{ p *path }

func (r productionQueueDependency) ReadDependencies(ctx context.Context, queued deploy.QueueCandidate) ([]deploy.DependencyReading, error) {
	it, err := item.Get(ctx, r.p.d.pool, queued.ItemID)
	if err != nil {
		return nil, err
	}
	readings := make([]deploy.DependencyReading, 0, len(it.WaitsOn))
	for _, waitsOn := range it.WaitsOn {
		dependency, err := item.Get(ctx, r.p.d.pool, waitsOn)
		if err != nil {
			return nil, err
		}
		addresses, err := r.p.addressesOf(ctx, dependency.ServiceID)
		if err != nil {
			return nil, err
		}
		current, found, err := deploy.Current(ctx, r.p.d.pool, dependency.ServiceID, r.p.production.ID, addresses)
		if err != nil {
			return nil, err
		}
		currentItemID := ""
		if found {
			rel, err := release.Get(ctx, r.p.d.pool, current.ReleaseID)
			if err != nil {
				return nil, err
			}
			currentItemID = rel.ItemID
		}
		readings = append(readings, deploy.DependencyReading{
			RequiredItemID: waitsOn, CurrentItemID: currentItemID,
		})
	}
	return readings, nil
}

type productionQueueDrift struct{ p *path }

func (r productionQueueDrift) ReadDrift(ctx context.Context, serviceID string) (bool, error) {
	if r.p.d.driftdetector == nil {
		return false, nil
	}
	found, _, err := driftdetector.NewStore(r.p.d.driftdetector).Mismatch(ctx, serviceID)
	return found, err
}

type productionQueueHuman struct{ candidates map[string]*candidate }

func (r productionQueueHuman) ReadHuman(_ context.Context, queued deploy.QueueCandidate) (bool, error) {
	c := r.candidates[queued.ItemID]
	return c != nil && (c.held || c.waiting != (gate.Row{})), nil
}

func (p *path) productionQueueReadings(svc service.Service, candidates map[string]*candidate) deploy.QueueReadings {
	return deploy.QueueReadings{
		Window: productionQueueWindow{p: p}, Budget: productionQueueBudget{p: p, service: svc},
		Rollback: productionQueueRollback{p: p}, Dependency: productionQueueDependency{p: p},
		Drift: productionQueueDrift{p: p}, Human: productionQueueHuman{candidates: candidates},
	}
}

func (p *path) deployOrder(ctx context.Context, svc service.Service, candidates []*candidate) ([]*candidate, error) {
	all := make([]deploy.QueueCandidate, 0, len(candidates))
	byItem := make(map[string]*candidate, len(candidates))
	unique := make([]*candidate, 0, len(candidates))
	for _, c := range candidates {
		if previous, found := byItem[c.itemID]; found {
			if queueCandidatePreferred(c, previous) {
				for i, one := range unique {
					if one.itemID == c.itemID {
						unique[i] = c
						break
					}
				}
				byItem[c.itemID] = c
			}
			continue
		}
		byItem[c.itemID] = c
		unique = append(unique, c)
	}
	for _, c := range unique {
		all = append(all, deploy.QueueCandidate{
			ItemID: c.itemID, IntentID: c.intentID, ServiceID: c.svc.ID, ReleaseID: c.releaseID,
			ReleaseNumber: c.releaseNumber, DeployID: c.deployID,
		})
		byItem[c.itemID] = c
	}
	readings := p.productionQueueReadings(svc, byItem)
	ordered, err := deploy.QueueOrder(ctx, svc.ID, all, readings)
	if err != nil {
		return nil, err
	}
	if len(ordered) > 0 {
		rollback, revertIntentID, outstanding, err := p.outstandingRevert(ctx, svc)
		if err != nil {
			return nil, err
		}
		if outstanding && ordered[0].ItemID != "" && byItem[ordered[0].ItemID].intentID == revertIntentID {
			fmt.Fprintf(p.d.out, "A revert of rollback %s deploys ahead of %d release(s) its hold is holding\n",
				rollback.ID, len(ordered)-1)
		}
	}
	result := make([]*candidate, 0, len(ordered))
	for _, one := range ordered {
		c := byItem[one.ItemID]
		if one.AwaitedRevert {
			c.awaitedRevert = true
		}
		if one.HoldReason != deploy.HoldNone && one.HoldReason != deploy.HoldDrift && one.HoldReason != deploy.HoldHuman {
			c.factoryHold = string(one.HoldReason)
			fmt.Fprintf(p.d.out, "Release %s waits at %s: %s\n", c.releaseID, gate.DeployToProduction, c.factoryHold)
			continue
		}
		if one.HoldReason == deploy.HoldHuman {
			continue
		}
		result = append(result, c)
	}
	return result, nil
}

func queueCandidatePreferred(next, previous *candidate) bool {
	if next.deployID != previous.deployID {
		return next.deployID != ""
	}
	if next.releaseID != previous.releaseID {
		return next.releaseID != ""
	}
	return false
}
