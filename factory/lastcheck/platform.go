package lastcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// The deployer's per-platform record: the one last check whose payload this
// package gives a shape, because the design names the three counts it carries
// and a screen reads them back.

// PlatformPass is what one pass over a platform reports: how many candidate
// environments the factory's records hold as standing, how many the platform
// reports holding for the factory, and the room the platform reports where it
// reports one.
//
// The room is read and never modelled. Where the platform reports no figure,
// RoomReported is false and the two counts are what a reader shows: the factory
// reads what the platform reports and computes nothing over it.
type PlatformPass struct {
	StandingByTheRecords int  `json:"standing_by_the_records"`
	HeldByThePlatform    int  `json:"held_by_the_platform"`
	Room                 int  `json:"room,omitempty"`
	RoomReported         bool `json:"room_reported"`
}

// Leaked is how many candidate environments the platform holds beyond what the
// records say stand: a teardown that failed, which the deployer tears down again
// on its next pass, keyed on the environment. It is never negative — a platform
// holding fewer than the records say is a platform that lost one, which this
// count does not describe.
func (p PlatformPass) Leaked() int {
	if p.HeldByThePlatform <= p.StandingByTheRecords {
		return 0
	}
	return p.HeldByThePlatform - p.StandingByTheRecords
}

// RecordPlatformPass writes the deployer's pass over one platform, overwriting
// the record it keeps for that platform. The subject is the platform's name, as
// the production environment record declares it.
//
// It is the one write here that composes the payload rather than taking it as
// text: the three counts are the design's, a screen reads them back through
// [PlatformPassOf], and a payload composed at each caller would be the same
// shape spelled twice.
//
// Nothing calls it yet. The pass is the deployer's, which lives in the
// command-line interface, and the composition owes one call per production
// environment record that declares a platform, each pass.
func (w *Writer) RecordPlatformPass(ctx context.Context, actor record.Actor, platformName string,
	interval time.Duration, pass PlatformPass) (LastCheck, error) {

	if platformName == "" {
		return LastCheck{}, fmt.Errorf("%w: the deployer's platform record names the platform it passed over",
			ErrSubjectDoesNotMatchComponent)
	}
	payload, err := json.Marshal(pass)
	if err != nil {
		return LastCheck{}, fmt.Errorf("lastcheck: encoding the pass over platform %q: %w", platformName, err)
	}
	return w.Record(ctx, actor, LastCheck{
		Component: ComponentDeployer,
		Subject:   platformName,
		Interval:  interval,
		Payload:   string(payload),
	})
}

// PlatformPassOf is the three counts off one of the deployer's platform records.
// A record whose payload is not a platform pass is an error and not a zero
// reading: three counts of nothing and a platform that reported nothing are
// different things.
func PlatformPassOf(c LastCheck) (PlatformPass, error) {
	if c.Component != ComponentDeployer || c.Subject == "" {
		return PlatformPass{}, fmt.Errorf("lastcheck: %s over %q is not a pass over a platform", c.Component, c.Subject)
	}
	var pass PlatformPass
	if err := json.Unmarshal([]byte(c.Payload), &pass); err != nil {
		return PlatformPass{}, fmt.Errorf("lastcheck: reading the pass over platform %q: %w", c.Subject, err)
	}
	return pass, nil
}
