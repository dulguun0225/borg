package main

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// Factory's numbers, computed at read time over the one factory-owned span:
// throughput, rework rate, gate rejection rate, cost per feature, the human's
// load, and the page channel. Every one is a query over records the factory
// already writes — no wait record is added, and the holds the factory sets over
// a record it never writes stay uncounted.

// numbers is throughput, rework rate, gate rejection rate and cost per
// feature. Throughput is releases per service; gate rejection rate is rejects
// over firings per gate row; rework rate is items with more than one attempt at
// any stage over items reaching a release; cost per feature is what the intents
// and their items spent on models, converted where every rate a feature ran on
// is authored and reported as units per model version where one is missing.
func (v *views) numbers(ctx context.Context, who principal.Principal) (screens.Numbers, error) {
	var out screens.Numbers
	services, err := service.All(ctx, v.p.d.pool)
	if err != nil {
		return out, err
	}
	releases, err := release.All(ctx, v.p.d.pool)
	if err != nil {
		return out, err
	}
	out.ThroughputPerService = map[string]int64{}
	for _, svc := range services {
		out.ThroughputPerService[svc.ID] = 0
	}
	for _, rel := range releases {
		out.ThroughputPerService[rel.ServiceID]++
	}

	if out.ReworkRate, err = v.reworkRate(ctx, releases); err != nil {
		return out, err
	}
	if out.GateRejectionRate, err = v.gateRejectionRate(ctx, who); err != nil {
		return out, err
	}
	if out.CostPerFeature, out.CostMeasured, err = v.costPerFeature(ctx); err != nil {
		return out, err
	}
	return out, nil
}

// reworkRate is items with more than one attempt at any stage over items
// reaching a release. An install with no release has no denominator and reports
// zero, which is what a factory that has shipped nothing has to say about
// rework.
func (v *views) reworkRate(ctx context.Context, releases []release.Release) (float64, error) {
	shipped := map[string]bool{}
	for _, rel := range releases {
		if rel.ItemID != "" {
			shipped[rel.ItemID] = true
		}
	}
	if len(shipped) == 0 {
		return 0, nil
	}
	stages, err := item.AllStages(ctx, v.p.d.pool)
	if err != nil {
		return 0, err
	}
	reworked := map[string]bool{}
	for _, one := range stages {
		if one.Attempts > 1 {
			reworked[one.ItemID] = true
		}
	}
	count := 0
	for id := range shipped {
		if reworked[id] {
			count++
		}
	}
	return float64(count) / float64(len(shipped)), nil
}

// gateRejectionRate is rejects over firings, per gate row.
func (v *views) gateRejectionRate(ctx context.Context, who principal.Principal) (map[string]float64, error) {
	closed, err := decisionlog.NewReader(v.p.d.pool, v.p.d.token).ClosedDecisions(ctx, who)
	if err != nil {
		return nil, err
	}
	firings, rejects := map[string]int{}, map[string]int{}
	for _, one := range closed {
		opened, is := openingOn(one.OpenEvent, func(gate.Opened) bool { return true })
		if !is {
			continue
		}
		row := opened.Gate.String()
		firings[row]++
		if one.CloseEvent.Verdict == string(gate.VerdictReject) {
			rejects[row]++
		}
	}
	rates := make(map[string]float64, len(firings))
	for row, count := range firings {
		rates[row] = float64(rejects[row]) / float64(count)
	}
	return rates, nil
}

// costPerFeature is what the intents and their items spent on models, and
// nothing of the substrate. Where every kind of unit every run reported has a
// rate authored it is one converted total; where one is missing the figure is
// units per model version and CostMeasured is false, which is the screen saying
// the factory does not measure cost.
func (v *views) costPerFeature(ctx context.Context) ([]screens.ModelCost, bool, error) {
	ids, err := v.intentIDs(ctx)
	if err != nil {
		return nil, false, err
	}
	var runs []agentrun.Run
	for _, id := range ids {
		onIntent, err := agentrun.ForIntent(ctx, v.p.d.pool, id)
		if err != nil {
			return nil, false, err
		}
		runs = append(runs, onIntent...)
		items, err := item.ForIntent(ctx, v.p.d.pool, id)
		if err != nil {
			return nil, false, err
		}
		for _, it := range items {
			onItem, err := agentrun.ForItem(ctx, v.p.d.pool, it.ID)
			if err != nil {
				return nil, false, err
			}
			runs = append(runs, onItem...)
		}
	}

	measured, total, currency := true, 0.0, ""
	units := map[string]int64{}
	for _, run := range runs {
		if !run.Priced {
			measured = false
		}
		total += run.ConvertedAmount
		if run.Currency != "" {
			currency = run.Currency
		}
		for _, count := range run.UnitsByKind {
			units[run.ModelVersion] += count
		}
	}
	if measured {
		return []screens.ModelCost{{Amount: total, Currency: currency, IsTotal: true}}, true, nil
	}
	versions := make([]string, 0, len(units))
	for version := range units {
		versions = append(versions, version)
	}
	sort.Strings(versions)
	costs := make([]screens.ModelCost, 0, len(versions))
	for _, version := range versions {
		costs = append(costs, screens.ModelCost{ModelVersion: version, Amount: float64(units[version])})
	}
	return costs, false, nil
}

