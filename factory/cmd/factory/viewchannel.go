// Factory's numbers over the one way into the factory from outside it: what
// arrived and was never grouped, what the way in refused, what the store could
// not read, the services serving a way in older than this factory's, and the
// services whose project has no notice for the way in to show. Beside them,
// each intent's outcome, which the close computed and this only reads.
package main

import (
	"context"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// reportChannelNumbers is the whole of that reading. A composition holding no
// report store answers with the zero value: every subcommand that makes one
// pass and exits has none, and a screen shown zeros would read as a channel
// nobody used rather than as one this process cannot see.
//
// Two of the numbers are counters the store keeps and the rest are queries at
// read time. The counters have to be counters: the record a query would count
// is the write the rate exists to refuse, so a row per refusal is the
// unbounded write the rate was placed to prevent — and what that costs is that
// a lost counter is lost, where the ungrouped count is derived again.
func (v *views) reportChannelNumbers(ctx context.Context) (screens.ReportChannel, error) {
	store := v.p.d.reports
	if store == nil {
		return screens.ReportChannel{}, nil
	}
	var channel screens.ReportChannel
	ungrouped, err := store.Ungrouped(ctx)
	if err != nil {
		return screens.ReportChannel{}, err
	}
	channel.Ungrouped = ungrouped

	counts, err := store.Counts(ctx)
	if err != nil {
		return screens.ReportChannel{}, err
	}
	counted := map[string]int64{}
	unreadable := map[string]int64{}
	for _, one := range counts {
		if one.ServiceID == "" {
			// The empty key is the whole channel: a submission naming no deploy
			// this factory placed a way in at is counted there and never on a
			// service.
			channel.RefusedOverTheChannel = one.Refusals
			continue
		}
		counted[one.ServiceID] = one.Refusals
		unreadable[one.ServiceID] = one.UnreadableShape
	}

	services, err := service.All(ctx, v.p.d.pool)
	if err != nil {
		return screens.ReportChannel{}, err
	}
	// The notice is read once per project and not once per service: it is a
	// constraint whose reach is a project, and several services lie in one.
	notices := map[string]bool{}
	for _, svc := range services {
		if _, read := notices[svc.ProjectID]; !read {
			_, authored, err := constraint.NoticeInForce(ctx, v.p.d.pool, svc.ProjectID)
			if err != nil {
				return screens.ReportChannel{}, err
			}
			notices[svc.ProjectID] = authored
		}
		channel.Services = append(channel.Services, screens.ServiceReportCounts{
			ServiceID: svc.ID, ServiceName: svc.Name,
			Refused: counted[svc.ID], UnreadableShape: unreadable[svc.ID],
			NoNoticeInForce: !notices[svc.ProjectID],
		})
		old, serving, err := v.wayInOlderThanThisFactory(ctx, svc)
		if err != nil {
			return screens.ReportChannel{}, err
		}
		if serving {
			channel.OnAnOldWayIn = append(channel.OnAnOldWayIn, old)
		}
	}
	return channel, nil
}

// wayInOlderThanThisFactory is whether one service's current release carries a
// way in built under another release of the product than the running one, read
// off the shipped-bundle identity the build record names.
//
// The comparison is for difference and not for order: the identity a build
// names is this binary's own factoryVersion, and an identity of the bundle's
// own is not built, so what a difference says is that the service last built
// under some other release of the factory. A factory only moves forward, so
// that release is an earlier one.
//
// A service with no release yet serves no way in at all and is not on the
// list: what it is waiting for is a first release and not an upgrade.
func (v *views) wayInOlderThanThisFactory(ctx context.Context,
	svc service.Service) (screens.ServiceOnAnOldWayIn, bool, error) {
	current, minted, err := release.Highest(ctx, v.p.d.pool, svc.ID)
	if err != nil || !minted {
		return screens.ServiceOnAnOldWayIn{}, false, err
	}
	made, err := build.Get(ctx, v.p.d.pool, current.BuildID)
	if err != nil {
		return screens.ServiceOnAnOldWayIn{}, false, err
	}
	if made.ShippedBundleIdentity == factoryVersion {
		return screens.ServiceOnAnOldWayIn{}, false, nil
	}
	return screens.ServiceOnAnOldWayIn{
		ServiceID: svc.ID, ServiceName: svc.Name,
		Identity: made.ShippedBundleIdentity, FactoryIdentity: factoryVersion,
	}, true, nil
}

// intentOutcomes is each closed intent's outcome, read beside cost per
// feature. The value was computed once at the close and stored on the intent,
// so this reads it and computes nothing: a rate recomputed at every read would
// keep moving after the release it is about.
//
// An intent the factory raised carries none, and it is listed with none rather
// than left out — absence is the answer there and not a gap.
func (v *views) intentOutcomes(ctx context.Context) ([]screens.IntentOutcome, error) {
	all, err := intent.InProject(ctx, v.p.d.pool, v.p.projectID)
	if err != nil {
		return nil, err
	}
	var outcomes []screens.IntentOutcome
	for _, one := range all {
		if one.State != intent.StateDelivered {
			continue
		}
		outcomes = append(outcomes, screens.IntentOutcome{
			IntentID: one.ID, Source: string(one.Source), Outcome: one.Outcome,
		})
	}
	return outcomes, nil
}
