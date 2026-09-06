package contractcheck

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/deploy"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// The store rule. A service's store is a contract too and its consumer is the
// service's own past, so a breaking schema change is five items: the store gains
// the new form beside the old, the code writes both forms and still reads the old,
// a backfill copies into the new form every row the old form already holds, the
// code reads the new form, and the old form is dropped.
//
// Enforcement over the first and the last is the ordinary diff, which check.go
// runs. What is here is the three the diff cannot see and the two things the
// candidate environment exercises:
//
//   - the middle three items have an empty diff by construction, both forms being
//     present throughout, so what decides them is the state the candidate
//     environment's own store holds — which decideConsumers reads;
//   - the item that moves reads and the removal after it are each rejected while
//     no deploy record marks the backfill for that element complete, and so is a
//     constraint added to an element of a store whose form marks something;
//   - every change is authored to be applied twice without effect, and the
//     candidate environment checks that by applying it twice — asked of a build
//     that declares a schema change and of a backfill item, whose change is data
//     and not form, and of nothing else, a form moving being the code's doing
//     and not a change for a deploy to apply;
//   - a change that destroys stored data is exercised with the snapshot the
//     deploy of it will rest on.

// Migration is what the store rule found about one store contract this candidate
// publishes.
type Migration struct {
	// Contract is the store's name in the build.
	Contract string
	// Moved is whether the candidate's form moves the store at all.
	Moved bool
	// Declared is whether the build declares a schema change: the reading the
	// build's own process made of its checkout, read through
	// [Checkout.DeclaresSchemaChange]. A form moves whenever the code that
	// derives it moves, and a build can move it and ship nothing for a deploy to
	// apply — so this and not Moved is what says there is a change to exercise.
	Declared bool
	// Backfill is what a backfill item's build declares it copies between, read
	// through [Checkout.DeclaresBackfill] and empty on every other item. A
	// backfill's change is data and not form, so neither Moved nor Declared
	// reaches it and this is the second thing that says there is a change to
	// run twice.
	Backfill deploy.Backfill
	// Destroys is whether the change destroys stored data — the drop, and any
	// diff the store rule forbids without one — which is what the snapshot is
	// taken before.
	Destroys bool
	// SecondApplication is what applying the change a second time on the
	// candidate environment did.
	SecondApplication SecondApplication
	// Snapshot is the snapshot that environment took and verified, and is asked
	// for only where the change destroys stored data.
	Snapshot Snapshot
	// Waiting is the elements whose backfill no deploy record marks complete.
	Waiting []Waiting
}

// Waiting is one element whose backfill is not complete, and which of the three
// things this candidate does to it: moves reads to it, drops the form it was
// filled from, or puts a constraint on it.
type Waiting struct {
	Element  string
	Moving   bool
	Dropping bool
	// Constraining is a not-null constraint or a domain check added to an
	// element of a store whose form marks something, which is a migration in
	// progress. It is addable once the backfill is complete and not before: the
	// rows the copy has not reached would violate it.
	Constraining bool
}

// Blocked reports whether the store rule rejects this candidate at Merge to
// master.
func (m Migration) Blocked() bool {
	if len(m.Waiting) > 0 {
		return true
	}
	if (m.Declared || m.Backfill.Any()) && !m.SecondApplication.Ran {
		return true
	}
	if m.SecondApplication.Changed {
		return true
	}
	return m.Destroys && !(m.Snapshot.Taken && m.Snapshot.Verified)
}

// Why is what the store rule's rejection says, one sentence per reason.
func (m Migration) Why() []string {
	var said []string
	for _, waiting := range m.Waiting {
		switch {
		case waiting.Moving:
			said = append(said, fmt.Sprintf(
				"%s moves its reads to %s and no deploy record marks that backfill complete, so every row the copy has not reached reads as absent",
				m.Contract, waiting.Element))
		case waiting.Dropping:
			said = append(said, fmt.Sprintf(
				"%s drops %s and no deploy record marks that backfill complete, so the drop would destroy the only copy",
				m.Contract, waiting.Element))
		case waiting.Constraining:
			said = append(said, fmt.Sprintf(
				"%s constrains %s and no deploy record marks that backfill complete, so the rows the copy has not reached would violate it",
				m.Contract, waiting.Element))
		}
	}
	if m.Declared && !m.SecondApplication.Ran {
		said = append(said, fmt.Sprintf(
			"%s changes the schema and the candidate environment did not apply the change twice", m.Contract))
	}
	if m.Backfill.Any() && !m.SecondApplication.Ran {
		said = append(said, fmt.Sprintf(
			"%s fills %s from %s and the candidate environment did not run the change twice over the seeded store",
			m.Contract, m.Backfill.Element, m.Backfill.FromElement))
	}
	if m.SecondApplication.Changed {
		said = append(said, fmt.Sprintf(
			"applying %s's change a second time changed something: %s", m.Contract, m.SecondApplication.What))
	}
	if m.Destroys && !(m.Snapshot.Taken && m.Snapshot.Verified) {
		said = append(said, fmt.Sprintf(
			"%s destroys stored data and the candidate environment could not take and verify a snapshot: %s",
			m.Contract, m.Snapshot.Why))
	}
	return said
}

