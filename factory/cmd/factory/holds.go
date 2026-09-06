package main

import (
	"context"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
)

// The three seams a gate and enforcement are composed with here, each a read
// over records the package that needs it may not reach: the factory's own holds
// at a deploy row, the candidate environment's own store after the run, and
// which backfills a deploy record marks complete.

// Standing is [gate.Holds]: the factory's own holds at one firing, recomputed
// every time it is asked. None of them is written anywhere — each is computed
// from records that already exist, and the design gives such a hold no row: a
// record for it would be a decision where nothing is decided, and re-testing
// would append one every time the gate re-fired.
//
// Six of the fourteen holds this answers, and eight it does not. The halt is
// package gate's own read; the drift mismatch is a firing's own read of that
// store; and the six left — a contract migration not shipped, a service not
// provisioned, an error budget exhausted, the maximum concurrent kept fleets, an
// advisory match, and the maximum concurrent candidate environments authored on
// the production environment record — each read a record or a field that is not
// built, so this reports none of them and a deploy they should have held goes to
// a verdict.
func (p *path) Standing(ctx context.Context, s gate.Subjects) ([]string, error) {
	if !s.Row.Deploys() || s.ItemID == "" {
		return nil, nil
	}
	it, err := item.Get(ctx, p.d.pool, s.ItemID)
	if err != nil {
		return nil, err
	}
	var standing []string
	switch s.Row.Kind {
	case gate.KindDeployToCandidateEnvironment:
		held, err := p.dependencyHold(ctx, it)
		if err != nil {
			return nil, err
		}
		if held != "" {
			standing = append(standing, gate.HoldDependencyNotLive)
		}
		live, err := environment.CountLiveCandidates(ctx, p.d.pool, p.production.ID)
		if err != nil {
			return nil, err
		}
		if live >= p.d.candidateCeiling {
			standing = append(standing, gate.HoldNoRoomOnThePlatform)
		}
	default:
		svc, err := p.serviceOf(ctx, s.ServiceID)
		if err != nil {
			return nil, err
		}
		held, err := p.dependencyHold(ctx, it)
		if err != nil {
			return nil, err
		}
		if held != "" {
			standing = append(standing, gate.HoldDependencyNotCurrent)
		}
		room, _, _, err := p.healthMonitor.Room(ctx, svc.ID)
		if err != nil {
			return nil, err
		}
		if !room {
			standing = append(standing, gate.HoldWindowLimitReached)
		}
		awaiting, err := p.rollbackHold(ctx, svc, it)
		if err != nil {
			return nil, err
		}
		if awaiting != "" {
			standing = append(standing, gate.HoldRollbackAwaitingRevert)
		}
		// The change freeze is read at the moment of the firing, which is what
		// makes the hold lift itself: the next firing reads a moment outside
		// every period the owner authored and finds none.
		frozen, _, err := service.Frozen(ctx, p.d.pool, svc.ID, record.Now())
		if err != nil {
			return nil, err
		}
		if frozen {
			standing = append(standing, gate.HoldChangeFreeze)
		}
	}
	return standing, nil
}

// Rows is [contractcheck.StoreState]: what the candidate's run left in its
// environment's own store for one store contract. Nothing in this interface
// writes a store contract and no build here declares one, so there is nothing to
// read and the answer is none — which enforcement reads as undecided, the way no
// exchange document is.
func (p *path) Rows(context.Context, contractcheck.Candidate, string) ([]consumercontract.Document, error) {
	return nil, nil
}

// AppliedTwice is [contractcheck.StoreState]: what a second application of the
// candidate's schema change changed on its environment. No build on this path
// declares a schema change — nothing here derives one from a build — so the
// second application did not run, which is what Ran false says.
func (p *path) AppliedTwice(context.Context, contractcheck.Candidate) (contractcheck.SecondApplication, error) {
	return contractcheck.SecondApplication{}, nil
}

// Snapshot is [contractcheck.StoreState]: the snapshot the candidate environment
// took and verified before a change that destroys stored data. There is no such
// change to take one before, for the reason [path.AppliedTwice] gives, so this
// reports one neither taken nor verified with the reason on it — and enforcement
// asks it only where the candidate declares a change that destroys data.
func (p *path) Snapshot(context.Context, contractcheck.Candidate) (contractcheck.Snapshot, error) {
	return contractcheck.Snapshot{
		Why: "no build on this path declares a schema change, so no candidate environment has taken a snapshot",
	}, nil
}
