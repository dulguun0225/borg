package reportstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// ReportTable is the report itself and CounterTable is the two counts the
// store keeps rather than computes, one row per service and one for the
// whole channel.
const (
	ReportTable  = "report"
	CounterTable = "report_counter"
)

// ReportIDPrefix is what [record.NewID] is called with for a report. The
// counter table has no identifier: its rows are counts keyed by service and
// are not records.
const ReportIDPrefix = "rep"

// FormatVersionReport is written into format_version on every report.
const FormatVersionReport = "report/1"

// Shape names how a submission was written. It is not the report row's
// format version: the row's shape is this store's, and a submission's is the
// way in's, shipped inside a deployed service and moving only when that
// service builds again.
type Shape string

// ShapeSubmission1 is the first submission shape any release shipped.
const ShapeSubmission1 Shape = "submission/1"

// Shapes is every shape any release ever shipped. A shape once shipped is
// added to and never changed, so this list only grows: a factory returned to
// an earlier release is the one thing that meets a submission written under a
// shape absent from its own list, and [Store.Submit] counts that per service
// rather than reading it as something else.
var Shapes = []Shape{ShapeSubmission1}

// Kind is what a report is: an end user's bug report or an end user's
// complaint.
type Kind string

const (
	// KindBug is duty 4's report: the software does not do what it says.
	KindBug Kind = "bug"
	// KindComplaint is duty 5's: the software does what it says and that is
	// the problem.
	KindComplaint Kind = "complaint"
)

// Kinds is every kind a report may have. The CHECK in [DDL] lists the same
// two, and TestDDLListsEveryKind fails if the two lists stop agreeing.
var Kinds = []Kind{KindBug, KindComplaint}

// URLEnv is the environment variable that names the report store. It is a
// variable of its own and not the factory's: the report store is sized for
// what a population of end users sends rather than for what a graph joins on,
// so pointing it at a store of its own is one setting.
const URLEnv = "REPORTSTORE_DATABASE_URL"

// ErrNoURL is returned by [URL] where [URLEnv] is not set. There is no
// default: a store the caller did not name is a channel silently pointed at
// the factory's own database, and this store is on the path every submission
// takes, so it fails where it is opened and the caller reports it.
var ErrNoURL = errors.New("reportstore: " + URLEnv + " names no store")

// URL is the store to open: what [URLEnv] holds, or [ErrNoURL].
func URL() (string, error) {
	url := os.Getenv(URLEnv)
	if url == "" {
		return "", ErrNoURL
	}
	return url, nil
}

// Open returns a pool over url and reaches the store once before it returns,
// so an unreachable store is an error here and not at the first submission.
//
// It is this package's own rather than the factory's opener, and doc.go says
// why: the factory's applies every record package's schema, and a store the
// factory applies is a store the factory owns.
func Open(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("reportstore: opening the pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("reportstore: reaching its store: %w", err)
	}
	return pool, nil
}

// Apply creates the report store's schema. It is called by whatever opens
// this store and by nothing in the factory.
//
// The schema itself is created first, named by the connection's search path,
// because a fresh store has none: the URL names a schema the DDL's
// unqualified names resolve in, and nothing but this call ever makes it
// exist. It is the arrangement driftdetector.Apply has.
func Apply(ctx context.Context, pool *pgxpool.Pool) error {
	var path string
	if err := pool.QueryRow(ctx, "show search_path").Scan(&path); err != nil {
		return fmt.Errorf("reportstore: reading the search path: %w", err)
	}
	schema := strings.Trim(strings.TrimSpace(strings.Split(path, ",")[0]), `"`)
	if schema == "" {
		return errors.New("reportstore: the connection names no schema to create in")
	}
	if _, err := pool.Exec(ctx, `create schema if not exists `+pgx.Identifier{schema}.Sanitize()); err != nil {
		return fmt.Errorf("reportstore: creating schema %s: %w", schema, err)
	}
	for n, statement := range DDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return fmt.Errorf("reportstore: applying statement %d: %w", n+1, err)
		}
	}
	return nil
}

// DDL is this package's schema, in the order the statements are applied.
//
// The report row carries no actor and composes neither [record.Columns] nor
// [record.Constraints]: it names no person at all, which doc.go states as the
// departure it is. What it does take from package record is the timestamp
// format, so an instant here reads back the way one in the graph does.
//
// collected_at is the instant the way in collected the report, supplied by
// the caller, which is the one time on the row: the store adds no second one,
// because a report has no author to record a writing by.
//
// deploy_id, service_id and environment_id are the deploy record the token's
// digest found, and never what the submission said. All three are present on
// every row: a report the store could not place is refused and counted, never
// written.
//
// intent_id empty is ungrouped, which is the count Factory reads; admitted_at
// empty is a report no human has admitted, which only the admission safeguard
// makes wait. notice_id empty is that no notice was in force when the report
// arrived, which the row says as much as it says which one was.
//
// The counter table is keyed by service, the empty key being the whole
// channel. It holds counts and not rows, because the record a query would
// count is the write the rate exists to refuse: a row per refusal is the
// unbounded write the bound was placed to prevent. What that costs is that
// neither count can be recomputed — a lost counter is lost.
var DDL = []string{
	`create table if not exists ` + ReportTable + ` (
	id text not null primary key,
	format_version text not null,
	collected_at text not null,
	shipped_bundle_identity text not null,
	deploy_id text not null,
	service_id text not null,
	environment_id text not null,
	kind text not null,
	text text not null,
	harm_marked boolean not null,
	source_key text not null,
	notice_id text not null default '',
	intent_id text not null default '',
	admitted_at text not null default '',
	constraint format_version_present check (format_version <> ''),
	constraint collected_at_is_time_layout check (collected_at ~ '` + record.TimePattern + `'),
	constraint shipped_bundle_identity_present check (shipped_bundle_identity <> ''),
	constraint deploy_id_present check (deploy_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint environment_id_present check (environment_id <> ''),
	constraint kind_known check (kind in ('` + string(KindBug) + `', '` + string(KindComplaint) + `')),
	constraint admitted_at_is_time_layout check (admitted_at = '' or admitted_at ~ '` + record.TimePattern + `')
)`,

	// Every rate is a count over the reports collected since one instant, and
	// the store makes that count on every submission, so the column it reads
	// is indexed. The layout is fixed width and always UTC, so ordering the
	// text orders the instants.
	`create index if not exists report_collected_at on ` + ReportTable + ` (collected_at)`,

	`create table if not exists ` + CounterTable + ` (
	service_id text not null primary key,
	refusals bigint not null default 0,
	unreadable_shape bigint not null default 0,
	constraint counts_not_negative check (refusals >= 0 and unreadable_shape >= 0)
)`,
}
