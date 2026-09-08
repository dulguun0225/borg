// The one round that follows production: the factory asks it once every item
// of an intent is live, and a human answers it at this terminal.
package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/screens"
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

// outcomeAtTheClose is the intent's outcome, computed once here and stored on
// the delivery: the acceptance round's verdict on the intended effect for an
// intent somebody requested, and the rate of reports before and after the
// release meant to fix what they describe for one grouped from reports. An
// intent the factory raised carries none and never reaches this.
//
// The rate is computed at the close and never again, because it is a fact
// about that release and not a number that keeps moving. The reports counted
// are the intent's own and those of every intent recurring on it — a report
// matching work already shipped is a new intent linked to the first as a
// recurrence, and it is the same problem still arriving.
func (p *path) outcomeAtTheClose(ctx context.Context, in intent.Intent, verdict string) (string, error) {
	if in.Source != intent.SourceReports {
		return verdict, nil
	}
	if p.d.reports == nil {
		return "", fmt.Errorf("%w: the outcome of an intent grouped from reports is the rate of reports before and after the release, and this composition holds no report store to count them in",
			screens.ErrRefused)
	}
	shippedAt, err := p.shippedAt(ctx, in.ID)
	if err != nil {
		return "", err
	}
	if shippedAt == "" {
		return "", fmt.Errorf("%w: %s carries no release, so there is no release for the report rate to be measured around",
			screens.ErrRefused, in.ID)
	}
	group, err := p.groupAndItsRecurrences(ctx, in)
	if err != nil {
		return "", err
	}
	counted, err := p.d.reports.CountsAround(ctx, group, shippedAt)
	if err != nil {
		return "", err
	}
	return reportRate(counted, shippedAt, time.Now())
}

// shippedAt is when the newest release carrying an item of this intent was
// minted, which is when what the reports describe was fixed. It is the newest
// and not the first: the acceptance round is asked once every item is live, so
// the reports stop arriving — if the fix worked — after the last of them
// shipped.
func (p *path) shippedAt(ctx context.Context, intentID string) (string, error) {
	items, err := item.ForIntent(ctx, p.d.pool, intentID)
	if err != nil {
		return "", err
	}
	newest := ""
	for _, it := range items {
		rel, minted, err := release.ForItem(ctx, p.d.pool, it.ID)
		if err != nil {
			return "", err
		}
		if minted && rel.At > newest {
			newest = rel.At
		}
	}
	return newest, nil
}

// groupAndItsRecurrences is the intent and every intent linked to it as a
// recurrence, which together are the reports of one problem.
func (p *path) groupAndItsRecurrences(ctx context.Context, in intent.Intent) ([]string, error) {
	group := []string{in.ID}
	all, err := intent.InProject(ctx, p.d.pool, in.ProjectID)
	if err != nil {
		return nil, err
	}
	for _, one := range all {
		if one.RecurrenceOf == in.ID {
			group = append(group, one.ID)
		}
	}
	return group, nil
}

// reportRate is the outcome itself, in words: the reports of the group on each
// side of the release, each side divided by its own span — the oldest report
// to the release, and the release to the close. A side with no measurable span
// is reported as its count alone, a rate over no time being no rate.
func reportRate(counted reportstore.BeforeAndAfter, releaseAt string, now time.Time) (string, error) {
	release, err := record.ParseTime(releaseAt)
	if err != nil {
		return "", err
	}
	beforeDays := 0.0
	if counted.OldestAt != "" {
		oldest, err := record.ParseTime(counted.OldestAt)
		if err != nil {
			return "", err
		}
		beforeDays = release.Sub(oldest).Hours() / 24
	}
	return fmt.Sprintf("the report rate: %s before the release, %s after",
		reportsADay(counted.Before, beforeDays),
		reportsADay(counted.After, now.Sub(release).Hours()/24)), nil
}

// reportsADay is one side of that measurement.
func reportsADay(reports int64, days float64) string {
	if days <= 0 {
		return fmt.Sprintf("%d report(s) over no measurable span", reports)
	}
	return fmt.Sprintf("%.2f a day (%d report(s) over %.2f day(s))",
		float64(reports)/days, reports, days)
}
