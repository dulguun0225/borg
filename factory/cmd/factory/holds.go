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
// Eight of the fourteen holds this answers, and six it does not. The halt is
// package gate's own read; the drift mismatch is a firing's own read of that
// store; and the four left — a contract migration not shipped, the maximum
// concurrent kept fleets, an advisory match, and the maximum concurrent
// candidate environments authored on the production environment record — each
// read a record or a field that is not built, so this reports none of them and a
// deploy they should have held goes to a verdict.
func (p *path) Standing(ctx context.Context, s gate.Subjects) ([]string, error) {
	if !s.Row.Deploys() || s.ItemID == "" {
		return nil, nil
	}
	it, err := item.Get(ctx, p.d.pool, s.ItemID)
	if err != nil {
		return nil, err
	}
	svc, err := p.serviceOf(ctx, s.ServiceID)
	if err != nil {
		return nil, err
	}
	var standing []string
	// A service its owner has not marked provisioned holds at both deploy rows:
	// the repository, or the store on a persistent environment, is not written
	// as existing, so what the deploy reaches for is not there. It lifts when
	// the owner writes the field and holds nothing else meanwhile.
	if !svc.Provisioned.Written() {
		standing = append(standing, gate.HoldServiceNotProvisioned)
	}
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
		// The error budget, read at the firing the way every hold here is, and
		// raising the objective's own intent on the same reading. The two items
		// that pass it are [path.passesTheBudgetHold]'s, so an item that passes
		// is not reported as held here either.
		spent, err := p.objectiveHold(ctx, svc, it)
		if err != nil {
			return nil, err
		}
		if spent != "" {
			standing = append(standing, gate.HoldErrorBudgetExhausted)
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

// The three [contractcheck.StoreState] readings below are all of one thing this
// composition does not have: a candidate environment with a store in it. This
// platform's environment is a directory the deployer copies a binary into, and
// the target seam it reaches that directory through applies a schema change and
// takes a snapshot against nothing. So each answers with what it has, which is
// nothing, and none of them answers by claiming the candidate had no change.
//
// What that costs is that a candidate whose build declares a store change cannot
// merge here: the store rule rejects a declared schema change and a declared
// backfill the candidate environment did not exercise, and this is the seam that
// would have exercised them. That is the reading the design gives — an exercise
// nothing performed is not one that passed — and what lifts it is a candidate
// environment with a store, not a value here.

// Rows is [contractcheck.StoreState]: what the candidate's run left in its
// environment's own store for one store contract. There is no store on this
// platform's candidate environment, so there is nothing to read and the answer
// is none — which enforcement reads as undecided, the way no exchange document
// is.
func (p *path) Rows(context.Context, contractcheck.Candidate, string) ([]consumercontract.Document, error) {
	return nil, nil
}

// AppliedTwice is [contractcheck.StoreState]: what a second application of the
// candidate's change changed on its environment. Nothing applied it here, there
// being no store on this platform's candidate environment to apply it to, which
// is what Ran false says — and a candidate that declares a schema change or a
// backfill is rejected at Merge to master on the strength of it.
func (p *path) AppliedTwice(context.Context, contractcheck.Candidate) (contractcheck.SecondApplication, error) {
	return contractcheck.SecondApplication{}, nil
}

// Snapshot is [contractcheck.StoreState]: the snapshot the candidate environment
// took and verified before a change that destroys stored data. There is no store
// on this platform's candidate environment to snapshot, so this reports one
// neither taken nor verified with the reason on it — and enforcement asks it only
// where the candidate declares a change that destroys data.
func (p *path) Snapshot(context.Context, contractcheck.Candidate) (contractcheck.Snapshot, error) {
	return contractcheck.Snapshot{
		Why: "this platform's candidate environment has no store, so nothing snapshotted one",
	}, nil
}
