package deploy

import (
	"context"
	"testing"

	"github.com/dulguun0225/borg/factory/buildrunner"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

func TestPerformMutationsRestoresPlacesReachesAndRecordsOnlyTheScore(t *testing.T) {
	target := targetseam.NewFake()
	var restored, runs, hours int
	var recorded criterion.Mutation
	got, err := PerformMutations(context.Background(), MutationPerformance{
		Actor:     record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed},
		Principal: principal.OfComponent("deployer"), Target: target, Service: "demo",
		Credential: secretref.MustNew("deploy.local"), Seed: targetseam.Seed{
			Service: "demo", Version: "seed-1", Credential: secretref.MustNew("deploy.local"),
		}, Cap: 2,
		Artifacts: []buildrunner.MutantArtifact{{ArtifactPath: "/tmp/mutant-1", File: "main.go", Line: 4}, {ArtifactPath: "/tmp/mutant-2", File: "main.go", Line: 5}},
		Run: func(_ context.Context, ids []string) (bool, error) {
			restored++
			runs++
			if len(ids) != 1 || ids[0] != "cr_demo" {
				t.Fatalf("reached ids = %v, want the selected criterion", ids)
			}
			return runs == 1, nil
		},
		Reach: func(buildrunner.MutantArtifact) []string { return []string{"cr_demo"} },
		Hours: func(context.Context) (float64, error) { hours++; return float64(hours), nil },
		Record: func(_ context.Context, _ record.Actor, _ criterion.Run, reading criterion.Mutation) error {
			recorded = reading
			return nil
		},
		RunRecord: criterion.Run{BuildID: "bl_demo", Number: 1, Place: criterion.PlaceCandidateEnvironment},
	})
	if err != nil {
		t.Fatal(err)
	}
	if restored != 2 || runs != 2 || hours != 2 || got.MutantsTested != 2 || got.MutantsDetected != 1 || recorded != got {
		t.Fatalf("got %+v, restored %d, runs %d, hours %d, recorded %+v", got, restored, runs, hours, recorded)
	}
	for _, call := range target.Calls() {
		if call.Op == targetseam.OpDeploy {
			t.Fatal("mutation pass called ordinary deploy")
		}
	}
	if len(target.Calls()) != 4 || target.Calls()[0].Op != targetseam.OpRestoreSeed || target.Calls()[1].Op != targetseam.OpPlaceMutant {
		t.Fatalf("target calls = %+v, want restore and placement per mutant", target.Calls())
	}
}

type targetWithoutSeeder struct{ targetseam.Target }

func TestPerformMutationsAllowsAnUnseededTargetWithoutRestore(t *testing.T) {
	fake := targetseam.NewFake()
	var ran, recorded int
	_, err := PerformMutations(context.Background(), MutationPerformance{
		Actor:      record.Actor{Kind: record.KindComponent, Key: "deployer", Basis: record.BasisClaimed},
		Principal:  principal.OfComponent("deployer"),
		Target:     targetWithoutSeeder{Target: fake},
		Service:    "demo",
		Credential: secretref.MustNew("deploy.local"),
		Seed:       targetseam.Seed{Service: "demo", Credential: secretref.MustNew("deploy.local")},
		Cap:        1,
		Artifacts:  []buildrunner.MutantArtifact{{ArtifactPath: "/tmp/mutant-1", File: "main.go", Line: 4}},
		Run: func(context.Context, []string) (bool, error) {
			ran++
			return false, nil
		},
		Reach: func(buildrunner.MutantArtifact) []string { return []string{"cr_demo"} },
		Record: func(context.Context, record.Actor, criterion.Run, criterion.Mutation) error {
			recorded++
			return nil
		},
		RunRecord: criterion.Run{BuildID: "bl_demo", Number: 1, Place: criterion.PlaceCandidateEnvironment},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 || recorded != 1 {
		t.Fatalf("ran %d times and recorded %d times, want one each", ran, recorded)
	}
	calls := fake.Calls()
	if len(calls) != 1 || calls[0].Op != targetseam.OpPlaceMutant {
		t.Fatalf("target calls = %+v, want placement without restore", calls)
	}
}
