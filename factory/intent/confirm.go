package intent

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// Confirmation is the confirming round: the round fixed in form that ends the
// interview. The factory states, in the requester's own terms, what it has
// understood is wanted; the requester's answer confirms that reading, and this
// is the call that records it.
//
// An intent the factory raised has no requester and takes no such round: it
// enumerates its requirements from the evidence instead, so QuestionID,
// Answer, IntendedEffect and Tier are all refused on one and Requirements is
// all it carries.
type Confirmation struct {
	IntentID string
	// QuestionID is the confirming round's own question record, answered with
	// Answer in this call. Both are empty on an intent the factory raised.
	QuestionID string
	Answer     string
	// IntendedEffect is who the change is for and what they should be able to
	// do, or stop having to do, once it ships. It is not a requirement: no
	// item answers it and no criterion names it.
	IntendedEffect string
	// Tier is the ordinal the requester proposes, with the policy version it
	// is in force under.
	Tier Tier
	// Requirements is the reading, one statement each. At a reopened
	// interview's confirming round a statement that restates an earlier one
	// unchanged keeps that record and its id, and every other requirement of
	// the earlier reading is superseded in this same call, pointing at the
	// statements that name it in Supersedes.
	Requirements []NewRequirement
}

// Confirm records the confirming round: it writes the requirement set, the
// intended effect and the tier in one transaction and moves the intent to
// refined. It returns the reading in force after the call, in the order the
// statements were given.
//
// A corrected round writes no requirements — [Intake.Correct] is that round —
// because the factory states a new reading and the round that confirms that
// one writes it.
func (i *Intake) Confirm(ctx context.Context, actor record.Actor, confirmation Confirmation) ([]Requirement, error) {
	if err := actor.Validate(); err != nil {
		return nil, err
	}
	if len(confirmation.Requirements) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrRequirementsEmpty, confirmation.IntentID)
	}
	patterns := make([]Pattern, len(confirmation.Requirements))
	for n, r := range confirmation.Requirements {
		pattern, err := patternOf(r.Statement, r.EscapeReason)
		if err != nil {
			return nil, err
		}
		patterns[n] = pattern
	}

	var reading []Requirement
	err := i.write(ctx, confirmation.IntentID, "confirming the reading of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateUnrefined {
			return fmt.Errorf("%w: %s is %s", ErrNotUnrefined, in.ID, in.State)
		}
		kind, err := confirmationOwed(in, confirmation)
		if err != nil {
			return err
		}
		if confirmation.QuestionID != "" {
			answered, err := answerQuestion(ctx, tx, confirmation.QuestionID, confirmation.Answer)
			if err != nil {
				return err
			}
			if answered.IntentID != in.ID {
				return fmt.Errorf("%w: %s is of %s", ErrQuestionElsewhere, answered.ID, answered.IntentID)
			}
		}
		reading, err = writeRequirements(ctx, tx, actor, in, kind, confirmation.Requirements, patterns)
		if err != nil {
			return err
		}
		if !confirmation.Tier.Written() {
			// A detector's intent arrived with its tier and is refused one
			// here, so this round has none to write and leaves the arrival's
			// where it is.
			_, err = tx.Exec(ctx, `update `+Table+`
				set state = $1, intended_effect = $2 where id = $3`,
				string(StateRefined), confirmation.IntendedEffect, in.ID)
			return err
		}
		_, err = tx.Exec(ctx, `update `+Table+`
			set state = $1, intended_effect = $2, tier = $3, tier_policy_version = $4 where id = $5`,
			string(StateRefined), confirmation.IntendedEffect,
			confirmation.Tier.Value, confirmation.Tier.PolicyVersion, in.ID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return reading, nil
}

// confirmationOwed is what the intent's source owes at this round, and the
// kind the requirements it writes take. An intent somebody asked for owes a
// confirming question, an answer, an intended effect and a tier; one the
// factory raised owes none of the four and is refused each.
func confirmationOwed(in Intent, confirmation Confirmation) (Kind, error) {
	if in.Source == SourceDetector {
		for _, unwanted := range []struct{ what, value string }{
			{"a confirming question", confirmation.QuestionID},
			{"an answer", confirmation.Answer},
			{"an intended effect", confirmation.IntendedEffect},
		} {
			if unwanted.value != "" {
				return "", fmt.Errorf("%w: %s takes no %s", ErrNoRequester, in.ID, unwanted.what)
			}
		}
		if confirmation.Tier.Written() {
			return "", fmt.Errorf("%w: %s takes its tier from what raised it", ErrNoRequester, in.ID)
		}
		return KindEnumeratedFromEvidence, nil
	}
	for _, owed := range []struct{ what, value string }{
		{"a confirming question", confirmation.QuestionID},
		{"an answer", confirmation.Answer},
	} {
		if owed.value == "" {
			return "", fmt.Errorf("%w: %s owes %s", ErrRequesterOwed, in.ID, owed.what)
		}
	}
	if confirmation.IntendedEffect == "" {
		return "", fmt.Errorf("%w: %s", ErrIntendedEffectEmpty, in.ID)
	}
	if (confirmation.Tier.Value == 0) != (confirmation.Tier.PolicyVersion == "") {
		return "", fmt.Errorf("%w: %+v", ErrTierIncomplete, confirmation.Tier)
	}
	if !confirmation.Tier.Written() {
		return "", fmt.Errorf("%w: %s owes a tier", ErrRequesterOwed, in.ID)
	}
	return KindConfirmed, nil
}

