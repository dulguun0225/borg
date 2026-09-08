// The database tests of this package are in reportstore_test rather than in
// reportstore, the way every record package's are — except here it changes
// nothing in deps.txt: this store is not the factory's, so these tests open
// it through [reportstore.Open] and [reportstore.Apply] rather than through
// package postgres, and the packages this file imports besides reportstore
// itself are the ones reportstore's own line already allows.
//
// None of these tests skips when the database is unreachable. The milestone
// is demonstrated by them running, so an unreachable database fails the run.
package reportstore_test

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/reportstore"
)

// baseURL is the server the test schema is made on. This store's own
// REPORTSTORE_DATABASE_URL has no default on purpose, so the tests name the
// development database the same way package postgres does rather than
// pointing at a store an owner installed. The two lines are duplicated
// instead of imported: a test line for one URL would be an edge for nothing
// else.
func baseURL() string {
	if named := os.Getenv("DATABASE_URL"); named != "" {
		return named
	}
	return "postgres://factory:factory@localhost:5433/factory"
}

// stub is what the composition implements, in one value the tests script:
// the deploy a token's digest finds, the two rates and the retention an owner
// authored, the legal holds, the read events appended, and the redactions
// naming a report.
type stub struct {
	deploys    map[string]reportstore.Deploy
	channel    reportstore.Rate
	services   map[string]reportstore.Rate
	retention  time.Duration
	authored   bool
	held       map[string]bool
	reads      []string
	redactions []reportstore.Redaction
	// listPath is the erasure list the store was handed, which the tests read
	// back to see what a redaction wrote there.
	listPath string
}

func newStub() *stub {
	return &stub{
		deploys:  map[string]reportstore.Deploy{},
		services: map[string]reportstore.Rate{},
		held:     map[string]bool{},
	}
}

// place records the deploy a way-in token was minted at, under the digest the
// deployer writes on the deploy record. The two lines are package deploy's
// own, written out here so that a store computing the digest differently
// finds nothing.
func (s *stub) place(token string, d reportstore.Deploy) {
	sum := sha256.Sum256([]byte(token))
	s.deploys[hex.EncodeToString(sum[:])] = d
}

func (s *stub) ByWayInTokenDigest(_ context.Context, digest string) (reportstore.Deploy, bool, error) {
	d, found := s.deploys[digest]
	return d, found, nil
}

func (s *stub) ChannelRate(context.Context) (reportstore.Rate, error) { return s.channel, nil }

func (s *stub) ServiceRate(_ context.Context, serviceID string) (reportstore.Rate, error) {
	return s.services[serviceID], nil
}

func (s *stub) ReportRetention(context.Context) (time.Duration, bool, error) {
	return s.retention, s.authored, nil
}

func (s *stub) ReachingService(_ context.Context, serviceID string) (bool, error) {
	return s.held[serviceID], nil
}

func (s *stub) Append(_ context.Context, p principal.Principal, read string) error {
	s.reads = append(s.reads, p.Actor.Key+" read "+read)
	return nil
}

func (s *stub) OverReports(context.Context) ([]reportstore.Redaction, error) {
	return s.redactions, nil
}

func (s *stub) ForReport(_ context.Context, reportID string) ([]reportstore.Redaction, error) {
	var naming []reportstore.Redaction
	for _, r := range s.redactions {
		if r.ReportID == reportID {
			naming = append(naming, r)
		}
	}
	return naming, nil
}

// newStore opens a schema of its own on the development server, applies this
// store's DDL into it, and drops the schema at cleanup. The erasure list is a
// file in the test's own directory, the way the composition supplies a path
// on the host.
func newStore(t *testing.T) (context.Context, *pgxpool.Pool, *reportstore.Store, *stub) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "reportstore_" + hex.EncodeToString(suffix[:])

	pool, err := reportstore.Open(ctx, inSchema(t, baseURL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", baseURL(), err)
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
	// The schema is not created here: a fresh store has none, and Apply is
	// what makes it exist.
	if err := reportstore.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}

	s := newStub()
	s.listPath = t.TempDir() + "/erasures"
	return ctx, pool, reportstore.NewStore(pool, s.listPath, s, s, s, s, s), s
}

// inSchema points a connection URL at one schema and nothing else, so every
// unqualified name in the DDL and in the store's statements resolves there.
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

// submission is a complete submission against the deploy the caller placed,
// so a test that needs one does not repeat every field.
func submission(token string) reportstore.Submission {
	return reportstore.Submission{
		Shape:                 reportstore.ShapeSubmission1,
		ShippedBundleIdentity: "0.1.0",
		Token:                 token,
		Kind:                  reportstore.KindBug,
		Text:                  "the button does nothing",
		SourceKey:             "src_a",
	}
}

func deploy() reportstore.Deploy {
	return reportstore.Deploy{
		ID:            record.NewID("dep"),
		ServiceID:     record.NewID("svc"),
		EnvironmentID: record.NewID("env"),
	}
}

// TestURLHasNoDefault: this store is on the path every submission takes, so a
// store nobody named is an error the caller reports and never the factory's
// own database by default.
func TestURLHasNoDefault(t *testing.T) {
	t.Setenv(reportstore.URLEnv, "")
	if _, err := reportstore.URL(); err == nil {
		t.Fatal("URL with the variable unset returned no error, want ErrNoURL")
	}
	t.Setenv(reportstore.URLEnv, "postgres://elsewhere/reports")
	named, err := reportstore.URL()
	if err != nil || named != "postgres://elsewhere/reports" {
		t.Errorf("URL = %q, %v; want what the variable holds", named, err)
	}
}

