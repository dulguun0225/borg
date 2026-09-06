package score

import "context"

// What an agent that authored the version under decision worked from, which the
// vector names beside the factors: the input manifest, the effort, and the
// versions of the role prompt and the skills.

// Authored is that reading. It is empty where a human authored the version, and
// where nothing read the run — the two are not distinguished here, neither being
// a factor and neither resolving anything.
//
// The effort and the versions are deliberately left out of the per-author
// prior's key, which is kept per model version alone, so they are recorded here
// instead: they are what a human arguing with the number reads, and what a query
// grouping decisions by the version in force reads, without any of it moving the
// prior.
type Authored struct {
	// InputManifestID is the manifest the artifact was authored from, so the
	// input that varies most between two runs of one model is read back with the
	// words and the effort that were fixed.
	InputManifestID string `json:"input_manifest_id,omitempty"`
	// Effort is how long the entry's model worked before it answered, and is
	// empty where the provider offers none.
	Effort string `json:"effort,omitempty"`
	// RolePromptVersionID and SkillVersionIDs are the versions of what the agent
	// was told, in force at the run.
	RolePromptVersionID string   `json:"role_prompt_version_id,omitempty"`
	SkillVersionIDs     []string `json:"skill_version_ids,omitempty"`
}

// Empty reports whether nothing was read, which is what the open event omits.
func (a Authored) Empty() bool {
	return a.InputManifestID == "" && a.Effort == "" &&
		a.RolePromptVersionID == "" && len(a.SkillVersionIDs) == 0
}

// Authorship is what reads it. It is an interface because the join is over two
// records this package does not read — the artifact version names the input
// manifest and the agent run names the effort and the versions, and no read of
// either package walks from one to the other — so the composition hands the
// score whatever performs it.
type Authorship interface {
	// OfArtifact is what an agent authoring one version worked from, and an
	// empty reading for a version a human authored, for one no run was recorded
	// for, and for a firing naming no version at all.
	OfArtifact(ctx context.Context, artifactID string) (Authored, error)
}

// NoAuthorship reads nothing. It is the value a composition with no such reader
// hands in, and what [New] composes for a nil one.
type NoAuthorship struct{}

// OfArtifact is nothing.
func (NoAuthorship) OfArtifact(context.Context, string) (Authored, error) {
	return Authored{}, nil
}
