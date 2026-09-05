package score

import "context"

// Marks is what a named human at Ops marked as not caused by the release: a
// record of its own, written once, pointing at the rollback's deploy record, and
// read by everything that learns from outcomes and by nothing that acts. It is
// an interface here because the record's writer is Ops and no package owns it
// yet: the composition hands the score whatever reads it, and [NoMarks] is what
// a factory with no such record composes.
//
// What a mark changes is the evidence and only the evidence: the rollback is
// excluded from the per-author prior of every release it undid, from the
// window's size and its power alike, and from the window limit. It teaches the
// confidence nothing, a confounded comparison saying what crossed was not the
// change and nothing about how confident the comparison should have been.
type Marks interface {
	// NotCausedByTheRelease is the ids of the releases whose rollback a human
	// marked, which is what every rule here excludes.
	NotCausedByTheRelease(ctx context.Context) ([]string, error)
}

// NoMarks is the reading of a factory where nothing has been marked, and the
// value a composition with no mark record hands in. It is not the same as a
// factory that has marks and cannot read them: that would be an unavailable
// input, and this is an empty one.
type NoMarks struct{}

// NotCausedByTheRelease is nothing.
func (NoMarks) NotCausedByTheRelease(context.Context) ([]string, error) { return nil, nil }

// marked is the set of releases a mark excludes, read once per call.
func (s *Score) marked(ctx context.Context) (map[string]bool, error) {
	return markedSet(ctx, s.marks)
}

func markedSet(ctx context.Context, marks Marks) (map[string]bool, error) {
	excluded := map[string]bool{}
	if marks == nil {
		return excluded, nil
	}
	releases, err := marks.NotCausedByTheRelease(ctx)
	if err != nil {
		return nil, err
	}
	for _, id := range releases {
		excluded[id] = true
	}
	return excluded, nil
}
