package reportstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// RatePeriod is what a rate is counted over. The two rates are authored as a
// number of reports and the design names no period, so the store fixes one:
// an hour is coarse enough that what either rate bounds is arrival and never
// any individual, which is what keeps the opaque key from becoming a way to
// rate a person.
const RatePeriod = time.Hour

// HarmMarkedShare is the share of each rate reserved for a report marking
// harm, so a surge drops an ordinary report before it drops a marked one. It
// is fixed and not authored, so it costs nothing beyond the rate it divides.
//
// SourceShare is the share of each rate one source key may spend. It is fixed
// for the same reason and bounds what a source can do with the mark: nothing
// stops a false one, so a source that marks every report spends its own share
// of both rates and no more. A quarter leaves four sources able to fill a
// rate between them, and a fresh session is a fresh key, so what it bounds is
// one session.
const (
	HarmMarkedShare = 0.25
	SourceShare     = 0.25
)

// Submit writes one report, or refuses it and counts the refusal. It is the
// whole of arrival: the deploy the token's digest finds is what says which
// service and which environment the report is against, and nothing the
// submission says decides that.
//
// A submission naming no deploy this store knows is refused and counted on
// the whole channel and never on a service, so a safeguard narrowing one
// service's rate is not evaded by submitting under another service's name.
//
// collectedAt is when the way in collected the report. It is the caller's
// instant rather than this store's, because it is when the words were given,
// and it is what every rate is counted over.
func (s *Store) Submit(ctx context.Context, sub Submission, collectedAt time.Time) (Result, error) {
	// The token and the identity are what a submission carries first, under a
	// shape no release moves, so they are read before the shape is decided.
	if sub.Token == "" {
		return Result{}, ErrTokenEmpty
	}
	if sub.ShippedBundleIdentity == "" {
		return Result{}, ErrIdentityEmpty
	}

	found, deploy, err := s.deploy(ctx, sub.Token)
	if err != nil {
		return Result{}, err
	}
	if !found {
		if err := s.countRefusal(ctx, ""); err != nil {
			return Result{}, err
		}
		return Result{Refusal: RefusedNoDeploy}, nil
	}
	if !slices.Contains(Shapes, sub.Shape) {
		// A submission of a shape this store does not read is itself a
		// refusal, and the counter counts how many and never which bound
		// refused one: the reason is on the result the way in renders.
		if err := s.countRefusal(ctx, deploy.ServiceID); err != nil {
			return Result{}, err
		}
		return Result{Refusal: RefusedShape}, nil
	}

	// The rest of the submission is read under the shape that release wrote,
	// so it is checked only once the shape is one this store reads.
	if !slices.Contains(Kinds, sub.Kind) {
		return Result{}, fmt.Errorf("%w: %q", ErrKindUnknown, sub.Kind)
	}
	if sub.Text == "" {
		return Result{}, ErrTextEmpty
	}

	at := record.FormatTime(collectedAt)
	refusal, err := s.overARate(ctx, sub, deploy.ServiceID, at)
	if err != nil {
		return Result{}, err
	}
	if refusal != "" {
		if err := s.countRefusal(ctx, deploy.ServiceID); err != nil {
			return Result{}, err
		}
		return Result{Refusal: refusal}, nil
	}

	report := Report{
		ID:                    record.NewID(ReportIDPrefix),
		CollectedAt:           at,
		ShippedBundleIdentity: sub.ShippedBundleIdentity,
		DeployID:              deploy.ID,
		ServiceID:             deploy.ServiceID,
		EnvironmentID:         deploy.EnvironmentID,
		Kind:                  sub.Kind,
		Text:                  sub.Text,
		HarmMarked:            sub.HarmMarked,
		SourceKey:             sub.SourceKey,
		NoticeID:              sub.NoticeID,
	}
	_, err = s.pool.Exec(ctx, `insert into `+ReportTable+`
		(id, format_version, collected_at, shipped_bundle_identity, deploy_id, service_id, environment_id,
		 kind, text, harm_marked, source_key, notice_id, intent_id, admitted_at)
		values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, '', '')`,
		report.ID, FormatVersionReport, report.CollectedAt, report.ShippedBundleIdentity,
		report.DeployID, report.ServiceID, report.EnvironmentID, string(report.Kind), report.Text,
		report.HarmMarked, report.SourceKey, report.NoticeID)
	if err != nil {
		return Result{}, fmt.Errorf("reportstore: writing a report against %s: %w", report.ServiceID, err)
	}
	return Result{Accepted: true, Report: report}, nil
}

// deploy is the deploy record the way-in token's digest finds.
func (s *Store) deploy(ctx context.Context, token string) (bool, Deploy, error) {
	d, found, err := s.deploys.ByWayInTokenDigest(ctx, wayInTokenDigest(token))
	if err != nil {
		return false, Deploy{}, fmt.Errorf("reportstore: resolving the way-in token's deploy: %w", err)
	}
	return found, d, nil
}

