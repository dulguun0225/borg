package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agentrun"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/score"
)

// What an agent authoring a version worked from, which the vector names beside
// the factors. It is composed here because the join is over two records and
// neither package walks to the other: the artifact version names the input
// manifest, and the agent run of that manifest names the effort and the versions
// of the role prompt and the skills.

// authorship is that join over one pool.
type authorship struct{ pool *pgxpool.Pool }

// OfArtifact is what the agent that authored one version worked from. It reads
// nothing for a version a human or a gate authored, for one naming no input
// manifest, and for a firing naming no version: none of the three is an agent's
// run, and the vector names nothing rather than naming a blank.
//
// A version whose record cannot be read reads as nothing too. The four fields
// are what a human arguing with the number sees and no factor weighs, so a read
// that failed leaves them out rather than failing the firing that would have
// carried them.
func (a authorship) OfArtifact(ctx context.Context, artifactID string) (score.Authored, error) {
	if artifactID == "" {
		return score.Authored{}, nil
	}
	version, err := artifact.Get(ctx, a.pool, artifactID)
	if err != nil || version.Authorship != artifact.AuthorshipAgent {
		return score.Authored{}, nil
	}
	authored := score.Authored{InputManifestID: version.InputManifestID}
	if version.InputManifestID == "" || version.ItemID == "" {
		return authored, nil
	}
	runs, err := agentrun.ForItem(ctx, a.pool, version.ItemID)
	if err != nil {
		return authored, nil
	}
	for _, run := range runs {
		if run.InputManifestID != version.InputManifestID {
			continue
		}
		authored.Effort = run.Effort
		authored.RolePromptVersionID = run.RolePromptVersionID
		authored.SkillVersionIDs = run.SkillVersionIDs
		break
	}
	return authored, nil
}
