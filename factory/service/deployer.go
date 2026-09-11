package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// ErrShapeNotAdmitted is returned by [Adopt] where found does not hold every
// reachability property. Adoption admits one shape — instances on a
// deploy target, already taking organic traffic, replaceable one at a time
// and returnable by shifting traffic — and a shape short one property is
// refused rather than partially recorded; the error names the property
// missing.
var ErrShapeNotAdmitted = errors.New("service: adoption admits only a shape holding every reachability property")

// Reachability is the fields that are the deployer's, not the owner's and not
// decomposition's. They are distinct from provisioned: provisioned says the
// repository and the store exist, and these say what runs on them can be
// reached, replaced, undone, read, and is already taking traffic.
//
// All of them are false and At is empty until the deployer writes them, which
// is what tells a service nothing has adopted yet from one the deployer found
// wanting.
type Reachability struct {
	// TargetReached is that the deployer reached a target of this service.
	TargetReached bool
	// InstancesReplaceable is that the platform can replace this service's
	// instances one at a time.
	InstancesReplaceable bool
	// RollbackPathPresent is that there is a path back to what ran before.
	RollbackPathPresent bool
	// EmissionReadable is that the health monitor can read this service's
	// emission.
	EmissionReadable bool
	// TakingTraffic is that the emission reports a request rate above zero on
	// the target — "already taking organic traffic" in the shape adoption
	// admits, and distinct from EmissionReadable: the health monitor needing
	// something to read is not the same fact as traffic already flowing, even
	// where one platform's emission answers both from the same reading.
	TakingTraffic bool
	// At is when the deployer wrote these, and empty while it has not.
	At string
}

// Written reports whether the deployer has written these at all.
func (r Reachability) Written() bool { return r.At != "" }

// Adopt writes the deployer's fields on one service. The deployer calls it at
// adoption and at every first release, so they say what the last such deploy
// found rather than what any of them ever found.
//
// Adoption admits one shape and refuses every other: found must hold every
// property, or the write is refused with [ErrShapeNotAdmitted] naming the
// one missing, and nothing is written — a service the deployer found wanting
// is left exactly as reachable as the last adoption or first release found it.
//
// It takes the lease token and fences the caller's transaction, which the
// owner-authored writes on this record do not: their caller is package policy,
// which fences the transaction it appends the policy version in, and this one's
// caller is the deployer, which is not built. doc.go says so.
func Adopt(ctx context.Context, tx pgx.Tx, token lease.Token, actor record.Actor,
	serviceID string, found Reachability) error {
	if err := lease.Fence(ctx, tx, token); err != nil {
		return err
	}
	if err := actor.Validate(); err != nil {
		return err
	}
	switch {
	case !found.TargetReached:
		return fmt.Errorf("%w: no target reached", ErrShapeNotAdmitted)
	case !found.InstancesReplaceable:
		return fmt.Errorf("%w: instances are not replaceable one at a time", ErrShapeNotAdmitted)
	case !found.RollbackPathPresent:
		return fmt.Errorf("%w: no rollback path is present", ErrShapeNotAdmitted)
	case !found.EmissionReadable:
		return fmt.Errorf("%w: the emission is not readable", ErrShapeNotAdmitted)
	case !found.TakingTraffic:
		return fmt.Errorf("%w: no traffic is reported on the target", ErrShapeNotAdmitted)
	}
	tag, err := tx.Exec(ctx, `update `+Table+`
		set target_reached = $1, instances_replaceable = $2, rollback_path_present = $3,
		emission_readable = $4, taking_traffic = $5, deployer_wrote_at = $6
		where id = $7`,
		found.TargetReached, found.InstancesReplaceable, found.RollbackPathPresent,
		found.EmissionReadable, found.TakingTraffic, record.Now(), serviceID)
	if err != nil {
		return fmt.Errorf("service: writing what the deployer found on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}
