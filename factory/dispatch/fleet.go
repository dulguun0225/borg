package dispatch

import (
	"context"
	"errors"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
)

// Entry is what an owner composed for one role: the model an agent in it runs
// on, the credential that model is reached through, the scope the entry may be
// put on, and the operations it runs under.
//
// It is a value the composition supplies and not a record. The fleet entry the
// design has is a record an owner writes at Factory, with an effort, a
// processing location, and a lender's key on it; none of that is built, so
// what an entry carries here is what the composition knows — which is why
// [Fleet] is an interface and this package writes no fleet table.
type Entry struct {
	Role  Role
	Scope Scope
	// Model is what the role calls. Two entries may name one model: the
	// per-author prior is kept per model version, not per role or entry.
	Model agent.Model
	// ModelVersion is the author every version this entry authors names, and
	// the author the principal on every call carries.
	ModelVersion string
	// CredentialName is the reference the model was reached through, recorded
	// on every agent run this entry performs and never resolved here.
	CredentialName string
	// Effort is how long the model works before it answers, and is empty where
	// the provider offers none.
	Effort string
	// Operations narrows [Role.Operations]. An entry naming none runs under
	// the role's whole list.
	Operations []string
}

// Fleet is the entries an owner composed, matched by role and scope. It is an
// interface because the fleet entry is a record this factory does not write:
// the composition holds whatever it was configured with and answers this.
type Fleet interface {
	// EntryFor is the entry covering this role on this item, and false where
	// no entry covers it — which is the first of the six conditions that stop
	// a dispatch, and a hold row rather than a failure.
	EntryFor(ctx context.Context, role Role, on On) (Entry, bool, error)
}

// Prompts is the role prompt version in force per role, read off the artifact
// store's chain by the composition, which holds the approved version ids the
// store's own in-force read needs. False is the second condition that stops a
// dispatch: a stage whose role has no role prompt version in force.
type Prompts interface {
	InForce(ctx context.Context, role Role) (artifact.Artifact, bool, error)
}

// ErrHeld is returned where one of the conditions that stop a dispatch held.
// It is not a failure of the work: no page fires and no attempt counts, and
// the [Run] returned names the wait row the hold stands as, so a caller can
// say what is holding and how it would clear.
var ErrHeld = errors.New("dispatch: a condition stopped this dispatch, and it is a hold rather than a failed attempt")

// ErrOutOfAttempts is returned where the stage spent its attempt limit. The
// item is escalated before this is returned, which is the factory saying it
// cannot do this one.
var ErrOutOfAttempts = errors.New("dispatch: the stage used every attempt its limit allows, and the item is escalated")
