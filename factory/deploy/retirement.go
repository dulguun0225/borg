package deploy

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
)

// The removal: what an owner's write of retired on a service record calls the
// deployer for. It ends every instance of the service on every target of every
// persistent environment and writes a deploy record per environment naming no
// release, complete per target as the instances there end.

// ErrRemovalIncomplete is returned by [Remove] for a removal naming no service
// or reaching no environment. Either would end nothing while reading as a
// retirement that had been performed.
var ErrRemovalIncomplete = errors.New("deploy: the removal names no service or reaches no environment")

// Environment is one persistent environment a removal is performed on: its
// record's id, the credential a deploy into it is performed with, and its
// targets in the environment's order. The caller reads all three off the
// environment record, which this package does not import.
type Environment struct {
	EnvironmentID string
	Credential    secretref.Ref
	// Reaches are the targets the service runs on, in the environment's order.
	Reaches []Reach
}

// Removal is what the deployer is asked to perform when a service is retired.
type Removal struct {
	// Actor is who the records name, which is the deployer: retiring is an
	// owner's write on the service record, and performing the removal is the
	// deployer's own act.
	Actor record.Actor
	// Principal is who the calls at seam 4 are made as.
	Principal principal.Principal

	ServiceID string
	// ServiceName is what the target acts on, where ServiceID is what the record
	// stores.
	ServiceName string
	// From is every persistent environment the service runs in, in whatever order
	// the caller read them: a removal is one deploy record per environment and no
	// environment is reached before another.
	From []Environment
}

// Remove performs the removal and returns the record it wrote per environment.
// Each is an ordinary deploy of nothing: [Perform] already reaches a target with
// the seam's operation that ends instances where the deploy names no build, so
// what this adds is the record per environment and the refusals above.
//
// A removal names no strategy and is not performed into production as a deploy
// of a release is, because a strategy is how a release takes traffic from the
// build it replaces and a removal delivers no release. Nothing on the record
// says a strategy ran, which is what a reader of it reads.
//
// A stop at any environment leaves the records already written standing, each
// complete on the targets it reached: what the drift detector then reads is the
// intended state as far as the deployer got, and the caller performs the removal
// again for the rest.
func Remove(ctx context.Context, w *Writer, r Removal) ([]Deploy, error) {
	if r.ServiceID == "" || r.ServiceName == "" || len(r.From) == 0 {
		return nil, fmt.Errorf("%w: service %q named %q, %d environments",
			ErrRemovalIncomplete, r.ServiceID, r.ServiceName, len(r.From))
	}

	written := make([]Deploy, 0, len(r.From))
	for _, from := range r.From {
		if from.EnvironmentID == "" || len(from.Reaches) == 0 {
			return written, fmt.Errorf("%w: environment %q with %d targets",
				ErrRemovalIncomplete, from.EnvironmentID, len(from.Reaches))
		}
		removed, err := Perform(ctx, w, Performance{
			Actor:       r.Actor,
			Principal:   r.Principal,
			ServiceID:   r.ServiceID,
			ServiceName: r.ServiceName,

			EnvironmentID: from.EnvironmentID,
			What:          OfRemoval(),
			Credential:    from.Credential,
			Reaches:       from.Reaches,
		})
		if err != nil {
			return written, err
		}
		written = append(written, removed)
	}
	return written, nil
}
