// The database tests of this package are in score_test rather than in score,
// because they open the pool through package postgres, which imports this one to
// apply its DDL. deps.txt records the edge as "test score -> postgres gate".
//
// The gate is imported here on purpose. The outcomes this package counts are the
// decisions a gate wrote, and a test that wrote those rows by hand would prove
// that the score can read what the test writes rather than what the factory
// writes. So the decisions here are fired and closed by the real gate, over the
// real score, and only the policy is a fake — the threshold is not what these
// tests are about.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package score_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/score"
)

var (
	owner            = record.Actor{Kind: record.KindHuman, Name: "owner"}
	scoreActor       = record.Actor{Kind: record.KindComponent, Name: "score"}
	cutActor         = record.Actor{Kind: record.KindComponent, Name: "cut"}
	implementerActor = record.Actor{Kind: record.KindComponent, Name: "agent.implementer"}
	mergeActor       = record.Actor{Kind: record.KindComponent, Name: "merge"}
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

// fakePolicy answers with one threshold and no pin. What a gate does with a
// threshold is package gate's demonstration; here it only has to be answerable
// so a decision can be written.
type fakePolicy struct {
	threshold float64
	// pinned is whether a pin adds a human at the row, which is the one answer the
	// score's own sample reads: it may pass a gate the number gated and never one
	// a pin gated.
	pinned bool
}

func (f fakePolicy) AtGate(context.Context, policy.Subjects) (policy.Applied, error) {
	return policy.Applied{
		PolicyVersion: "pv_00000000000000000000000000000001",
		Threshold:     f.threshold,
		ThresholdFrom: policy.FromSupplied,
		HumanPinned:   f.pinned,
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

// cutItem writes one item in the area and an implementation version on it by
// modelVersion, which is the pair the score follows to an author.
func cutItem(t *testing.T, ctx context.Context, pool *pgxpool.Pool, branch string) (item.Item, artifact.Artifact) {
	t.Helper()
	it, err := item.NewCut(pool).Create(ctx, cutActor, item.New{
		IntentID:  "in_0000000000000000000000000000000a",
		ServiceID: serviceID,
		AreaID:    areaID,
		Branch:    branch,
	})
	if err != nil {
		t.Fatalf("cutting the item: %v", err)
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

// TestAFirstItemIsDecidedByAHumanAndTheNextIsNot is the milestone's
// demonstration at the level of the score. On a factory that has just been
// installed the first item reads over the supplied threshold — no earlier release
// to return to, an author nobody has approved, an area with no history, and every
// file in the tree touched — and after a human has approved that one, the item
// after it reads under it.
func TestAFirstItemIsDecidedByAHumanAndTheNextIsNot(t *testing.T) {
	ctx, pool, s := newScore(t)
	supplied, _ := score.Starting(gatepolicy.RiskThreshold)
	threshold := supplied.Value
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: threshold}, gate.NoReconciler{})

	first, firstImplementation := cutItem(t, ctx, pool, "item/one")
	opened, err := g.Fire(ctx, firing(first, firstImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 2}))
	if err != nil {
		t.Fatalf("Fire over the first item: %v", err)
	}
	if !opened.HumanDecides {
		t.Fatalf("the first item on a fresh factory reads %v against a threshold of %v, and a human is meant to decide it",
			opened.Assessment.Number, threshold)
	}
	// The readings that put it there, each stated so a failure says which moved.
	for _, c := range []struct {
		name string
		want float64
	}{
		{"authorship.prior", 1},
		{"context.business_area", 1},
		{"change.reversibility", 1},
		{"change.reach", 1},
		{"change.area_churn", 0},
		{"context.consumers", 0},
	} {
		if got := levelOf(t, opened.Assessment, c.name); got != c.want {
			t.Errorf("the first item's %s reads %v, want %v", c.name, got, c.want)
		}
	}

	// The human approves, and the release is minted. Those two are what the next
	// item's prior, area history, and reversibility read.
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictApprove, ""); err != nil {
		t.Fatalf("Decide: %v", err)
	}
	if _, err := release.NewWriter(pool).Mint(ctx, mergeActor, serviceID, "bl_0000000000000000000000000000000a", first.ID); err != nil {
		t.Fatalf("Mint: %v", err)
	}

	second, secondImplementation := cutItem(t, ctx, pool, "item/two")
	openedAgain, err := g.Fire(ctx, firing(second, secondImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire over the second item: %v", err)
	}
	if openedAgain.HumanDecides {
		t.Fatalf("the second item reads %v against a threshold of %v, and nobody is meant to decide it: %s",
			openedAgain.Assessment.Number, threshold, openedAgain.WhyHuman)
	}
	for _, c := range []struct {
		name string
		want float64
	}{
		{"authorship.prior", 0.5},
		{"context.business_area", 0.5},
		{"change.reversibility", 0.3},
		{"change.area_churn", 0.2},
	} {
		if got := levelOf(t, openedAgain.Assessment, c.name); got != c.want {
			t.Errorf("the second item's %s reads %v, want %v", c.name, got, c.want)
		}
	}
	if _, err := g.AutoPass(ctx, openedAgain); err != nil {
		t.Fatalf("AutoPass: %v", err)
	}

	// The auto-pass is not evidence about the author: it is the factory agreeing
	// with itself, so a third item's prior reads what the second's did.
	third, thirdImplementation := cutItem(t, ctx, pool, "item/three")
	openedThird, err := g.Fire(ctx, firing(third, thirdImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 2, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire over the third item: %v", err)
	}
	if got := levelOf(t, openedThird.Assessment, "authorship.prior"); got != 0.5 {
		t.Errorf("the prior after an auto-pass reads %v, want the 0.5 one human approval left it at", got)
	}
}

// TestAHoldTeachesTheScoreNothing: a hold is not a reject and not an approval —
// it leaves the event queued with the change still good — so no factor moves.
func TestAHoldTeachesTheScoreNothing(t *testing.T) {
	ctx, pool, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.1}, gate.NoReconciler{})

	first, firstImplementation := cutItem(t, ctx, pool, "item/one")
	deployRow := firing(first, firstImplementation, score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4})
	deployRow.Row = gate.DeployToProduction
	deployRow.ArtifactID = ""
	opened, err := g.Fire(ctx, deployRow)
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictHold, "the window is still open"); err != nil {
		t.Fatalf("Decide(hold): %v", err)
	}

	second, secondImplementation := cutItem(t, ctx, pool, "item/two")
	openedAgain, err := g.Fire(ctx, firing(second, secondImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if got := levelOf(t, openedAgain.Assessment, "authorship.prior"); got != 1 {
		t.Errorf("the prior after a hold reads %v, want the top of the scale a hold leaves it at", got)
	}
	if got := levelOf(t, openedAgain.Assessment, "context.business_area"); got != 1 {
		t.Errorf("the area's history after a hold reads %v, want the top of the scale", got)
	}
}

// TestARejectCountsAgainstTheAuthor: the score learns from a reject, which is
// what separates it from a hold.
func TestARejectCountsAgainstTheAuthor(t *testing.T) {
	ctx, pool, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.1}, gate.NoReconciler{})

	first, firstImplementation := cutItem(t, ctx, pool, "item/one")
	opened, err := g.Fire(ctx, firing(first, firstImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if _, err := g.Decide(ctx, opened, owner, gate.VerdictReject, "the encoding asserts the code"); err != nil {
		t.Fatalf("Decide(reject): %v", err)
	}

	second, secondImplementation := cutItem(t, ctx, pool, "item/two")
	openedAgain, err := g.Fire(ctx, firing(second, secondImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	// One rejection and no approval leaves the level at the top of the scale, the
	// same place an unseen author sits: what the level says is how far the score
	// trusts the author, and it cannot trust one less than not at all.
	if got := levelOf(t, openedAgain.Assessment, "authorship.prior"); got != 1 {
		t.Errorf("the prior after a reject reads %v, want the top of the scale", got)
	}
	// An approval after the rejection narrows it less than an approval alone
	// would have, which is the rejection counting.
	if _, err := g.Decide(ctx, openedAgain, owner, gate.VerdictApprove, ""); err != nil {
		t.Fatalf("Decide(approve): %v", err)
	}
	third, thirdImplementation := cutItem(t, ctx, pool, "item/three")
	openedThird, err := g.Fire(ctx, firing(third, thirdImplementation,
		score.Measurement{LinesChanged: 20, FilesChanged: 1, FilesInTree: 4}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if got := levelOf(t, openedThird.Assessment, "authorship.prior"); got <= 0.5 {
		t.Errorf("one approval against one rejection reads %v, want worse than the 0.5 an approval alone leaves", got)
	}
}

// TestAMeasurementThatCouldNotBeTakenGatesTheChange: the diff is the one input
// that is not a record, so the component that measures it says why it could not,
// and the two factors it feeds carry that reason. The formula then reduces the
// whole vector to the top of the scale.
func TestAMeasurementThatCouldNotBeTakenGatesTheChange(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, _ := cutItem(t, ctx, pool, "item/one")
	const reason = "the diff against master could not be taken: the commit is not in the repository"
	assessment, err := s.Assess(ctx, score.Change{
		ItemID:          it.ID,
		ServiceID:       serviceID,
		AreaID:          areaID,
		Measurement:     score.Measurement{Unavailable: reason},
		CriteriaInForce: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if assessment.Number != 1 {
		t.Errorf("the number is %v, want the top of the scale", assessment.Number)
	}
	unavailable := assessment.UnavailableFactors()
	if len(unavailable) != 2 {
		t.Fatalf("%v are unavailable, want the two the diff feeds", unavailable)
	}
	for _, f := range assessment.Vector {
		switch f.Name {
		case "change.size", "change.reach":
			if f.Unavailable != reason {
				t.Errorf("%s says it is unavailable because %q, want the measurement's reason", f.Name, f.Unavailable)
			}
			if f.Level != 1 {
				t.Errorf("%s resolves to %v, want the top of the scale", f.Name, f.Level)
			}
		default:
			if f.Unavailable != "" {
				t.Errorf("%s is unavailable: %s", f.Name, f.Unavailable)
			}
		}
	}

	// A tree with no files is the other way reach cannot be computed: the share
	// one change touches is undefined rather than large.
	assessment, err = s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: areaID,
		Measurement: score.Measurement{LinesChanged: 3, FilesChanged: 1, FilesInTree: 0},
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := assessment.UnavailableFactors(); len(got) != 1 || got[0] != "change.reach" {
		t.Errorf("%v are unavailable, want change.reach alone", got)
	}
}

// TestAnItemWithNoAreaCannotBeScoredOnContext: an item may name no area, and the
// two factors that read one then say so rather than reading as low risk.
func TestAnItemWithNoAreaCannotBeScoredOnContext(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, err := item.NewCut(pool).Create(ctx, cutActor, item.New{
		IntentID:  "in_a",
		ServiceID: serviceID,
		Branch:    "item/no-area",
	})
	if err != nil {
		t.Fatalf("cutting the item: %v", err)
	}
	if _, err = artifact.NewStore(pool).SubmitImplementation(ctx, implementerActor,
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: modelVersion}, it.ID, "a commit"); err != nil {
		t.Fatalf("submitting the implementation: %v", err)
	}

	assessment, err := s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID,
		Measurement:     score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10},
		CriteriaInForce: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := assessment.UnavailableFactors(); len(got) != 2 {
		t.Errorf("%v are unavailable, want the two factors that read an area", got)
	}
	if assessment.Number != 1 {
		t.Errorf("the number is %v, want the top of the scale", assessment.Number)
	}
}

// TestAnItemWithNoImplementationHasNoAuthorToHoldAPriorOn: the prior is computed
// from that author's own work, so an item with no implementation version leaves
// the factor unavailable rather than reading as an author with no history.
func TestAnItemWithNoImplementationHasNoAuthorToHoldAPriorOn(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, err := item.NewCut(pool).Create(ctx, cutActor, item.New{
		IntentID: "in_a", ServiceID: serviceID, AreaID: areaID, Branch: "item/unbuilt",
	})
	if err != nil {
		t.Fatalf("cutting the item: %v", err)
	}
	assessment, err := s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: areaID,
		Measurement:     score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10},
		CriteriaInForce: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := assessment.UnavailableFactors(); len(got) != 1 || got[0] != "authorship.prior" {
		t.Errorf("%v are unavailable, want authorship.prior alone", got)
	}
}

// TestAFailedCriterionIsTheTopOfItsScale: the gate above is what rejects a
// failing build; a number that read low on one would be the score disagreeing
// with a run.
func TestAFailedCriterionIsTheTopOfItsScale(t *testing.T) {
	ctx, pool, s := newScore(t)

	it, _ := cutItem(t, ctx, pool, "item/one")
	assessment, err := s.Assess(ctx, score.Change{
		ItemID: it.ID, ServiceID: serviceID, AreaID: areaID,
		Measurement:     score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10},
		CriteriaInForce: 4, CriteriaFailed: 1,
	})
	if err != nil {
		t.Fatalf("Assess: %v", err)
	}
	if got := levelOf(t, assessment, "change.test_coverage"); got != 1 {
		t.Errorf("a build with a failed criterion reads coverage %v, want the top of the scale", got)
	}
}

// TestAChangeNamingNoItemIsACallersDefect: every firing has an item and a
// service, so a blank is not a factor to mark unavailable.
func TestAChangeNamingNoItemIsACallersDefect(t *testing.T) {
	ctx, _, s := newScore(t)

	if _, err := s.Assess(ctx, score.Change{ServiceID: serviceID}); err == nil {
		t.Error("Assess over a change naming no item was accepted")
	}
	if _, err := s.Assess(ctx, score.Change{ItemID: "it_a"}); err == nil {
		t.Error("Assess over a change naming no service was accepted")
	}
}

// TestTheVersionIsAppendedOnlyWhenWhatItPublishesChanges: starting the factory
// twice on unchanged source appends nothing, and the version in force is the one
// every decision names.
func TestTheVersionIsAppendedOnlyWhenWhatItPublishesChanges(t *testing.T) {
	ctx, pool, s := newScore(t)
	w := score.NewWriter(pool)

	again, err := w.Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if again.ID != s.Version().ID {
		t.Errorf("a second Ensure on unchanged source appended %s beside %s", again.ID, s.Version().ID)
	}

	version := s.Version()
	if version.FormulaVersion != score.FormulaVersion || version.Formula != score.Formula {
		t.Error("the version does not name the published formula")
	}
	if version.FactorSet != score.FactorSet() || version.Rules != score.Rules {
		t.Error("the version does not name the factor set and the published rules")
	}
	if len(version.Supplied) == 0 {
		t.Error("the version names no supplied value")
	}
	if version.Supersedes != "" {
		t.Errorf("the first version supersedes %q", version.Supersedes)
	}
	if version.Actor != scoreActor {
		t.Errorf("the version's actor is %+v, want the score", version.Actor)
	}

	read, err := score.Get(ctx, pool, version.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.ID != version.ID || read.Formula != version.Formula || read.Rules != version.Rules ||
		len(read.Supplied) != len(version.Supplied) {
		t.Errorf("the version reads back as %+v", read)
	}

	// A version whose supplied values differ is a version of its own, and it
	// names the one it replaced.
	if _, err := pool.Exec(ctx, `insert into `+score.Table+`
		(id, actor_kind, actor_name, at, formula_version, formula, factor_set, rules, supplied, supersedes)
		values ('scv_next', 'component', 'score', $1, $2, $3, $4, $5,
			'[{"parameter":"risk_threshold","value":0.9,"why":"a hand-written row this test appended"}]', $6)`,
		record.Now(), version.FormulaVersion, version.Formula, version.FactorSet, version.Rules, version.ID); err != nil {
		t.Fatalf("appending a second version: %v", err)
	}
	newest, found, err := score.Newest(ctx, pool)
	if err != nil || !found {
		t.Fatalf("Newest = %+v, %v, %v", newest, found, err)
	}
	if newest.ID != "scv_next" || newest.Supersedes != version.ID {
		t.Errorf("the newest version is %s superseding %s", newest.ID, newest.Supersedes)
	}
	// The newest version no longer says what the source publishes, so Ensure
	// appends one that does and names the newest as its predecessor — the same
	// path a change to the source takes, and the reason nothing refuses two
	// versions that say the same thing: this one says what the first one said,
	// and it is not the first.
	ensured, err := w.Ensure(ctx, scoreActor)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if ensured.Supersedes != "scv_next" {
		t.Errorf("the appended version supersedes %q, want scv_next", ensured.Supersedes)
	}
	if len(ensured.Supplied) != len(version.Supplied) {
		t.Error("the appended version does not say what the source publishes")
	}
	if ensured.ID == version.ID {
		t.Error("Ensure reused the id of the version that said the same thing")
	}
}

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the key rather than trusting
// the constant, which is what keeps it a value no other part of the factory
// derives from a name of its own.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	sum := sha256.Sum256([]byte("borg/factory/score"))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if got := score.AdvisoryLockKey(); got != want {
		t.Errorf("AdvisoryLockKey() = %#x, want %#x", got, want)
	}
	if score.AdvisoryLockKey() <= 0 {
		t.Errorf("AdvisoryLockKey() = %d, want a positive value", score.AdvisoryLockKey())
	}
}

// TestEnsuringAtOnceAppendsOneVersion: the lock is what makes the read of the
// newest and the append that supersedes it one step, which is what nothing in the
// schema can enforce — two versions saying the same thing are legitimate where
// they are not adjacent.
func TestEnsuringAtOnceAppendsOneVersion(t *testing.T) {
	ctx, pool, s := newScore(t)
	w := score.NewWriter(pool)

	const ensures = 8
	done := make(chan error, ensures)
	for range ensures {
		go func() {
			_, err := w.Ensure(ctx, scoreActor)
			done <- err
		}()
	}
	for range ensures {
		if err := <-done; err != nil {
			t.Errorf("Ensure: %v", err)
		}
	}

	var rows int
	if err := pool.QueryRow(ctx, `select count(*) from `+score.Table).Scan(&rows); err != nil {
		t.Fatalf("counting the versions: %v", err)
	}
	if rows != 1 {
		t.Errorf("%d ensures at once left %d versions, want the one the first append wrote", ensures, rows)
	}
	newest, _, err := score.Newest(ctx, pool)
	if err != nil {
		t.Fatalf("Newest: %v", err)
	}
	if newest.ID != s.Version().ID {
		t.Errorf("the version in force is %s, want %s", newest.ID, s.Version().ID)
	}
}

// TestEveryDecisionNamesTheVersionInForce: the version moves as outcomes arrive
// in the design, so a decision that did not name it could not be read back
// against what the score published when it was taken.
func TestEveryDecisionNamesTheVersionInForce(t *testing.T) {
	ctx, pool, s := newScore(t)
	g := gate.New(decisionlog.NewWriter(pool), s, fakePolicy{threshold: 0.9}, gate.NoReconciler{})

	it, implementation := cutItem(t, ctx, pool, "item/one")
	opened, err := g.Fire(ctx, firing(it, implementation,
		score.Measurement{LinesChanged: 5, FilesChanged: 1, FilesInTree: 10}))
	if err != nil {
		t.Fatalf("Fire: %v", err)
	}
	if opened.Row.ScoreVersion != s.Version().ID {
		t.Errorf("the opening names score version %q, want %q", opened.Row.ScoreVersion, s.Version().ID)
	}
	if opened.Assessment.Version != s.Version().ID {
		t.Errorf("the assessment names version %q", opened.Assessment.Version)
	}
	if opened.Assessment.FormulaVersion != score.FormulaVersion {
		t.Errorf("the assessment names formula %q", opened.Assessment.FormulaVersion)
	}
}
