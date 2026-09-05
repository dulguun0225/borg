package service

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

// CredentialShape is which of two shapes the repository host gave the factory's
// credentials for one service.
type CredentialShape string

const (
	// ShapeOne is a host that cannot restrict a credential by branch: one
	// credential does everything, and the exclusivity master is meant to have is
	// a convention detected after the fact by the queue's reading.
	ShapeOne CredentialShape = "one"
	// ShapeTwo is a host that can tell master from a branch: one credential
	// pushes a candidate branch and nothing to master, and the other, resolved
	// to the merge queue alone, fast-forwards master.
	ShapeTwo CredentialShape = "two"
)

// Shapes is both, in the order the design names them.
var Shapes = []CredentialShape{ShapeOne, ShapeTwo}

// Provisioned is what an owner recorded once the repository exists with the
// factory's credentials holding write access to it and the service's store
// exists on every persistent environment. It is empty until they write it, and
// an item on a service that is not provisioned is stopped where the factory
// would first reach for what is missing.
type Provisioned struct {
	// At is when the owner wrote it, and empty while nothing has.
	At string
	// Shape is which of the two shapes the repository host gave the credentials.
	Shape CredentialShape
	// BranchCredential is resolved to the implementation stage's run and to the
	// build runner's clone. It is the only one under [ShapeOne].
	BranchCredential secretref.Ref
	// MasterCredential is resolved to the merge queue alone, and names nothing
	// under [ShapeOne].
	MasterCredential secretref.Ref
}

// Written reports whether an owner has written the field at all.
func (p Provisioned) Written() bool { return p.At != "" }

var (
	// ErrShapeUnknown is returned by [SetProvisioned] for a shape that is
	// neither of the two the repository host can give.
	ErrShapeUnknown = errors.New("service: the repository credential shape is one or two")
	// ErrCredentialsDoNotMatchShape is returned by [SetProvisioned] where the
	// credentials named do not match the shape: shape two without a master
	// credential, or shape one with one.
	ErrCredentialsDoNotMatchShape = errors.New("service: the credentials named do not match the shape")
	// ErrTargetNotInEnvironment is returned by [SetTargets] for a target the
	// environment does not hold.
	ErrTargetNotInEnvironment = errors.New("service: the environment holds no such target")
	// ErrRetiredNotEmpty is returned by [Retire] where something still names the
	// service: a consumer contract in force, an unmerged item, or an unmerged
	// item's declared dependency.
	ErrRetiredNotEmpty = errors.New("service: something still names the service")
)

// SetProvisioned writes provisioned on one service, with the shape the
// repository host gave the credentials and the names of the credentials
// themselves. Decomposition never writes it, which is what keeps the seam
// doc.go states.
//
// It takes the caller's transaction and no token, the way every other
// owner-authored write on this record does: the caller is package policy, which
// fences the transaction it appends the policy version in.
func SetProvisioned(ctx context.Context, tx pgx.Tx, serviceID string,
	shape CredentialShape, branch, master secretref.Ref) error {
	if !slices.Contains(Shapes, shape) {
		return fmt.Errorf("%w: %q", ErrShapeUnknown, shape)
	}
	if branch.IsZero() {
		return fmt.Errorf("%w: shape %s names no branch credential", ErrCredentialsDoNotMatchShape, shape)
	}
	if (shape == ShapeTwo) != !master.IsZero() {
		return fmt.Errorf("%w: shape %s with the master credential %s", ErrCredentialsDoNotMatchShape, shape, master)
	}
	tag, err := tx.Exec(ctx, `update `+Table+`
		set provisioned_at = $1, repository_credential_shape = $2,
		repository_credential_branch = $3, repository_credential_master = $4
		where id = $5`,
		record.Now(), string(shape), branch.Name(), master.Name(), serviceID)
	if err != nil {
		return fmt.Errorf("service: writing provisioned on %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// SetTargets writes which of the environment's targets this service runs on, in
// the order a rollout reaches them. Where an owner names none the service runs
// on every target of the environment, so an empty list is a real value and not a
// refusal.
//
// environmentTargets is the target list of the environment the owner is naming
// targets in, supplied by the caller: this package cannot read an environment
// record — the edge is not in deps.txt and the direction would be the wrong one
// — so the check the design requires is made here over what the caller read.
func SetTargets(ctx context.Context, tx pgx.Tx, serviceID string, targets, environmentTargets []string) error {
	for _, target := range targets {
		if target == "" {
			return fmt.Errorf("%w: an empty address", ErrTargetNotInEnvironment)
		}
		if strings.Contains(target, "\n") {
			return fmt.Errorf("%w: %q holds a line ending", ErrTargetNotInEnvironment, target)
		}
		if !slices.Contains(environmentTargets, target) {
			return fmt.Errorf("%w: %q", ErrTargetNotInEnvironment, target)
		}
	}
	tag, err := tx.Exec(ctx, `update `+Table+` set targets = $1 where id = $2`,
		joinTargets(targets), serviceID)
	if err != nil {
		return fmt.Errorf("service: writing the targets of %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// Retire writes retired on one service, which is the one thing that ends one.
// The write is refused while a consumer contract in force names the service,
// while an unmerged item names it, or while an unmerged item's declared
// dependency points at it.
//
// The three are counts the caller computed and passed, not queries made here:
// each is a read of a package this one may not import — deps.txt has
// consumercontract and item pointing the other way — so the refusal is stated
// here over numbers the caller read inside the same transaction. What that costs
// is a caller that can pass a count it did not read.
//
// Retiring a service the owner has already retired writes the timestamp again,
// which is the same state and not a second retirement; nothing revives one, so
// there is no write that clears the field.
func Retire(ctx context.Context, tx pgx.Tx, serviceID string,
	consumerContractsInForce, unmergedItems, unmergedItemsDependingOnIt int) error {
	for _, still := range []struct {
		count int
		what  string
	}{
		{consumerContractsInForce, "consumer contracts in force name it"},
		{unmergedItems, "unmerged items name it"},
		{unmergedItemsDependingOnIt, "unmerged items declare a dependency on it"},
	} {
		if still.count < 0 {
			return fmt.Errorf("service: %s is not a count: %d", still.what, still.count)
		}
		if still.count > 0 {
			return fmt.Errorf("%w: %d %s", ErrRetiredNotEmpty, still.count, still.what)
		}
	}
	tag, err := tx.Exec(ctx, `update `+Table+` set retired_at = $1 where id = $2`, record.Now(), serviceID)
	if err != nil {
		return fmt.Errorf("service: retiring %s: %w", serviceID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: %s", ErrNotFound, serviceID)
	}
	return nil
}

// The targets a service runs on are stored as one column holding one address per
// line, the way an environment's own targets are: plural is what the design
// requires, a column is what it says they are, and a newline is the separator
// because an address may hold a comma and may not hold a line ending.

func joinTargets(targets []string) string { return strings.Join(targets, "\n") }

func splitTargets(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}
