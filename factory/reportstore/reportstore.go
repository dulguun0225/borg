package reportstore

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/principal"
)

var (
	// ErrTokenEmpty is returned by [Store.Submit] for a submission
	// presenting no token. A submission calls as the deploy that placed the
	// way in, so one presenting nothing names no deploy to place it under.
	ErrTokenEmpty = errors.New("reportstore: the submission presents no way-in token")
	// ErrKindUnknown is returned for a submission whose kind is neither
	// [KindBug] nor [KindComplaint].
	ErrKindUnknown = errors.New("reportstore: the kind is neither a bug report nor a complaint")
	// ErrTextEmpty is returned for a submission carrying no words. What the
	// grouper reads is the words, and a report with none is nothing to group.
	ErrTextEmpty = errors.New("reportstore: the submission carries no text")
	// ErrIdentityEmpty is returned for a submission naming no shipped-bundle
	// identity. Every submission names the identity of the release that built
	// the way in, first and under a shape no release moves.
	ErrIdentityEmpty = errors.New("reportstore: the submission names no shipped-bundle identity")
	// ErrNotFound is returned where no report has that id.
	ErrNotFound = errors.New("reportstore: no report has that id")
	// ErrIDEmpty is returned by the writes and the reads that name a report,
	// for a call naming none.
	ErrIDEmpty = errors.New("reportstore: the report id is empty")
)

// Report is one end user's report as it is stored. It names no person:
// there is no actor on this row and no field a person can be recovered from,
// SourceKey included.
type Report struct {
	ID string
	// CollectedAt is when the way in collected the report, in
	// record.TimeLayout. It is the caller's instant and not this store's,
	// because what it says is when the words were given.
	CollectedAt string
	// ShippedBundleIdentity is the release that built the way in that wrote
	// this report, so which way in wrote it is on the row.
	ShippedBundleIdentity string
	// DeployID, ServiceID and EnvironmentID are the deploy record the way-in
	// token's digest found, and never what the submission said.
	DeployID      string
	ServiceID     string
	EnvironmentID string
	Kind          Kind
	// Text is the words the reporter wrote, read through the redactions
	// naming this report on every read that returns it.
	Text string
	// HarmMarked is the one field the reporter sets beside the kind: whether
	// the software is harming a person. Nothing infers it and nothing
	// classifies content to set it.
	HarmMarked bool
	// SourceKey is the opaque key the deployed software supplies where it
	// supplies one, derived from its own session and salted. Nothing joins it
	// to a person and it cannot be added to a report already written.
	SourceKey string
	// NoticeID is the notice in force when the report was collected, and
	// empty where none was.
	NoticeID string
	// IntentID is the intent the report was grouped into, and empty where it
	// is ungrouped.
	IntentID string
	// AdmittedAt is when a human admitted the report at Work, and empty where
	// none has. Only the admission safeguard makes a report wait for it.
	AdmittedAt string
}

// Submission is what a way in presents. Nothing on it decides which service
// the report counts against: Token does, through the deploy record its digest
// finds.
type Submission struct {
	// Shape is how the way in wrote this submission. A shape absent from
	// [Shapes] is one this store does not read, which is counted per service.
	Shape Shape
	// ShippedBundleIdentity is the release that built the way in.
	ShippedBundleIdentity string
	// Token is the way-in token the deployer minted at the deploy that placed
	// this way in. The store holds it for as long as it takes to digest it
	// and stores neither it nor anything derived from it.
	Token      string
	Kind       Kind
	Text       string
	HarmMarked bool
	SourceKey  string
	NoticeID   string
}

// Refusal is why a submission was refused, which the way in renders in the
// session that made it. Each names the bound that made the refusal and
// nothing about the submission.
type Refusal string

const (
	// RefusedNoDeploy is a submission naming no deploy this store knows. It
	// is counted on the whole channel and never on a service.
	RefusedNoDeploy Refusal = "the submission names no deploy this factory placed a way in at"
	// RefusedShape is a submission written under a shape this store does not
	// read, which only a factory returned to an earlier release meets.
	RefusedShape Refusal = "the submission is written under a shape this factory version does not read"
	// RefusedChannelRate is the factory-wide rate spent. Authored to zero it
	// closes the channel.
	RefusedChannelRate Refusal = "the report channel is over the rate an owner authored for the whole factory"
	// RefusedServiceRate is one service's rate spent. Narrowed to zero it
	// closes that service's way in.
	RefusedServiceRate Refusal = "the report channel is over the rate in force for this service"
	// RefusedSourceRate is one source's share of a rate spent. A fresh
	// session is a fresh key, so it bounds a session and no longer.
	RefusedSourceRate Refusal = "this source has spent its share of the report channel's rate"
)