// humanReadings is everything on Factory that is a read of the log's own rows:
// the human's load, what each human approved and how often it was undone, the
// self-approval counts, how many gates a resolved factor put a human at, the
// page channel's four numbers, and the load split at the first delivery a
// transport accepted and again at the first acknowledgement.
func (v *views) humanReadings(ctx context.Context, who principal.Principal, view *screens.Factory) error {
	rows, err := decisionlog.NewReader(v.p.d.pool, v.p.d.token).Read(ctx, who)
	if err != nil {
		return err
	}
	closings, acknowledgements := pairings(rows)
	ended := map[string]bool{}
	for _, row := range rows {
		if row.Shape == decisionlog.ShapeDecision && row.Part == decisionlog.PartAbandonment {
			ended[row.Closes] = true
		}
	}

	loads := map[string]*screens.HumanLoad{}
	splits := map[string]*screens.LoadSplit{}
	approved := map[string]*screens.ApproveUndonePair{}
	selfApprovals := map[string]int64{}
	resolvedFactors := map[string]int64{}
	firedPages := int64(0)
	pagesPerService := map[string]int64{}
	pagesPerHuman := map[string]int64{}

	for _, row := range rows {
		if row.Shape == decisionlog.ShapeDecision && row.Part == decisionlog.PartOpen {
			opened, is := openingOn(row, func(gate.Opened) bool { return true })
			if !is {
				continue
			}
			for _, one := range opened.Assessment.Resolved {
				resolvedFactors[one.Factor]++
			}
			if !opened.HumanDecides {
				continue
			}
			key := loadKey(int64(opened.WaitsOn.Duty), opened.WaitsOn.Human)
			load := loads[key]
			if load == nil {
				load = &screens.HumanLoad{Duty: int64(opened.WaitsOn.Duty), HumanKey: opened.WaitsOn.Human}
				loads[key] = load
			}
			closing, closed := closings[row.ID]
			if !closed && !ended[row.ID] {
				load.WaitingNow++
				continue
			}
			if !closed {
				continue
			}
			load.MedianWaitSeconds = seconds(row.At, closing.At)
			load.MedianOpenInFrontSeconds = seconds(closing.OpenedInWorkAt, closing.At)
			split := splits[key]
			if split == nil {
				split = &screens.LoadSplit{Duty: load.Duty, HumanKey: load.HumanKey}
				splits[key] = split
			}
			v.splitAt(ctx, row, closing, acknowledgements[row.ID], split)
			continue
		}
		if row.Shape == decisionlog.ShapePageEvent {
			var payload notifier.Payload
			if json.Unmarshal([]byte(row.Payload), &payload) != nil {
				continue
			}
			if payload.WaitKind == string(notifier.KindOwnerFired) {
				firedPages++
			}
			pagesPerHuman[payload.Reached]++
			// A page about no service counts against no service: the wait
			// belongs to a component or to the factory as a whole, and a row
			// written before the page event carried the field reads the same
			// way — the count is lower and never wrong about a service.
			if payload.ServiceID != "" {
				pagesPerService[payload.ServiceID]++
			}
		}
	}

	for _, one := range closings {
		if one.Verdict != string(gate.VerdictApprove) {
			continue
		}
		pair := approved[one.Actor.Key]
		if pair == nil {
			pair = &screens.ApproveUndonePair{HumanKey: one.Actor.Key}
			approved[one.Actor.Key] = pair
		}
		pair.Approved++
		if one.SelfApproval {
			selfApprovals[one.Actor.Key]++
		}
	}
	undone, err := v.undoneApprovals(ctx)
	if err != nil {
		return err
	}
	factory := &screens.ApproveUndonePair{}
	for _, pair := range approved {
		factory.Approved += pair.Approved
	}
	factory.Undone = undone

	// A field named for the key carries the key, and every other human-facing
	// field carries the name the mapping gives it: a key is what every record
	// of the graph holds and what these rows are grouped by, and People is the
	// one view that carries the pair, being where the mapping is. So a screen
	// resolves a name through that view rather than through a name repeated on
	// every row of this one.
	for _, load := range loads {
		view.HumanLoad = append(view.HumanLoad, *load)
	}
	for _, split := range splits {
		view.LoadSplits = append(view.LoadSplits, *split)
	}
	for _, pair := range approved {
		view.ApproveUndone = append(view.ApproveUndone, *pair)
	}
	view.ApproveUndone = append(view.ApproveUndone, *factory)
	for key, count := range selfApprovals {
		view.SelfApprovalCounts = append(view.SelfApprovalCounts,
			screens.SelfApprovalCount{HumanKey: key, Count: count})
	}
	for factor, count := range resolvedFactors {
		view.ResolvedFactorGates = append(view.ResolvedFactorGates,
			screens.ResolvedFactorCount{Factor: factor, Count: count})
	}
	// Every one of these is collected in a map, so the order it is rendered in
	// is fixed here: a screen re-reading an address after a change would
	// otherwise show the same rows in a different order.
	sort.Slice(view.ResolvedFactorGates, func(i, j int) bool {
		return view.ResolvedFactorGates[i].Factor < view.ResolvedFactorGates[j].Factor
	})
	sort.Slice(view.HumanLoad, func(i, j int) bool {
		if view.HumanLoad[i].Duty != view.HumanLoad[j].Duty {
			return view.HumanLoad[i].Duty < view.HumanLoad[j].Duty
		}
		return view.HumanLoad[i].HumanKey < view.HumanLoad[j].HumanKey
	})
	sort.Slice(view.LoadSplits, func(i, j int) bool {
		if view.LoadSplits[i].Duty != view.LoadSplits[j].Duty {
			return view.LoadSplits[i].Duty < view.LoadSplits[j].Duty
		}
		return view.LoadSplits[i].HumanKey < view.LoadSplits[j].HumanKey
	})
	sort.Slice(view.ApproveUndone, func(i, j int) bool {
		return view.ApproveUndone[i].HumanKey < view.ApproveUndone[j].HumanKey
	})
	sort.Slice(view.SelfApprovalCounts, func(i, j int) bool {
		return view.SelfApprovalCounts[i].HumanKey < view.SelfApprovalCounts[j].HumanKey
	})

	named := map[string]int64{}
	for key, count := range pagesPerHuman {
		named[v.nameOf(ctx, key)] = count
	}
	view.PageChannel = screens.PageChannel{
		PagesPerService: pagesPerService,
		PagesPerHuman:   named,
		HumanFiredPages: firedPages,
	}
	return nil
}

