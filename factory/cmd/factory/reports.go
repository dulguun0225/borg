package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/redaction"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/wayin"
)

// The report store as this process composes it: the store over its own
// database, the five interfaces it reaches the factory's graph through, and
// the [wayin.Store] the entrance is served over. The report store is a second
// database and imports no record package, so every fact it needs about the
// graph arrives across these five seams.
//
// Only serve composes this. It is the process that serves the entrance, and a
// subcommand that makes one pass and exits has nothing to answer a submission
// with.

// reportChannel is what the entrance reaches: the store the report is written
// into, and the graph the notice is read from. It writes no record of its own
// and holds nothing about a session.
type reportChannel struct {
	store *reportstore.Store
	// pool is the factory's own store, read here for the deploy a way-in
	// token names and the notice in force over the project that deploy's
	// service lies in. The store's own pool is behind store.
	pool *pgxpool.Pool

	// mu guards groupNow, which newGrouper sets once and Submit reads on
	// every accepted report.
	mu sync.Mutex
	// groupNow is the composition's own pass, wired in by newGrouper once the
	// path that owns it exists: serve opens the report store before it
	// composes the path that reads it, so this channel is built before the
	// pass is. It is nil until that wiring runs, and in every composition
	// that opens a report store but composes no grouper, which is every one
	// but the process that serves the entrance — a report Submit takes
	// before then, or through such a composition, is left for the periodic
	// pass to find.
	groupNow func(ctx context.Context) (bool, error)
}

var _ wayin.Store = (*reportChannel)(nil)

// theChannel is the channel [openReportStore] most recently composed, held so
// that [newGrouper] — built a moment later, on the same goroutine, by the
// same call to serve — can hand it the pass it just built. Only serve opens a
// report store, and it opens at most one, so the single slot names the one
// channel there ever is to wire.
var theChannel *reportChannel

// openReportStore opens the report store, applies its schema, and composes the
// channel over it. It returns what closes the store's pool.
//
// erasureList is where the erasure list lives on this host. The store is the
// one writer of it — an erasure at Factory and People's deletion of a mapping
// both append through it — so the path is chosen here and by nobody else.
//
// token is the lease this process holds, carried by the reader of the decision
// log the store appends its read events through: a read of a report's words is
// a write of that log, so it is fenced the way every write this composition
// makes is.
func openReportStore(ctx context.Context, url, erasureList string,
	pool *pgxpool.Pool, token lease.Token) (*reportChannel, func(), error) {
	reports, err := reportstore.Open(ctx, url)
	if err != nil {
		return nil, func() {}, err
	}
	if err := reportstore.Apply(ctx, reports); err != nil {
		reports.Close()
		return nil, func() {}, err
	}
	store := reportstore.NewStore(reports, erasureList,
		deploysTheTokenNames{pool: pool},
		reportRatesAnOwnerAuthored{pool: pool},
		holdsOverAService{pool: pool},
		readEventsOfAReport{log: decisionlog.NewReader(pool, token)},
		redactionsOverReports{pool: pool})
	channel := &reportChannel{store: store, pool: pool}
	theChannel = channel
	return channel, reports.Close, nil
}

// wireGrouper hands this channel the composition's own pass, so that Submit
// can hand each accepted report to it at once instead of leaving every one
// for the periodic tick. newGrouper calls it once, after the path that owns
// the pass is composed.
func (c *reportChannel) wireGrouper(groupNow func(ctx context.Context) (bool, error)) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.groupNow = groupNow
}

// handOffToTheGrouper hands the report just accepted to the grouper's own
// pass at once: the first of a group raises its intent and a later matching
// one attaches without waiting for the periodic tick, which stays only as the
// catch-up a restart needs, for what arrived while the factory was down.
//
// A failure here is not the submission's: the report is already written, and
// the periodic pass reads it again whatever this call could not finish.
func (c *reportChannel) handOffToTheGrouper(ctx context.Context) {
	c.mu.Lock()
	groupNow := c.groupNow
	c.mu.Unlock()
	if groupNow == nil {
		return
	}
	_, _ = groupNow(ctx)
}