// wayInTokenDigest is what the deployer wrote on the deploy record at the
// deploy that placed the way in: SHA-256 over the token, hexadecimal. The two
// lines are package deploy's, duplicated rather than imported — the report
// store is a second database and importing across it is what the interfaces
// here exist to avoid — and both spellings are one search away from each
// other.
func wayInTokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// overARate is the refusal the two rates make, and empty where the submission
// is under both. The channel is decided before the service, the wider bound
// first, and each rate bounds the source that submitted as well as the
// arrival as a whole.
func (s *Store) overARate(ctx context.Context, sub Submission, serviceID, at string) (Refusal, error) {
	channel, err := s.settings.ChannelRate(ctx)
	if err != nil {
		return "", fmt.Errorf("reportstore: reading the channel's rate: %w", err)
	}
	service, err := s.settings.ServiceRate(ctx, serviceID)
	if err != nil {
		return "", fmt.Errorf("reportstore: reading the rate of %s: %w", serviceID, err)
	}
	if !channel.Authored && !service.Authored {
		return "", nil
	}

	collected, err := time.Parse(record.TimeLayout, at)
	if err != nil {
		return "", fmt.Errorf("reportstore: reading the instant %q the report was collected: %w", at, err)
	}
	counted, err := s.arrivalsSince(ctx, record.FormatTime(collected.Add(-RatePeriod)), serviceID, sub.SourceKey)
	if err != nil {
		return "", err
	}

	if channel.Authored {
		if counted.channel >= allowance(channel.Reports, sub.HarmMarked) {
			return RefusedChannelRate, nil
		}
		if sub.SourceKey != "" && counted.sourceOnChannel >= sourceAllowance(channel.Reports) {
			return RefusedSourceRate, nil
		}
	}
	if service.Authored {
		if counted.service >= allowance(service.Reports, sub.HarmMarked) {
			return RefusedServiceRate, nil
		}
		if sub.SourceKey != "" && counted.sourceOnService >= sourceAllowance(service.Reports) {
			return RefusedSourceRate, nil
		}
	}
	return "", nil
}

// arrivals is what one period holds, in the four counts the two rates and the
// source's share of each are decided against.
type arrivals struct {
	channel         int64
	service         int64
	sourceOnChannel int64
	sourceOnService int64
}

// arrivalsSince counts the reports collected at or after since. The counts
// are made over the reports themselves and not kept as counters: what a rate
// admits is a row, and only what it refuses has no row to count.
func (s *Store) arrivalsSince(ctx context.Context, since, serviceID, sourceKey string) (arrivals, error) {
	var a arrivals
	err := s.pool.QueryRow(ctx, `select count(*),
		count(*) filter (where service_id = $2),
		count(*) filter (where $3 <> '' and source_key = $3),
		count(*) filter (where $3 <> '' and source_key = $3 and service_id = $2)
		from `+ReportTable+` where collected_at >= $1`,
		since, serviceID, sourceKey).Scan(&a.channel, &a.service, &a.sourceOnChannel, &a.sourceOnService)
	if err != nil {
		return arrivals{}, fmt.Errorf("reportstore: counting what arrived since %s: %w", since, err)
	}
	return a, nil
}

// allowance is how much of rate a report of this kind may take: the whole of
// it where the report marks harm, and the rest where it does not — so the
// store refuses an unmarked report once the ordinary share is spent and a
// marked one only once the whole rate is. A rate of zero admits neither,
// which is what closes the channel or one service's way in.
func allowance(rate int64, harmMarked bool) int64 {
	if harmMarked {
		return rate
	}
	return rate - reserved(rate)
}

// reserved is the part of a rate no unmarked report may take. A rate above
// zero reserves at least one report, so the smallest rate an owner can author
// still leaves a marked report somewhere to arrive.
func reserved(rate int64) int64 {
	if rate <= 0 {
		return 0
	}
	if n := int64(float64(rate) * HarmMarkedShare); n > 1 {
		return n
	}
	return 1
}

// sourceAllowance is how much of a rate one source key may take. A rate above
// zero leaves a source at least one report, so a rate too small to divide
// bounds a source by the rate itself rather than closing the channel to
// anything carrying a key.
func sourceAllowance(rate int64) int64 {
	if rate <= 0 {
		return 0
	}
	if n := int64(float64(rate) * SourceShare); n > 1 {
		return n
	}
	return 1
}

// countRefusal counts one refusal over the whole channel, and on the service
// as well where the store knows which one. A submission naming no deploy
// names no service, and is counted on the channel alone.
func (s *Store) countRefusal(ctx context.Context, serviceID string) error {
	if err := s.bumpRefusals(ctx, ""); err != nil {
		return err
	}
	if serviceID == "" {
		return nil
	}
	return s.bumpRefusals(ctx, serviceID)
}

func (s *Store) bumpRefusals(ctx context.Context, serviceID string) error {
	_, err := s.pool.Exec(ctx, `insert into `+CounterTable+` (service_id, refusals)
		values ($1, 1)
		on conflict (service_id) do update set refusals = `+CounterTable+`.refusals + 1`, serviceID)
	if err != nil {
		return fmt.Errorf("reportstore: counting a refusal against %q: %w", serviceID, err)
	}
	return nil
}
