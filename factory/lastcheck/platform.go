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
// reads what the platform reports and computes nothing over it — which is why
// this holds three counts and derives no fourth.
type PlatformPass struct {
	StandingByTheRecords int  `json:"standing_by_the_records"`
	HeldByThePlatform    int  `json:"held_by_the_platform"`
	Room                 int  `json:"room,omitempty"`
	RoomReported         bool `json:"room_reported"`
}

// RecordPlatformPass writes the deployer's pass over one platform, overwriting
// the record it keeps for that platform. The subject is the production
// environment record that declares the platform, and not the platform's own
// name: one of each per production environment, the way the maximum concurrent
// candidate environments is already keyed, so an install whose projects run on
// two platforms adds neither count across them.
//
// It is the one write here that composes the payload rather than taking it as
// text: the three counts are the design's, a screen reads them back through
// [PlatformPassOf], and a payload composed at each caller would be the same
// shape spelled twice.
//
// It is the sole writer of that record. The pass is the deployer's, which lives
// in the command-line interface, and the composition calls it once per
// production environment record that declares a platform, on every production
// deploy, beside deploy.RecordTargetCheck over each of that environment's
// targets.
func (w *Writer) RecordPlatformPass(ctx context.Context, actor record.Actor, environmentID string,
	interval time.Duration, pass PlatformPass) (LastCheck, error) {

	if environmentID == "" {
		return LastCheck{}, fmt.Errorf("%w: the deployer's platform record names the production environment it passed over",
			ErrSubjectDoesNotMatchComponent)
	}
	payload, err := json.Marshal(pass)
	if err != nil {
		return LastCheck{}, fmt.Errorf("lastcheck: encoding the pass over the platform of %q: %w", environmentID, err)
	}
	return w.Record(ctx, actor, LastCheck{
		Component: ComponentDeployer,
		Subject:   environmentID,
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
