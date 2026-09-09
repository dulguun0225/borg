package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/healthmonitor"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
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
		// that pass it are [healthmonitor.Budget.Admits]'s, so an item that passes
		// is not reported as held here either.
		spent, err := p.objectiveHold(ctx, svc, it)
		if err != nil {
			return nil, err
		}
		if spent != "" {
			standing = append(standing, gate.HoldErrorBudgetExhausted)
		}
		frozen, err := p.changeFreezeHold(ctx, svc, it)
		if err != nil {
			return nil, err
		}
		if frozen != "" {
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

// changeFreezeHold is the change freeze at the moment of the firing, read the
// way every hold here is: whether the service names a period covering now, and
// the condition where it does — empty where either it does not or the item
// passes the same two exceptions a halt takes. The hold lifts itself: the next
// firing reads a moment outside every period the owner authored and finds
// none, so nothing here writes anything.
func (p *path) changeFreezeHold(ctx context.Context, svc service.Service, it item.Item) (string, error) {
	frozen, period, err := service.Frozen(ctx, p.d.pool, svc.ID, record.Now())
	if err != nil || !frozen {
		return "", err
	}
	excepted, err := p.passesAFreeze(ctx, it)
	if err != nil || excepted {
		return "", err
	}
	return fmt.Sprintf("%s — %s freezes changes from %s to %s", gate.HoldChangeFreeze, svc.ID, period.StartsAt, period.EndsAt), nil
}

// passesAFreeze is the two items a change freeze lets through, the same two a
// halt does: a revert, which passes the halt's own exception for the same
// reason, and an item whose intent's source is the factory's own with the
// health monitor as the detector. Without either a freeze would hold the fix
// for what the freeze made worse — a rollback's own revert, or the item a
// crossing raised while the freeze already stood.
func (p *path) passesAFreeze(ctx context.Context, it item.Item) (bool, error) {
	revert, err := p.IsARevert(ctx, it)
	if err != nil || revert {
		return revert, err
	}
	return p.raisedByTheHealthMonitor(ctx, it.ID)
}

// IsARevert is [mergequeue.Reverts]: whether an item is a revert, read from
// the intent it was decomposed from. Nothing on the item says it is one — the
// release it undoes is reachable through the intent's evidence instead,
// written the same way whichever of the two sources raised it: the health
// monitor at a failed exit, or a named human at Ops naming the release. A
// halt or freeze passes a revert by reading that link, so this reads the
// evidence and never the intent's source.
func (p *path) IsARevert(ctx context.Context, it item.Item) (bool, error) {
	if it.IntentID == "" {
		return false, nil
	}
	raised, err := intent.Get(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return false, err
	}
	if raised.Evidence == "" {
		return false, nil
	}
	var evidence intent.Evidence
	if err := json.Unmarshal([]byte(raised.Evidence), &evidence); err != nil {
		return false, fmt.Errorf("factory: reading the evidence on intent %s: %w", raised.ID, err)
	}
	return evidence.ReleaseID != "", nil
}

// dependencyHold is the factory's own hold at both deploy rows: a declared
// dependency that is not its service's current release. At the candidate deploy
// row the question is whether it is live at all, the environment being composed
// from it; at the production deploy row, whether it is live still.
//
// It returns the words the hold is reported with, and nothing where every
// dependency is live. Nothing is written either way — a hold over a record that
// already exists is recomputed at every firing.
func (p *path) dependencyHold(ctx context.Context, it item.Item) (string, error) {
	for _, waitsOn := range it.WaitsOn {
		dependency, err := item.Get(ctx, p.d.pool, waitsOn)
		if err != nil {
			return "", err
		}
		addresses, err := p.addressesOf(ctx, dependency.ServiceID)
		if err != nil {
			return "", err
		}
		current, found, err := deploy.Current(ctx, p.d.pool, dependency.ServiceID, p.production.ID, addresses)
		if err != nil {
			return "", err
		}
		if !found {
			return fmt.Sprintf("%s — %s is running nothing, so item %s is not live",
				gate.HoldDependencyNotLive, dependency.ServiceID, waitsOn), nil
		}
		rel, err := release.Get(ctx, p.d.pool, current.ReleaseID)
		if err != nil {
			return "", err
		}
		if rel.ItemID != waitsOn {
			return fmt.Sprintf("%s — %s is running release %d, which is item %s and not item %s",
				gate.HoldDependencyNotLive, dependency.ServiceID, rel.Number, rel.ItemID, waitsOn), nil
		}
	}
	return "", nil
}

// objectiveHold is the two things a service level objective does, read from one
// budget: the hold an exhausted budget sets on that service's production
// deploys, and the intent the objective raises. The hold lifts itself when the
// period rolls forward far enough to restore the budget, nothing is decided and
// no page fires — the shape the hold a dependency that is not current sets
// already has. A budget the store does not cover is uncomputed and holds the way
// an exhausted one does, a budget taken as intact over records that are not
// there being an absent input read as evidence.
//
// The raise is on the same reading because the two are one mechanism: the fix
// for whatever exhausted the budget is itself a production deploy, and the item
// that passes the hold on a service that crossed nothing is the one this raise
// takes in. A budget read as exhausted with nothing raised on it would be a hold
// no item could lift.
//
// Where an owner authored no objective there is no budget, nothing is held and
// nothing is raised: that reading and the window are the whole of what protects
// the service.
func (p *path) objectiveHold(ctx context.Context, svc service.Service, it item.Item) (string, error) {
	w := healthmonitor.Watching{ID: svc.ID, Name: svc.Name, EnvironmentID: p.production.ID}
	budget, err := p.healthMonitor.ErrorBudget(ctx, w)
	if err != nil {
		return "", err
	}
	if _, err := p.healthMonitor.RaiseObjectiveIntent(ctx, w, budget); err != nil {
		return "", err
	}
	return p.budgetHold(ctx, svc, it, budget)
}

// budgetHold is the hold half of [path.objectiveHold], over a budget the
// caller has already read. It is apart from that call because the raise beside
// it is a write and [views] performs none: a screen reading which hold stands
// at an item's production deploy row reads the budget and then this, and the
// pass that fires the row raises the intent as well.
func (p *path) budgetHold(ctx context.Context, svc service.Service, it item.Item,
	budget healthmonitor.Budget) (string, error) {
	if !budget.Holds() {
		return "", nil
	}
	source, raisedOnThisService, revert, err := p.itemAgainstTheBudget(ctx, svc, it)
	if err != nil {
		return "", err
	}
	if budget.Admits(source, raisedOnThisService, revert) {
		return "", nil
	}
	if !budget.Covered {
		return fmt.Sprintf("%s — the store does not cover the objective's period of %.0f seconds, so the budget is uncomputed and holds the way a spent one does",
			gate.HoldErrorBudgetExhausted, budget.PeriodSeconds), nil
	}
	return fmt.Sprintf("%s — %.0f%% of the allowance is left over a period of %.0f seconds",
		gate.HoldErrorBudgetExhausted, budget.Remaining*100, budget.PeriodSeconds), nil
}

// itemAgainstTheBudget is the three facts about one item that
// [healthmonitor.Budget.Admits] decides on: what raised the intent the item was
// decomposed from, whether that intent's evidence names this service, and
// whether the item is the revert of the rollback outstanding on it. Which of
// them passes the hold is the objective's rule and lives with the objective;
// what is here is the read of the records, which is this path's.
func (p *path) itemAgainstTheBudget(ctx context.Context, svc service.Service,
	it item.Item) (intent.Source, bool, bool, error) {
	if it.IntentID == "" {
		return "", false, false, nil
	}
	_, revertIntentID, outstanding, err := p.outstandingRevert(ctx, svc)
	if err != nil {
		return "", false, false, err
	}
	revert := outstanding && it.IntentID == revertIntentID
	raised, err := intent.Get(ctx, p.d.pool, it.IntentID)
	if err != nil {
		return "", false, false, err
	}
	if raised.Evidence == "" {
		return raised.Source, false, revert, nil
	}
	// The evidence is stored as the key package intent composes, and the service
	// it names is what says the intent was raised on this service and not on
	// another.
	var evidence intent.Evidence
	if err := json.Unmarshal([]byte(raised.Evidence), &evidence); err != nil {
		return "", false, false, fmt.Errorf("factory: reading the evidence on intent %s: %w", raised.ID, err)
	}
	return raised.Source, evidence.ServiceID == svc.ID, revert, nil
}
