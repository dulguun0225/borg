package score

import "testing"

// TestAnUnreliableCriterionsQueueRejectionTeachesTheThresholdNothing: a failure
// that repeats is real, and what the score learns turns on the criterion and
// the two compositions the queue's re-verification compared — an unreliable
// one teaches nothing, so the row it wrote is skipped rather than read as a
// gate the factory needed. Where the criterion was not unreliable and the
// candidate failed against the master it merges into, the score learns as
// from a reject, at the row [_the merge queue_] follows the merge to master
// gate with. A queue rejection the score learns as from a hold moves nothing
// either way, whatever the criterion.
func TestAnUnreliableCriterionsQueueRejectionTeachesTheThresholdNothing(t *testing.T) {
	for _, c := range []struct {
		what   string
		reject queueRejection
		want   int
	}{
		{
			what:   "a repeated failure a reliable criterion decided, learned as from a reject",
			reject: queueRejection{ItemID: "it_a", LearnsAs: VerdictRejected},
			want:   1,
		},
		{
			what:   "the same, but the criterion is unreliable over the two builds compared",
			reject: queueRejection{ItemID: "it_a", LearnsAs: VerdictRejected, TeachesNothing: true},
			want:   0,
		},
		{
			what:   "a dependency's release moved between the runs, learned as from a hold",
			reject: queueRejection{ItemID: "it_a", LearnsAs: "hold"},
			want:   0,
		},
		{
			what:   "a hold an unreliable criterion also names",
			reject: queueRejection{ItemID: "it_a", LearnsAs: "hold", TeachesNothing: true},
			want:   0,
		},
	} {
		e := newEvidence()
		e.queueRejections = []queueRejection{c.reject}
		e.index()
		if got := rejectionsTheFactoryNeeded(e)[queueRejectionRow]; got != c.want {
			t.Errorf("%s: merge_to_master needed %d, want %d", c.what, got, c.want)
		}
	}
}