// Result is what one submission did: the report where it was accepted, and
// the refusal where it was not. It is what the way in renders in the session
// that submitted, and it carries no identity and nothing about a person.
type Result struct {
	Accepted bool
	Report   Report
	Refusal  Refusal
}

// Deploy is the deploy record a way-in token's digest found, in the three
// fields this store takes from it. It is this package's own spelling of the
// fields rather than package deploy's record: the report store is a second
// database, and an import would cross that boundary.
type Deploy struct {
	ID            string
	ServiceID     string
	EnvironmentID string
}

// Deploys resolves a way-in token's digest to the deploy that placed that way
// in. The composition implements it over the deploy record, which is where
// the deployer wrote the digest at the deploy.
type Deploys interface {
	// ByWayInTokenDigest is the deploy whose way-in token digest is digest,
	// and false where none is.
	ByWayInTokenDigest(ctx context.Context, digest string) (Deploy, bool, error)
}

// Rate is one of the two rates an owner authored, and whether they authored
// it at all. Absent is unbounded; zero closes what it bounds.
type Rate struct {
	Reports  int64
	Authored bool
}

// Settings is what an owner authored on the factory-wide settings record that
// this store reads: the two rates bounding arrival, and how long a report is
// kept. The composition implements it over package factorysettings — the
// factory-wide rate a column of its record and the per-service one a row
// beside it.
type Settings interface {
	// ChannelRate is the rate over the whole channel.
	ChannelRate(ctx context.Context) (Rate, error)
	// ServiceRate is the rate in force for one service.
	ServiceRate(ctx context.Context, serviceID string) (Rate, error)
	// ReportRetention is how long a report is kept, and false where an owner
	// authored nothing, which is the life of the install.
	ReportRetention(ctx context.Context) (time.Duration, bool, error)
}

// Holds is whether a legal hold stands over a service, which suspends
// retention's removal of that service's reports until the hold is withdrawn.
// The composition implements it over package legalhold's read.
type Holds interface {
	ReachingService(ctx context.Context, serviceID string) (bool, error)
}

// ReadEvents is the read event this store appends before it answers with a
// report's words, which is what makes who had already read them answerable
// after a redaction. The composition implements it over the decision log.
type ReadEvents interface {
	Append(ctx context.Context, p principal.Principal, read string) error
}

// Span is a half-open byte range of a report's text, [Start, End), the unit a
// redaction names and [Store.Redact] destroys.
type Span struct {
	Start, End int
}

// Redaction is one redaction record naming a report, in the fields this store
// needs to destroy what it names. ID is the redaction's own id, which is also
// the key its erasure-list row is written under, so the same redaction
// applied twice appends one row.
type Redaction struct {
	ID       string
	ReportID string
	Spans    []Span
}

// Redactions is every redaction naming a report. The composition implements
// it over the redaction record, which is in the factory's graph: this store
// is a second database, so it is handed the redactions rather than reaching
// across for them.
type Redactions interface {
	// OverReports is every redaction naming a report of this store, oldest
	// first, which is what this store's own destruction pass walks.
	OverReports(ctx context.Context) ([]Redaction, error)
	// ForReport is every redaction naming one report, which is what a read
	// of that report's words is served through before the pass has run.
	ForReport(ctx context.Context, reportID string) ([]Redaction, error)
}

// Store is this package's one writer and the only thing that may remove a
// report. It reaches the factory's graph through the five interfaces its
// caller implements and imports no record package: the store is a second
// database, and every fact it needs from the graph arrives across that seam.
type Store struct {
	pool        *pgxpool.Pool
	erasureList string
	deploys     Deploys
	settings    Settings
	holds       Holds
	events      ReadEvents
	redactions  Redactions
}

// NewStore returns the store over pool. erasureList is the path of the
// erasure list on the host, which this store is the one writer of.
func NewStore(pool *pgxpool.Pool, erasureList string, deploys Deploys, settings Settings,
	holds Holds, events ReadEvents, redactions Redactions) *Store {
	return &Store{
		pool: pool, erasureList: erasureList, deploys: deploys, settings: settings,
		holds: holds, events: events, redactions: redactions,
	}
}
