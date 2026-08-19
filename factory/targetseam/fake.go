package targetseam

import (
	"context"

	"github.com/dulguun0225/borg/factory/secretref"
)

// Call is one operation performed on a [Fake]: which one, on what, and with
// which credential reference. Credential is a reference, so a recorded call
// cannot contain a secret value however the fake is used.
type Call struct {
	Op         Op
	Service    string
	Build      string
	Credential secretref.Ref
}

// Fake is the only implementation of [Target]. It reaches nothing: it records
// the calls made on it and answers ReadRunning from what Deploy and Stop left
// behind.
type Fake struct {
	calls   []Call
	running map[string]string
}

var _ Target = (*Fake)(nil)

// NewFake returns a fake target with nothing running on it.
func NewFake() *Fake {
	return &Fake{running: make(map[string]string)}
}

// Deploy records the call and remembers the build as what is running for
// that service. It refuses a deployment [Deployment.Validate] refuses, which
// is what an implementation that reached something would do first.
func (f *Fake) Deploy(_ context.Context, d Deployment) error {
	if err := d.Validate(); err != nil {
		return err
	}
	f.calls = append(f.calls, Call{Op: OpDeploy, Service: d.Service, Build: d.Build, Credential: d.Credential})
	f.running[d.Service] = d.Build
	return nil
}

// Stop records the call and forgets what was running for that service.
func (f *Fake) Stop(_ context.Context, service string, credential secretref.Ref) error {
	if err := check(service, credential); err != nil {
		return err
	}
	f.calls = append(f.calls, Call{Op: OpStop, Service: service, Credential: credential})
	delete(f.running, service)
	return nil
}

// ReadRunning records the call and answers with what Deploy last left for that
// service, or an empty build when nothing did.
func (f *Fake) ReadRunning(_ context.Context, service string, credential secretref.Ref) (Running, error) {
	if err := check(service, credential); err != nil {
		return Running{}, err
	}
	f.calls = append(f.calls, Call{Op: OpReadRunning, Service: service, Credential: credential})
	return Running{Service: service, Build: f.running[service]}, nil
}

// Calls is every operation performed on the fake, in the order it was
// performed.
func (f *Fake) Calls() []Call { return f.calls }
