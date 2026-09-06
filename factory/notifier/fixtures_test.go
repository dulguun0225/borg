// fixtures_test.go is what db_test.go and driftpass_test.go share: the
// notifier composed over a recorder in place of the three channels, the
// People writer that appends no policy version, and the small reads a test
// makes directly against the log and against this package's own delivery
// table. Splitting it out of db_test.go is what keeps that file, and
// driftpass_test.go, under the line bound with their own tests read
// together.
package notifier_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/notifier"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// theOwner is who a page widens to. The notifier is composed with the
// identifier, because the design gives the owner no record.
const theOwner = "owner"

// testActor is who this test's own reads and writes around the notifier are made
// as, where the write is not the notifier's own.
var testActor = record.Actor{Kind: record.KindComponent, Key: "test", Basis: record.BasisClaimed}

// testReading is the same test actor as a principal, which is what a read of
// the log takes.
var testReading = principal.OfComponent("test")

// theHumanOwner is the owner as a human actor, for writes the People declaration
// takes.
var theHumanOwner = record.Actor{Kind: record.KindHuman, Key: theOwner, Basis: record.BasisClaimed}

// recorder is a [notifier.Deliverer] that reaches nothing and keeps what it was
// handed, which is what says a delivery happened on a channel that writes no log
// record of its own.
type recorder struct {
	delivered []notifier.Delivery
	refuse    error
}

func (r *recorder) Deliver(_ context.Context, d notifier.Delivery) error {
	if r.refuse != nil {
		return r.refuse
	}
	r.delivered = append(r.delivered, d)
	return nil
}

// on is how many deliveries went out on one channel.
func (r *recorder) on(channel notifier.Channel) int {
	n := 0
	for _, d := range r.delivered {
		if d.Channel == channel {
			n++
		}
	}
	return n
}

// newNotifier gives a test a schema of its own with the whole factory schema
// applied, a recorder in place of the three channels, and the notifier over both.
func newNotifier(t *testing.T) (context.Context, *pgxpool.Pool, lease.Token, *notifier.Notifier, *recorder) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "notifier_" + hex.EncodeToString(suffix[:])

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
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("acquiring the lease: %v", err)
	}

	channels := &recorder{}
	n, err := notifier.New(pool, decisionlog.NewWriter(pool, token), token, channels, theOwner)
	if err != nil {
		t.Fatalf("composing the notifier: %v", err)
	}
	return ctx, pool, token, n, channels
}

// peopleWriter is the People writer this test declares holdings through,
// appending no policy version — a nil *policy.Factory, which is a factory
// composed with no policy writer.
func peopleWriter(pool *pgxpool.Pool, token lease.Token) *people.Writer {
	return people.NewWriter(pool, token, (*policy.Factory)(nil))
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the writers' statements resolves there.
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

// readLog is every row in the log, read as [testActor] through a reader of the
// test's own — a read this test wants and not one the notifier makes on its
// behalf.
func readLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) []decisionlog.Row {
	t.Helper()
	rows, err := decisionlog.NewReader(pool, token).Read(ctx, testReading)
	if err != nil {
		t.Fatalf("reading the log: %v", err)
	}
	return rows
}

// verifyLog fails the test if the chain does not verify, as [testActor].
func verifyLog(t *testing.T, ctx context.Context, pool *pgxpool.Pool, token lease.Token) {
	t.Helper()
	if err := decisionlog.NewReader(pool, token).Verify(ctx, testReading); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// deliveryRow reads back one row of [notifier.DeliveryTable] directly: this
// package exposes no reader of its own delivery record yet, and a direct
// select is the test's own business rather than this package's public API.
func deliveryRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, rowID string, channel notifier.Channel) (string, bool, bool) {
	t.Helper()
	var recipient string
	var accepted bool
	err := pool.QueryRow(ctx, `select recipient_key, transport_accepted from `+notifier.DeliveryTable+`
		where row_id = $1 and channel = $2`, rowID, string(channel)).Scan(&recipient, &accepted)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, false
	}
	if err != nil {
		t.Fatalf("reading the delivery record for %s on %s: %v", rowID, channel, err)
	}
	return recipient, accepted, true
}
