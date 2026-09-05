package decisionlog_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
)

// TestConcurrentAppendsChainInOrder is the one-writer rule under load. Without
// the advisory lock two transactions read the same head and write two rows
// naming the same predecessor, which is the fork the unique constraint on
// prev_hash refuses and Verify would otherwise find.
func TestConcurrentAppendsChainInOrder(t *testing.T) {
	ctx, pool, log := newLog(t)

	const appenders, each = 8, 6
	var wg sync.WaitGroup
	failures := make(chan error, appenders*each)
	for a := range appenders {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := range each {
				_, err := log.AppendWait(ctx, decisionlog.Entry{
					Actor:   record.Actor{Kind: record.KindComponent, Name: "appender"},
					Payload: strings.Repeat("x", a) + "-" + strings.Repeat("y", n),
				})
				if err != nil {
					failures <- err
				}
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("AppendWait: %v", err)
	}

	if err := decisionlog.Verify(ctx, pool); err != nil {
		t.Fatalf("Verify: %v", err)
	}
	rows, err := decisionlog.Read(ctx, pool)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(rows) != appenders*each {
		t.Fatalf("the log holds %d rows, want %d", len(rows), appenders*each)
	}
}
