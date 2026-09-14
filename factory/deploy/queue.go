package deploy

import (
	"context"
	"sort"
)

// QueueCandidate is a release waiting for a production deploy. The deploy
// queue owns the order and the hold decision; callers supply its six readings.
type QueueCandidate struct {
	ItemID        string
	IntentID      string
	ServiceID     string
	ReleaseID     string
	ReleaseNumber int64
	DeployID      string
	HoldReason    HoldKind
	AwaitedRevert bool
}

// WindowReading is the service's open-window count and limit.
type WindowReading struct {
	Open  int
	Limit int
}

// DependencyReading names the item a candidate waits on and the current item
// of that dependency's service. An empty current item means that service runs
// no current release.
type DependencyReading struct {
	RequiredItemID string
	CurrentItemID  string
}

// RollbackReading is the deployer's newest rollback and the intent of its
// revert, where that revert is still outstanding.
type RollbackReading struct {
	Holding        bool
	RevertIntentID string
}

// WindowReader reads the service's window record and its limit.
type WindowReader interface {
	ReadWindow(context.Context, string) (WindowReading, error)
}

// BudgetReader reads whether the service's error budget holds deploys.
type BudgetReader interface {
	ReadBudget(context.Context, string) (bool, error)
}

// RollbackReader reads the deployer's rollback record.
type RollbackReader interface {
	ReadRollback(context.Context, QueueCandidate) (RollbackReading, error)
}

// DependencyReader reads the records of a candidate's declared dependencies.
type DependencyReader interface {
	ReadDependencies(context.Context, QueueCandidate) ([]DependencyReading, error)
}

// DriftReader reads the standing drift record for a service.
type DriftReader interface {
	ReadDrift(context.Context, string) (bool, error)
}

// HumanReader reads whether a human decision is pending at the production
// deploy row.
type HumanReader interface {
	ReadHuman(context.Context, QueueCandidate) (bool, error)
}

// QueueReadings are the six record readers the deploy queue composes into its
// hold decision.
type QueueReadings struct {
	Window     WindowReader
	Budget     BudgetReader
	Rollback   RollbackReader
	Dependency DependencyReader
	Drift      DriftReader
	Human      HumanReader
}

// HoldKind is the condition that holds a release in the deploy queue.
type HoldKind string

const (
	HoldNone       HoldKind = ""
	HoldWindow     HoldKind = "the service holds as many analysis windows open as the window limit allows"
	HoldBudget     HoldKind = "the service's error budget is exhausted"
	HoldRollback   HoldKind = "a rollback's revert has not shipped, so deploying would redeliver the defect it removed"
	HoldDependency HoldKind = "a declared dependency is not its service's current release"
	HoldDrift      HoldKind = "the drift detector found a record disagreeing with what runs"
	HoldHuman      HoldKind = "a human holds the production deploy gate"
)

// QueueDecision is the deploy queue's decision and the condition behind it.
// AwaitedRevert says the candidate is excepted from the rollback and window
// holds and is ordered ahead of the other releases.
type QueueDecision struct {
	Held          bool
	Why           HoldKind
	AwaitedRevert bool
	Drift         bool
	Human         bool
}

// Held reads all six conditions and decides whether the candidate waits. The
// first condition in queue order supplies Why; a rollback's awaited revert is
// excepted from the rollback and window conditions only.
func (r QueueReadings) Held(ctx context.Context, candidate QueueCandidate) (QueueDecision, error) {
	var decision QueueDecision
	set := func(kind HoldKind) {
		if decision.Why == HoldNone {
			decision.Why = kind
		}
	}

	rollback, err := r.Rollback.ReadRollback(ctx, candidate)
	if err != nil {
		return QueueDecision{}, err
	}
	decision.AwaitedRevert = rollback.Holding && candidate.IntentID != "" &&
		candidate.IntentID == rollback.RevertIntentID

	window, err := r.Window.ReadWindow(ctx, candidate.ServiceID)
	if err != nil {
		return QueueDecision{}, err
	}
	if window.Open >= window.Limit && !decision.AwaitedRevert {
		set(HoldWindow)
	}

	budget, err := r.Budget.ReadBudget(ctx, candidate.ServiceID)
	if err != nil {
		return QueueDecision{}, err
	}
	if budget {
		set(HoldBudget)
	}

	if rollback.Holding && !decision.AwaitedRevert {
		set(HoldRollback)
	}

	dependencies, err := r.Dependency.ReadDependencies(ctx, candidate)
	if err != nil {
		return QueueDecision{}, err
	}
	for _, dependency := range dependencies {
		if dependency.RequiredItemID != dependency.CurrentItemID {
			set(HoldDependency)
			break
		}
	}

	drift, err := r.Drift.ReadDrift(ctx, candidate.ServiceID)
	if err != nil {
		return QueueDecision{}, err
	}
	if drift {
		decision.Drift = true
		set(HoldDrift)
	}

	human, err := r.Human.ReadHuman(ctx, candidate)
	if err != nil {
		return QueueDecision{}, err
	}
	if human {
		decision.Human = true
		set(HoldHuman)
	}

	decision.Held = decision.Why != HoldNone
	return decision, nil
}

// QueueOrder returns the releases of serviceID that still wait to deploy,
// ordered by release number, with the revert a rollback awaits first. A
// candidate already deployed is not waiting, and a held candidate remains in
// the queue rather than its deployable result.
func QueueOrder(ctx context.Context, serviceID string, candidates []QueueCandidate,
	readings QueueReadings) ([]QueueCandidate, error) {
	var unique []QueueCandidate
	seen := make(map[string]int, len(candidates))
	for _, candidate := range candidates {
		if candidate.ServiceID != serviceID || candidate.ReleaseID == "" {
			continue
		}
		if at, found := seen[candidate.ItemID]; found {
			if candidate.DeployID != "" && unique[at].DeployID == "" {
				unique[at] = candidate
			}
			continue
		}
		seen[candidate.ItemID] = len(unique)
		unique = append(unique, candidate)
	}
	var waiting []QueueCandidate
	for _, candidate := range unique {
		if candidate.DeployID != "" {
			continue
		}
		decision, err := readings.Held(ctx, candidate)
		if err != nil {
			return nil, err
		}
		candidate.HoldReason = decision.Why
		candidate.AwaitedRevert = decision.AwaitedRevert
		waiting = append(waiting, candidate)
	}
	sort.SliceStable(waiting, func(i, j int) bool {
		return waiting[i].ReleaseNumber < waiting[j].ReleaseNumber
	})

	var reverts, releases []QueueCandidate
	for _, candidate := range waiting {
		if candidate.AwaitedRevert {
			reverts = append(reverts, candidate)
		} else {
			releases = append(releases, candidate)
		}
	}
	return append(reverts, releases...), nil
}
