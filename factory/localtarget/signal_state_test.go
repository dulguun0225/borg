package localtarget

import (
	"errors"
	"strings"
	"testing"
)

func TestSignalErrorsKeepFirstAndCountLater(t *testing.T) {
	var failures signalFailures
	first := errors.New("first acceptance failure")
	failures.record(first)
	failures.record(errors.New("later acceptance failure"))

	got, later := failures.take()
	if !errors.Is(got, first) {
		t.Fatalf("first error = %v, want %v", got, first)
	}
	if later != 1 || !strings.Contains(got.Error(), "first acceptance failure") {
		t.Fatalf("later count = %d or error = %v, want one later failure", later, got)
	}
	if next, count := failures.take(); next != nil || count != 0 {
		t.Fatalf("take after surfacing = %v, %d; want empty", next, count)
	}
}
