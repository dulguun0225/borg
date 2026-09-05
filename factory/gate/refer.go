package gate

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

// The fourth verdict. A human put at a row may be unable to judge what they were
// shown, and refer is the verdict that says so: not a fault found, which is a
// reject, and not a stop on the event, which is a hold, but a reading the
// instrument could not take.

// ErrNobodyLeftToReferTo is returned by [Gate.Refer] where the row already waits
// on the owner, every holder of its duty having referred it. What the human has
// left is a reject whose reason says what they could not read: a diff too large
// to read is a fault of the artifact and not of the reader.
var ErrNobodyLeftToReferTo = errors.New("gate: nobody is left to refer this row to")

// Referred is what a refer leaves: the close event that gave the verdict, and
// the row re-fired to the holders who have not referred it — or to the owner,
// where the last holder just did.
type Referred struct {
	Close    decisionlog.Row
	Reopened Opened
}

// Refer gives the refer verdict. It appends the close event and then re-fires
// the row, naming the row it was referred from and carrying forward everyone who
// has referred it, so a later refer is refused without a walk back through the
// chain.
//
// A refer counts no attempt, sends nothing back, and teaches the score nothing:
// the pair Factory reports counts approvals and their undos, and a refer is
// neither a gate the factory needed nor a human agreeing.
//
// The firing it re-fires with is the caller's, because the vector is computed
// again over what is now under decision; what this call sets on it is the row it
// was referred from and the referrers, which a caller cannot set.
func (g *Gate) Refer(ctx context.Context, opened Opened, actor record.Actor, reason string, again Firing) (Referred, error) {
	if err := permits(opened.Gate, VerdictRefer); err != nil {
		return Referred{}, err
	}
	if reason == "" {
		return Referred{}, fmt.Errorf("%w: the refer of %s carries none", ErrReasonMissing, opened.Row.ID)
	}
	refusals, err := g.refusalsFor(ctx, opened, actor)
	if err != nil {
		return Referred{}, err
	}
	if err := refusals.refuse(VerdictRefer); err != nil {
		return Referred{}, err
	}

	closed, err := g.close(ctx, opened, actor, "", ClosingPayload{
		CloseEvent: score.CloseEvent{Verdict: string(VerdictRefer)},
		Reason:     reason,
	})
	if err != nil {
		return Referred{}, err
	}

	again.Row = opened.Gate
	again.referredFrom = opened.Row.ID
	again.referrers = append(append([]string{}, refusals.referrers...), refusals.actor)
	reopened, err := g.Fire(ctx, again)
	if err != nil {
		return Referred{Close: closed}, err
	}
	return Referred{Close: closed, Reopened: reopened}, nil
}

// withoutReferrers is who a re-fired row waits on: the holders of its duty who
// have not referred it. Where the last of them just did, the list is empty and
// the row widens to the owner, which is where every unheld row goes.
func withoutReferrers(holders, referrers []string) []string {
	if len(referrers) == 0 {
		return holders
	}
	var left []string
	for _, holder := range holders {
		if !slices.Contains(referrers, holder) {
			left = append(left, holder)
		}
	}
	return left
}
