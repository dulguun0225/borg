// The database tests of this package are in intent_test rather than in
// intent, because they open the pool through package postgres. An external
// test package is a separate package to the compiler, so the edge is a test
// edge and not a cycle. deps.txt records it as "test intent -> postgres".
//
// The package's own DDL is applied statement by statement rather than through
// postgres.Apply, so these tests depend on this package's schema alone.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package intent_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// newIntake gives a test a schema of its own, this package's DDL applied
// inside it, and a writer over it. The schema is dropped when the test ends,
// so a rerun on a database a previous run left dirty starts clean.
func newIntake(t *testing.T) (context.Context, *pgxpool.Pool, *intent.Intake) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m1in_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		// t.Context is already cancelled by the time cleanup runs.
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
	for n, statement := range lease.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying lease statement %d: %v", n+1, err)
		}
	}
	for n, statement := range intent.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying intent statement %d: %v", n+1, err)
		}
	}
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, intent.NewIntake(pool, token)
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writer's statements resolves there.
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

var owner = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}
var intake = record.Actor{Kind: record.KindComponent, Key: "intake", Basis: record.BasisClaimed}

// requested is an owner's intent, taken in and ready to be interviewed.
func requested(t *testing.T, ctx context.Context, in *intent.Intake, statement string) intent.Intent {
	t.Helper()
	taken, err := in.TakeIn(ctx, owner, intent.Arrival{Source: intent.SourceOwner, Statement: statement})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	return taken
}

// crossing is one detector's evidence, used wherever a test needs an intent
// the factory raised itself.
var crossing = intent.Evidence{ServiceID: "sv_checkout", ReleaseID: "rl_9"}

// raised is a detector's intent on the evidence given.
func raised(t *testing.T, ctx context.Context, in *intent.Intake, evidence intent.Evidence, statement string) intent.Intent {
	t.Helper()
	taken, err := in.TakeIn(ctx, intake, intent.Arrival{
		Source: intent.SourceDetector, Statement: statement, Evidence: evidence,
		Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"},
	})
	if err != nil {
		t.Fatalf("TakeIn a detector's intent: %v", err)
	}
	return taken
}

func TestTakeInStartsUnrefined(t *testing.T) {
	ctx, pool, in := newIntake(t)

	taken, err := in.TakeIn(ctx, owner, intent.Arrival{
		Source:       intent.SourceOwner,
		Statement:    "checkout should retry a failed charge once",
		ProjectID:    "pr_shop",
		ConstraintID: "cn_pci",
	})
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if taken.State != intent.StateUnrefined || taken.Rounds != 0 || taken.ReDecompositions != 0 {
		t.Errorf("a new intent is %s with %d rounds and %d re-decompositions, want unrefined with 0 and 0",
			taken.State, taken.Rounds, taken.ReDecompositions)
	}
	if taken.Source != intent.SourceOwner {
		t.Errorf("the intent's source is %s, want owner", taken.Source)
	}
	if taken.ProjectID != "pr_shop" || taken.ConstraintID != "cn_pci" {
		t.Errorf("the intent names project %q and constraint %q, want pr_shop and cn_pci", taken.ProjectID, taken.ConstraintID)
	}
	if taken.Tier.Written() || taken.IntendedEffect != "" || taken.Outcome != "" {
		t.Errorf("nothing is judged on the way in, and this arrived judged: %+v", taken)
	}
	if _, err := time.Parse(record.TimeLayout, taken.At); err != nil {
		t.Errorf("the intent's timestamp %q: %v", taken.At, err)
	}

	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read != taken {
		t.Errorf("Get = %+v, want the intent as taken in, %+v", read, taken)
	}

	if _, err := intent.Get(ctx, pool, "in_missing"); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("Get on a missing id = %v, want ErrIntentNotFound", err)
	}
}

func TestTakeInRefusals(t *testing.T) {
	ctx, _, in := newIntake(t)

	for _, refused := range []struct {
		name    string
		actor   record.Actor
		arrival intent.Arrival
		want    error
	}{
		{"a source outside the three", owner,
			intent.Arrival{Source: "weather", Statement: "anything"}, intent.ErrSourceUnknown},
		{"no statement", owner,
			intent.Arrival{Source: intent.SourceOwner}, intent.ErrStatementEmpty},
		{"no actor", record.Actor{},
			intent.Arrival{Source: intent.SourceOwner, Statement: "anything"}, record.ErrKindUnknown},
		{"a detector's intent with no evidence", intake,
			intent.Arrival{Source: intent.SourceDetector, Statement: "anything"}, intent.ErrEvidenceEmpty},
		{"evidence on a request", owner,
			intent.Arrival{Source: intent.SourceOwner, Statement: "anything", Evidence: crossing},
			intent.ErrEvidenceOnARequest},
		{"a tier with no policy version", intake,
			intent.Arrival{Source: intent.SourceDetector, Statement: "anything", Evidence: crossing,
				Tier: intent.Tier{Value: 2}}, intent.ErrTierIncomplete},
		{"a tier on a request", owner,
			intent.Arrival{Source: intent.SourceOwner, Statement: "anything",
				Tier: intent.Tier{Value: 2, PolicyVersion: "pv_1"}}, intent.ErrRequesterOwed},
	} {
		if _, err := in.TakeIn(ctx, refused.actor, refused.arrival); !errors.Is(err, refused.want) {
			t.Errorf("TakeIn with %s = %v, want %v", refused.name, err, refused.want)
		}
	}
}

