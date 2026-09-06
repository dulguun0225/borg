package score

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
)

// Which score version decides a gate an authored threshold binds, which is not
// always the newest: a version that changed the published formula, the factor
// set or the weights waits at that scope on the owner confirming or re-authoring
// the threshold against it.

// policyVersionEvent is the part of a policy version row this package reads
// back: which scope the owner wrote on, and the score version that write
// confirmed or re-authored a threshold against. Package policy composes the row
// and cannot be imported here, importing this package itself, so these field
// names are declared here and TestThePolicyVersionFieldsTheScoreReads in that
// package is what holds the two spellings together.
type policyVersionEvent struct {
	Scope                policyScope `json:"scope"`
	ConfirmsScoreVersion string      `json:"confirms_score_version"`
}

// policyScope is the record an authored value is a field of, as a policy version
// names it. It is the second half of the same duplication: the three field names
// and the way they compose into one string are package policy's, written out
// again here rather than shared, so that a caller of [InForceAt] can pass the
// scope as one string.
type policyScope struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Key  string `json:"key"`
}

func (s policyScope) String() string {
	if s.Key == "" {
		return s.Kind + ":" + s.ID
	}
	return s.Kind + ":" + s.ID + ":" + s.Key
}

// InForceAt is the version that decides a gate an authored threshold binds at
// one scope: the newest version, where every version that changed the published
// formula or the factor set since has been confirmed at that scope, and the last
// version confirmed there otherwise. A version differing in a supplied value
// takes effect as it is appended and is never what this waits on.
//
// The caller is package policy, and it asks only where a threshold is authored
// at the scope: where nobody authored one, the newest version is in force and
// [Newest] is the read. Which scope a gate row reads is that package's, so the
// scope arrives here as the string an owner authored on.
func InForceAt(ctx context.Context, pool *pgxpool.Pool, token lease.Token, scope string) (Version, bool, error) {
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, componentPrincipal)
	if err != nil {
		return Version{}, false, err
	}
	read, err := versionsIn(rows)
	if err != nil || len(read) == 0 {
		return Version{}, false, err
	}
	confirmed := map[string]bool{}
	for _, row := range rows {
		if row.Shape != decisionlog.ShapePolicyVersion {
			continue
		}
		var event policyVersionEvent
		if json.Unmarshal([]byte(row.Payload), &event) != nil {
			continue
		}
		if event.Scope.String() == scope && event.ConfirmsScoreVersion != "" {
			confirmed[event.ConfirmsScoreVersion] = true
		}
	}

	inForce := read[0]
	for _, v := range read[1:] {
		if v.Branch != BranchSupplied && !confirmed[v.ID] {
			// A version that redefined the number waits on the owner, and every
			// version above it was computed under that redefinition, so the
			// sequence stops here rather than skipping to the newest.
			break
		}
		inForce = v
	}
	return inForce, true, nil
}
