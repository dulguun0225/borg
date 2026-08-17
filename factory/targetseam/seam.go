package targetseam

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/secretref"
)

// Op is one of the operations [Target] declares. It is a name a caller can
// record and a policy can match on.
type Op string

const (
	// OpDeploy puts a release on a target.
	OpDeploy Op = "deploy"
	// OpStop takes a service off a target. What it does not do is delete
	// anything the service wrote.
	OpStop Op = "stop"
	// OpReadRunning asks the target what is running, which is the one
	// operation that changes nothing. The reconciler is its first caller.
	OpReadRunning Op = "read_running"
)

// Target is the whole of what an agent may do to a deploy target. Three
// operations start it; a fourth is added by editing this interface and
// nothing else, so what production access exists is answered by reading one
// declaration.
type Target interface {
	// Deploy puts the release d names on the target, reaching it with the
	// credential d references.
	Deploy(ctx context.Context, d Deployment) error
	// Stop takes the named service off the target, reaching it with the
	// credential the reference names.
	Stop(ctx context.Context, service string, credential secretref.Ref) error
	// ReadRunning is what the target says is running for the named service.
	ReadRunning(ctx context.Context, service string, credential secretref.Ref) (Running, error)
}

// Deployment is what one deploy names: the service, the release, and the
// credential to reach the target with. The credential is a reference and
// there is no field on this struct that could hold a value.
type Deployment struct {
	Service    string
	Release    string
	Credential secretref.Ref
}

// ErrIncomplete is returned for an operation missing a service, a release, or
// a credential reference.
var ErrIncomplete = errors.New("targetseam: the operation is incomplete")

// Validate reports whether the deployment may be attempted. An implementation
// calls it before it reaches anything.
func (d Deployment) Validate() error {
	if err := check(d.Service, d.Credential); err != nil {
		return err
	}
	if d.Release == "" {
		return fmt.Errorf("%w: service %q names no release", ErrIncomplete, d.Service)
	}
	return nil
}

// check is what every operation on a [Target] requires: something to act on,
// and a credential to reach the target with. [Deployment.Validate] is this
// plus the release, which is the one field only a deploy has.
func check(service string, credential secretref.Ref) error {
	switch {
	case service == "":
		return fmt.Errorf("%w: it names no service", ErrIncomplete)
	case credential.IsZero():
		return fmt.Errorf("%w: service %q references no credential", ErrIncomplete, service)
	}
	return nil
}

// Running is what a target reports for one service. An empty Release means
// nothing is running there.
type Running struct {
	Service string
	Release string
}
