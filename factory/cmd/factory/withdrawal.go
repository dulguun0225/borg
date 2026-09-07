package main

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
)

// The four rows outside every item that a human at this terminal closes: a
// safeguard's withdrawal, a halt's withdrawal, a legal hold's withdrawal, and
// the shortening of decision-log retention. Each fires like any other row, reads
// no threshold and no factor set, and waits on a human always.
//
// Each of the four is two appends and then one write: the gate fires the row,
// the human closes it, and the record's own approval is made from that close and
// names it. Nothing here approves a record without firing the row first — a
// record that removed a protection with no decision on it is the mechanism those
// rows exist to refuse — and package policy refuses an approval naming no close
// event, so the order cannot be taken the other way round.
//
// A withdrawal's row is routed away from the human who wrote the withdrawal: the
// actor on that record is [gate.RoutedTo.NotHuman], and the safeguard's own
// routing field gives the duty or the named human its own withdrawal waits on. A
// shortening of decision-log retention routes away the same way: the shorter
// value is written pending as a record of its own before the row fires, and the
// actor on that record is who the row is routed away from.
//
// Where nobody else can decide the row the close carries the self-approval
// field rather than being refused, which is what every one of these rows does
// on an install with one owner: none of the four names a duty, so a row nobody
// holds widens to the owner.
//
// Each row names the record it decides — the safeguard, the halt, the legal
// hold, and the factory-wide settings record whose retention a shortening moves
// — because that is the subject one such row is pending per: a withdrawal of one
// safeguard does not stop a second safeguard's being decided beside it.

// priorsRestartedBy is every author the row that decides a shortening names:
// those whose per-author prior stands drifted and whose held-out decisions the
// cut a value of this length permits would remove. The prior the score learned
// from them restarts when those decisions go, and the human at the row is told
// whose before they approve it.
//
// The two halves are read from two places, which is why the composition
// computes it: which priors stand drifted is a field of the score version in
// force, and which decisions the cut removes is a walk of the log's own rows —
// every open event older than the value reaches back to that the score held
// out, resolved to an author through the version it was decided over.
func priorsRestartedBy(ctx context.Context, pool *pgxpool.Pool, token lease.Token,
	inForce score.Version, actor record.Actor, seconds int64) ([]string, error) {
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, asPrincipal(actor))
	if err != nil {
		return nil, err
	}
	// Every timestamp is fixed width and always UTC, so comparing two as text
	// is comparing them as times — the same comparison the truncation makes
	// against the boundary it is given.
	reachesBackTo := record.FormatTime(time.Now().Add(-time.Duration(seconds) * time.Second))

	var restarted []string
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen || row.At > reachesBackTo {
			continue
		}
		var opening gate.OpeningPayload
		if json.Unmarshal([]byte(row.Payload), &opening) != nil {
			continue
		}
		if !opening.HeldOut || opening.ArtifactID == "" {
			continue
		}
		decided, err := artifact.Get(ctx, pool, opening.ArtifactID)
		if err != nil {
			return nil, err
		}
		if decided.Author == "" || !inForce.PriorDrifted(decided.Author) ||
			slices.Contains(restarted, decided.Author) {
			continue
		}
		restarted = append(restarted, decided.Author)
	}
	return restarted, nil
}

// safeguardWithdrawalRouting is who the row that decides one safeguard's
// withdrawal waits on: the duty or the named human the safeguard's own routing
// field gives, and the owner where it names neither — and never the actor on the
// withdrawal. It answers with the safeguard beside it, which is the record that
// row decides and what its open event names.
func safeguardWithdrawalRouting(ctx context.Context, pool *pgxpool.Pool, withdrawalID string) (gate.RoutedTo, string, error) {
	written, err := safeguard.GetWithdrawal(ctx, pool, withdrawalID)
	if err != nil {
		return gate.RoutedTo{}, "", err
	}
	placed, err := safeguard.All(ctx, pool)
	if err != nil {
		return gate.RoutedTo{}, "", err
	}
	routed := gate.RoutedTo{NotHuman: written.Actor.Key}
	for _, one := range placed {
		if one.ID != written.SafeguardID {
			continue
		}
		routed.Duty = people.Duty(one.Routing.Duty)
		routed.Human = one.Routing.HumanKey
		return routed, one.ID, nil
	}
	return gate.RoutedTo{}, "", fmt.Errorf("%w: %s", safeguard.ErrNotFound, written.SafeguardID)
}

// rowGate is the gate a row outside every item is fired through, and the score
// version in force beside it: the log, that version, and the policy read
// against it. It composes none of
// what a row on an item's path reads — no holds, no drift detector's store, no
// reader of an intent's state — because none of the four rows here reads one:
// each decides a record, names no item, and reads no threshold.
//
// The score version is ensured rather than read, the way a run's own
// composition ensures it: every decision names the version in force at its
// firing, and a factory that has never run a path has none to name.
func rowGate(ctx context.Context, pool *pgxpool.Pool, token lease.Token) (*gate.Gate, score.Version, error) {
	version, err := score.NewWriter(pool, token, marksOf(pool)).Ensure(ctx, scoreActor)
	if err != nil {
		return nil, score.Version{}, err
	}
	return gate.New(gate.Composition{
		Pool:  pool,
		Token: token,
		Log:   decisionlog.NewWriter(pool, token),
		Score: score.New(score.Composition{
			Pool: pool, Version: version, Draw: score.NeverDraw{},
			Marks: marksOf(pool), Token: token,
		}),
		Policy: policy.NewReader(pool, token, version),
	}), version, nil
}
