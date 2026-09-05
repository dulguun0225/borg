package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
)

// stageAttempts is one stage's limit, its remaining attempts, and what each of
// them spent. The limit is per stage and not per call, which is what the design
// compares it against: a stage that asks the model twice — the interview's
// question and then the spec — has its attempts across both calls and not that
// many of each. The spends are kept because a refused attempt cost tokens and
// dispatch is told about every one of them, so the item's stored count is the
// count the limit was applied to.
//
// The limit is read through package policy, so it is what an owner authored, or
// what the score supplies where they authored nothing, clamped by any
// safeguard.
type stageAttempts struct {
	limit  int
	left   int
	spends []int64
}

// limitFor is one stage's attempt limit as it is in force. The read happens once
// per stage rather than once per attempt: an owner re-authoring the limit while a
// stage is retrying would otherwise change the number the stage is being held to
// half way through it.
func limitFor(ctx context.Context, reader *policy.Reader, stage item.Stage, s policy.Subjects) (*stageAttempts, error) {
	s.Stage = stage
	effective, err := reader.AttemptLimit(ctx, s)
	if err != nil {
		return nil, err
	}
	limit := int(effective.Number)
	if limit < 1 {
		return nil, fmt.Errorf("factory: the attempt limit in force at %s is %v, and a stage gets at least one attempt",
			stage, effective.Number)
	}
	return &stageAttempts{limit: limit, left: limit}, nil
}

// ErrOutOfAttempts is what a stage that spent its limit returns. It is a sentinel
// because the escalation page turns on it: the factory giving up on an item is what
// the design shows in Work as an escalation, and whether it also pages depends on the
// intent behind the item, which the caller reads and this generic function cannot.
var ErrOutOfAttempts = errors.New("factory: the stage used every attempt its limit allows")

// attempt runs one authoring call, retrying while the stage has attempts left
// and returning what the call produced as soon as a reply parses.
//
// What is retried is a reply the protocol refused and an answer the client could
// not read — both are the model failing to say the thing, which another sample
// may say correctly. Nothing else is: a rate-limited or unauthorised account is
// not an attempt at the work, and what the design does with an account that has
// run out is a hold — ../../../end-goal/how-the-factory-works/10-fleet/05-an-account-that-runs-out-is-a-hold.md
// — so those return on the first failure rather than spending the limit on a
// refusal that will not change. There is no wait between attempts, which costs
// nothing on a refused reply and would be the wrong shape for a rate limit
// anyway.
//
// A stage out of attempts is the factory saying it cannot do this one, which the
// design shows in Work as an escalation. There is no screen to show it on yet, so
// the run stops and the message says so, the human being at the terminal already.
func attempt[T any](out io.Writer, a *stageAttempts, role string, call func() (T, int64, error)) (T, error) {
	var zero T
	var last error
	for a.left > 0 {
		result, spend, err := call()
		a.left--
		a.spends = append(a.spends, spend)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, agent.ErrReply) && !errors.Is(err, agent.ErrAnswer) {
			return zero, err
		}
		last = err
		fmt.Fprintf(out, "The %s's reply was refused; %d attempt(s) left: %v\n", role, a.left, err)
	}
	return zero, fmt.Errorf("%w: the %s used all %d without a reply the protocol accepts, and the factory is stuck on this item: %w",
		ErrOutOfAttempts, role, a.limit, last)
}
