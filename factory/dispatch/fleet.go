package dispatch

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/fleetentry"
)

// Entry is the fleet entry a dispatch matched, as this component reads it: the
// record's own fields, plus the client an agent in it calls.
//
// Every field but [Entry.Model] and [Entry.Operations] is a column of
// [fleetentry.Entry], copied here rather than the record carried, because a
// dispatch is matched once and what it ran on goes onto each agent run record
// from this. [Entry.Operations] is the role's list narrowed by the entry, which
// this package computes; [Entry.Model] is the one thing a record cannot hold.
type Entry struct {
	// ID is the fleet entry record this was read from, which the reason on a
	// withheld class of material names.
	ID    string
	Role  Role
	Scope Scope
	// Model is what the role calls, constructed by [Models] for this entry.
	// Two entries may name one model: the per-author prior is kept per model
	// version, not per role or entry.
	Model agent.Model
	// ModelVersion is the author every version this entry authors names, and
	// the author the principal on every call carries.
	ModelVersion string
	// CredentialName is the reference the model was reached through, recorded
	// on every agent run this entry performs and never resolved here. It is
	// what the two credential holds are computed against.
	CredentialName string
	// Effort is how long the model works before it answers, and is empty where
	// the provider offers none.
	Effort string
	// ProcessingLocation is the provider and the region the credential
	// resolves to, written onto every agent run record this entry performs.
	ProcessingLocation string
	// ReadsAtOnce is how much the model reads at once, recorded on the input
	// manifest as the bound that was applied. Nothing truncates a read against
	// it: the selection that would is context assembly's, which is not built.
	ReadsAtOnce int64
	// MaterialClasses is the classes of material this entry may be handed, out
	// of [fleetentry.MaterialClasses]. A class it does not name is withheld
	// before the run and recorded on the manifest as excluded.
	MaterialClasses []string
	// Operations narrows [Role.Operations]. The operations belong to the role
	// and an owner may narrow them on the entry, which is not built: the record
	// holds no column for a narrowed list, so every entry read from it runs
	// under the role's whole list, and [Role.Narrow] is what an owner's
	// narrowing would go through once there is one to read.
	Operations []string
}

// Models is the client an agent in one entry calls, which is the one thing the
// fleet entry record cannot hold: the record names a model version and a
// credential, and what answers them is a provider client the composition
// constructs. It is an interface for the reason [Escalation] is — which
// provider a credential resolves to is the composition's knowledge and not
// this package's.
type Models interface {
	// For is the client for this entry, constructed from its model version and
	// its credential name.
	For(ctx context.Context, entry fleetentry.Entry) (agent.Model, error)
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

// ErrMaterialClassUnknown is returned for material whose class is not one of
// [fleetentry.MaterialClasses]. The classes an entry names and the classes a
// stage hands over are one vocabulary: a class outside it could be matched
// against no entry, so it is refused rather than withheld silently.
var ErrMaterialClassUnknown = errors.New("dispatch: the material names a class no fleet entry can name")

// matchFor is the match against the record: the entries in force for the role,
// in the order an owner wrote them, and the first whose scope covers the item.
// None is [HoldNoEntryCoversTheStage].
//
// It reads the record and constructs no client, which is what a re-match needs:
// re-testing a hold asks whether an entry covers the stage and not what would
// answer its calls.
func (d *Dispatch) matchFor(ctx context.Context, role Role, on On) (fleetentry.Entry, bool, error) {
	if _, err := role.Stage(); err != nil && !role.OnAnIntent() {
		return fleetentry.Entry{}, false, err
	}
	inForce, err := fleetentry.InForceForRole(ctx, d.c.Pool, string(role))
	if err != nil {
		return fleetentry.Entry{}, false, err
	}
	for _, stored := range inForce {
		if scopeOf(stored.Scope).Covers(on) {
			return stored, true, nil
		}
	}
	return fleetentry.Entry{}, false, nil
}

// entryFor is [Dispatch.matchFor] with the client the entry runs on, which is
// what a run needs and a re-match does not.
func (d *Dispatch) entryFor(ctx context.Context, role Role, on On) (Entry, bool, error) {
	stored, found, err := d.matchFor(ctx, role, on)
	if err != nil || !found {
		return Entry{}, false, err
	}
	model, err := d.c.Models.For(ctx, stored)
	if err != nil {
		return Entry{}, false, fmt.Errorf("dispatch: the client for entry %s on %s: %w", stored.ID, stored.ModelVersion, err)
	}
	return Entry{
		ID:                 stored.ID,
		Role:               role,
		Scope:              scopeOf(stored.Scope),
		Model:              model,
		ModelVersion:       stored.ModelVersion,
		CredentialName:     stored.CredentialName,
		Effort:             stored.Effort,
		ProcessingLocation: stored.ProcessingLocation,
		ReadsAtOnce:        stored.ReadsAtOnce,
		MaterialClasses:    stored.MaterialClasses,
	}, true, nil
}

// scopeOf is the record's scope as this package's own: the same three fields,
// spelled apart because the match is made here and the record is stored there,
// and a scope this package could not name would be one it could not match.
func scopeOf(stored fleetentry.Scope) Scope {
	return Scope{ProjectID: stored.ProjectID, ServiceID: stored.ServiceID, AreaID: stored.AreaID}
}
