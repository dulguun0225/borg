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
	for n, statement := range intent.DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatalf("applying intent statement %d: %v", n+1, err)
		}
	}
	return ctx, pool, intent.NewIntake(pool)
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

var owner = record.Actor{Kind: record.KindHuman, Name: "owner"}
var intake = record.Actor{Kind: record.KindComponent, Name: "intake"}

func TestTakeInStartsUnrefined(t *testing.T) {
	ctx, pool, in := newIntake(t)

	taken, err := in.TakeIn(ctx, owner, intent.SourceOwner, "checkout should retry a failed charge once")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if taken.State != intent.StateUnrefined || taken.Rounds != 0 {
		t.Errorf("a new intent is %s with %d rounds, want unrefined with 0", taken.State, taken.Rounds)
	}
	if taken.Source != intent.SourceOwner {
		t.Errorf("the intent's source is %s, want owner", taken.Source)
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

	if _, err := in.TakeIn(ctx, owner, intent.Source("weather"), "anything"); !errors.Is(err, intent.ErrSourceUnknown) {
		t.Errorf("TakeIn with source weather = %v, want ErrSourceUnknown", err)
	}
	if _, err := in.TakeIn(ctx, owner, intent.SourceOwner, ""); !errors.Is(err, intent.ErrStatementEmpty) {
		t.Errorf("TakeIn with no statement = %v, want ErrStatementEmpty", err)
	}
	if _, err := in.TakeIn(ctx, record.Actor{}, intent.SourceOwner, "anything"); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("TakeIn with no actor = %v, want record.ErrKindUnknown", err)
	}
}

func TestAskStartsARound(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken, err := in.TakeIn(ctx, owner, intent.SourceOwner, "checkout should retry")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}

	first, err := in.Ask(ctx, intake, taken.ID, "Retry against which payment provider?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	if first.Round != 1 {
		t.Errorf("the first question is round %d, want 1", first.Round)
	}
	if first.Answered() || first.Answer != "" || first.AnsweredAt != "" {
		t.Errorf("a new question reads as answered: %+v", first)
	}

	second, err := in.Ask(ctx, intake, taken.ID, "Once per charge or once per session?")
	if err != nil {
		t.Fatalf("Ask again: %v", err)
	}
	if second.Round != 2 {
		t.Errorf("the second question is round %d, want 2", second.Round)
	}

	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Rounds != 2 {
		t.Errorf("the intent counts %d rounds, want 2", read.Rounds)
	}

	questions, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(questions) != 2 || questions[0].ID != first.ID || questions[1].ID != second.ID {
		t.Errorf("Questions = %+v, want the two asked in round order", questions)
	}

	if _, err := in.Ask(ctx, intake, "in_missing", "anything?"); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("Ask on a missing intent = %v, want ErrIntentNotFound", err)
	}
	if _, err := in.Ask(ctx, intake, taken.ID, ""); !errors.Is(err, intent.ErrQuestionEmpty) {
		t.Errorf("Ask with no question = %v, want ErrQuestionEmpty", err)
	}
	// An empty link names nothing, and the writer refuses it the way it
	// refuses every other required field. record's doc.go states what a link
	// is checked for.
	if _, err := in.Ask(ctx, intake, "", "anything?"); !errors.Is(err, intent.ErrIntentIDEmpty) {
		t.Errorf("Ask naming no intent = %v, want ErrIntentIDEmpty", err)
	}
}

func TestAnswerIsWriteOnce(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken, err := in.TakeIn(ctx, owner, intent.SourceOwner, "checkout should retry")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Retry against which payment provider?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	answered, err := in.Answer(ctx, owner, asked.ID, "The primary one only.")
	if err != nil {
		t.Fatalf("Answer: %v", err)
	}
	if answered.Answer != "The primary one only." || !answered.Answered() {
		t.Errorf("the answer was not written: %+v", answered)
	}
	if _, err := time.Parse(record.TimeLayout, answered.AnsweredAt); err != nil {
		t.Errorf("answered_at %q: %v", answered.AnsweredAt, err)
	}
	// The row keeps the actor and the time of the ask.
	if answered.Actor != asked.Actor || answered.At != asked.At {
		t.Errorf("the answer rewrote the ask's actor or time: %+v", answered)
	}

	questions, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(questions) != 1 || questions[0] != answered {
		t.Errorf("Questions = %+v, want the answered question, %+v", questions, answered)
	}

	if _, err := in.Answer(ctx, owner, asked.ID, "No, both."); !errors.Is(err, intent.ErrAlreadyAnswered) {
		t.Errorf("Answer on an answered question = %v, want ErrAlreadyAnswered", err)
	}
	if _, err := in.Answer(ctx, owner, "q_missing", "anything"); !errors.Is(err, intent.ErrQuestionNotFound) {
		t.Errorf("Answer on a missing question = %v, want ErrQuestionNotFound", err)
	}
}

