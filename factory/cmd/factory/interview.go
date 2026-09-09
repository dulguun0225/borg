package main

import (
	"context"
	"strings"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/record"
)

// The confirming round as two halves in two passes: the pass states the
// reading in the question it asks, and the requester confirms it at Work
// between passes. What joins them is the question record itself — the reading
// is stated in its text, and nothing else holds it before the round that
// confirms it writes the requirement set.
//
// So the format is a convention over one record's own words, and it is here
// rather than at either end because both ends have to spell it the same way:
// [confirmingQuestion] writes it and [readingIn] reads it, and a search finds
// the pair.

const (
	// confirmingQuestionPrefix and confirmingQuestionSuffix bracket the
	// reading in the confirming round's question, and confirmingSeparator
	// divides one statement from the next.
	confirmingQuestionPrefix = "The factory understood: "
	confirmingQuestionSuffix = " — confirm?"
	confirmingSeparator      = "; "
)

// confirmingQuestion is the confirming round's question: what the factory has
// understood is wanted, in the requester's own terms, and the request to
// confirm it.
func confirmingQuestion(requirements []string) string {
	return confirmingQuestionPrefix + strings.Join(requirements, confirmingSeparator) + confirmingQuestionSuffix
}

// readingIn is the reading a confirming question stated, read back off the
// question record. A question that is not one of these states no reading and
// answers with nothing, which is what a caller confirming a round that is not
// the confirming one gets.
func readingIn(question string) []string {
	rest, is := strings.CutPrefix(question, confirmingQuestionPrefix)
	if !is {
		return nil
	}
	rest = strings.TrimSuffix(rest, confirmingQuestionSuffix)
	if strings.TrimSpace(rest) == "" {
		return nil
	}
	statements := strings.Split(rest, confirmingSeparator)
	for n, one := range statements {
		statements[n] = strings.TrimSpace(one)
	}
	return statements
}

// confirmTheReading writes the confirming round: the requirement set the
// question stated, the intended effect, and the tier, in the one transaction
// that moves the intent to refined.
//
// The write is made as intake and not as the human at the screen. One call
// writes the answer and the requirement records, and the requirement record's
// writer is intake — the design's own — so the actor stays intake and what
// records the requester is the answer on the question row.
func (p *path) confirmTheReading(ctx context.Context, actor record.Actor,
	in intent.Intent, waiting intent.Question) error {
	confirmation := intent.Confirmation{
		IntentID:       in.ID,
		QuestionID:     waiting.ID,
		Answer:         "confirmed by " + actor.Key,
		IntendedEffect: in.Statement,
		Tier:           defaultTier,
	}
	for _, statement := range readingIn(waiting.Question) {
		escapeReason := ""
		if _, matched := criterion.Classify(statement); !matched {
			escapeReason = "not classified by the command-line interface"
		}
		confirmation.Requirements = append(confirmation.Requirements,
			intent.NewRequirement{Statement: statement, EscapeReason: escapeReason})
	}
	if _, err := p.intake.Confirm(ctx, intakeActor, confirmation); err != nil {
		return err
	}
	// Refined is the intent leaving the state that stopped every item
	// decomposed from it, so the holds that state opened are re-matched here,
	// where it was written.
	return p.intentLeftItsStop(ctx)
}

// servicesFor is which services an intent's statement says its decomposition
// yields items on: the prefix `svcA,svcB: ` the terminal's -intent flag takes,
// read back with the same parser, and the first service this install knows
// where the statement carries no readable prefix.
//
// The intent record holds no service, which service an item changes being the
// item's own field. So the statement is where an intent taken in at Work says
// what it changes, and this is the one place that convention is read.
func servicesFor(statement string, known []serviceRepo) ([]string, string, error) {
	var parsed statements
	if err := parsed.setFor(statement, known); err != nil {
		return nil, "", err
	}
	return parsed[0].services, parsed[0].statement, nil
}

// namesItsServices reports whether the statement carries a service prefix this
// install can read, which is what says an intent nobody in this process took in
// still says what its decomposition yields items on.
//
// It is the same parse [servicesFor] makes and reads the result rather than
// repeating it: a statement with no readable prefix parses to the first service
// this install knows, so what says the prefix was there is that the statement
// came back shorter than it went in.
func namesItsServices(statement string, known []serviceRepo) bool {
	_, rest, err := servicesFor(statement, known)
	return err == nil && rest != strings.TrimSpace(statement)
}
