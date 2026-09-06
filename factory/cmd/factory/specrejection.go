package main

import (
	"context"
	"errors"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/gate"
)

// What a build promises, checked at the Spec row and mechanically, whatever the
// score returns. It is the per-item counterpart of [setRejection], which is the
// Decomposition row's check over the set, and it is computed here for the same
// reason: the criteria are package criterion's and the requirements are package
// intent's, and package gate imports neither's reads.

// specRejection is the Spec row's mechanical rejections, all three of them: a
// build in an irreversible area with no criterion in force naming that area's
// hazardous operation, a requirement assigned to the item that no criterion in
// force for it names, and a criterion naming a requirement assigned elsewhere.
//
// The hazard is read first, which is the order [gate.SpecChecks] fixes, and it
// is read from package criterion, which owns the hazard-derived field the query
// is over. The two beside it are decided by [gate.SpecRejection] over two lists
// read here — the requirements decomposition assigned this item, and the
// requirement each criterion in force for a build of this item alone names.
//
// c.hazard carries the item's area only where the grade in force for it is
// irreversible, which is what [path.hazardInForce] already decided, so an empty
// area is a build no hazard rejection reaches.
func (p *path) specRejection(ctx context.Context, c *candidate) (check, found string, err error) {
	build := []string{c.itemID}
	irreversible := c.hazard.AreaID != ""
	err = criterion.CheckHazardControlled(ctx, p.d.pool, c.svc.ID, build, c.hazard.AreaID, irreversible)
	var uncontrolled *criterion.HazardUncontrolledError
	switch {
	case errors.As(err, &uncontrolled):
		return gate.AutoRejectedByUncontrolledHazard, uncontrolled.Error(), nil
	case err != nil:
		return "", "", err
	}
	inForce, err := criterion.InForce(ctx, p.d.pool, c.svc.ID, build)
	if err != nil {
		return "", "", err
	}
	named := make([]string, 0, len(inForce))
	for _, one := range inForce {
		named = append(named, one.RequirementID)
	}
	check, found, _ = gate.SpecRejection(c.requirementIDs, named)
	return check, found, nil
}
