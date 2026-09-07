package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/area"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/constraint"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// Factory's own writes: the project and production's environment with it, an
// area, every parameter an owner authors, the safeguards, the halt, the legal
// hold, the fleet entries, the permanent constraints, and the five rows that
// decide a record rather than an item.

// CreateProject writes a project and, in the same event, production's
// environment for it: production exists everywhere and an owner does not
// choose it. This is the first caller of [policy.Factory.CreateProject] — the
// install's own first take creates the one project a run works in, and every
// project after it is written here.
func (c *calls) CreateProject(ctx context.Context, who principal.Principal, args screens.CreateProjectArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(args.Name) == "" {
		return "", fmt.Errorf("%w: a project is written with a name, and this one names none", screens.ErrRefused)
	}
	created, _, err := c.p.factory.CreateProject(ctx, actor, args.Name,
		[]string{c.p.d.dir}, c.p.d.credential)
	if err != nil {
		return "", err
	}
	c.changed("factory", listAddressID)
	return created.Project.ID, nil
}

// DeclareArea declares an area inside another area or, where it names none,
// inside the project. It goes through [policy.Factory] and not through package
// area's own writer, so the declaration appends a policy version the way every
// other write an owner makes at Factory does.
func (c *calls) DeclareArea(ctx context.Context, who principal.Principal, args screens.DeclareAreaArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	inside := area.Inside{}
	if args.InsideAreaID != "" {
		outer, err := area.Get(ctx, c.p.d.pool, args.InsideAreaID)
		if err != nil {
			return "", fmt.Errorf("%w: %s", screens.ErrNotFound, args.InsideAreaID)
		}
		inside = area.InsideArea(outer.ID)
	} else {
		projectID, err := c.projectNamed(ctx, args.ProjectName)
		if err != nil {
			return "", err
		}
		inside = area.InsideProject(projectID)
	}
	declared, _, err := c.p.factory.DeclareArea(ctx, actor, args.Name, inside, area.Hazard{})
	if err != nil {
		return "", err
	}
	c.changed("factory", listAddressID)
	return declared.ID, nil
}

// AuthorParameter authors one parameter: duty 8. Which subject fields are read
// follows from the parameter, because the record a parameter is a field of is a
// fact of the parameter and not a choice — the same dispatch the terminal's
// author subcommand makes, in the one function both reach.
func (c *calls) AuthorParameter(ctx context.Context, who principal.Principal, args screens.AuthorParameterArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := authorParameter(ctx, c.p.d.pool, c.p.factory, actor, authoring{
		parameter:   args.Parameter,
		value:       args.Value,
		serviceName: args.ServiceID,
		areaName:    args.AreaID,
		projectName: args.ProjectName,
		gateRow:     args.GateRow,
		stage:       args.Stage,
		quantity:    args.Quantity,
	}); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	return nil
}

// PlaceSafeguard places a safeguard: duty 9. It takes no direction — the
// direction differs per parameter and points the same way in each, so an owner
// chooses only the subject and the bound.
func (c *calls) PlaceSafeguard(ctx context.Context, who principal.Principal, args screens.PlaceSafeguardArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	routing := safeguard.Routing{Duty: int(args.RouteDuty), HumanKey: args.RouteHuman}
	if err := routing.Validate(); err != nil {
		return "", fmt.Errorf("%w: %v", screens.ErrRefused, err)
	}
	placed, err := placeSafeguard(ctx, c.p.d.pool, c.p.factory, actor,
		args.Parameter, args.SubjectKind+":"+args.SubjectName, args.ServiceName, args.Bound, routing)
	if err != nil {
		return "", err
	}
	c.changed("factory", listAddressID)
	return placed.ID, nil
}

// WithdrawSafeguard writes a safeguard's withdrawal. The safeguard stands until
// the row that decides it closes, which is [calls.DecideRecordRow].
func (c *calls) WithdrawSafeguard(ctx context.Context, who principal.Principal, args screens.WithdrawSafeguardArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, _, err := c.p.factory.WriteSafeguardWithdrawal(ctx, actor, args.SafeguardID); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	return nil
}

