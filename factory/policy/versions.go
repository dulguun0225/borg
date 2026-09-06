package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
)

// Versions is every policy version, oldest first, read as p. Reading the log
// appends a read event naming that principal, which is one row per call here as
// it is everywhere the log is read.
func (r *Reader) Versions(ctx context.Context, p principal.Principal) ([]Version, error) {
	rows, err := r.log.ByShape(ctx, p, decisionlog.ShapePolicyVersion)
	if err != nil {
		return nil, err
	}
	versions := make([]Version, 0, len(rows))
	for _, row := range rows {
		v, err := versionOf(row)
		if err != nil {
			return nil, err
		}
		versions = append(versions, v)
	}
	return versions, nil
}

// Newest is the policy version in force, which is the newest row of that shape.
// A gate firing names it, so a factory with no version is [ErrNoVersion] and not
// an empty string passed off as a version.
func (r *Reader) Newest(ctx context.Context, p principal.Principal) (Version, error) {
	versions, err := r.Versions(ctx, p)
	if err != nil {
		return Version{}, err
	}
	if len(versions) == 0 {
		return Version{}, fmt.Errorf("%w: in force", ErrNoVersion)
	}
	return versions[len(versions)-1], nil
}

// Version is one version by id, which is what a reader of a decision follows to
// the policy it was decided under.
func (r *Reader) Version(ctx context.Context, p principal.Principal, id string) (Version, error) {
	versions, err := r.Versions(ctx, p)
	if err != nil {
		return Version{}, err
	}
	for _, v := range versions {
		if v.ID == id {
			return v, nil
		}
	}
	return Version{}, fmt.Errorf("%w: %s", ErrNoVersion, id)
}

// AuthoredBy is who authored the value one parameter holds at one scope, and
// false where no version authored it there. It is a walk back through the
// versions because a later version restates the value and not who wrote it:
// what a version's own actor says is who made that write, and the write that
// put a value in force is the one this finds.
//
// A write a gate row decided names two humans, and the one this answers is the
// author: the version's own actor is the human who closed the row, and the
// record that row decided carries whoever wrote the value. Decision-log
// retention is the one such parameter, and the record is the shortening.
//
// Its reader is the decision log's truncation, whose row names who authored the
// retention value it enforced rather than whoever ran the pass or approved it.
func (r *Reader) AuthoredBy(ctx context.Context, p principal.Principal,
	parameter gatepolicy.Parameter, scope Scope) (record.Actor, bool, error) {
	versions, err := r.Versions(ctx, p)
	if err != nil {
		return record.Actor{}, false, err
	}
	for n := len(versions) - 1; n >= 0; n-- {
		v := versions[n]
		if v.Action != ActionAuthored || v.Parameter != parameter {
			continue
		}
		if v.Scope.Kind != scope.Kind || v.Scope.Key != scope.Key {
			continue
		}
		if scope.ID != "" && v.Scope.ID != scope.ID {
			continue
		}
		if v.ShorteningID != "" {
			written, err := factorysettings.GetShortening(ctx, r.pool, v.ShorteningID)
			if err != nil {
				return record.Actor{}, false, err
			}
			return written.Actor, true, nil
		}
		return v.Actor, true, nil
	}
	return record.Actor{}, false, nil
}

// AuthoredAutoPassRate is the realized auto-pass rate frozen on the newest
// version that set the risk threshold on this scope and this gate row, one rate
// per factor set, and false where no version set one there.
//
// It is a walk back through the versions rather than a read of the newest,
// because the rate is the one field a later version does not restate: a write
// that touches another parameter appends a version naming the threshold it did
// not change and no rate beside it. The reference for a threshold in force is
// the rate at the moment that threshold was set.
func (r *Reader) AuthoredAutoPassRate(ctx context.Context, p principal.Principal,
	scope Scope, gateRow string) ([]AutoPassRate, bool, error) {
	versions, err := r.Versions(ctx, p)
	if err != nil {
		return nil, false, err
	}
	for n := len(versions) - 1; n >= 0; n-- {
		v := versions[n]
		if v.Parameter != gatepolicy.RiskThreshold || len(v.AutoPassRates) == 0 {
			continue
		}
		if v.Scope.Kind == scope.Kind && v.Scope.ID == scope.ID && v.Scope.Key == gateRow {
			return v.AutoPassRates, true, nil
		}
	}
	return nil, false, nil
}
