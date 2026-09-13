package driftdetector_test

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/driftdetector"
)

// The detector must read every structured field and validate the stored
// format, both at the anchored row and in the rows appended after it.
func TestVerifyChainChecksStructuredFieldsAndFormat(t *testing.T) {
	for _, position := range []string{"checkpoint", "after checkpoint"} {
		for _, format := range []string{"legacy", "extended"} {
			t.Run(position+"/"+format, func(t *testing.T) {
				ctx, factory, token := newFactoryPool(t)
				own := newOwnStore(t, ctx)
				log := decisionlog.NewWriter(factory, token)
				first, err := log.AppendPageEvent(ctx, decisionlog.Entry{Actor: logActor, FormatVersion: "page_event/1", Payload: "first"})
				if err != nil {
					t.Fatal(err)
				}
				var subject decisionlog.Row
				if format == "legacy" {
					subject, err = log.AppendPageEvent(ctx, decisionlog.Entry{Actor: logActor, FormatVersion: "page_event/1", Payload: "subject"})
				} else {
					subject, err = log.AppendReworkRequest(ctx, decisionlog.Entry{Actor: logActor, FormatVersion: "rework_request/1", Reason: "defect", ReturnsTo: "spec"})
				}
				if err != nil {
					t.Fatal(err)
				}
				anchor := first
				if position == "checkpoint" {
					anchor = subject
				}
				if _, err := driftdetector.NewWriter(own).RecordHead(ctx, anchor.Hash, anchor.Seq); err != nil {
					t.Fatal(err)
				}
				head, mismatch, why, err := driftdetector.VerifyChain(ctx, own, factory)
				if err != nil || mismatch || head.Hash != subject.Hash {
					t.Fatalf("unchanged chain: head=%+v mismatch=%v why=%q err=%v", head, mismatch, why, err)
				}
				if format == "legacy" {
					if _, err := factory.Exec(ctx, `alter table decision_log drop constraint structured_fields_match_format`); err != nil {
						t.Fatal(err)
					}
					_, err = factory.Exec(ctx, `update decision_log set caller_kind = 'component', caller_key = 'work', caller_key_basis = 'claimed' where id = $1`, subject.ID)
				} else {
					_, err = factory.Exec(ctx, `update decision_log set returns_to = 'implementation' where id = $1`, subject.ID)
				}
				if err != nil {
					t.Fatal(err)
				}
				_, mismatch, why, err = driftdetector.VerifyChain(ctx, own, factory)
				if err != nil || !mismatch || !strings.Contains(why, subject.ID) {
					t.Fatalf("tampered chain: mismatch=%v why=%q err=%v", mismatch, why, err)
				}
			})
		}
	}
}