// TestDDLListsEveryKind fails if [reportstore.Kinds] and the CHECK in the DDL
// stop agreeing, the way every record package holds its own two lists
// together.
func TestDDLListsEveryKind(t *testing.T) {
	statement := reportstore.DDL[0]
	for _, kind := range reportstore.Kinds {
		if !strings.Contains(statement, "'"+string(kind)+"'") {
			t.Errorf("the DDL's kind CHECK does not list %q", kind)
		}
	}
}

// TestAReportIsReadThroughItsRedactionsAndAppendsAReadEvent: the read event
// lands before the words are served, and the words are served through the
// redactions naming the report whether or not the destruction pass has run.
func TestAReportIsReadThroughItsRedactionsAndAppendsAReadEvent(t *testing.T) {
	ctx, _, store, s := newStore(t)
	d := deploy()
	s.place("tok", d)

	written, err := store.Submit(ctx, submission("tok"), time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit = %+v, %v; want an accepted report", written, err)
	}

	who := principal.OfHuman("per_a", record.BasisClaimed)
	read, err := store.Get(ctx, who, written.Report.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Text != "the button does nothing" {
		t.Errorf("Get read %q, want the words as written", read.Text)
	}
	if len(s.reads) != 1 || !strings.Contains(s.reads[0], written.Report.ID) {
		t.Errorf("the read events are %v, want one naming the report", s.reads)
	}

	// The redaction exists and nothing has destroyed anything yet.
	s.redactions = []reportstore.Redaction{{
		ID: "red_a", ReportID: written.Report.ID, Spans: []reportstore.Span{{Start: 4, End: 10}},
	}}
	read, err = store.Get(ctx, who, written.Report.ID)
	if err != nil {
		t.Fatalf("Get after the redaction: %v", err)
	}
	if read.Text != "the xxxxxx does nothing" {
		t.Errorf("Get read %q, want the redacted span served as destroyed", read.Text)
	}
	if len(s.reads) != 2 {
		t.Errorf("the read events are %v, want one per read", s.reads)
	}
}

// TestAReportIsLinkedOrCounted: every report is either linked to the intent
// it was grouped into or counted as ungrouped, the link is written once, and
// grouping removes nothing.
func TestAReportIsLinkedOrCounted(t *testing.T) {
	ctx, _, store, s := newStore(t)
	d := deploy()
	s.place("tok", d)

	written, err := store.Submit(ctx, submission("tok"), time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit = %+v, %v", written, err)
	}
	ungrouped, err := store.Ungrouped(ctx)
	if err != nil || ungrouped != 1 {
		t.Fatalf("Ungrouped = %d, %v; want the arrived report counted", ungrouped, err)
	}

	if err := store.Link(ctx, written.Report.ID, "int_a"); err != nil {
		t.Fatalf("Link: %v", err)
	}
	if err := store.Link(ctx, written.Report.ID, "int_b"); err == nil {
		t.Error("a second Link succeeded; a later report attaches and never rewrites")
	}
	if err := store.Admit(ctx, written.Report.ID); err != nil {
		t.Fatalf("Admit: %v", err)
	}

	read, err := store.Get(ctx, principal.OfComponent("grouper"), written.Report.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.IntentID != "int_a" || read.AdmittedAt == "" || read.Text == "" {
		t.Errorf("the grouped report is %+v, want it marked with the intent, admitted, and kept", read)
	}
	if ungrouped, err = store.Ungrouped(ctx); err != nil || ungrouped != 0 {
		t.Errorf("Ungrouped = %d, %v; want the linked report no longer counted", ungrouped, err)
	}
}

// TestTheGroupsReportsAreReadByIntent: the reports of one intent are the ids
// Work renders under it, oldest first and none of another intent's. The read
// answers ids and never the words, so nothing here appends a read event, and
// each report's own admission is a write of its own — the intent's admission is
// the intent record's and not this store's.
func TestTheGroupsReportsAreReadByIntent(t *testing.T) {
	ctx, _, store, s := newStore(t)
	d := deploy()
	s.place("tok", d)

	collected := time.Now()
	var ids []string
	for n := 0; n < 3; n++ {
		written, err := store.Submit(ctx, submission("tok"), collected.Add(time.Duration(n)*time.Minute))
		if err != nil || !written.Accepted {
			t.Fatalf("Submit %d = %+v, %v", n, written, err)
		}
		ids = append(ids, written.Report.ID)
	}
	// Two of the three are one group; the third is another intent's, so a
	// read by intent that answered with every report would be caught here.
	for _, id := range ids[:2] {
		if err := store.Link(ctx, id, "int_a"); err != nil {
			t.Fatalf("Link %s: %v", id, err)
		}
	}
	if err := store.Link(ctx, ids[2], "int_b"); err != nil {
		t.Fatalf("Link %s: %v", ids[2], err)
	}

	grouped, err := store.ByIntent(ctx, "int_a")
	if err != nil {
		t.Fatalf("ByIntent: %v", err)
	}
	if len(grouped) != 2 || grouped[0] != ids[0] || grouped[1] != ids[1] {
		t.Errorf("ByIntent read %v, want %v oldest first", grouped, ids[:2])
	}
	if len(s.reads) != 0 {
		t.Errorf("the read events are %v, want none: an enumeration serves no words", s.reads)
	}

	if err := store.Admit(ctx, ids[0]); err != nil {
		t.Fatalf("Admit: %v", err)
	}
	for n, id := range ids {
		read, err := store.Get(ctx, principal.OfComponent("grouper"), id)
		if err != nil {
			t.Fatalf("Get %s: %v", id, err)
		}
		if admitted := read.AdmittedAt != ""; admitted != (n == 0) {
			t.Errorf("report %d is admitted %v, want %v: the one admitted and no other", n, admitted, n == 0)
		}
	}
}
