// TestTheOwnersNarrowingOfTheOperationsIsWhatTheRunCarries and the rest of this
// file are the role and scope vocabulary a dispatch is matched on: the
// operations a role carries with the narrowing an owner writes on an entry, and
// the area chain a scope's area is matched against. Split from hold_test.go by
// subject at the 500-line bound; they share db_test.go's fixtures and package.
package dispatch_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/intent"
)

// oneEntry writes a single entry for one role, narrowing the role's operations
// to the ones named. It is what a test that needs a narrowed entry writes after
// withdrawing the fixture's own.
func (c composed) oneEntry(t *testing.T, role dispatch.Role, operations []string) {
	t.Helper()
	if _, err := c.entries.Write(c.ctx, owner, fleetentry.New{
		ModelVersion:                    modelName,
		Role:                            string(role),
		CredentialName:                  theCredential,
		ProcessingLocation:              "vendor/test-region",
		MaterialClasses:                 fleetentry.MaterialClasses,
		Operations:                      operations,
		ReadsAtOnce:                     200000,
		DispatchesBetweenEvaluationRuns: 50,
	}); err != nil {
		t.Fatalf("writing the entry for %s: %v", role, err)
	}
}

// TestTheOwnersNarrowingOfTheOperationsIsWhatTheRunCarries: the operations
// belong to the role, and an owner may narrow them on the entry. The narrowing
// is read off the record, so the run carries what the owner left in and not the
// role's whole list.
func TestTheOwnersNarrowingOfTheOperationsIsWhatTheRunCarries(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.withdrawEveryEntry(t)
	c.oneEntry(t, dispatch.RoleSpecAuthor, []string{dispatch.OperationReadTheRepository})
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}
	if !slices.Equal(run.Entry.Operations, []string{dispatch.OperationReadTheRepository}) {
		t.Fatalf("the run's operations are %v, want the owner's narrowing", run.Entry.Operations)
	}
}

// TestAnEntryNarrowingNothingRunsUnderTheRolesWholeList: an entry that narrows
// nothing reaches the list by naming the role, which is the whole of it.
func TestAnEntryNarrowingNothingRunsUnderTheRolesWholeList(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor: %v", err)
	}
	whole, err := dispatch.RoleSpecAuthor.Operations()
	if err != nil {
		t.Fatalf("Operations: %v", err)
	}
	if !slices.Equal(run.Entry.Operations, whole) {
		t.Fatalf("the run's operations are %v, want the role's whole list %v", run.Entry.Operations, whole)
	}
}

// TestAnEntryWideningTheRolesOperationsIsRefused: an owner may narrow the list
// and never widen it, so an entry naming an operation the role does not carry
// stops the dispatch rather than running under it. It is refused and not held:
// no condition of the factory's is waiting to end.
func TestAnEntryWideningTheRolesOperationsIsRefused(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.withdrawEveryEntry(t)
	c.oneEntry(t, dispatch.RoleSpecAuthor,
		[]string{dispatch.OperationReadTheRepository, dispatch.OperationWriteTheRepository})
	it := c.oneItem(t, intent.StateRefined)

	_, _, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if !errors.Is(err, dispatch.ErrOperationWidened) {
		t.Fatalf("SpecAuthor = %v, want ErrOperationWidened", err)
	}
	if c.model.calls != 0 {
		t.Error("an agent ran under an entry that widened the role's operations")
	}
}

// TestAnEntryDrawnOnTheAreaAboveCoversTheItem: a scope drawn on any area in the
// item's chain reaches the item, and dispatch follows that chain from the
// item's area up to the project. The dispatch names the item's own area and
// nothing above it, which is all a caller has: matching that one area alone
// would take the item out of an entry drawn on a coarser one.
func TestAnEntryDrawnOnTheAreaAboveCoversTheItem(t *testing.T) {
	c := newDispatch(t, []agent.Reply{{Text: aSpec}}, nil, 3)
	c.withdrawEveryEntry(t)
	if _, err := c.entries.Write(c.ctx, owner, fleetentry.New{
		ModelVersion:                    modelName,
		Role:                            string(dispatch.RoleSpecAuthor),
		Scope:                           fleetentry.Scope{AreaID: c.theAreaAbove},
		CredentialName:                  theCredential,
		ProcessingLocation:              "vendor/test-region",
		MaterialClasses:                 fleetentry.MaterialClasses,
		ReadsAtOnce:                     200000,
		DispatchesBetweenEvaluationRuns: 50,
	}); err != nil {
		t.Fatalf("writing the entry scoped to the area above: %v", err)
	}
	it := c.oneItem(t, intent.StateRefined)

	_, run, err := c.dispatch.SpecAuthor(c.ctx, c.on(it), nil, agent.Refining{Statement: "s"})
	if err != nil {
		t.Fatalf("SpecAuthor under an entry drawn on the area above: %v", err)
	}
	if run.Held != "" {
		t.Fatalf("the run held on %q, want the entry above the item's area to cover it", run.Held)
	}
	if run.Entry.Scope.AreaID != c.theAreaAbove {
		t.Fatalf("the entry matched is scoped to %q, want the area above", run.Entry.Scope.AreaID)
	}
}

// TestAnEntryNarrowsARolesOperationsAndNeverWidensThem: the factory defines
// the operation list per role, an owner may leave one out, and adding one is
// refused.
func TestAnEntryNarrowsARolesOperationsAndNeverWidensThem(t *testing.T) {
	full, err := dispatch.RoleImplementer.Operations()
	if err != nil {
		t.Fatalf("Operations: %v", err)
	}
	narrowed, err := dispatch.RoleImplementer.Narrow(full[:1])
	if err != nil || len(narrowed) != 1 {
		t.Errorf("Narrow to one operation = %v, %v, want the one", narrowed, err)
	}
	if _, err := dispatch.RoleSpecAuthor.Narrow([]string{dispatch.OperationWriteTheRepository}); !errors.Is(err, dispatch.ErrOperationWidened) {
		t.Errorf("widening the spec author's list = %v, want ErrOperationWidened", err)
	}
}

// TestAScopeBindsWhatAnEntryMayBePutOn: a scope is drawn on a project, a
// service and an area, and a field it leaves empty matches whatever the item
// has. The area half is matched against the chain dispatch follows, which
// TestAnEntryDrawnOnTheAreaAboveCoversTheItem drives through a dispatch.
func TestAScopeBindsWhatAnEntryMayBePutOn(t *testing.T) {
	item := dispatch.On{ProjectID: oneProject, ServiceID: oneService}
	if !(dispatch.Scope{}).Covers(item) {
		t.Error("the empty scope covers nothing, and it is the whole factory")
	}
	if !(dispatch.Scope{ProjectID: oneProject}).Covers(item) {
		t.Error("a project-wide scope does not cover an item in the project")
	}
	if (dispatch.Scope{AreaID: "ar_" + strings.Repeat("2", 32)}).Covers(item) {
		t.Error("an area-scoped entry covers an item whose chain does not name that area")
	}
	if got := (dispatch.Scope{ServiceID: oneService}).String(); !strings.Contains(got, oneService) {
		t.Errorf("the scope reads as %q, want it to name the service the principal carries", got)
	}
}
