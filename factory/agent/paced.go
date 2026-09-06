package agent

import (
	"context"
	"sync"
	"time"

	"github.com/dulguun0225/borg/factory/principal"
)

// Paced is a [Model] that leaves at least an interval between the start of one
// call and the start of the next, so the factory cannot send requests in rapid
// succession however many calls a stage makes. It wraps the model a role holds
// and answers exactly what the inner model answered.
//
// Between starts and not between a reply and the next request: what this bounds
// is a rate, so a call slower than the interval imposes no wait at all and a
// stage retrying after a fast failure waits the whole of it. That is the case
// this exists for — a refused reply and a retry are two calls with nothing
// between them otherwise, and a provider that refused one request quickly is
// the last one to send the next request to immediately.
//
// It is a wrapper and not a field on [Anthropic] because Anthropic is a value
// with nowhere to keep the time of the last call, and pacing is not the
// provider client's subject: what it holds is one call's wire shape. The caller
// composes the two, which is also what lets a test hold an unpaced model.
//
// The wait is held under the mutex, so two callers cannot both pass it at once
// and concurrent calls queue one interval apart rather than arriving together.
// M1's path is sequential and makes no concurrent call, so nothing queues yet;
// the lock is what keeps that true of the milestones that do.
//
// What it costs: an interval per call added to a run's wall clock wherever the
// calls are faster than the interval, and one more number an operator sets with
// nothing to derive it from — the provider publishes no rate this is matched
// against, so it is a floor somebody picks and not a limit anything computed.
type Paced struct {
	// Inner is the model every call is passed to.
	Inner Model
	// Interval is the least time between two call starts. Zero waits never,
	// which is what a test that does not want to wait sets.
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

var _ Model = (*Paced)(nil)

// NewPaced returns inner paced to at most one call per interval.
func NewPaced(inner Model, interval time.Duration) *Paced {
	return &Paced{Inner: inner, Interval: interval}
}

// Complete waits out the rest of the interval since the last call started and
// then calls the inner model. A context cancelled while it waits returns the
// context's error and calls the inner model not at all, so a cancelled run
// sends no further request.
func (p *Paced) Complete(ctx context.Context, as principal.Principal, call Call) (Reply, error) {
	if err := p.wait(ctx); err != nil {
		return Reply{}, err
	}
	return p.Inner.Complete(ctx, as, call)
}

// wait blocks until the interval since the last call start has passed, and
// records this call's start. The first call never waits: pacing is what sits
// between two requests, and there is nothing before the first.
func (p *Paced) wait(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.last.IsZero() {
		if remaining := p.Interval - time.Since(p.last); remaining > 0 {
			timer := time.NewTimer(remaining)
			defer timer.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-timer.C:
			}
		}
	}
	p.last = time.Now()
	return nil
}