// storeRule is the store rule over every store contract this candidate publishes.
// A candidate that publishes none has nothing here, which is most candidates.
func (c *Check) storeRule(ctx context.Context, candidate Candidate, checked *Checked,
	binding []consumercontract.Predicate) error {
	for _, broken := range checked.Broken {
		if broken.Contract.Kind != contract.KindStore {
			continue
		}
		migration := Migration{Contract: broken.Contract.Name, Moved: broken.Change.Moved()}
		migration.Destroys = len(broken.Change.Removed) > 0 || len(broken.Change.Breaking) > 0
		form, published := formNamed(checked.Publishes, broken.Contract.Name)
		waiting, err := c.waiting(ctx, candidate, broken, checked.Declares.Drafts, binding, form, published)
		if err != nil {
			return err
		}
		migration.Waiting = waiting

		migration.Declared, err = c.checkout.DeclaresSchemaChange(ctx, candidate)
		if err != nil {
			return err
		}
		declared, err := c.checkout.DeclaresBackfill(ctx, candidate)
		if err != nil {
			return err
		}
		if declared.Contract == broken.Contract.Name {
			migration.Backfill = declared
		}

		if migration.Declared || migration.Backfill.Any() {
			migration.SecondApplication, err = c.store.AppliedTwice(ctx, candidate)
			if err != nil {
				return err
			}
		}
		if migration.Destroys {
			migration.Snapshot, err = c.store.Snapshot(ctx, candidate)
			if err != nil {
				return err
			}
		}
		checked.Migrations = append(checked.Migrations, migration)
	}
	return nil
}

// waiting is the elements this candidate cannot ship until a deploy record marks
// their backfill complete: the ones it moves its reads to, and the marked ones it
// drops.
//
// The mark is what says a migration is in progress. An element the producer's own
// item marked is the old form of a pair, so a drop of one and a read moved away
// from one are the two items the backfill stands between; a store that marks
// nothing has no backfill to wait on, which is every ordinary change.
func (c *Check) waiting(ctx context.Context, candidate Candidate, broken Broken,
	drafts []consumercontract.Draft, binding []consumercontract.Predicate,
	form contract.Form, published bool) ([]Waiting, error) {
	var waiting []Waiting
	for _, element := range broken.Change.Removed {
		was, found := elementOf(broken, element)
		if !found || !was.Marked {
			continue
		}
		complete, err := c.backfilled(ctx, candidate, broken.Contract.Name, element)
		if err != nil {
			return nil, err
		}
		if !complete {
			waiting = append(waiting, Waiting{Element: element, Dropping: true})
		}
	}
	if !published {
		return waiting, nil
	}
	for _, element := range c.readsMoved(candidate, drafts, binding, broken, form) {
		complete, err := c.backfilled(ctx, candidate, broken.Contract.Name, element)
		if err != nil {
			return nil, err
		}
		if !complete {
			waiting = append(waiting, Waiting{Element: element, Moving: true})
		}
	}

	// The other half of the constraint rule, the half check.go's diff does not
	// answer: a not-null constraint or a domain check is addable once the
	// backfill is complete, the completion standing for the rows no release in
	// force wrote. It is asked only where the form marks something, a store that
	// marks nothing having no backfill to wait on.
	if len(form.Marked()) > 0 {
		for _, element := range append(slices.Clone(broken.Change.Constrained), broken.Change.Narrowed...) {
			if slices.ContainsFunc(waiting, func(w Waiting) bool { return w.Element == element }) {
				continue
			}
			complete, err := c.backfilled(ctx, candidate, broken.Contract.Name, element)
			if err != nil {
				return nil, err
			}
			if !complete {
				waiting = append(waiting, Waiting{Element: element, Constraining: true})
			}
		}
	}
	return waiting, nil
}

// readsMoved is the elements this candidate newly reads of its own store while it
// stops reading one the form marks deprecated, which is the item that moves reads.
// A candidate that reads a new element and goes on reading the marked one is the
// item that adds the write, whose reads have not moved.
func (c *Check) readsMoved(candidate Candidate, drafts []consumercontract.Draft,
	binding []consumercontract.Predicate, broken Broken, form contract.Form) []string {
	before := map[string]bool{}
	for _, p := range binding {
		if p.ServiceID == candidate.ServiceID && p.ProducerServiceID == candidate.ServiceID &&
			p.Interface == broken.Contract.Name && p.Kind.Side() == gatepolicy.SideReceived {
			before[p.Element] = true
		}
	}
	now := map[string]bool{}
	for _, d := range drafts {
		if d.ProducerServiceID == candidate.ServiceID && d.Interface == broken.Contract.Name &&
			d.Kind.Side() == gatepolicy.SideReceived {
			now[d.Element] = true
		}
	}
	away := false
	for _, e := range form.Elements {
		if e.Marked && before[e.Name] && !now[e.Name] {
			away = true
		}
	}
	if !away {
		return nil
	}
	var moved []string
	for _, e := range form.Elements {
		if now[e.Name] && !before[e.Name] && !slices.Contains(moved, e.Name) {
			moved = append(moved, e.Name)
		}
	}
	return moved
}

// backfilled is whether a deploy record marks the backfill for one element
// complete: a backfill item's release names the element it fills and the element
// it fills from, and the deployer completes that record only once every row the
// old form holds is present in the new. Either side of the pair reads the same
// record, the one it filled and the one it filled from being one backfill.
func (c *Check) backfilled(ctx context.Context, candidate Candidate, storeName, element string) (bool, error) {
	_, complete, err := deploy.BackfillComplete(ctx, c.pool, candidate.ServiceID, storeName, element)
	return complete, err
}

// elementOf is one element of the form the version below this candidate publishes,
// which is where the mark on a dropped element is read from — the candidate's own
// form no longer has it.
func elementOf(broken Broken, name string) (contract.Element, bool) {
	for _, e := range broken.Before.Elements {
		if e.Name == name {
			return e, true
		}
	}
	return contract.Element{}, false
}

// formNamed is the form of that name among the ones the candidate publishes.
func formNamed(forms []contract.Form, name string) (contract.Form, bool) {
	for _, form := range forms {
		if form.Name == name {
			return form, true
		}
	}
	return contract.Form{}, false
}