// NoticeInForce is the notice in force over the project the way-in token's
// deploy lies in, read at every open so that a notice moves without the
// service building again. A token naming no deploy this factory placed a way
// in at is shown an empty notice, which is what a session is shown where an
// owner authored none: what the open renders says nothing about which deploy
// opened it.
func (c *reportChannel) NoticeInForce(ctx context.Context, token string) (wayin.Notice, error) {
	placed, found, err := deploy.ByWayInTokenDigest(ctx, c.pool, wayInTokenDigest(token))
	if err != nil || !found {
		return wayin.Notice{}, err
	}
	svc, err := service.Get(ctx, c.pool, placed.ServiceID)
	if err != nil {
		return wayin.Notice{}, err
	}
	notice, authored, err := constraint.NoticeInForce(ctx, c.pool, svc.ProjectID)
	if err != nil || !authored {
		return wayin.Notice{}, err
	}
	return wayin.Notice{ID: notice.ID, Text: notice.Statement}, nil
}

// Submit is the store's own arrival, which is where a submission is refused
// or written. Nothing is decided here: the service and the environment the
// report counts against are the deploy record's, resolved inside the store
// from the token, and this crossing carries the fields of the submission and
// no more.
//
// An accepted report is handed to the grouper at once, through
// [reportChannel.handOffToTheGrouper], rather than left for the periodic
// tick: that is what makes arrival the trigger a group's first report raises
// its intent on, and the tick behind it the catch-up a restart needs.
func (c *reportChannel) Submit(ctx context.Context, sub wayin.Submission,
	collectedAt time.Time) (wayin.Result, error) {
	result, err := c.store.Submit(ctx, reportstore.Submission{
		Shape:                 reportstore.Shape(sub.Shape),
		ShippedBundleIdentity: sub.ShippedBundleIdentity,
		Token:                 sub.Token,
		Kind:                  reportstore.Kind(sub.Kind),
		Text:                  sub.Text,
		HarmMarked:            sub.HarmMarked,
		SourceKey:             sub.SourceKey,
		NoticeID:              sub.NoticeID,
	}, collectedAt)
	if err != nil {
		return wayin.Result{}, err
	}
	if result.Accepted {
		c.handOffToTheGrouper(ctx)
	}
	return wayin.Result{Accepted: result.Accepted, Refusal: string(result.Refusal)}, nil
}

// wayInTokenDigest is what the deployer wrote on the deploy record at the
// deploy that placed the way in: SHA-256 over the token, hexadecimal. The two
// lines are package deploy's and package reportstore's, duplicated rather than
// shared, and all three spellings are one search away from each other.
func wayInTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// deploysTheTokenNames is [reportstore.Deploys] over the deploy record, which
// is where the deployer wrote the token's digest at the deploy.
type deploysTheTokenNames struct{ pool *pgxpool.Pool }

func (d deploysTheTokenNames) ByWayInTokenDigest(ctx context.Context,
	digest string) (reportstore.Deploy, bool, error) {
	placed, found, err := deploy.ByWayInTokenDigest(ctx, d.pool, digest)
	if err != nil || !found {
		return reportstore.Deploy{}, false, err
	}
	return reportstore.Deploy{
		ID:            placed.ID,
		ServiceID:     placed.ServiceID,
		EnvironmentID: placed.EnvironmentID,
	}, true, nil
}

// reportRatesAnOwnerAuthored is [reportstore.Settings] over the factory-wide
// settings record: the rate over the whole channel is a column of it, the rate
// for one service is a row beside it, and how long a report is kept is a
// second column. An owner who authored none of the three is what absent means
// here — unbounded arrival, and reports kept for the life of the install.
type reportRatesAnOwnerAuthored struct{ pool *pgxpool.Pool }

func (s reportRatesAnOwnerAuthored) ChannelRate(ctx context.Context) (reportstore.Rate, error) {
	settings, err := factorysettings.Get(ctx, s.pool)
	if err != nil {
		return reportstore.Rate{}, err
	}
	return reportstore.Rate{
		Reports:  int64(settings.ReportChannelRate.Number),
		Authored: settings.ReportChannelRate.Present,
	}, nil
}

