package intent

import (
	"context"
	"errors"
)

// Notifier is the component that delivers what waits on a human, as intake
// reaches it. Intake makes two calls on it and no third: at each round of the
// interview, and at an intent escalated.
//
// It is an interface the composition supplies rather than an import. What a
// wait is — its kind, whom it routes to, and whether anything live is worse
// until a human ends it — is the notifier's own vocabulary, and the notifier
// reads records that are written above this package in the graph.
type Notifier interface {
	// Interviewed is one question of a round, written: the intent, the
	// question record, and what it asks. A round asking three is three calls,
	// one wait each, there being no record of a round for a wait to name.
	Interviewed(ctx context.Context, intentID, questionID, question string) error
	// Escalated is an intent whose rounds or re-decompositions exceeded the
	// attempt limit.
	Escalated(ctx context.Context, intentID string) error
}

// NoNotifier is what an intake that reaches no human is composed with: nothing
// is delivered and every write stands. A caller that only ends or corrects an
// intent creates no wait, and a test of a write is not a test of what it tells
// a human.
type NoNotifier struct{}

// Interviewed delivers nothing.
func (NoNotifier) Interviewed(context.Context, string, string, string) error { return nil }

// Escalated delivers nothing.
func (NoNotifier) Escalated(context.Context, string) error { return nil }

// ErrNotifierNotComposed is returned where a write that creates a wait is made
// on an intake composed with no notifier. [NoNotifier] is what says a
// composition reaches no human on purpose, so a nil one is a defect in the
// composition rather than a factory without a notifier.
var ErrNotifierNotComposed = errors.New("intent: this intake was composed with no notifier")
