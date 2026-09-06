package decisionlog_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// TestConcurrentAppendsChainInOrder is the one-writer rule under load.
// Without the advisory lock two transactions read the same head and write
// two rows naming the same predecessor, which is the fork the unique
// constraint on prev_hash refuses and Verify would otherwise find.
func TestConcurrentAppendsChainInOrder(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	reader := decisionlog.NewReader(pool, token)

	const appenders, each = 8, 6
	var wg sync.WaitGroup
	failures := make(chan error, appenders*each)
	for a := range appenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range each {
				opened, err := log.AppendWaitOpen(ctx, decisionlog.Entry{
					Actor:         record.Actor{Kind: record.KindComponent, Key: "appender", Basis: record.BasisClaimed},
					Payload:       strings.Repeat("x", a) + "-" + strings.Repeat("y", n),
					FormatVersion: "wait/1",
				})
				if err != nil {
					failures <- err
					continue
				}
				if _, err := log.AppendWaitClose(ctx, decisionlog.Entry{
					Actor:   record.Actor{Kind: record.KindComponent, Key: "appender", Basis: record.BasisClaimed},
					Payload: "gone", FormatVersion: "wait/1", Closes: opened.ID,
				}); err != nil {
					failures <- err
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("appending a wait: %v", err)
	}

	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	rows, err := reader.Read(ctx, ownerReading)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	// Two rows per wait, plus the two read events this test's own Verify and
	// Read just appended.
	if want := appenders*each*2 + 2; len(rows) != want {
		t.Fatalf("the log holds %d rows, want %d", len(rows), want)
	}
}
