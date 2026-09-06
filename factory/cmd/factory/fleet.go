package main

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
)

// oneModelFleet is [dispatch.Fleet]: the entries this interface was composed
// with. A run is given one model and one credential on the command line, so
// every role's entry names them and every entry's scope is the whole factory —
// which is what an install that has written no fleet entry has, the fleet entry
// being a record nothing here writes.
//
// It answers for every role, so no dispatch of this interface holds on "a
// stage no fleet entry covers". A composition that answered for fewer would
// hold the rest, and dispatch's own tests are where that is demonstrated.
type oneModelFleet struct {
	model      agent.Model
	modelName  string
	credential string
}

// EntryFor is the entry for one role on one item.
func (f oneModelFleet) EntryFor(_ context.Context, role dispatch.Role, on dispatch.On) (dispatch.Entry, bool, error) {
	if _, err := role.Stage(); err != nil {
		return dispatch.Entry{}, false, err
	}
	scope := dispatch.Scope{}
	if !scope.Covers(on) {
		return dispatch.Entry{}, false, nil
	}
	return dispatch.Entry{
		Role: role, Scope: scope, Model: f.model,
		ModelVersion: f.modelName, CredentialName: f.credential,
	}, true, nil
}

// rolePrompts is [dispatch.Prompts]: the role prompt version in force per
// role. An install's entry is in force ungated and the store's own in-force
// read finds it by the event that entered it; the ids this holds are what an
// upgrade's entry would need once a human approved one, which is the row this
// interface never fires.
//
// So a chain whose head an upgrade entered reads as the version below it in
// force until that row is decided, which is what the design says of an
// unapproved version, and this interface has no way to decide it.
type rolePrompts struct {
	pool     *pgxpool.Pool
	approved map[dispatch.Role][]string
}

// InForce is the version in force for the role, read through the store's own
// in-force query with the approved ids this composition supplies.
func (r rolePrompts) InForce(ctx context.Context, role dispatch.Role) (artifact.Artifact, bool, error) {
	return artifact.InForce(ctx, r.pool, artifact.KindRolePrompt, string(role), "", r.approved[role])
}

// shippedPromptFor is the words the product ships for one role. It is the one
// place package agent's four constants are read: what a run reads is the
// version in force, and these are only what the first start enters.
func shippedPromptFor(role dispatch.Role) (string, error) {
	switch role {
	case dispatch.RoleSpecAuthor:
		return agent.ShippedSpecAuthorPrompt, nil
	case dispatch.RoleImplementationPlanner:
		return agent.ShippedPlannerPrompt, nil
	case dispatch.RoleTaskAuthor:
		return agent.ShippedTaskAuthorPrompt, nil
	case dispatch.RoleImplementer:
		return agent.ShippedImplementerPrompt, nil
	default:
		return "", fmt.Errorf("%w: %q", dispatch.ErrRoleUnknown, role)
	}
}

// enterShippedPrompts is the install's first-start step for what an agent is
// told: at install, and at a first start on an upgrade that changed the
// shipped words, the factory itself calls the artifact store to enter what
// shipped, with the factory's own start as the actor and the author pair
// empty.
//
// A chain whose head already holds the same words is left alone: an upgrade
// that changed nothing moves nothing. A chain whose head holds different words
// gets a version — which the design has awaiting the gate every version fires,
// and this interface fires none, so what it enters is what this composition
// then treats as in force. That is the departure, and it is stated in doc.go.
func enterShippedPrompts(ctx context.Context, store *artifact.Store, pool *pgxpool.Pool,
	actor record.Actor, bundle string) (rolePrompts, []string, error) {
	prompts := rolePrompts{pool: pool, approved: map[dispatch.Role][]string{}}
	var entered []string
	for _, role := range dispatch.Roles {
		shipped, err := shippedPromptFor(role)
		if err != nil {
			return rolePrompts{}, nil, err
		}
		head, found, err := artifact.Newest(ctx, pool, artifact.KindRolePrompt, string(role), "")
		if err != nil {
			return rolePrompts{}, nil, err
		}
		if found && head.Content == shipped {
			prompts.approved[role] = []string{head.ID}
			continue
		}
		// The chain being empty is an install and its entry is in force
		// ungated; a chain whose head holds other words is an upgrade that
		// changed them, and its entry awaits the gate every version fires.
		enteredBy := artifact.EnteredByInstall
		if found {
			enteredBy = artifact.EnteredByUpgradeFirstStart
		}
		version, err := store.EnterShipped(ctx, actor, artifact.KindRolePrompt, string(role), "",
			shipped, enteredBy, bundle)
		if err != nil {
			return rolePrompts{}, nil, err
		}
		prompts.approved[role] = []string{version.ID}
		entered = append(entered, string(role))
	}
	return prompts, entered, nil
}

// gateEscalation is [dispatch.Escalation]: dispatch decides that the stage has
// spent its limit and this performs it, which is the gate component's
// enforcement — the escalated value onto the item, every pending row of the
// item abandoned naming the limit, and the wait to the notifier, in that
// order.
//
// It is composed here because ../../../end-goal/components.md's row for
// dispatch names no gate: the two meet in the composition and not in either
// package.
type gateEscalation struct{ gate *gate.Gate }

// Escalate performs the escalation.
func (g gateEscalation) Escalate(ctx context.Context, actor record.Actor, itemID string, stage item.Stage) error {
	_, err := g.gate.EnforceAttemptLimit(ctx, actor, itemID, stage)
	return err
}
