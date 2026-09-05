package service

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// Reachability is the four fields that are the deployer's, not the owner's and
// not decomposition's. They are distinct from provisioned: provisioned says the
// repository and the store exist, and these four say what runs on them can be
// reached, replaced, undone, and read.
//
// All four are false and At is empty until the deployer writes them, which is
// what tells a service nothing has adopted yet from one the deployer found
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
	// At is when the deployer wrote the four, and empty while it has not.
	At string
}

// Written reports whether the deployer has written the four at all.
func (r Reachability) Written() bool { return r.At != "" }

// Adopt writes the deployer's four fields on one service. The deployer calls it
// at adoption and at every first release, so the four say what the last such
// deploy found rather than what any of them ever found.
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
	tag, err := tx.Exec(ctx, `update `+Table+`
		set target_reached = $1, instances_replaceable = $2, rollback_path_present = $3,
		emission_readable = $4, deployer_wrote_at = $5
		where id = $6`,
		found.TargetReached, found.InstancesReplaceable, found.RollbackPathPresent,
		found.EmissionReadable, record.Now(), serviceID)
	if err != nil {
		return fmt.Errorf("service: writing what the deployer found on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}