// splitAt splits one row's wait at the first delivery a transport accepted and
// again at the first acknowledgement: the part before the first delivery is the
// channel's, the part between it and the first acknowledgement is the shared
// duty's, and the part after is that human's own. A row no delivery was ever
// accepted for is charged to the channel and against nobody.
func (v *views) splitAt(ctx context.Context, opened, closed decisionlog.Row,
	acknowledged []decisionlog.Row, split *screens.LoadSplit) {
	deliveries, err := notifier.DeliveriesOf(ctx, v.p.d.pool, opened.ID)
	if err != nil {
		return
	}
	first := ""
	for _, one := range deliveries {
		if one.FirstAcceptedAt == "" {
			continue
		}
		if first == "" || one.FirstAcceptedAt < first {
			first = one.FirstAcceptedAt
		}
	}
	if first == "" {
		split.BeforeFirstDeliverySeconds += float64(seconds(opened.At, closed.At))
		return
	}
	split.BeforeFirstDeliverySeconds += float64(seconds(opened.At, first))
	if len(acknowledged) == 0 {
		split.BetweenDeliveryAndAcknowledgementSeconds += float64(seconds(first, closed.At))
		return
	}
	at := acknowledged[0].At
	split.BetweenDeliveryAndAcknowledgementSeconds += float64(seconds(first, at))
	split.AfterFirstAcknowledgementSeconds += float64(seconds(at, closed.At))
}

// undoneApprovals is how often what was approved was later undone: every
// rollback the deploy records hold, which is the pair the design reads beside
// the auto-approval rate.
func (v *views) undoneApprovals(ctx context.Context) (int64, error) {
	rollbacks, err := deploy.Rollbacks(ctx, v.p.d.pool)
	if err != nil {
		return 0, err
	}
	return int64(len(rollbacks)), nil
}

// loadKey is what the human's load is grouped by: the duty where the row names
// one, and the named human where a record's routing named one.
func loadKey(duty int64, human string) string {
	return strconv.FormatInt(duty, 10) + "/" + human
}

// seconds is the interval between two of the log's own timestamps, and zero
// where either is absent or unreadable — every timestamp is fixed-width UTC
// text, so a value that will not parse is one no writer of this graph wrote.
func seconds(from, to string) int64 {
	if from == "" || to == "" {
		return 0
	}
	began, err := record.ParseTime(from)
	if err != nil {
		return 0
	}
	ended, err := record.ParseTime(to)
	if err != nil {
		return 0
	}
	return int64(ended.Sub(began) / time.Second)
}

// formatNumber is a number as a screen shows it: the shortest form that reads
// back as the same value, so an authored 4 is "4" and not "4.000000".
func formatNumber(value float64) string {
	return strings.TrimSuffix(strconv.FormatFloat(value, 'f', -1, 64), ".0")
}