// SetHalt sets the one authored record whose subject is the factory.
func (c *calls) SetHalt(ctx context.Context, who principal.Principal, args screens.SetHaltArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	if args.Reason == "" {
		return "", fmt.Errorf("%w: a halt says why the factory is halted, and this one says nothing", screens.ErrRefused)
	}
	set, err := halt.NewWriter(c.p.d.pool, c.p.d.token).Insert(ctx, actor, args.Reason)
	if err != nil {
		return "", err
	}
	c.changed("factory", listAddressID)
	return set.ID, nil
}

// WithdrawHalt writes a halt's withdrawal, pending until the row that decides
// it approves it.
func (c *calls) WithdrawHalt(ctx context.Context, who principal.Principal, args screens.WithdrawHaltArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, err := halt.NewWriter(c.p.d.pool, c.p.d.token).InsertWithdrawal(ctx, actor, args.HaltID); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	return nil
}

// SetLegalHold sets a hold on a service, a project, or the whole install. It is
// refused wherever it reaches: a hold on a project reaches the project's
// environment and every service in it.
func (c *calls) SetLegalHold(ctx context.Context, who principal.Principal, args screens.SetLegalHoldArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	if args.Reason == "" {
		return "", fmt.Errorf("%w: a legal hold says why it was set, and this one says nothing", screens.ErrRefused)
	}
	on, err := legalHoldSubject(ctx, c.p.d.pool, args.SubjectKind+":"+args.SubjectName)
	if err != nil {
		return "", err
	}
	set, _, err := c.p.factory.SetLegalHold(ctx, actor, on, args.Reason)
	if err != nil {
		return "", err
	}
	c.changed("factory", listAddressID)
	return set.ID, nil
}

// WithdrawLegalHold writes a legal hold's withdrawal, pending until the row
// that decides it approves it.
func (c *calls) WithdrawLegalHold(ctx context.Context, who principal.Principal, args screens.WithdrawLegalHoldArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if _, _, err := c.p.factory.WriteLegalHoldWithdrawal(ctx, actor, args.LegalHoldID); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	return nil
}

// SupplyConstraint is duty 2 at Factory: a permanent constraint over the
// factory, a project, or an area, read here rather than found through the
// requests it arrived with.
func (c *calls) SupplyConstraint(ctx context.Context, who principal.Principal, args screens.SupplyConstraintArgs) (string, error) {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return "", err
	}
	reach, subjectID, err := c.permanentReach(ctx, args.ReachKind, args.ReachName)
	if err != nil {
		return "", err
	}
	arriving, err := arrivingConstraint(args.Statement, reach, subjectID,
		args.BindsFrom, args.ReviewDate, args.Zone, args.RequiresSeam5Enforced)
	if err != nil {
		return "", err
	}
	written, err := constraint.NewWriter(c.p.d.pool, c.p.d.token).Arrive(ctx, actor, arriving)
	if err != nil {
		return "", err
	}
	c.changed("constraint", written.ID)
	c.changed("factory", listAddressID)
	return written.ID, nil
}

// permanentReach is the reach a constraint supplied at Factory binds over, and
// the record that reach names. The factory names none; a project and an area
// are resolved by name, so a reach pointing at nothing is refused at the write.
func (c *calls) permanentReach(ctx context.Context, kind, name string) (constraint.Reach, string, error) {
	switch constraint.Reach(kind) {
	case constraint.ReachFactory:
		return constraint.ReachFactory, "", nil
	case constraint.ReachProject:
		projectID, err := c.projectNamed(ctx, name)
		return constraint.ReachProject, projectID, err
	case constraint.ReachArea:
		ar, err := namedArea(ctx, c.p.d.pool, name)
		return constraint.ReachArea, ar.ID, err
	default:
		return "", "", fmt.Errorf(
			"%w: a permanent constraint reaches the factory, a project or an area, not %q", screens.ErrRefused, kind)
	}
}

