package main

import (
	"context"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screenstatemachine"
)

// What a spec version under decision removes, which the score reads at the Spec
// row: a criterion whose provenance names an authority withdrawn, and a
// superseding screen state machine declaring a transition a human-confirmed
// machine did not. Each is a query over records the score does not read, so the
// composition performs them and hands over what it read.
//
// Human-confirmed is no field of either table: it is a query over the
// introducing spec version's decision, which is the decision log's fact. Both
// reads take the versions a human decided as an argument for that reason, and
// this is where the log is walked for them.

// withdrawals is that reader.
type withdrawals struct {
	pool  *pgxpool.Pool
	token lease.Token
}

// ProtectionRemovedBy is what the version under decision removes, and nothing
// for a version that removes none.
func (w withdrawals) ProtectionRemovedBy(ctx context.Context, artifactID string) ([]score.ProtectionRemoved, error) {
	if artifactID == "" {
		return nil, nil
	}
	// The walk of the log bounds the screen half alone. Human-confirmed is the
	// only one of the three provenances that is a decision rather than a column,
	// and a supersession removes protection only over a human-confirmed machine;
	// the constraint-derived and hazard-derived fields are the criterion's own
	// row, so an install where the score auto-passes every Spec row — no spec
	// version human-approved anywhere — still resolves their withdrawal.
	confirmed, decidedBy, err := humanConfirmedSpecVersions(ctx, w.pool, w.token)
	if err != nil {
		return nil, err
	}
	holders, err := people.Holders(ctx, w.pool, people.OfDuty(gate.DutyConfirmTheCriteria))
	if err != nil {
		return nil, err
	}

	var removed []score.ProtectionRemoved
	withdrawn, err := criterion.WithdrawalsWithAnAuthority(ctx, w.pool, artifactID, confirmed)
	if err != nil {
		return nil, err
	}
	for _, one := range withdrawn {
		for _, source := range one.Provenances {
			provenance, routes := provenanceOf(source)
			if provenance == "" {
				continue
			}
			entry := score.ProtectionRemoved{
				What: score.RemovedCriterion, SubjectID: one.Criterion.ID, Provenance: provenance,
			}
			if routes {
				entry.RoutedTo = stillHolding(decidedBy[one.Criterion.SpecArtifactID], holders)
			}
			removed = append(removed, entry)
		}
	}

	revisions, err := screenstatemachine.SupersessionsRemovingProtection(ctx, w.pool, artifactID, confirmed)
	if err != nil {
		return nil, err
	}
	for _, revision := range revisions {
		removed = append(removed, score.ProtectionRemoved{
			What:       score.RemovedScreenTransition,
			SubjectID:  revision.Superseded.ID,
			Provenance: score.ProvenanceHumanConfirmed,
			RoutedTo:   stillHolding(decidedBy[revision.Superseded.SpecArtifactID], holders),
		})
	}
	return removed, nil
}

// provenanceOf is the criterion table's provenance in the score's own words, and
// whether that provenance names a human this composition can resolve. Only the
// human-confirmed one does: the actor of the introducing decision is a row of
// the log, where the human a constraint-derived or a hazard-derived criterion
// routes to is whoever holds a duty over the constraint or the area, which is a
// narrowing the People declaration does not carry. Both route to the row's own
// duty instead, which is where an unresolved routing already goes.
func provenanceOf(source criterion.Provenance) (string, bool) {
	switch source {
	case criterion.ProvenanceHumanConfirmed:
		return score.ProvenanceHumanConfirmed, true
	case criterion.ProvenanceConstraintDerived:
		return score.ProvenanceConstraintDerived, false
	case criterion.ProvenanceHazardDerived:
		return score.ProvenanceHazardDerived, false
	default:
		return "", false
	}
}

// stillHolding is the human the provenance names where that human still holds
// the row's duty, and nobody otherwise — which routes the row to another holder
// of that duty, the way the design says a withdrawal of a human-confirmed
// criterion routes when its decider no longer holds it.
func stillHolding(decider string, holders []string) string {
	if decider == "" || !slices.Contains(holders, decider) {
		return ""
	}
	return decider
}

// humanConfirmedSpecVersions is every spec version a human decided, and who
// decided each. A version approved by the factory is not one: what the design
// protects is a criterion a human confirmed, and the gate component approving on
// the number confirmed nothing.
func humanConfirmedSpecVersions(ctx context.Context, pool *pgxpool.Pool,
	token lease.Token) ([]string, map[string]string, error) {

	rows, err := decisionlog.NewReader(pool, token).
		Read(ctx, gate.ComponentPrincipal(gate.Spec))
	if err != nil {
		return nil, nil, err
	}
	atSpec := map[string]string{}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision || row.Part != decisionlog.PartOpen {
			continue
		}
		var opening gate.OpeningPayload
		if json.Unmarshal([]byte(row.Payload), &opening) != nil {
			continue
		}
		if opening.Gate != gate.Spec.String() || opening.ArtifactID == "" {
			continue
		}
		atSpec[row.ID] = opening.ArtifactID
	}

	var confirmed []string
	decidedBy := map[string]string{}
	for _, row := range rows {
		if row.Part != decisionlog.PartClose || row.Verdict != string(gate.VerdictApprove) ||
			row.Actor.Kind != record.KindHuman {
			continue
		}
		version, closed := atSpec[row.Closes]
		if !closed {
			continue
		}
		if !slices.Contains(confirmed, version) {
			confirmed = append(confirmed, version)
		}
		decidedBy[version] = row.Actor.Key
	}
	return confirmed, decidedBy, nil
}
