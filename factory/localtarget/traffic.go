package localtarget

import (
	"context"
	"errors"
	"fmt"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// The two operations this platform cannot perform, and what it answers instead.
// It moves a process rather than traffic: one process per service in one
// directory, with nothing in front of it deciding what fraction of arriving
// requests reaches which build and nothing able to run two instances of one
// build. Both refuse and say so, rather than returning without having done
// anything: a shift that reported success would be a rollout with a control
// recorded as having compared two builds while one of them never served a
// request, which is the reading the strategy performed exists to prevent.
//
// The deployer is what records the refusal. A target declared as serving a
// share that refuses the shift is a production deploy whose strategy performed
// is without a control beside a strategy picked with one; a mitigation the
// deployer cannot perform is a mitigation record naming what was asked and the
// deploy it would have modified.

var (
	// ErrNoShare is returned by [Local.ShiftTraffic]. This platform serves no
	// share, which is what an environment record declares per target with
	// `noshare` and what makes the row with a control unavailable there.
	ErrNoShare = errors.New("localtarget: this platform moves a process rather than traffic, so it serves no share")
	// ErrOneInstance is returned by [Local.SetInstanceCount] for any count but
	// the one instance this platform runs. A count of one is what already runs
	// after a deploy and is answered without doing anything.
	ErrOneInstance = errors.New("localtarget: this platform runs one instance of a build and cannot run another number")
)

// ShiftTraffic refuses: this platform decides no fraction of arriving traffic.
// The refusal is the operation's answer and not an error in performing it — the
// deployer reads it and writes the strategy it performed.
func (l *Local) ShiftTraffic(_ context.Context, p principal.Principal, s targetseam.Shift) error {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if err := s.Validate(); err != nil {
		return err
	}
	return fmt.Errorf("%w: service %q asked for %v", ErrNoShare, s.Service, s.Share)
}

// SetInstanceCount answers a count of one, which is what a deploy already left
// running, and refuses every other: this platform runs one process per service.
// A count of none is [Local.Stop]'s and not this operation's, so it is refused
// here too rather than quietly ending what runs.
func (l *Local) SetInstanceCount(_ context.Context, p principal.Principal, c targetseam.InstanceCount) error {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if c.Count == 1 {
		return nil
	}
	return fmt.Errorf("%w: service %q asked for %d", ErrOneInstance, c.Service, c.Count)
}
