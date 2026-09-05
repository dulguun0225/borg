package release

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
)

// The authorship rollup is a query and not a field of the release. Which of the
// three authorship attributes wrote each stage of an item is a fact of the
// artifact store, written when each version was submitted, and copying it here
// would be the same fact in two places on a record written once at the merge.

// Stage is one of an item's stages that produced a version, named by the kind
// of version it produced. The rollup answers per stage because that is what a
// rights question over the produced software asks: which stage was written by
// an agent, which by a human, and which by a gate.
type Stage struct {
	// Kind is the artifact kind the stage produced.
	Kind artifact.Kind
	// Authorship is which of the three attributes wrote it.
	Authorship artifact.Authorship
	// Author is the identity the prior is kept on: the model version for a
	// version an agent wrote, the person for one a human wrote.
	Author string
	// VersionID is the artifact version the answer was read from, so a reader
	// arguing with it has the row.
	VersionID string
}

// stagesOfAnItem are the artifact kinds an item's own stages produce, in the
// order the stages run. The other three kinds — a role prompt, a skill, the one
// selection rule — belong to a role, a subject, or the factory, not to an item,
// so no release names one.
var stagesOfAnItem = []artifact.Kind{
	artifact.KindSpec,
	artifact.KindImplementation,
	artifact.KindConsumerContract,
}

// AuthorshipRollup is which of the three authorship attributes wrote each stage
// of the item this release names, read from the artifact store's record of that
// item's versions. It is the newest version of each kind, that being the one the
// build was made from.
//
// A release naming no item rolls up nothing and returns no error: a commit a
// human accepted was decided by no gate and passed no stage, so its rollup names
// nothing the factory wrote. A stage the item has no version for is absent from
// the answer rather than present and empty.
func AuthorshipRollup(ctx context.Context, pool *pgxpool.Pool, releaseID string) ([]Stage, error) {
	r, err := Get(ctx, pool, releaseID)
	if err != nil {
		return nil, err
	}
	if !r.NamesAnItem() {
		return nil, nil
	}

	var rollup []Stage
	for _, kind := range stagesOfAnItem {
		version, found, err := artifact.NewestOfKind(ctx, pool, r.ItemID, kind)
		if err != nil {
			return nil, fmt.Errorf("release: rolling up the authorship of %s: %w", releaseID, err)
		}
		if !found {
			continue
		}
		rollup = append(rollup, Stage{
			Kind:       kind,
			Authorship: version.Authorship,
			Author:     version.Author,
			VersionID:  version.ID,
		})
	}
	return rollup, nil
}
