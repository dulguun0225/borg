package main

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/dulguun0225/borg/factory/buildrunner"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// runCandidateMutations composes the deployer's artifact compilation, target
// placement, seeded-store restoration, and criterion score writer. Mutants are
// never source mutations of the candidate checkout and never ordinary deploys.
func (p *path) runCandidateMutations(ctx context.Context, c *candidate, buildID string) (criterion.Mutation, error) {
	base := ""
	if c.basedOnMaster {
		var err error
		base, err = p.masterHead(ctx, c.svc)
		if err != nil {
			return criterion.Mutation{}, err
		}
	}
	seed, err := deploy.CandidateSeed(ctx, p, deploy.Candidate{
		ServiceID: c.svc.ID, ServiceName: c.svc.Name, Credential: p.d.credential,
	}, c.composition)
	if err != nil {
		return criterion.Mutation{}, err
	}
	artifacts, err := p.runner.CompileMutants(ctx, buildrunner.MutantRequest{
		Checkout: buildrunner.Checkout{Directory: c.svc.Repository, Base: base, Commit: c.commit},
		Cap:      int(service.MutantCapInForce(c.svc.MutantCap)), OutputDirectory: filepath.Join(c.environmentDir, ".mutants"),
	})
	if err != nil {
		return criterion.Mutation{}, err
	}
	run, err := nextCriterionRun(ctx, p.d.pool, buildID)
	if err != nil {
		return criterion.Mutation{}, err
	}
	composition, err := json.Marshal(c.composition)
	if err != nil {
		return criterion.Mutation{}, fmt.Errorf("factory: marshalling the mutation composition: %w", err)
	}
	hours := func(context.Context) (float64, error) {
		cycles, err := environment.Cycles(ctx, p.d.pool, c.environmentID)
		if err != nil {
			return 0, err
		}
		return environment.EnvironmentHours(cycles, time.Now())
	}
	return deploy.PerformMutations(ctx, deploy.MutationPerformance{
		Actor: deployActor, Principal: deployerPrincipal, Target: p.d.targets.at(c.environmentDir),
		Service: c.svc.Name, Credential: p.d.credential, Configuration: c.configuration,
		WayInAddress: p.d.wayInAddress, Artifacts: artifacts,
		Cap: int(service.MutantCapInForce(c.svc.MutantCap)), Seed: seed,
		Run: func(_ context.Context, ids []string) (bool, error) {
			passed, output := runSelectedEncodings(c.svc.Repository, ids)
			if !passed {
				fmt.Fprintf(p.d.out, "  candidate mutant run failed: %s\n", firstLines(output))
			}
			return passed, nil
		},
		Reach: func(buildrunner.MutantArtifact) []string { return mutationCriterionIDs(c.criteria) },
		Hours: hours,
		Record: func(ctx context.Context, actor record.Actor, run criterion.Run, reading criterion.Mutation) error {
			_, err := criterion.RecordMutation(ctx, p.d.pool, p.d.token, actor, run, reading)
			return err
		},
		RunRecord: criterion.Run{BuildID: buildID, Number: run, Place: criterion.PlaceCandidateEnvironment, Composition: string(composition)},
	})
}

func mutationCriterionIDs(results []gate.CriterionResult) []string {
	ids := make([]string, 0, len(results))
	for _, result := range results {
		ids = append(ids, result.CriterionID)
	}
	return ids
}

func runSelectedEncodings(repo string, ids []string) (bool, string) {
	if len(ids) == 0 {
		return true, ""
	}
	pattern := "(" + strings.Join(ids, "|") + ")"
	out, err := inDir(repo, "go", "test", "./...", "-run", pattern)
	return err == nil, strings.TrimSpace(out)
}