// TestAnEmptyAnswerIsRefused is the one write-once field a human types. An
// empty answer would stamp the question answered with nothing in it, and the
// retry after it is ErrAlreadyAnswered forever, so it is refused before it is
// written.
func TestAnEmptyAnswerIsRefused(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken, err := in.TakeIn(ctx, owner, intent.SourceOwner, "checkout should retry")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Retry against which payment provider?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}

	if _, err := in.Answer(ctx, owner, asked.ID, ""); !errors.Is(err, intent.ErrAnswerEmpty) {
		t.Errorf("Answer with no answer = %v, want ErrAnswerEmpty", err)
	}
	// The refusal left the question unanswered, so the round is not spent.
	read, err := intent.Questions(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Questions: %v", err)
	}
	if len(read) != 1 || read[0].Answered() {
		t.Errorf("Questions = %+v, want the one question still unanswered", read)
	}
	if _, err := in.Answer(ctx, owner, asked.ID, "The primary one only."); err != nil {
		t.Errorf("Answer after the refusal: %v, want the question still answerable", err)
	}
}

func TestMarkRefinedIsOneTransition(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken, err := in.TakeIn(ctx, owner, intent.SourceOwner, "checkout should retry")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}

	if err := in.MarkRefined(ctx, intake, taken.ID); err != nil {
		t.Fatalf("MarkRefined: %v", err)
	}
	read, err := intent.Get(ctx, pool, taken.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.State != intent.StateRefined {
		t.Errorf("the intent is %s, want refined", read.State)
	}

	if err := in.MarkRefined(ctx, intake, taken.ID); !errors.Is(err, intent.ErrNotUnrefined) {
		t.Errorf("MarkRefined on a refined intent = %v, want ErrNotUnrefined", err)
	}
	if err := in.MarkRefined(ctx, intake, "in_missing"); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("MarkRefined on a missing intent = %v, want ErrIntentNotFound", err)
	}
}

// TestTheStoreRefusesAroundTheWriter inserts and updates by raw SQL, so what
// it exercises is the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken, err := in.TakeIn(ctx, owner, intent.SourceOwner, "checkout should retry")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}

	insertIntent := `insert into intent (id, actor_kind, actor_name, at, source, statement, state, rounds, recuts)
		values ($1, 'human', 'owner', $2, $3, $4, $5, $6, 0)`
	for _, refused := range []struct {
		name       string
		source     string
		statement  string
		state      string
		rounds     int
		constraint string
	}{
		{"a source outside the three", "weather", "anything", "unrefined", 0, "source_known"},
		{"an empty statement", "owner", "", "unrefined", 0, "statement_present"},
		{"a state outside the three", "owner", "anything", "done", 0, "state_known"},
		{"negative rounds", "owner", "anything", "unrefined", -1, "rounds_not_negative"},
	} {
		_, err := pool.Exec(ctx, insertIntent,
			record.NewID(intent.IDPrefix), record.Now(),
			refused.source, refused.statement, refused.state, refused.rounds)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}

	insertQuestion := `insert into intent_question (id, actor_kind, actor_name, at, intent_id, round, question, answer, answered_at)
		values ($1, 'human', 'owner', $2, $3, $4, $5, $6, $7)`
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

	// An empty link, at this package's one link column: the store refuses it
	// around the writer too.
	if _, err := pool.Exec(ctx, insertQuestion,
		record.NewID(intent.QuestionIDPrefix), record.Now(), "", 1, "anything?", "", "",
	); err == nil || !strings.Contains(err.Error(), "intent_id_present") {
		t.Errorf("inserting a question naming no intent = %v, want a violation of intent_id_present", err)
	}
}

// TestTheRecutCountIsAFieldOfItsOwnBesideTheRounds: both are counted against the same
// attempt bound and both live on the intent, and they are two fields because they are
// two stretches of work — an owner answering an escalated interview clears one alone.
func TestTheRecutCountIsAFieldOfItsOwnBesideTheRounds(t *testing.T) {
	ctx, pool, intake := newIntake(t)

	in, err := intake.TakeIn(ctx, owner, intent.SourceOwner, "a request that is cut wrong twice")
	if err != nil {
		t.Fatalf("TakeIn: %v", err)
	}
	if in.Recuts != 0 {
		t.Fatalf("an intent arrives with %d re-cuts", in.Recuts)
	}

	// One round of the interview, which must not move the re-cut count.
	if _, err := intake.Ask(ctx, owner, in.ID, "which service?"); err != nil {
		t.Fatalf("Ask: %v", err)
	}
	read, err := intent.Get(ctx, pool, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Rounds != 1 || read.Recuts != 0 {
		t.Fatalf("after one round the intent stands at %d rounds and %d re-cuts", read.Rounds, read.Recuts)
	}

	for want := 1; want <= 2; want++ {
		reached, err := intake.CountRecut(ctx, owner, in.ID)
		if err != nil {
			t.Fatalf("CountRecut: %v", err)
		}
		if reached != want {
			t.Fatalf("the re-cut count reached %d, want %d", reached, want)
		}
	}
	read, err = intent.Get(ctx, pool, in.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Rounds != 1 || read.Recuts != 2 {
		t.Fatalf("the intent stands at %d rounds and %d re-cuts, and one field would have spent the other's budget",
			read.Rounds, read.Recuts)
	}
	if _, err := intake.CountRecut(ctx, owner, "in_nothing"); !errors.Is(err, intent.ErrIntentNotFound) {
		t.Errorf("counting a re-cut on an intent that does not exist = %v", err)
	}
}
