package criterion

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// ForConstraint is every criterion in force for the build that stands for the
// named constraint: the first of the two questions the constraint-derived link
// is a link for. Factory lists it so that what a rule in force is actually
// enforced by is read off the records.
//
// It is the in-force set narrowed, so it takes the same build — a service and
// the set of items the build is made of — and no items is no criteria.
func ForConstraint(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs []string, constraintID string) ([]Criterion, error) {
	if len(itemIDs) == 0 || constraintID == "" {
		return nil, nil
	}
	return query(ctx, pool, serviceID,
		selectCriterion+` where service_id = $1 and item_id = any($2) and $3 = any(constraint_derived)`+
			notWithdrawn+` order by at`,
		serviceID, itemIDs, constraintID)
}

// UnderWithdrawnConstraints is every criterion in force for the build that was
// drafted under one of the constraints given: the second question. The
// withdrawn constraints are the caller's, because which constraints have been
// withdrawn or replaced is the constraint record's fact and not this table's.
//
// Factory lists these beside each withdrawn constraint whose criteria still
// stand, so a rule no longer in force and still enforced is visible rather than
// silent. Withdrawing such a criterion is an item like any other, and the
// constraint's own withdrawal removes none of them.
func UnderWithdrawnConstraints(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs, constraintIDs []string) ([]Criterion, error) {
	if len(itemIDs) == 0 || len(constraintIDs) == 0 {
		return nil, nil
	}
	return query(ctx, pool, serviceID,
		selectCriterion+` where service_id = $1 and item_id = any($2) and constraint_derived && $3`+
			notWithdrawn+` order by at`,
		serviceID, itemIDs, constraintIDs)
}

// ControllingHazard is every criterion in force for the build that bounds the
// hazardous operation of the named area: the question an auditor and the human
// at an irreversible Implementation gate ask, and the read the Spec gate's
// mechanical rejection is made from — a build in an area graded irreversible
// with no criterion in force naming its operation. That rejection is the gate's
// and doc.go names it as a caller this package does not hold.
func ControllingHazard(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs []string, areaID string) ([]Criterion, error) {
	if len(itemIDs) == 0 || areaID == "" {
		return nil, nil
	}
	return query(ctx, pool, serviceID,
		selectCriterion+` where service_id = $1 and item_id = any($2) and hazard_derived = $3`+
			notWithdrawn+` order by at`,
		serviceID, itemIDs, areaID)
}
