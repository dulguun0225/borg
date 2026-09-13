package deploy

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/buildrunner"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// MutationRecorder is criterion's mutation writer as supplied by the
// composition. Deploy owns when the score is recorded; criterion owns its
// stored shape and validation.
type MutationRecorder func(context.Context, record.Actor, criterion.Run, criterion.Mutation) error

// MutationPerformance is one serialized mutation pass on an item's one target.
// Restore returns a selected seeded store before every mutant, Place reaches the
// target seam without a deploy record, and Run executes only selected encodings.
type MutationPerformance struct {
	Actor         record.Actor
	Principal     principal.Principal
	Target        targetseam.Target
	Service       string
	Credential    secretref.Ref
	Configuration targetseam.ValueSet
	WayInAddress  string
	Seed          targetseam.Seed
	Artifacts     []buildrunner.MutantArtifact
	Cap           int
	Run           func(context.Context, []string) (bool, error)
	Reach         func(buildrunner.MutantArtifact) []string
	Hours         func(context.Context) (float64, error)
	Record        MutationRecorder
	RunRecord     criterion.Run
	Coverage      string
}

// PerformMutations restores, places, and runs each mutant in order. A mutant
// that does not compile is detected without reaching the target. No mutant
// calls the ordinary deploy operation and none creates a build or deploy row.
func PerformMutations(ctx context.Context, p MutationPerformance) (criterion.Mutation, error) {
	if err := p.Actor.Validate(); err != nil {
		return criterion.Mutation{}, err
	}
	if p.Target == nil || p.Run == nil || p.Record == nil {
		return criterion.Mutation{}, fmt.Errorf("deploy: mutation performance is incomplete")
	}
	var seeder targetseam.Seeder
	if p.Seed.Version != "" {
		var ok bool
		seeder, ok = p.Target.(targetseam.Seeder)
		if !ok {
			return criterion.Mutation{}, fmt.Errorf("deploy: mutation target cannot restore a seeded store")
		}
	}
	if p.Cap <= 0 {
		return criterion.Mutation{}, fmt.Errorf("deploy: mutant cap must be positive")
	}
	if p.RunRecord.Place != criterion.PlaceCandidateEnvironment || p.RunRecord.Number < 1 {
		return criterion.Mutation{}, fmt.Errorf("deploy: mutation run is not a candidate-environment run")
	}
	if p.Coverage == "" {
		p.Coverage = "the candidate encodings ran on the candidate environment"
	}
	reading := criterion.Mutation{Coverage: p.Coverage, Toolchain: "go"}
	for n, artifact := range p.Artifacts {
		if n >= p.Cap {
			break
		}
		reading.MutantsTested++
		if artifact.CompileError != "" {
			reading.MutantsDetected++
			continue
		}
		if artifact.ArtifactPath == "" {
			return criterion.Mutation{}, fmt.Errorf("deploy: mutant %d has no artifact", n+1)
		}
		if seeder != nil {
			if err := seeder.RestoreSeed(ctx, p.Principal, p.Seed); err != nil {
				return criterion.Mutation{}, err
			}
		}
		if _, err := p.Target.PlaceMutant(ctx, p.Principal, targetseam.Mutant{
			Service: p.Service, Artifact: artifact.ArtifactPath, Credential: p.Credential,
			Configuration: p.Configuration, WayInAddress: p.WayInAddress,
		}); err != nil {
			return criterion.Mutation{}, fmt.Errorf("deploy: placing mutant %s:%d: %w", artifact.File, artifact.Line, err)
		}
		ids := p.Reach(artifact)
		passed, err := p.Run(ctx, ids)
		if err != nil {
			return criterion.Mutation{}, err
		}
		if !passed {
			reading.MutantsDetected++
		}
		if p.Hours != nil {
			hours, err := p.Hours(ctx)
			if err != nil {
				return criterion.Mutation{}, err
			}
			reading.Coverage = strings.TrimSpace(fmt.Sprintf("%s; environment-hours %.6f", reading.Coverage, hours))
		}
	}
	if reading.MutantsTested == 0 {
		reading.CouldNotDerive = "the diff's Go lines contain no supported mutation operator"
		reading.MutantsDetected = 0
	}
	if err := p.Record(ctx, p.Actor, p.RunRecord, reading); err != nil {
		return criterion.Mutation{}, err
	}
	return reading, nil
}
