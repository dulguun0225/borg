package main

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/screens"
)

// The writes at Factory that dispatch re-matches on: the fleet entries, and the
// seam 5 field a document-kind constraint waits for. Each is a record
// ../../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md
// names as able to lift a hold, so each ends in the same re-match. Split from
// callsfactory.go by subject at the 500-line bound.

// WriteFleetEntry is the owner's first act at Factory: a model at an effort in
// a role with a scope, the credential it runs on, and the rest of the nine
// fields. An install holding no entry for a role dispatches nothing, which is
// what the readiness reading on the home view says before anything is wrong.
func (c *calls) WriteFleetEntry(ctx context.Context, who principal.Principal, args screens.WriteFleetEntryArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	scope, err := c.entryScope(ctx, args)
	if err != nil {
		return "", err
	}
	written, err := fleetentry.NewWriter(c.p.d.pool, c.p.d.token).Write(ctx, actor, fleetentry.New{
		ModelVersion:                    args.ModelVersion,
		Effort:                          args.Effort,
		Role:                            args.Role,
		Scope:                           scope,
		CredentialName:                  args.Credential,
		ProcessingLocation:              args.ProcessingLocation,
		MaterialClasses:                 args.MaterialClasses,
		Operations:                      args.Operations,
		ReadsAtOnce:                     args.ReadAtOnceBound,
		DispatchesBetweenEvaluationRuns: args.DispatchesBetweenEvalRuns,
	})
	if err != nil {
		return "", err
	}
	return written.ID, c.rematched(ctx)
}

// entryScope is the scope an entry is written with, resolved from the names an
// owner typed at Factory: a project, a service and an area, each optional, and
// all three empty scoping the entry to the whole factory. A name that resolves
// to nothing is refused here rather than stored, the way a permanent
// constraint's reach is — an entry scoped to a record that does not exist
// matches no item and reads as an entry an owner wrote and nothing dispatches
// on.
func (c *calls) entryScope(ctx context.Context, args screens.WriteFleetEntryArgs) (fleetentry.Scope, error) {
	var scope fleetentry.Scope
	if args.ScopeProjectName != "" {
		prj, err := namedProject(ctx, c.p.d.pool, args.ScopeProjectName)
		if err != nil {
			return fleetentry.Scope{}, fmt.Errorf("%w: %v", screens.ErrRefused, err)
		}
		scope.ProjectID = prj.ID
	}
	if args.ScopeServiceName != "" {
		svc, err := namedService(ctx, c.p.d.pool, args.ScopeServiceName)
		if err != nil {
			return fleetentry.Scope{}, fmt.Errorf("%w: %v", screens.ErrRefused, err)
		}
		scope.ServiceID = svc.ID
	}
	if args.ScopeAreaName != "" {
		ar, err := namedArea(ctx, c.p.d.pool, args.ScopeAreaName)
		if err != nil {
			return fleetentry.Scope{}, fmt.Errorf("%w: %v", screens.ErrRefused, err)
		}
		scope.AreaID = ar.ID
	}
	return scope, nil
}

// rematched re-tests every hold dispatch has open, after a record the design
// names as able to lift one is written here, and announces the three addresses
// that changed: the board's rows in Work, Factory — the count per cause, and
// the record this call just wrote beside it — and the home view, whose badge
// counts the holds only a human clears and whose readiness reading is per role.
// It is the whole announcement of each call here, which is why each ends in it.
//
// The item timelines under the lifted rows are not announced: mapping a lifted
// row back to its item is a second read of the log, and a timeline open on a
// screen is re-read from the board beside it.
func (c *calls) rematched(ctx context.Context) error {
	if _, err := c.p.dispatch.Rematch(ctx); err != nil {
		return err
	}
	c.changed("work", listAddressID)
	c.changed("home", listAddressID)
	c.changed("factory", listAddressID)
	return nil
}

// WithdrawFleetEntry withdraws a fleet entry. A stage no entry covers holds,
// which is a row in Work and a count at Factory.
func (c *calls) WithdrawFleetEntry(ctx context.Context, who principal.Principal, args screens.WithdrawFleetEntryArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := fleetentry.NewWriter(c.p.d.pool, c.p.d.token).Withdraw(ctx, actor, args.FleetEntryID); err != nil {
		return err
	}
	// A withdrawal narrows what the entries cover, so it lifts no hold of its
	// own. The re-match is made here for the same reason a start makes one: a
	// hold is a row, this is a read of every one of them, and the two conditions
	// whose own record has no caller are closed at whatever read reaches them
	// first.
	return c.rematched(ctx)
}

// SetSeam5Enforced turns enforcement of seam 5 on. It is the record a
// document-kind constraint requiring seam 5 waits for, so the re-match follows
// the write: every item held on that constraint is dispatchable the moment this
// is true, and the design has the field turning on lift those holds.
//
// It is one-way — off at install, turned on once, and never off again — so
// Enforced false is refused here rather than reaching a writer that refuses it,
// and there is no call that turns it off.
func (c *calls) SetSeam5Enforced(ctx context.Context, who principal.Principal, args screens.SetSeam5EnforcedArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if !args.Enforced {
		return fmt.Errorf("%w: %v", screens.ErrRefused, factorysettings.ErrSeam5NotTurnedOff)
	}
	if _, err := c.p.factory.SetSeam5Enforced(ctx, actor); err != nil {
		return err
	}
	return c.rematched(ctx)
}
