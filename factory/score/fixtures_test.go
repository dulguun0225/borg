// The database tests of this package are in score_test rather than score
// because they reach the pool through package postgres, the edge deps.txt states
// on its test line for score. This file holds the fixtures they share.
package score_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/score"
)

var (
	owner              = record.Actor{Kind: record.KindHuman, Name: "owner"}
	scoreActor         = record.Actor{Kind: record.KindComponent, Name: "score"}
	decompositionActor = record.Actor{Kind: record.KindComponent, Name: "decomposition"}
	implementerActor   = record.Actor{Kind: record.KindComponent, Name: "agent.implementer"}
	mergeActor         = record.Actor{Kind: record.KindComponent, Name: "merge"}
)

// modelVersion is the author every artifact here is written by, which is the
// identity the prior is kept on.
const modelVersion = "claude-opus-5"

// The ids of records this test does not create. There are no foreign keys
// between record tables, so a service and an environment are ids the score never
// follows — what it reads about a service is its releases.
const (
	serviceID     = "svc_0000000000000000000000000000000a"
	environmentID = "env_000000000000000000000000000000a"
	areaID        = "ar_0000000000000000000000000000000a"
)

// fakePolicy answers with one threshold and no safeguard. What a gate does with
// a threshold is package gate's demonstration; here it only has to be
// answerable so a decision can be written.
type fakePolicy struct {
	threshold float64
	// bySafeguard is whether a safeguard adds a human at the row, which is the one
	// answer the score's own sample reads: it may pass a gate the number gated and
	// never one a safeguard gated.
	bySafeguard bool
}

func (f fakePolicy) AtGate(context.Context, policy.Subjects) (policy.Applied, error) {
	return policy.Applied{
		PolicyVersion:    "pv_00000000000000000000000000000001",
		Threshold:        f.threshold,
		ThresholdFrom:    policy.FromSupplied,
		HumanBySafeguard: f.bySafeguard,
	}, nil
}

func newScore(t *testing.T) (context.Context, *pgxpool.Pool, *score.Score) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m2_score_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	version, err := score.NewWriter(pool).Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	return ctx, pool, score.New(pool, version, score.NeverDraw{})
}

func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// decomposeItem writes one item in the area and an implementation version on it by
// modelVersion, which is the pair the score follows to an author.
func decomposeItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, branch string) (item.Item, artifact.Artifact) {
	t.Helper()
	it, err := item.NewDecomposition(pool).Create(ctx, decompositionActor, item.New{
		IntentID:  "in_0000000000000000000000000000000a",
		ServiceID: serviceID,
		AreaID:    areaID,
		Branch:    branch,
	})
	if err != nil {
		t.Fatalf("decomposing the item: %v", err)
	}
	implementation, err := artifact.NewStore(pool).SubmitImplementation(ctx, implementerActor,
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: modelVersion}, it.ID, "a commit")
	if err != nil {
		t.Fatalf("submitting the implementation: %v", err)
	}
	return it, implementation
}

// firing is the merge row's firing over one item, with the measurement a caller
// would have taken where the repository is.
func firing(it item.Item, implementation artifact.Artifact, m score.Measurement) gate.Firing {
	return gate.Firing{
		Row:             gate.MergeToMaster,
		ItemID:          it.ID,
		BuildID:         "bl_0000000000000000000000000000000a",
		ArtifactID:      implementation.ID,
		ServiceID:       serviceID,
		AreaID:          areaID,
		EnvironmentID:   environmentID,
		CriteriaInForce: 1,
		Criteria:        []gate.CriterionResult{{CriterionID: "cr_a", Outcome: criterion.OutcomePassed}},
		Measurement:     m,
	}
}

func levelOf(t *testing.T, a score.Assessment, name string) float64 {
	t.Helper()
	for _, f := range a.Vector {
		if f.Name == name {
			if f.Unavailable != "" {
				t.Fatalf("%s is unavailable: %s", name, f.Unavailable)
			}
			return f.Level
		}
	}
	t.Fatalf("the vector names no %s", name)
	return 0
}
