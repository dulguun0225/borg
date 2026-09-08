package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/principal"
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
}

var _ wayin.Store = (*reportChannel)(nil)

// openReportStore opens the report store, applies its schema, and composes the
// channel over it. It returns what closes the store's pool.
//
// erasureList is where the erasure list lives on this host. Nothing writes to
// it until the redaction pass exists; the store is the one writer of it, so
// the path is chosen here and by nobody else.
func openReportStore(ctx context.Context, url, erasureList string,
	pool *pgxpool.Pool) (*reportChannel, func(), error) {
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
		readEventsOfAReport{},
		redactionsOverReports{})
	return &reportChannel{store: store, pool: pool}, reports.Close, nil
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
// appends before it answers with a report's words. It appends none yet: what
// appends one is package decisionlog's own read event, unexported until the
// erasure step exports it, and what a read event makes answerable — who had
// already read words a redaction later destroyed — is that step's too.
type readEventsOfAReport struct{}

func (readEventsOfAReport) Append(context.Context, principal.Principal, string) error { return nil }

// redactionsOverReports is [reportstore.Redactions]. It answers with none:
// the redaction record is the erasure step's, and until it exists there is no
// redaction to serve a report through or to destroy the spans of.
type redactionsOverReports struct{}

func (redactionsOverReports) OverReports(context.Context) ([]reportstore.Redaction, error) {
	return nil, nil
}

func (redactionsOverReports) ForReport(context.Context, string) ([]reportstore.Redaction, error) {
	return nil, nil
}

// wayInAddressOn is the address the way in inside a deployed service posts to:
// this process's own port, on the loopback address. The deploy targets this
// platform has are directories on this host and the processes they start are
// this host's, so what a started process dials is this process where it is.
func wayInAddressOn(port int) string {
	return fmt.Sprintf("http://127.0.0.1:%d", port)
}
