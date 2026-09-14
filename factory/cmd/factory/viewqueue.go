package main

import (
	"context"
	"encoding/json"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

func (v *views) queueAndWindows(ctx context.Context, filter screens.Filter) ([]screens.QueueRow, []screens.WindowRow, error) {
	services, err := service.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, err
	}
	items, err := item.All(ctx, v.p.d.pool)
	if err != nil {
		return nil, nil, err
	}
	priorities := make(map[string]int64, len(items))
	for _, one := range items {
		priorities[one.ID] = int64(one.Priority)
	}
	waits, err := decisionlog.NewReader(v.p.d.pool, v.p.d.token).PendingWaits(ctx, principal.OfComponent("screens"))
	if err != nil {
		return nil, nil, err
	}
	waiting := queueWaits(waits)

	queueRows := make([]screens.QueueRow, 0)
	windows := make([]screens.WindowRow, 0)
	for _, svc := range services {
		members, err := v.p.queue.Members(ctx, svc.ID)
		if err != nil {
			return nil, nil, err
		}
		seen := make(map[string]bool, len(members))
		for _, member := range members {
			seen[member.ID] = true
			if filter.WaitingOnAHuman && waiting.byItem[member.ID] == "" {
				continue
			}
			queueRows = append(queueRows, screens.QueueRow{
				ServiceID: svc.ID,
				ItemID:    member.ID,
				Priority:  int64(member.Priority),
				Holder:    mergequeue.Actor.Key,
				Waiting:   waiting.byItem[member.ID],
			})
		}
		for _, wait := range waiting.rows {
			if wait.serviceID != svc.ID || wait.itemID != "" && seen[wait.itemID] {
				continue
			}
			if filter.WaitingOnAHuman && wait.waiting == "" {
				continue
			}
			queueRows = append(queueRows, screens.QueueRow{
				ServiceID: svc.ID,
				ItemID:    wait.itemID,
				Priority:  priorities[wait.itemID],
				Holder:    wait.holder,
				Waiting:   wait.waiting,
			})
		}
		if filter.WaitingOnAHuman {
			continue
		}
		open, err := window.AllOpen(ctx, v.p.d.pool, svc.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, one := range open {
			windows = append(windows, screens.WindowRow{
				ID: one.ID, ServiceID: one.ServiceID, ReleaseID: one.ReleaseID,
				BuildID: one.BuildID, Holder: one.Actor.Key, OpenedAt: one.At,
			})
		}
	}
	return queueRows, windows, nil
}

type queueWait struct {
	serviceID string
	itemID    string
	holder    string
	waiting   string
}

type queueWaitReading struct {
	rows   []queueWait
	byItem map[string]string
}

func queueWaits(rows []decisionlog.Row) queueWaitReading {
	reading := queueWaitReading{byItem: make(map[string]string)}
	for _, row := range rows {
		var payload mergequeue.WaitPayload
		if json.Unmarshal([]byte(row.Payload), &payload) != nil || !isQueueWait(payload.Kind) {
			continue
		}
		one := queueWait{serviceID: payload.ServiceID, itemID: payload.ItemID,
			holder: row.Actor.Key, waiting: string(payload.Kind)}
		reading.rows = append(reading.rows, one)
		if one.itemID != "" {
			reading.byItem[one.itemID] = one.waiting
		}
	}
	return reading
}

func isQueueWait(kind mergequeue.WaitKind) bool {
	for _, each := range mergequeue.WaitKinds {
		if each == kind {
			return true
		}
	}
	return false
}
