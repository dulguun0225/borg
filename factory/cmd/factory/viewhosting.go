package main

import (
	"context"
	"time"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

func (v *views) environmentView(ctx context.Context, env environment.Environment) (screens.Environment, error) {
	view := screens.Environment{ID: env.ID, Kind: string(env.Kind), ItemID: env.ItemID}
	for _, target := range env.Targets {
		view.Targets = append(view.Targets, target.Address)
	}
	cycles, err := environment.Cycles(ctx, v.p.d.pool, env.ID)
	if err != nil {
		return screens.Environment{}, err
	}
	for _, cycle := range cycles {
		composition, err := cycle.CompositionHours()
		if err != nil {
			return screens.Environment{}, err
		}
		view.CompositionHours += composition
	}
	if env.ItemID != "" {
		reading, err := environment.HoursForItem(ctx, v.p.d.pool, env.ItemID, time.Now())
		if err != nil {
			return screens.Environment{}, err
		}
		view.EnvironmentHours = reading.Hours
		view.EnvironmentAmount = reading.Amount
		view.EnvironmentAmountInForce = reading.Priced
	}
	if env.Kind != environment.KindProduction {
		return view, nil
	}
	standing, err := environment.CountLiveCandidates(ctx, v.p.d.pool, env.ID)
	if err != nil {
		return screens.Environment{}, err
	}
	view.StandingCandidateEnvironments = standing
	check, found, err := lastcheck.Get(ctx, v.p.d.pool, lastcheck.ComponentDeployer, env.ID)
	if err != nil || !found {
		return view, err
	}
	pass, err := lastcheck.PlatformPassOf(check)
	if err != nil {
		return screens.Environment{}, err
	}
	view.HeldCandidateEnvironments = pass.HeldByThePlatform
	view.Room = pass.Room
	view.RoomReported = pass.RoomReported
	return view, nil
}

func (v *views) hostingAndCriteria(ctx context.Context, services []service.Service, releases []release.Release) ([]screens.HostingHours, []screens.MutationScore, []screens.CriteriaCount, error) {
	items, err := item.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, nil, err
	}
	itemIDs := make(map[string][]string)
	for _, one := range items {
		itemIDs[one.ServiceID] = append(itemIDs[one.ServiceID], one.ID)
	}
	releaseByItem := make(map[string]release.Release)
	for _, one := range releases {
		if one.ItemID != "" {
			releaseByItem[one.ItemID] = one
		}
	}

	hosting := make([]screens.HostingHours, 0, len(items))
	for _, one := range items {
		environmentHours, err := environment.HoursForItem(ctx, v.p.d.pool, one.ID, time.Now())
		if err != nil {
			return nil, nil, nil, err
		}
		row := screens.HostingHours{ServiceID: one.ServiceID, ItemID: one.ID,
			EnvironmentHours: environmentHours.Hours, EnvironmentAmount: environmentHours.Amount,
			EnvironmentAmountInForce: environmentHours.Priced}
		if rel, found := releaseByItem[one.ID]; found {
			row.ReleaseNumber = rel.Number
			instanceHours, err := deploy.InstanceHoursReadingForRelease(ctx, v.p.d.pool, rel.ID)
			if err != nil {
				return nil, nil, nil, err
			}
			row.InstanceHours, row.InstanceAmount, row.InstanceAmountInForce = instanceHours.Hours, instanceHours.Amount, instanceHours.Priced
		}
		if row.EnvironmentHours != 0 || row.ReleaseNumber != 0 {
			hosting = append(hosting, row)
		}
	}

	scores := make([]screens.MutationScore, 0, len(services))
	for _, svc := range services {
		var newest screens.MutationScore
		var newestRelease int64
		for _, rel := range releases {
			if rel.ServiceID != svc.ID || rel.ItemID == "" || rel.Number < newestRelease {
				continue
			}
			applicability, reading, err := criterion.LatestMutation(ctx, v.p.d.pool, rel.BuildID, false)
			if err != nil {
				return nil, nil, nil, err
			}
			if applicability != criterion.MutationApplicable {
				continue
			}
			newestRelease = rel.Number
			newest = screens.MutationScore{ServiceID: svc.ID, ItemID: rel.ItemID, BuildID: rel.BuildID,
				Score: reading.Mutation.Score(), MutantsTested: reading.Mutation.MutantsTested,
				MutantsDetected: reading.Mutation.MutantsDetected, Derived: reading.Mutation.Derived()}
		}
		if newest.BuildID != "" {
			scores = append(scores, newest)
		}
	}

	criteria := make([]screens.CriteriaCount, 0, len(services))
	for _, svc := range services {
		standing, err := criterion.InForce(ctx, v.p.d.pool, svc.ID, itemIDs[svc.ID])
		if err != nil {
			return nil, nil, nil, err
		}
		buildIDs := make([]string, 0)
		for _, rel := range releases {
			if rel.ServiceID == svc.ID {
				buildIDs = append(buildIDs, rel.BuildID)
			}
		}
		unreliable := int64(0)
		for _, one := range standing {
			reliability, err := criterion.Unreliable(ctx, v.p.d.pool, one.ID, buildIDs, svc.UnreliableBound, nil, "")
			if err != nil {
				return nil, nil, nil, err
			}
			if reliability.Unreliable {
				unreliable++
			}
		}
		criteria = append(criteria, screens.CriteriaCount{ServiceID: svc.ID, InForce: int64(len(standing)), Unreliable: unreliable})
		withdrawals, err := criterion.WithdrawalsForService(ctx, v.p.d.pool, svc.ID, itemIDs[svc.ID])
		if err != nil {
			return nil, nil, nil, err
		}
		byAuthor := make(map[string]int64)
		for _, withdrawal := range withdrawals {
			byAuthor[withdrawal.Actor.Key]++
		}
		for author, count := range byAuthor {
			criteria = append(criteria, screens.CriteriaCount{ServiceID: svc.ID, Author: author, Withdrawn: count})
		}
	}
	return hosting, scores, criteria, nil
}
