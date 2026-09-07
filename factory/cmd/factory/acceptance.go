// The one round that follows production: the factory asks it once every item
// of an intent is live, and a human answers it at this terminal.
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/release"
)

// acceptanceRounds asks the acceptance round of every intent this run took as
// far as production. It runs after the layers, because what makes an intent
// ready for it is its last item going live and the layers are where that
// happens.
//
// Three intents take nothing here and each for its own reason: one whose items
// are not all live has not reached the round at all, and what it is instead —
// partly delivered, or still moving — is read off the items and reported;
// one the factory raised has no requester, so it is delivered when its last
// item goes live and nobody is asked whether the evidence was misread; and one
// already dropped, delivered, escalated or sent back is not in the state the
// round is asked from.
// It reports whether it wrote anything — an intent delivered, or a round
// asked — which is what the process's own pass announces on: an intent whose
// items are not all live reaches no round and moves nothing.
func (p *path) acceptanceRounds(ctx context.Context, sets []*decompositionSet) (bool, error) {
	moved := false
	for _, set := range sets {
		in, err := intent.Get(ctx, p.d.pool, set.intentID)
		if err != nil {
			return moved, err
		}
		if in.State != intent.StateRefined {
			continue
		}
		live, of, err := p.liveItems(ctx, in.ID)
		if err != nil {
			return moved, err
		}
		if of == 0 || len(live) != of {
			partly, err := item.PartlyDelivered(ctx, p.d.pool, in.ID, live)
			if err != nil {
				return moved, err
			}
			if partly {
				fmt.Fprintf(p.d.out, "Intent %s is partly delivered: %d of %d item(s) live and the rest stopped\n",
					in.ID, len(live), of)
				fmt.Fprintln(p.d.out, "  it reaches no acceptance round, so what shipped of it is validated by nobody")
			}
			continue
		}

		if in.Source == intent.SourceDetector {
			// Nobody can say the evidence was misread, so the intent is
			// delivered when its last item goes live and carries no question,
			// no answer and no outcome.
			if err := p.intake.Delivered(ctx, intakeActor, intent.Delivery{IntentID: in.ID}); err != nil {
				return moved, err
			}
			moved = true
			fmt.Fprintf(p.d.out, "Intent %s is delivered: the factory raised it, so it takes no acceptance round\n", in.ID)
			continue
		}

		question, err := p.acceptanceQuestion(ctx, in)
		if err != nil {
			return moved, err
		}
		asked, err := p.intake.AcceptanceRound(ctx, intakeActor, in.ID, question)
		if err != nil {
			return moved, err
		}
		moved = true
		fmt.Fprintf(p.d.out, "Acceptance round %s asked of intent %s, delivered by mail and chat and never a page\n",
			asked.ID, in.ID)
		fmt.Fprintf(p.d.out, "  %s\n", question)
		fmt.Fprintln(p.d.out, "  it waits on the requester, unbounded and spending nothing; the requester confirms it at Work")
	}
	return moved, nil
}

// acceptanceQuestion is what the round asks: what was asked for, the intended
// effect the requester confirmed, what shipped, and the releases that carry it,
// and whether the effect was had.
func (p *path) acceptanceQuestion(ctx context.Context, in intent.Intent) (string, error) {
	items, err := item.ForIntent(ctx, p.d.pool, in.ID)
	if err != nil {
		return "", err
	}
	shipped := make([]string, 0, len(items))
	for _, it := range items {
		rel, minted, err := release.ForItem(ctx, p.d.pool, it.ID)
		if err != nil {
			return "", err
		}
		if minted {
			shipped = append(shipped, fmt.Sprintf("item %s as release %s", it.ID, rel.ID))
		}
	}
	return fmt.Sprintf("You asked for: %s. The effect confirmed was: %s. What shipped: %s. Did the effect happen?",
		in.Statement, in.IntendedEffect, strings.Join(shipped, ", ")), nil
}

// liveItems is the ids of the intent's items that are live and how many items
// it has. An item is live where a production deploy record names its release
// and is complete, which is the reading every other reader of what shipped
// makes.
func (p *path) liveItems(ctx context.Context, intentID string) ([]string, int, error) {
	items, err := item.ForIntent(ctx, p.d.pool, intentID)
	if err != nil {
		return nil, 0, err
	}
	live := make([]string, 0, len(items))
	for _, it := range items {
		rel, minted, err := release.ForItem(ctx, p.d.pool, it.ID)
		if err != nil {
			return nil, 0, err
		}
		if !minted {
			continue
		}
		deploys, err := deploy.ByRelease(ctx, p.d.pool, p.production.ID, rel.ID)
		if err != nil {
			return nil, 0, err
		}
		for _, one := range deploys {
			if one.Status == deploy.StatusComplete {
				live = append(live, it.ID)
				break
			}
		}
	}
	return live, len(items), nil
}

// outstandingRound is the acceptance round waiting to be answered: the intent's
// newest question with no answer on it. An intent with none has not been asked,
// which is what an intent whose items are not all live looks like here.
func outstandingRound(ctx context.Context, pool *pgxpool.Pool, intentID string) (intent.Question, error) {
	questions, err := intent.Questions(ctx, pool, intentID)
	if err != nil {
		return intent.Question{}, err
	}
	for n := len(questions) - 1; n >= 0; n-- {
		if !questions[n].Answered() {
			return questions[n], nil
		}
	}
	return intent.Question{}, fmt.Errorf(
		"factory: intent %s has no round waiting on an answer; the acceptance round is asked once every item of it is live", intentID)
}
