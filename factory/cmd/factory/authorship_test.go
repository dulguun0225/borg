// What an agent authoring a version worked from, as the vector names it: the
// join over the artifact version and the agent run of the manifest it names.
package main

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/inputmanifest"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// TestTheVectorNamesTheSkillVersionsTheRunWorkedFrom: the vector names the
// effort the entry ran at and the versions of the role prompt and the skills the
// run worked from, all four read off the run of the manifest the version names.
// A run that names skill versions is read back with them.
func TestTheVectorNamesTheSkillVersionsTheRunWorkedFrom(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v", err)
	}
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading service %s: found %v, %v", theService, found, err)
	}

	const itemID = "it_authorship"
	manifest, err := inputmanifest.NewWriter(d.pool, d.token).Write(ctx, record.Actor{
		Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed,
	}, inputmanifest.New{ItemID: itemID, Stage: "spec"})
	if err != nil {
		t.Fatalf("writing the input manifest: %v", err)
	}

	skills := []string{"art_skill_payments", "art_skill_ledger"}
	if _, err := agentrun.NewWriter(d.pool, d.token).Record(ctx, record.Actor{
		Kind: record.KindComponent, Key: "dispatch", Basis: record.BasisClaimed,
	}, agentrun.New{
		Role:                "spec_author",
		RolePromptVersionID: "art_prompt",
		SkillVersionIDs:     skills,
		ModelVersion:        d.modelName,
		Effort:              "high",
		CredentialName:      d.modelCredentialName,
		ItemID:              itemID,
		Stage:               "spec",
		InputManifestID:     manifest.ID,
		Outcome:             "answered",
	}); err != nil {
		t.Fatalf("recording the agent run: %v", err)
	}

	version, _, _, err := p.store.SubmitSpec(ctx, p.specAuthorActor(),
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName},
		itemID, svc.ID, "the spec that run authored", []criterion.Draft{}, nil, nil, manifest.ID)
	if err != nil {
		t.Fatalf("submitting the spec version: %v", err)
	}

	authored, err := (authorship{pool: d.pool}).OfArtifact(ctx, version.ID)
	if err != nil {
		t.Fatalf("reading what the agent worked from: %v", err)
	}
	if authored.InputManifestID != manifest.ID || authored.Effort != "high" ||
		authored.RolePromptVersionID != "art_prompt" {
		t.Errorf("the vector names %+v, want the manifest, the effort and the role prompt version of that run", authored)
	}
	if !slices.Equal(authored.SkillVersionIDs, skills) {
		t.Errorf("the vector names skill versions %v, want %v", authored.SkillVersionIDs, skills)
	}
}
