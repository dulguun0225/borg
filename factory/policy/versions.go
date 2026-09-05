package policy

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// Versions is every policy version, oldest first, read as principal. Reading
// the log appends a read event naming that principal, which is one row per call
// here as it is everywhere the log is read.
func (r *Reader) Versions(ctx context.Context, principal record.Actor) ([]Version, error) {
	rows, err := r.log.ByShape(ctx, principal, decisionlog.ShapePolicyVersion)
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
func (r *Reader) Newest(ctx context.Context, principal record.Actor) (Version, error) {
	versions, err := r.Versions(ctx, principal)
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
func (r *Reader) Version(ctx context.Context, principal record.Actor, id string) (Version, error) {
	versions, err := r.Versions(ctx, principal)
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

// AuthoredAutoPassRate is the realized auto-pass rate frozen on the newest
// version that set the risk threshold on this scope and this gate row, one rate
// per factor set, and false where no version set one there.
//
// It is a walk back through the versions rather than a read of the newest,
// because the rate is the one field a later version does not restate: a write
// that touches another parameter appends a version naming the threshold it did
// not change and no rate beside it. The reference for a threshold in force is
// the rate at the moment that threshold was set.
func (r *Reader) AuthoredAutoPassRate(ctx context.Context, principal record.Actor,
	scope Scope, gateRow string) ([]AutoPassRate, bool, error) {
	versions, err := r.Versions(ctx, principal)
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