// TestADetectorAttachesOnTheEvidence is the key the design gives these raises.
// A detector raises an intent for a condition and not for an observation, so
// before raising one it looks for an intent on the same evidence that has not
// finished. A statement is not the key: two raises whose text differs are one
// condition where the evidence is the same.
func TestADetectorAttachesOnTheEvidence(t *testing.T) {
	ctx, pool, in := newIntake(t)

	first := raised(t, ctx, in, crossing, "Revert release 9 of checkout: its window closed failed.")

	found, ok, err := intent.OnEvidence(ctx, pool, crossing)
	if err != nil {
		t.Fatalf("OnEvidence: %v", err)
	}
	if !ok || found.ID != first.ID {
		t.Fatalf("OnEvidence = %+v, %v, want the intent already raised on it", found, ok)
	}

	// Another release of the same service is other evidence and other work.
	elsewhere := intent.Evidence{ServiceID: "sv_checkout", ReleaseID: "rl_10"}
	if _, ok, err := intent.OnEvidence(ctx, pool, elsewhere); err != nil || ok {
		t.Errorf("OnEvidence on another release = %v, %v, want nothing", ok, err)
	}
	if _, ok, err := intent.OnEvidence(ctx, pool, intent.Evidence{}); err != nil || ok {
		t.Errorf("OnEvidence on no evidence at all = %v, %v, want nothing", ok, err)
	}

	// An intent still open past its interview is still the intent on that
	// evidence: matching on the statement stopped working at decomposition,
	// and this does not.
	confirmEnumerated(t, ctx, in, first.ID)
	found, ok, err = intent.OnEvidence(ctx, pool, crossing)
	if err != nil || !ok || found.ID != first.ID {
		t.Errorf("OnEvidence on a refined intent = %+v, %v, %v, want the same intent", found, ok, err)
	}

	// Finished is delivered or dropped, and nothing attaches to one.
	if err := in.Delivered(ctx, intake, intent.Delivery{IntentID: first.ID}); err != nil {
		t.Fatalf("Delivered: %v", err)
	}
	if _, ok, err := intent.OnEvidence(ctx, pool, crossing); err != nil || ok {
		t.Errorf("OnEvidence on a delivered intent = %v, %v, want nothing", ok, err)
	}
}

// TestTheEvidenceKeyIsTheContractAndTheElement is deprecation's raise: two
// marked elements of one contract are two removals, so the element is part of
// the key and one raise does not stand for the other.
func TestTheEvidenceKeyIsTheContractAndTheElement(t *testing.T) {
	ctx, pool, in := newIntake(t)

	first := intent.Evidence{ContractID: "ct_orders", Element: "legacy_total"}
	second := intent.Evidence{ContractID: "ct_orders", Element: "legacy_currency"}
	raised(t, ctx, in, first, "Remove legacy_total from the orders contract.")

	if _, ok, err := intent.OnEvidence(ctx, pool, second); err != nil || ok {
		t.Errorf("OnEvidence on the second element = %v, %v, want nothing", ok, err)
	}
	raised(t, ctx, in, second, "Remove legacy_currency from the orders contract.")
	for _, evidence := range []intent.Evidence{first, second} {
		if _, ok, err := intent.OnEvidence(ctx, pool, evidence); err != nil || !ok {
			t.Errorf("OnEvidence on %+v = %v, %v, want the raise for that element", evidence, ok, err)
		}
	}
}

// TestTheDeadlineIsWrittenWhenTheTriggerOccurs: the deadline is the trigger's
// own time plus the constraint's period, and the trigger is later than the
// arrival for the two triggers that are records and for a human's mark.
func TestTheDeadlineIsWrittenWhenTheTriggerOccurs(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "erase a customer's data on request")

	if taken.Deadline != "" {
		t.Errorf("an intent arrives with a deadline of %q", taken.Deadline)
	}
	deadline := record.FormatTime(time.Now().Add(72 * time.Hour))
	if err := in.SetDeadline(ctx, owner, taken.ID, deadline); err != nil {
		t.Fatalf("SetDeadline: %v", err)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Deadline != deadline {
		t.Errorf("the deadline is %q, want %q", read.Deadline, deadline)
	}
	if err := in.SetDeadline(ctx, owner, taken.ID, "in three days"); err == nil {
		t.Error("SetDeadline with a deadline outside the layout was accepted")
	}
}

