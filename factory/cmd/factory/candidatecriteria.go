package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// decideCriteria runs the encodings on the candidate environment and records
// each criterion's result against this build, twice over the same composition.
func (p *path) decideCriteria(ctx context.Context, c *candidate, buildID string,
	inForce []criterion.Criterion) ([]gate.CriterionResult, error) {
	if err := p.checkEncodings(ctx, c, c.svc.Repository, c.svc.ID, []string{c.itemID}, inForce); err != nil {
		return nil, err
	}
	composition, err := json.Marshal(c.composition)
	if err != nil {
		return nil, fmt.Errorf("factory: marshalling the composition for the criterion run: %w", err)
	}
	nextRun, err := nextCriterionRun(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}

	first, firstOutput := runEncodings(c.svc.Repository)
	firstOutcome := criterion.OutcomeFailed
	if first {
		firstOutcome = criterion.OutcomePassed
	}
	if configurationHasUnsetValue(c.configuration) {
		firstOutcome = criterion.OutcomeUndecided
	}
	if err := p.recordCriterionRun(ctx, buildID, nextRun, string(composition), inForce, firstOutcome); err != nil {
		return nil, err
	}
	second, secondOutput := runEncodings(c.svc.Repository)
	secondOutcome := criterion.OutcomeFailed
	if second {
		secondOutcome = criterion.OutcomePassed
	}
	if configurationHasUnsetValue(c.configuration) {
		secondOutcome = criterion.OutcomeUndecided
	}
	if err := p.recordCriterionRun(ctx, buildID, nextRun+1, string(composition), inForce, secondOutcome); err != nil {
		return nil, err
	}
	switch {
	case first && second:
		fmt.Fprintln(p.d.out, "The encodings ran twice on the candidate environment and passed both times")
	case !first && !second:
		fmt.Fprintf(p.d.out, "The encodings ran twice on the candidate environment and failed both times:\n%s\n", firstOutput)
	default:
		fmt.Fprintf(p.d.out, "The encodings disagreed between two runs, so every criterion is undecided for build %s:\n%s\n%s\n",
			buildID, firstOutput, secondOutput)
	}

	undecided, err := criterion.Undecided(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	isUndecided := make(map[string]bool, len(undecided))
	for _, id := range undecided {
		isUndecided[id] = true
	}
	latest, err := criterion.Latest(ctx, p.d.pool, buildID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]criterion.Outcome, len(latest))
	for _, r := range latest {
		byID[r.CriterionID] = r.Outcome
	}
	results := make([]gate.CriterionResult, 0, len(inForce))
	for _, cr := range inForce {
		outcome := byID[cr.ID]
		if isUndecided[cr.ID] {
			outcome = criterion.OutcomeUndecided
		}
		results = append(results, gate.CriterionResult{
			CriterionID: cr.ID, Outcome: outcome, Place: criterion.PlaceCandidateEnvironment,
		})
	}
	if err := p.markUnreliable(ctx, c, buildID, results); err != nil {
		return nil, err
	}
	c.securityPredicates = p.decideSecurityPredicates(c)
	mutation, err := p.runCandidateMutations(ctx, c, buildID)
	if err != nil {
		return nil, err
	}
	c.mutation = mutation
	if mutation.Derived() {
		fmt.Fprintf(p.d.out, "The candidate mutation run tested %d mutant(s), detected %d, score %.2f\n",
			mutation.MutantsTested, mutation.MutantsDetected, mutation.Score())
	} else {
		fmt.Fprintf(p.d.out, "The candidate mutation run could not derive a score: %s\n", mutation.CouldNotDerive)
	}
	return results, nil
}

func nextCriterionRun(ctx context.Context, pool *pgxpool.Pool, buildID string) (int, error) {
	results, err := criterion.ResultsForBuild(ctx, pool, buildID)
	if err != nil {
		return 0, err
	}
	highest := 0
	for _, r := range results {
		if r.Run > highest {
			highest = r.Run
		}
	}
	return highest + 1, nil
}

func (p *path) recordCriterionRun(ctx context.Context, buildID string, run int, composition string,
	inForce []criterion.Criterion, outcome criterion.Outcome) error {
	outcomes := make(map[string]criterion.Outcome, len(inForce))
	for _, cr := range inForce {
		outcomes[cr.ID] = outcome
	}
	return criterion.RecordResults(ctx, p.d.pool, p.d.token, deployActor,
		criterion.Run{BuildID: buildID, Number: run, Place: criterion.PlaceCandidateEnvironment, Composition: composition},
		outcomes)
}

func configurationHasUnsetValue(values targetseam.ValueSet) bool {
	for _, value := range values.Values {
		if value == "" {
			return true
		}
	}
	return false
}
