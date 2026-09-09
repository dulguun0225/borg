package mergequeue_test

import (
	"encoding/json"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/mergequeue"
)

// TestTheMintTakesTheHigherOfTwoReadingsAndWritesTheNumbersItSkipped: the numbers
// a restore lost are above the highest record, and what says how high they went
// is the health monitor's store. The mint seats the release above the higher of
// the two readings and writes the numbers it passed over into the log. It is the
// first mint on the service since the install event says there was a restore,
// which is what the reading is taken for; nothing writes that row yet, so the
// test writes it directly, against the shape a writer will one day fill.
func TestTheMintTakesTheHigherOfTwoReadingsAndWritesTheNumbersItSkipped(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{
		Repository: repo,
		Numbers:    seen{serviceID: 7},
	})
	if _, err := decisionlog.NewWriter(pool, token).AppendInstallEvent(ctx, decisionlog.Entry{
		Actor: mergequeue.Actor, Payload: `{"event":"restore"}`, FormatVersion: "install_event/1",
	}); err != nil {
		t.Fatalf("writing the install event: %v", err)
	}

	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-eight", BuildID: "bl_eight", Passed: true}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || pass.Outcomes[0].Release.Number != 8 {
		t.Fatalf("the outcomes are %+v, want the release seated at number eight", pass.Outcomes)
	}
	skipped := pass.Outcomes[0].SkippedNumbers
	if len(skipped) != 7 || skipped[0] != 1 || skipped[6] != 7 {
		t.Errorf("the mint reports %v skipped, want one through seven", skipped)
	}

	var payload mergequeue.SkippedNumbersPayload
	found := false
	for _, row := range readLog(t, ctx, pool, token) {
		if row.Shape != decisionlog.ShapeInstallEvent {
			continue
		}
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			t.Fatalf("reading the skipped-number payload: %v", err)
		}
		found = payload.Kind == mergequeue.SkippedNumbersKind
	}
	if !found {
		t.Fatal("the log holds no row naming the numbers the mint skipped")
	}
	if payload.Seen != 7 || payload.InRecords != 0 || payload.Number != 8 {
		t.Errorf("the row reports seen %d, in records %d, seated at %d", payload.Seen, payload.InRecords, payload.Number)
	}

	// The next mint on the same service skips nothing: the records now run ahead
	// of the store.
	second := queued(ctx, t, pool, token, 2)
	repo.verified[second.ID] = mergequeue.Verified{Commit: "commit-nine", BuildID: "bl_nine", Passed: true}
	pass, err = q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("the second Run: %v", err)
	}
	last := pass.Outcomes[len(pass.Outcomes)-1]
	if last.Release.Number != 9 || len(last.SkippedNumbers) != 0 {
		t.Errorf("the second mint is number %d and skipped %v", last.Release.Number, last.SkippedNumbers)
	}
}

// TestTheMintReadsTheRecordsAloneWithNoInstallEvent: nothing says there was a
// restore, so the health monitor's store is not read at all, and a number the
// store runs ahead of is not taken.
func TestTheMintReadsTheRecordsAloneWithNoInstallEvent(t *testing.T) {
	repo := newRepository()
	ctx, pool, token, q := newQueue(t, mergequeue.Composition{
		Repository: repo,
		Numbers:    seen{serviceID: 7},
	})
	it := queued(ctx, t, pool, token, 1)
	repo.verified[it.ID] = mergequeue.Verified{Commit: "commit-one", BuildID: "bl_one", Passed: true}

	pass, err := q.Run(ctx, serviceID)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(pass.Outcomes) != 1 || pass.Outcomes[0].Release.Number != 1 {
		t.Fatalf("the outcomes are %+v, want the release seated at number one — nothing says there was a restore",
			pass.Outcomes)
	}
	if len(pass.Outcomes[0].SkippedNumbers) != 0 {
		t.Errorf("the mint reports %v skipped, and it took no reading of the store", pass.Outcomes[0].SkippedNumbers)
	}
}