func (s reportRatesAnOwnerAuthored) ServiceRate(ctx context.Context,
	serviceID string) (reportstore.Rate, error) {
	settings, err := factorysettings.Get(ctx, s.pool)
	if err != nil {
		return reportstore.Rate{}, err
	}
	authored, err := factorysettings.ReportChannelRate(ctx, s.pool, settings.ID, serviceID)
	if err != nil {
		return reportstore.Rate{}, err
	}
	return reportstore.Rate{Reports: int64(authored.Number), Authored: authored.Present}, nil
}

func (s reportRatesAnOwnerAuthored) ReportRetention(ctx context.Context) (time.Duration, bool, error) {
	settings, err := factorysettings.Get(ctx, s.pool)
	if err != nil {
		return 0, false, err
	}
	if !settings.ReportRetentionSeconds.Present {
		return 0, false, nil
	}
	return time.Duration(settings.ReportRetentionSeconds.Number) * time.Second, true, nil
}

// holdsOverAService is [reportstore.Holds] over package legalhold's own read,
// which answers for a hold on the service, on the project it lies in, or on
// the whole factory.
type holdsOverAService struct{ pool *pgxpool.Pool }

func (h holdsOverAService) ReachingService(ctx context.Context, serviceID string) (bool, error) {
	return legalhold.Reaching(ctx, h.pool,
		legalhold.Subject{Kind: legalhold.SubjectService, ID: serviceID})
}

// readEventsOfAReport is [reportstore.ReadEvents], the read event the store
// appends before it answers with a report's words — at Work, under the intent
// a report was grouped into, and at every pass of the grouper. It is the log's
// own tenth shape and not a second kind of record: the words a redaction
// reaches are in a store the log never reads, so the reader that serves them
// appends the event and the log writes it.
type readEventsOfAReport struct{ log *decisionlog.Reader }

// Append appends the read event naming the principal and the report read.
func (r readEventsOfAReport) Append(ctx context.Context, p principal.Principal, read string) error {
	return r.log.AppendReadEvent(ctx, p, read)
}

// redactionsOverReports is [reportstore.Redactions] over the redaction
// record. A redaction is a record of the factory's graph and the report store
// is a second database, so the redactions naming a report are read here and
// handed across rather than their writer reaching into that store: the store
// serves every report through them and destroys what they name on a pass of
// its own.
type redactionsOverReports struct{ pool *pgxpool.Pool }

func (r redactionsOverReports) OverReports(ctx context.Context) ([]reportstore.Redaction, error) {
	over, err := redaction.OverKind(ctx, r.pool, redaction.KindReport)
	if err != nil {
		return nil, err
	}
	return crossedToTheStore(over), nil
}

func (r redactionsOverReports) ForReport(ctx context.Context,
	reportID string) ([]reportstore.Redaction, error) {
	naming, err := redaction.ForTarget(ctx, r.pool,
		redaction.Target{Kind: redaction.KindReport, ID: reportID})
	if err != nil {
		return nil, err
	}
	return crossedToTheStore(naming), nil
}

// crossedToTheStore is the crossing itself: the four fields the store needs to
// serve a report through a redaction and to destroy what it names, and no
// record type of the graph's.
func crossedToTheStore(over []redaction.Redaction) []reportstore.Redaction {
	crossed := make([]reportstore.Redaction, 0, len(over))
	for _, one := range over {
		spans := make([]reportstore.Span, 0, len(one.Spans))
		for _, span := range one.Spans {
			spans = append(spans, reportstore.Span{Start: span.Start, End: span.End})
		}
		crossed = append(crossed, reportstore.Redaction{
			ID: one.ID, ErasureKey: one.ErasureKey, ReportID: one.Target.ID, Spans: spans,
		})
	}
	return crossed
}

// wayInAddressOn is the address the way in inside a deployed service posts to:
// this process's own port, on the loopback address. The deploy targets this
// platform has are directories on this host and the processes they start are
// this host's, so what a started process dials is this process where it is.
func wayInAddressOn(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}
