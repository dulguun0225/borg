package targetseam

import (
	"context"
	"fmt"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/secretref"
)

// Call is one operation performed on a [Fake]: which one, who made it, on what,
// and with which credential reference. Credential is a reference, so a recorded
// call cannot contain a secret value however the fake is used — the way-in
// token a deployment carries is not recorded for the same reason.
type Call struct {
	Op        Op
	Principal principal.Principal
	Service   string
	Build     string
	// Share is what a shift asked for, and zero on every other operation.
	Share float64
	// Count is what an instance count asked for, and zero on every other
	// operation.
	Count int
	// Change is the schema change's identity, or the snapshot's name, and empty
	// on every other operation.
	Change     string
	Credential secretref.Ref
}

// Fake is the implementation of [Target] that reaches nothing: it records the
// calls made on it and answers ReadRunning from what Deploy and Stop left
// behind. Package localtarget is what the demonstrations deploy against.
type Fake struct {
	calls   []Call
	running map[string]string

	// Drains is whether Deploy reports a drain. A fake platform that cuts is
	// what the deploy record's per-target replacement field is read against.
	Drains bool
	// Instances is what ReadRunning reports as the capacity of every service,
	// which is what a kept-instance count is computed from.
	Instances int
	// SchemaHistory is what ReadRunning reports per service, written by
	// ApplySchemaChange as it applies each change.
	SchemaHistory map[string][]SchemaChangeApplied
	// ArtifactDigests is what ReadRunning reports per service, set by the test
	// that needs a digest to be compared against a build's.
	ArtifactDigests map[string]string
	// Snapshots is every snapshot taken, in order.
	Snapshots []Snapshot
	// RefuseShift is what a target declared as serving a share but unable to
	// shift one answers, which is what makes the strategy performed differ from
	// the one picked.
	RefuseShift error
}

var _ Target = (*Fake)(nil)

// NewFake returns a fake target with nothing running on it, draining what it
// replaces.
func NewFake() *Fake {
	return &Fake{
		running:         make(map[string]string),
		Drains:          true,
		SchemaHistory:   make(map[string][]SchemaChangeApplied),
		ArtifactDigests: make(map[string]string),
	}
}

// Deploy records the call and remembers the build as what is running for
// that service. It refuses a deployment [Deployment.Validate] refuses, which
// is what an implementation that reached something would do first.
func (f *Fake) Deploy(_ context.Context, p principal.Principal, d Deployment) (Placement, error) {
	if err := CheckPrincipal(p); err != nil {
		return Placement{}, err
	}
	if err := d.Validate(); err != nil {
		return Placement{}, err
	}
	f.calls = append(f.calls, Call{
		Op: OpDeploy, Principal: p, Service: d.Service, Build: d.Build, Credential: d.Credential,
	})
	f.running[d.Service] = d.Build
	if f.Drains {
		return Placement{Replacement: ReplacementDrained}, nil
	}
	return Placement{Replacement: ReplacementCut}, nil
}

// Stop records the call, forgets what was running for that service, and reports
// how those instances ended — the same [Fake.Drains] a deploy's replacement is
// reported by, a fake platform that cuts being what the deploy record's
// per-target replacement field is read against.
func (f *Fake) Stop(_ context.Context, p principal.Principal, service string, credential secretref.Ref) (Placement, error) {
	if err := CheckPrincipal(p); err != nil {
		return Placement{}, err
	}
	if err := check(service, credential); err != nil {
		return Placement{}, err
	}
	f.calls = append(f.calls, Call{Op: OpStop, Principal: p, Service: service, Credential: credential})
	delete(f.running, service)
	if f.Drains {
		return Placement{Replacement: ReplacementDrained}, nil
	}
	return Placement{Replacement: ReplacementCut}, nil
}

// ReadRunning records the call and answers with what Deploy last left for that
// service, or an empty build when nothing did.
func (f *Fake) ReadRunning(_ context.Context, p principal.Principal, service string, credential secretref.Ref) (Running, error) {
	if err := CheckPrincipal(p); err != nil {
		return Running{}, err
	}
	if err := check(service, credential); err != nil {
		return Running{}, err
	}
	f.calls = append(f.calls, Call{Op: OpReadRunning, Principal: p, Service: service, Credential: credential})
	running := Running{Service: service, Build: f.running[service], SchemaHistory: f.SchemaHistory[service]}
	if running.Build != "" {
		running.Instances = f.Instances
		running.ArtifactDigest = f.ArtifactDigests[service]
	}
	return running, nil
}

// ShiftTraffic records the call, or answers [Fake.RefuseShift] where the test
// set one — the platform that declared a share and cannot serve it.
func (f *Fake) ShiftTraffic(_ context.Context, p principal.Principal, s Shift) error {
	if err := CheckPrincipal(p); err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	if f.RefuseShift != nil {
		return f.RefuseShift
	}
	f.calls = append(f.calls, Call{
		Op: OpShiftTraffic, Principal: p, Service: s.Service, Build: s.Build,
		Share: s.Share, Credential: s.Credential,
	})
	return nil
}

// SetInstanceCount records the call and changes nothing else: the fake runs no
// instances, so what it has to show for one is the record of what was asked.
func (f *Fake) SetInstanceCount(_ context.Context, p principal.Principal, c InstanceCount) error {
	if err := CheckPrincipal(p); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	f.calls = append(f.calls, Call{
		Op: OpSetInstanceCount, Principal: p, Service: c.Service, Build: c.Build,
		Count: c.Count, Credential: c.Credential,
	})
	return nil
}

// ApplySchemaChange records the call and appends the change to the service's
// schema history, which is what [Fake.ReadRunning] then reports. A change marked
// found applied is written into the history and performed on nothing, which is
// what the deploy of an adopted service's first release asks for.
func (f *Fake) ApplySchemaChange(_ context.Context, p principal.Principal, c SchemaChange) error {
	if err := CheckPrincipal(p); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	f.calls = append(f.calls, Call{
		Op: OpApplySchemaChange, Principal: p, Service: c.Service,
		Change: c.Change, Credential: c.Credential,
	})
	f.SchemaHistory[c.Service] = append(f.SchemaHistory[c.Service], SchemaChangeApplied{
		Release: c.Release, Change: c.Change, Checksum: fmt.Sprintf("%x", len(c.Text)),
		Widened: !c.Destroys, FoundApplied: c.FoundApplied,
	})
	return nil
}

// Snapshot records the call and answers with a copy named as asked, digested
// over what the fake holds for the service.
func (f *Fake) Snapshot(_ context.Context, p principal.Principal, s SnapshotRequest) (Snapshot, error) {
	if err := CheckPrincipal(p); err != nil {
		return Snapshot{}, err
	}
	if err := s.Validate(); err != nil {
		return Snapshot{}, err
	}
	f.calls = append(f.calls, Call{
		Op: OpSnapshot, Principal: p, Service: s.Service, Change: s.Name, Credential: s.Credential,
	})
	taken := Snapshot{Name: s.Name, Digest: fmt.Sprintf("%x", len(f.SchemaHistory[s.Service]))}
	f.Snapshots = append(f.Snapshots, taken)
	return taken, nil
}

// Calls is every operation performed on the fake, in the order it was
// performed.
func (f *Fake) Calls() []Call { return f.calls }
