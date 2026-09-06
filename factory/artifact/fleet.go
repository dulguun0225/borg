package artifact

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
)

// SubmitFleet writes a version of one of [FleetKinds]: a role prompt named by
// role, a skill named by subject, or the one selection rule, which names
// neither. Its callers are the same three [Authorships] names — an agent
// authoring it is [_What an agent is told_]'s "another caller in the agent
// that authors a version" — and [Store.EnterShipped] is the fourth, ungated
// path for the words that shipped with the product.
func (s *Store) SubmitFleet(ctx context.Context, actor record.Actor, by By, kind Kind,
	role, subject, content, inputManifestID string) (Artifact, error) {
	key, err := fleetKey(kind, role, subject)
	if err != nil {
		return Artifact{}, err
	}
	if err := refuseAuthored(actor, by); err != nil {
		return Artifact{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: beginning the submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return Artifact{}, err
	}

	submitted, err := insertVersion(ctx, tx, actor, by, key, kind, content, inputManifestID, shipped{})
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, nil
}

// EnterShipped writes the one kind of version an authorship does not name:
// what shipped with the product, at install or at an upgrade's first start
// that changed the shipped words. The authorship and the author are both
// empty, the pair [By.Empty] names, and shippedBundleIdentity is required —
// it is the release of the product that entered this version, and it is
// present on this entry and on no other.
//
// enteredBy is which of the two events wrote the row, and it is required:
// both events write the same columns and either can write version 1 of a
// chain, so without it the row does not say whether it entered in force
// ungated — the install's alone do — or entered awaiting the gate every
// version fires. [InForce] reads it, and the caller does not have to know
// which start wrote each row.
func (s *Store) EnterShipped(ctx context.Context, actor record.Actor, kind Kind,
	role, subject, content string, enteredBy EnteredBy, shippedBundleIdentity string) (Artifact, error) {
	key, err := fleetKey(kind, role, subject)
	if err != nil {
		return Artifact{}, err
	}
	if err := actor.Validate(); err != nil {
		return Artifact{}, err
	}
	if !slices.Contains(EnteredBys, enteredBy) {
		return Artifact{}, fmt.Errorf("%w: %q", ErrEnteredByUnknown, enteredBy)
	}
	if shippedBundleIdentity == "" {
		return Artifact{}, ErrShippedBundleIdentityEmpty
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Artifact{}, fmt.Errorf("artifact: beginning the submission: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := lease.Fence(ctx, tx, s.token); err != nil {
		return Artifact{}, err
	}

	submitted, err := insertVersion(ctx, tx, actor, By{}, key, kind, content, "",
		shipped{EnteredBy: enteredBy, BundleIdentity: shippedBundleIdentity})
	if err != nil {
		return Artifact{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Artifact{}, fmt.Errorf("artifact: committing %s: %w", submitted.ID, err)
	}
	return submitted, nil
}

// fleetKey validates kind against [FleetKinds] and returns the chain key that
// belongs to it: role for a role prompt, subject for a skill, and neither for
// the one selection rule.
func fleetKey(kind Kind, role, subject string) (chainKey, error) {
	if !slices.Contains(FleetKinds, kind) {
		return chainKey{}, fmt.Errorf("%w: %q", ErrFleetKindUnknown, kind)
	}
	switch kind {
	case KindRolePrompt:
		if role == "" {
			return chainKey{}, ErrRoleEmpty
		}
		return chainKey{Role: role}, nil
	case KindSkill:
		if subject == "" {
			return chainKey{}, ErrSubjectEmpty
		}
		return chainKey{Subject: subject}, nil
	default: // KindSelectionRule
		return chainKey{}, nil
	}
}
