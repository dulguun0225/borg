package criterion

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Provenance is one of the three sources that name an authority a withdrawal is
// routed to. Everything else is factory-drafted, which names none and is not a
// value here: the fourth source is the absence of these three.
type Provenance string

const (
	// ProvenanceHumanConfirmed is a criterion whose introducing spec version a
	// human decided. It is no field of this table: it is a query over that
	// version's decision, which carries the actor and the resolved vector.
	ProvenanceHumanConfirmed Provenance = "human_confirmed"
	// ProvenanceConstraintDerived is a criterion the drafting stage named a
	// constraint as its evidence for.
	ProvenanceConstraintDerived Provenance = "constraint_derived"
	// ProvenanceHazardDerived is a criterion that bounds the hazardous
	// operation of a named area.
	ProvenanceHazardDerived Provenance = "hazard_derived"
)

// Provenances is the three sources that name an authority.
var Provenances = []Provenance{
	ProvenanceHumanConfirmed, ProvenanceConstraintDerived, ProvenanceHazardDerived,
}

// HumanConfirmed is every criterion in force for the build whose introducing
// spec version a human decided. Human-confirmed is not a field: it is a query
// over that version's decision, the way in force is already a query, and the
// decision is the decision log's fact and not this table's — so the caller
// reads which spec versions a human decided and passes what it read, the way
// [CheckHazardControlled] takes the grade.
//
// A criterion whose introducing decision the log's retention cut removed is not
// in the set the caller assembled and reads here as unknown provenance, which
// is the cost the design states.
func HumanConfirmed(ctx context.Context, pool *pgxpool.Pool, serviceID string,
	itemIDs, humanConfirmedSpecVersions []string) ([]Criterion, error) {
	if len(itemIDs) == 0 || len(humanConfirmedSpecVersions) == 0 {
		return nil, nil
	}
	return query(ctx, pool, serviceID,
		selectCriterion+` where service_id = $1 and item_id = any($2) and spec_artifact_id = any($3)`+
			notWithdrawn+` order by at`,
		serviceID, itemIDs, humanConfirmedSpecVersions)
}

// WithdrawalWithAnAuthority is one criterion a spec version withdraws whose
// provenance names an authority: the criterion, and which of the three sources
// it has. A criterion may have more than one, and the routing is per source.
type WithdrawalWithAnAuthority struct {
	Criterion   Criterion
	Provenances []Provenance
}

// WithdrawalsWithAnAuthority is every criterion the spec version withdraws
// whose provenance names an authority. The score reads it: each is a resolved
// factor at the Spec row, routed to the human that provenance names — the actor
// of the introducing decision for a human-confirmed one, whoever holds the duty
// over the constraint the field names for a constraint-derived one, and whoever
// holds it over the area for a hazard-derived one. Which human that is, the
// score's own reading, is not this package's; doc.go names the caller.
//
// humanConfirmedSpecVersions is the spec versions a human decided, assembled by
// the caller for the reason [HumanConfirmed] gives. A withdrawn criterion whose
// provenance names no authority is not returned: it is factory-drafted, and its
// withdrawal is silent.
func WithdrawalsWithAnAuthority(ctx context.Context, pool *pgxpool.Pool,
	specArtifactID string, humanConfirmedSpecVersions []string) ([]WithdrawalWithAnAuthority, error) {
	if specArtifactID == "" {
		return nil, nil
	}
	withdrawn, err := query(ctx, pool, "",
		selectCriterion+` where exists (select 1 from `+WithdrawalTable+` w
			where w.criterion_id = `+Table+`.id and w.spec_artifact_id = $1) order by at`,
		specArtifactID)
	if err != nil {
		return nil, err
	}

	var withAnAuthority []WithdrawalWithAnAuthority
	for _, c := range withdrawn {
		var sources []Provenance
		for _, one := range humanConfirmedSpecVersions {
			if c.SpecArtifactID == one {
				sources = append(sources, ProvenanceHumanConfirmed)
				break
			}
		}
		if len(c.ConstraintDerived) > 0 {
			sources = append(sources, ProvenanceConstraintDerived)
		}
		if c.HazardDerived != "" {
			sources = append(sources, ProvenanceHazardDerived)
		}
		if len(sources) == 0 {
			continue
		}
		withAnAuthority = append(withAnAuthority,
			WithdrawalWithAnAuthority{Criterion: c, Provenances: sources})
	}
	return withAnAuthority, nil
}

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

// HazardUncontrolledError is the Spec gate's mechanical rejection: a build in
// an area graded irreversible with no criterion in force bounding that area's
// hazardous operation. The gate rejects on it whatever the score returns, and
// it names the area so the rejection says which grade it was read against.
type HazardUncontrolledError struct {
	AreaID string
}

func (e *HazardUncontrolledError) Error() string {
	return "criterion: no criterion in force bounds the hazardous operation of " + e.AreaID +
		", which is graded irreversible"
}

// CheckHazardControlled is that rejection, made from [ControllingHazard]: for a
// build in an area graded irreversible, it is a [*HazardUncontrolledError]
// where no criterion in force for the build names the area, and nil where one
// does.
//
// irreversible is the grade in force for the item's area, which is the highest
// named anywhere on its chain up to the project — package area walks the chain
// and this package does not import it, so the caller reads the grade and
// passes what it read. A build in an area of any other grade is nil here and
// not a rejection: the derivation and the rejection are the irreversible
// grade's alone.
//
// Its caller is the gate component at the Spec row, and it is not built.
func CheckHazardControlled(ctx context.Context, pool *pgxpool.Pool,
	serviceID string, itemIDs []string, areaID string, irreversible bool,
) error {
	if !irreversible {
		return nil
	}
	if areaID == "" {
		return ErrAreaIDEmpty
	}
	controlling, err := ControllingHazard(ctx, pool, serviceID, itemIDs, areaID)
	if err != nil {
		return err
	}
	if len(controlling) == 0 {
		return &HazardUncontrolledError{AreaID: areaID}
	}
	return nil
}
