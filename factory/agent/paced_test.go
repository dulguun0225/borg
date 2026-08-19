package agent

import (
	"context"
	"errors"
	"testing"
	"time"
)

// countingModel answers instantly and records when each call arrived, which is
// what a pace is measured from.
type countingModel struct {
	at []time.Time
}

func (m *countingModel) Complete(_ context.Context, _, _ string) (Reply, error) {
	m.at = append(m.at, time.Now())
	return Reply{Text: "SPEC:\nx\nCRITERION: The system shall answer.", Tokens: 1}, nil
}

// TestPacedLeavesTheIntervalBetweenCalls is the promise: however fast the inner
// model answers, two calls start at least an interval apart. The intervals are
// short because what is asserted is the ordering and not a duration a human
// would wait.
func TestPacedLeavesTheIntervalBetweenCalls(t *testing.T) {
	const interval = 60 * time.Millisecond
	inner := &countingModel{}
	paced := NewPaced(inner, interval)

	start := time.Now()
	for n := range 3 {
		if _, err := paced.Complete(context.Background(), "s", "u"); err != nil {
			t.Fatalf("call %d: %v", n+1, err)
		}
	}

	if len(inner.at) != 3 {
		t.Fatalf("the inner model was called %d times, want 3", len(inner.at))
	}
	// The first call does not wait: there is nothing before it to pace against.
	if first := inner.at[0].Sub(start); first > interval {
		t.Errorf("the first call waited %v, and the first call waits for nothing", first)
	}
	for n := 1; n < len(inner.at); n++ {
		if gap := inner.at[n].Sub(inner.at[n-1]); gap < interval {
			t.Errorf("calls %d and %d are %v apart, the interval is %v", n, n+1, gap, interval)
		}
	}
}

// TestPacedSendsNothingOnceTheContextIsCancelled is what a cancelled run must
// not do: send one more request. The first call goes through and the second is
// waiting out the interval when the context ends.
func TestPacedSendsNothingOnceTheContextIsCancelled(t *testing.T) {
	inner := &countingModel{}
	paced := NewPaced(inner, time.Hour)

	if _, err := paced.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("the first call: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := paced.Complete(ctx, "s", "u")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Complete = %v, want the context's error", err)
	}
	if len(inner.at) != 1 {
		t.Errorf("the inner model was called %d times, and a cancelled call reaches it not at all", len(inner.at))
	}
}

// TestPacedWithNoIntervalWaitsNever is the zero value a test wires when it
// wants the calls back to back.
func TestPacedWithNoIntervalWaitsNever(t *testing.T) {
	inner := &countingModel{}
	paced := NewPaced(inner, 0)

	start := time.Now()
	for range 5 {
		if _, err := paced.Complete(context.Background(), "s", "u"); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("five unpaced calls took %v, and a zero interval waits never", elapsed)
	}
	if len(inner.at) != 5 {
		t.Errorf("the inner model was called %d times, want 5", len(inner.at))
	}
}