// writeRequirements writes one reading against the one before it. A statement
// the new reading restates unchanged keeps its record and its id; every other
// requirement of the earlier reading is superseded here, pointing at the
// statements that named it, and empty where the requester retracted it.
//
// The sweep reads the reading and not the shares: a derived requirement is
// superseded by the re-decomposition with the item that carried it, which
// doc.go states.
func writeRequirements(ctx context.Context, tx pgx.Tx, actor record.Actor, in Intent, kind Kind,
	statements []NewRequirement, patterns []Pattern,
) ([]Requirement, error) {
	earlier, err := requirementsInForce(ctx, tx, in.ID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]Requirement, len(earlier))
	byStatement := make(map[string]Requirement, len(earlier))
	for _, r := range earlier {
		byID[r.ID] = r
		if r.Kind != KindDerived {
			byStatement[r.Statement] = r
		}
	}

	restated := make(map[string]bool, len(earlier))
	supersededBy := make(map[string][]string, len(earlier))
	reading := make([]Requirement, 0, len(statements))
	for n, s := range statements {
		for _, id := range s.Supersedes {
			if _, ok := byID[id]; !ok {
				return nil, fmt.Errorf("%w: %s", ErrSupersedesUnknown, id)
			}
		}
		kept, unchanged := byStatement[s.Statement]
		if unchanged {
			restated[kept.ID] = true
			reading = append(reading, kept)
		} else {
			written := Requirement{
				ID:           record.NewID(RequirementIDPrefix),
				Actor:        actor,
				At:           record.Now(),
				IntentID:     in.ID,
				Statement:    s.Statement,
				Pattern:      patterns[n],
				EscapeReason: s.EscapeReason,
				Kind:         kind,
			}
			if err := insertRequirement(ctx, tx, written); err != nil {
				return nil, err
			}
			kept = written
			reading = append(reading, written)
		}
		for _, id := range s.Supersedes {
			supersededBy[id] = append(supersededBy[id], kept.ID)
		}
	}

	at := record.Now()
	for _, r := range earlier {
		if restated[r.ID] || r.Kind == KindDerived {
			continue
		}
		by, err := encodeSuperseded(supersededBy[r.ID])
		if err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `update `+RequirementTable+`
			set superseded_at = $1, superseded_by = $2 where id = $3`, at, by, r.ID); err != nil {
			return nil, fmt.Errorf("intent: superseding requirement %s: %w", r.ID, err)
		}
	}
	return reading, nil
}

// Correction is a correction at the confirming round: the requester's answer
// corrects the reading, the correction attaches like any other answer, and the
// factory states a new reading at a new round.
type Correction struct {
	IntentID string
	// QuestionID is the confirming round's question, answered with Correction.
	QuestionID string
	Correction string
	// Question is the new reading, asked at the round this call opens.
	Question string
}

// Correct records a correction at the confirming round: it attaches the
// correction to the question that was asked and asks again at a round of its
// own, the correction counting a round like every round. The intent stays
// unrefined and no requirement is written, because what writes the set is the
// round that confirms a reading.
func (i *Intake) Correct(ctx context.Context, actor record.Actor, correction Correction) (Question, error) {
	if err := actor.Validate(); err != nil {
		return Question{}, err
	}
	if correction.Correction == "" {
		return Question{}, ErrAnswerEmpty
	}
	if correction.Question == "" {
		return Question{}, ErrQuestionEmpty
	}
	var asked Question
	err := i.write(ctx, correction.IntentID, "correcting the reading of", func(ctx context.Context, tx pgx.Tx, in Intent) error {
		if in.State != StateUnrefined {
			return fmt.Errorf("%w: %s is %s", ErrNotUnrefined, in.ID, in.State)
		}
		if in.Source == SourceDetector {
			return fmt.Errorf("%w: %s takes no confirming round", ErrNoRequester, in.ID)
		}
		answered, err := answerQuestion(ctx, tx, correction.QuestionID, correction.Correction)
		if err != nil {
			return err
		}
		if answered.IntentID != in.ID {
			return fmt.Errorf("%w: %s is of %s", ErrQuestionElsewhere, answered.ID, answered.IntentID)
		}
		round := in.Rounds + 1
		if _, err := tx.Exec(ctx, `update `+Table+` set rounds = $1 where id = $2`, round, in.ID); err != nil {
			return err
		}
		asked, err = insertQuestion(ctx, tx, actor, in.ID, round, correction.Question)
		return err
	})
	if err != nil {
		return Question{}, err
	}
	return asked, nil
}