// TestTheStoreRefusesAroundTheWriter inserts by raw SQL, so what it exercises
// is the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	insertIntent := `insert into intent (id, format_version, actor_kind, actor_key, actor_key_basis, at,
		source, statement, state, rounds, re_decompositions, tier, tier_policy_version, project_id,
		intended_effect, evidence, deadline, constraint_id, sent_back_by, outcome)
		values ($1, '` + intent.FormatVersion + `', 'human', 'person:owner', 'claimed', $2,
		$3, $4, $5, $6, 0, $7, $8, '', $9, $10, $11, '', $12, $13)`
	for _, refused := range []struct {
		name                                    string
		source, statement, state                string
		rounds, tier                            int
		tierVersion, effect, evidence, deadline string
		sentBackBy, outcome                     string
		constraint                              string
	}{
		{"a source outside the three", "weather", "anything", "unrefined", 0, 0, "", "", "", "", "", "", "source_known"},
		{"an empty statement", "owner", "", "unrefined", 0, 0, "", "", "", "", "", "", "statement_present"},
		{"a state outside the six", "owner", "anything", "done", 0, 0, "", "", "", "", "", "", "state_known"},
		{"negative rounds", "owner", "anything", "unrefined", -1, 0, "", "", "", "", "", "", "rounds_not_negative"},
		{"a negative tier", "owner", "anything", "unrefined", 0, -1, "pv_1", "", "", "", "", "", "tier_not_negative"},
		{"a tier with no policy version", "owner", "anything", "unrefined", 0, 2, "", "", "", "", "", "",
			"tier_and_its_policy_version_together"},
		{"a detector's intent with no evidence", "detector", "anything", "unrefined", 0, 0, "", "", "", "", "", "",
			"evidence_on_the_factorys_own"},
		{"evidence on a request", "owner", "anything", "unrefined", 0, 0, "", "", `{"service_id":"sv_1"}`, "", "", "",
			"evidence_on_the_factorys_own"},
		{"an intended effect on the factory's own", "detector", "anything", "unrefined", 0, 0, "", "who it is for",
			`{"service_id":"sv_1"}`, "", "", "", "intended_effect_not_on_the_factorys_own"},
		{"an outcome on the factory's own", "detector", "anything", "unrefined", 0, 0, "", "",
			`{"service_id":"sv_1"}`, "", "", "the effect was had", "outcome_not_on_the_factorys_own"},
		{"a deadline outside the layout", "owner", "anything", "unrefined", 0, 0, "", "", "", "tomorrow", "", "",
			"deadline_is_time_layout"},
		{"a cause outside the four", "owner", "anything", "unrefined", 0, 0, "", "", "", "", "somebody", "",
			"sent_back_by_known"},
	} {
		_, err := pool.Exec(ctx, insertIntent,
			record.NewID(intent.IDPrefix), record.Now(),
			refused.source, refused.statement, refused.state, refused.rounds,
			refused.tier, refused.tierVersion, refused.effect, refused.evidence, refused.deadline,
			refused.sentBackBy, refused.outcome)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	insertQuestion := `insert into intent_question (id, format_version, actor_kind, actor_key, actor_key_basis, at, intent_id, round, question, answer, answered_at)
		values ($1, '` + intent.FormatVersionQuestion + `', 'human', 'person:owner', 'claimed', $2, $3, $4, $5, $6, $7)`
	for _, refused := range []struct {
		name       string
		round      int
		question   string
		answer     string
		answeredAt string
		constraint string
	}{
		{"a round below one", 0, "anything?", "", "", "round_positive"},
		{"an empty question", 1, "", "", "", "question_present"},
		{"an answer with no time", 1, "anything?", "yes", "", "answered_together"},
		{"a time outside the layout", 1, "anything?", "yes", "yesterday", "answered_together"},
		{"a time on an empty answer", 1, "anything?", "", record.Now(), "answered_together"},
	} {
		_, err := pool.Exec(ctx, insertQuestion,
			record.NewID(intent.QuestionIDPrefix), record.Now(), taken.ID,
			refused.round, refused.question, refused.answer, refused.answeredAt)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	// An empty link, at one of this package's link columns: the store refuses
	// it around the writer too.
	if _, err := pool.Exec(ctx, insertQuestion,
		record.NewID(intent.QuestionIDPrefix), record.Now(), "", 1, "anything?", "", "",
	); err == nil || !strings.Contains(err.Error(), "intent_id_present") {
		t.Errorf("inserting a question naming no intent = %v, want a violation of intent_id_present", err)
	}
}