// RetireService is the owner's write of retired on the service record, which
// is the one thing that ends a service and what calls the deployer's removal.
// The three counts the write is refused on are read here — package policy takes
// them as arguments, each being a read of a package it may not import.
//
// Where the args name an environment, the removal is performed for that one
// environment and nothing is written on the service record: that is the step an
// owner takes before an environment other than production may be withdrawn.
func (c *calls) RetireService(ctx context.Context, who principal.Principal, args screens.RetireServiceArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	svc, err := service.Get(ctx, c.p.d.pool, args.ServiceID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ServiceID)
	}
	if args.EnvironmentName != "" {
		if err := c.p.removeFromEnvironment(ctx, actor, svc, args.EnvironmentName); err != nil {
			return err
		}
	} else if err := c.p.retire(ctx, actor, svc); err != nil {
		return err
	}
	c.changed("service", svc.ID)
	c.changed("ops", listAddressID)
	c.changed("factory", listAddressID)
	return nil
}

// EndProject ends a project once every service in it is retired, and withdraws
// production's environment for it in the same write — the pairing that created
// the two. The count of services still holding a current release is read here:
// package environment refuses the withdrawal on it and may not count them.
func (c *calls) EndProject(ctx context.Context, who principal.Principal, args screens.EndProjectArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	projectID, err := c.projectNamed(ctx, args.ProjectName)
	if err != nil {
		return err
	}
	if err := c.p.endProject(ctx, actor, projectID); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	c.changed("ops", listAddressID)
	return nil
}

// DecideRecordRow decides one of the five rows that decide a record rather than
// an item: the four withdrawals and shortenings [approveWithdrawal] already
// fires, and the row every version of what an agent is told fires.
func (c *calls) DecideRecordRow(ctx context.Context, who principal.Principal, args screens.DecideRecordRowArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if args.RowKind == gate.RolePromptOrSkill.String() {
		return c.decideRolePrompt(ctx, actor, args)
	}
	if err := decideOutsideEveryItemAt(ctx, c.p.d.pool, c.p.d.token, actor, recordRow{
		kind:           args.RowKind,
		recordID:       args.RecordID,
		verdict:        gate.Verdict(args.Verdict),
		reason:         args.Reason,
		openedInWorkAt: args.OpenedInWorkAt,
	}); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	return nil
}

// EditRecordRow is the role-prompt row's third action: not a verdict but
// authoring a version and re-firing the row. It is refused on the four
// record-deciding rows, which have no version to author — a withdrawal and a
// shortening are records and not documents.
func (c *calls) EditRecordRow(ctx context.Context, who principal.Principal, args screens.EditRecordRowArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	if args.Row != gate.RolePromptOrSkill.String() {
		return fmt.Errorf("%w: %w: %s decides a record and not a document, so there is no version to author",
			screens.ErrRefused, gate.ErrEditInPlaceRefused, args.Row)
	}
	if strings.TrimSpace(args.Version) == "" {
		return fmt.Errorf("%w: an edit in place authors a version, and this one is empty", screens.ErrRefused)
	}
	head, err := artifact.Get(ctx, c.p.d.pool, args.RecordID)
	if err != nil {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.RecordID)
	}
	written, err := c.p.store.SubmitFleet(ctx, gate.Component(gate.RolePromptOrSkill),
		artifact.By{Authorship: artifact.AuthorshipGate, Author: actor.Key},
		artifact.KindRolePrompt, head.Role, "", args.Version, "")
	if err != nil {
		return err
	}
	if err := c.fireRolePromptRow(ctx, written); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	// The row fired here is a row pending on a human, which the home view's
	// badge counts — the same pair [calls.decideRolePrompt] announces when it
	// closes one.
	c.changed("home", listAddressID)
	return nil
}
