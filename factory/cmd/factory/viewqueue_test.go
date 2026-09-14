package main

import (
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/mergequeue"
	"github.com/dulguun0225/borg/factory/record"
)

func TestQueueWaitsKeepOnlyQueuePayloads(t *testing.T) {
	queuePayload, err := json.Marshal(mergequeue.WaitPayload{
		Kind: mergequeue.WaitHalt, ServiceID: "svc_1", ItemID: "it_1",
	})
	if err != nil {
		t.Fatal(err)
	}
	notQueuePayload, err := json.Marshal(struct {
		Kind      string `json:"kind"`
		ServiceID string `json:"service_id"`
	}{Kind: "a different wait", ServiceID: "svc_1"})
	if err != nil {
		t.Fatal(err)
	}
	reading := queueWaits([]decisionlog.Row{
		{Payload: string(queuePayload), Actor: record.Actor{Key: "merge_queue"}},
		{Payload: string(notQueuePayload), Actor: record.Actor{Key: "other"}},
		{Payload: "not json"},
	})
	if len(reading.rows) != 1 {
		t.Fatalf("queue waits = %+v, want one queue wait", reading.rows)
	}
	one := reading.rows[0]
	if one.serviceID != "svc_1" || one.itemID != "it_1" || one.holder != "merge_queue" ||
		one.waiting != string(mergequeue.WaitHalt) {
		t.Errorf("queue wait = %+v, want the queue's service, item, holder, and kind", one)
	}
	if reading.byItem["it_1"] != string(mergequeue.WaitHalt) {
		t.Errorf("wait by item = %+v, want the halt for it_1", reading.byItem)
	}
}
