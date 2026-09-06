package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lastcheck"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// What the deployer writes on records other packages own. Both are the
// deployer's own facts about itself and about what it reached, written through
// the writer of the record they belong to: four fields of the service record,
// and a last check per target and per platform.

// Adopt writes the deployer's four fields on the service record: a target it
// reached, instances the platform can replace, a rollback path, and an emission
// the health monitor can read. The deployer writes them at adoption and at every
// first release, so the four say what the last such deploy found rather than
// what any of them ever found.
//
// It is the deployer's write and not the owner's or decomposition's, which is
// why it is here and not at a caller: the service record has three writers and
// the seam between them is the field.
func Adopt(ctx context.Context, w *Writer, actor record.Actor, serviceID string, found service.Reachability) error {
	return w.inTransaction(ctx, "writing what the deployer found on "+serviceID, func(tx pgx.Tx) error {
		return service.Adopt(ctx, tx, w.token, actor, serviceID, found)
	})
}

// Found is what a deploy learned about a service's reachability from the target
// it reached: whether the target answered at all, whether it reports instances
// the platform can replace, whether an earlier build is there to return to, and
// whether the service emits what the health monitor reads. The deployer assembles
// it from the deploy it just performed and hands it to [Adopt].
//
// The emission is the one of the four this package cannot see: what the health
// monitor reads is behind an interface of its own, so the caller says whether it
// found one. doc.go says which caller is not built.
func Found(running int, rollbackPath, emissionReadable bool) service.Reachability {
	return service.Reachability{
		TargetReached:        true,
		InstancesReplaceable: running > 0,
		RollbackPathPresent:  rollbackPath,
		EmissionReadable:     emissionReadable,
	}
}

// RecordTargetCheck writes the deployer's last check over one target of a
// persistent environment. A rollout advances only while the deployer runs, so a
// target whose deployer last check is past the interval it names, with a further
// pass owed, is what stops a drift-detection exemption standing on a rollout
// that is not advancing.
//
// lastPass is the writer saying this is the last check owed over that target,
// which is what a target leaving the environment gets.
func RecordTargetCheck(ctx context.Context, checks *lastcheck.Writer, actor record.Actor,
	address string, interval time.Duration, lastPass bool, payload string) error {
	if address == "" {
		return fmt.Errorf("%w: the last check names no target", ErrTargetNotFound)
	}
	_, err := checks.Record(ctx, actor, lastcheck.LastCheck{
		Component: lastcheck.ComponentDeployer,
		Subject:   address,
		Interval:  interval,
		LastPass:  lastPass,
		Payload:   payload,
	})
	return err
}

// RecordPlatformCheck writes the deployer's last check over one platform, which
// is one per production environment record: the platform a candidate environment
// is composed on, and what the deployer's counts over it were at that pass.
//
// It is one of two writers of that one record and nothing calls either, the
// other being lastcheck.Writer.RecordPlatformPass; doc.go says so and does not
// pick between them.
func RecordPlatformCheck(ctx context.Context, checks *lastcheck.Writer, actor record.Actor,
	platform string, interval time.Duration, lastPass bool, payload string) error {
	if platform == "" {
		return fmt.Errorf("%w: the last check names no platform", ErrTargetNotFound)
	}
	_, err := checks.Record(ctx, actor, lastcheck.LastCheck{
		Component: lastcheck.ComponentDeployer,
		Subject:   platform,
		Interval:  interval,
		LastPass:  lastPass,
		Payload:   payload,
	})
	return err
}

// Token is the fencing token this writer carries, which the writes it makes
// through another package's transaction-taking write need.
func (w *Writer) Token() lease.Token { return w.token }
