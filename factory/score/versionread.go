package score

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
)

// The reads over score versions, split from version.go at the length a file
// is held to: [Newest], [Get] and [All], versions/versionsIn beneath them, and
// the comparison [Writer.append]'s composers use to decide whether a version
// says anything the one below it does not.

// versions is every score version in the log, oldest first.
func versions(ctx context.Context, pool *pgxpool.Pool, token lease.Token) ([]Version, error) {
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, componentPrincipal)
	if err != nil {
		return nil, err
	}
	return versionsIn(rows)
}

// versionsIn is every score version among rows already read, oldest first. It is
// separate from [versions] so that a caller reading the log for two shapes at
// once — [InForceAt], which also reads the confirmations off the policy version
// rows — reads it once and appends one read event.
func versionsIn(rows []decisionlog.Row) ([]Version, error) {
	var read []Version
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeScoreVersion {
			continue
		}
		var payload versionPayload
		if err := json.Unmarshal([]byte(row.Payload), &payload); err != nil {
			return nil, fmt.Errorf("score: reading the version in row %s: %w", row.ID, err)
		}
		read = append(read, Version{
			ID: row.ID, Actor: row.Actor, At: row.At,
			FormulaVersion: payload.FormulaVersion, Formula: payload.Formula, Weights: payload.Weights,
			FactorSets: payload.FactorSets, ControlBound: payload.ControlBound,
			Rules: payload.Rules, LearningVersion: payload.LearningVersion,
			BandWidth: payload.BandWidth, ShippedPriors: payload.ShippedPriors,
			Scale: payload.Scale, RecalibratedThrough: payload.RecalibratedThrough,
			PriorRestarts: payload.PriorRestarts,
			Supplied:      payload.Supplied, Bands: payload.Bands, Drift: payload.Drift,
			FalseAlarms: payload.FalseAlarms, Branch: payload.Branch,
			ShippedBundleIdentity: payload.ShippedBundleIdentity, Supersedes: payload.Supersedes,
		})
	}
	return read, nil
}

// Newest is the version in force, and false where none has been appended. The
// order is the log's own: a row that came later in the chain is a later version.
func Newest(ctx context.Context, pool *pgxpool.Pool, token lease.Token) (Version, bool, error) {
	read, err := versions(ctx, pool, token)
	if err != nil || len(read) == 0 {
		return Version{}, false, err
	}
	return read[len(read)-1], true, nil
}

// Get is one version by id, which is what a reader of a decision follows to
// what the score published when it was decided.
func Get(ctx context.Context, pool *pgxpool.Pool, token lease.Token, id string) (Version, error) {
	read, err := versions(ctx, pool, token)
	if err != nil {
		return Version{}, err
	}
	for _, v := range read {
		if v.ID == id {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("%w: %s", ErrNoVersion, id)
}

// All is every version, oldest first. It is what a reader following a supplied
// value's movement walks: each names the one it superseded, so the sequence is
// readable from either end, and what makes a movement readable beside it is
// every decision naming the version it was decided under.
func All(ctx context.Context, pool *pgxpool.Pool, token lease.Token) ([]Version, error) {
	return versions(ctx, pool, token)
}

// differs is whether the version this pass computed says anything the newest
// stored one does not. Nothing refuses two versions that say the same thing where
// they are not adjacent — a learned value that moved and moved back is ordinary —
// so what is compared is this version against the one below it and nothing else.
func differs(newest, next Version) bool {
	if newest.FormulaVersion != next.FormulaVersion || newest.Formula != next.Formula ||
		newest.FactorSets != next.FactorSets || newest.Rules != next.Rules ||
		newest.LearningVersion != next.LearningVersion ||
		newest.ControlBoundOrShipped() != next.ControlBound ||
		newest.BandWidthOrShipped() != next.BandWidthOrShipped() ||
		newest.RecalibratedThrough != next.RecalibratedThrough {
		return true
	}
	return !sameJSON(newest.Supplied, next.Supplied) || !sameJSON(newest.Bands, next.Bands) ||
		!sameJSON(newest.Drift, next.Drift) || !sameJSON(newest.FalseAlarms, next.FalseAlarms) ||
		!sameJSON(newest.ShippedPriors, next.ShippedPriors) || !sameJSON(newest.Scale, next.Scale) ||
		!sameJSON(newest.PriorRestarts, next.PriorRestarts)
}

func sameJSON(a, b any) bool {
	left, err := json.Marshal(a)
	if err != nil {
		return false
	}
	right, err := json.Marshal(b)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}
