package main

import (
	"context"
	"fmt"
	"io"

	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
)

// mergeGate is the Merge to master row: where the verdict on the candidate is
// given. What it reads is the candidate's own run — the acceptance criteria decided
// against the candidate environment with undecided read the way a failure is, every
// consumer contract in force decided against that same run, and the
// producer's own contract diff against the version production is running.
//
// The last two reject on their own terms before anyone gives a verdict, which is
// what the design says of this row: the row fires so the vector exists and the
// rejection is readable against it, and then the factory's own reject closes it. A
// human who was going to approve is not overruled — there is nothing left to
// approve, and a schema diff is not a judgment they could have made differently.
//
// Approving admits the candidate to the merge queue, which is the stage the item
// advances to; rejecting sends the item back with an attempt counted where it goes.
func (p *path) mergeGate(ctx context.Context, c *candidate) error {
	if c.environmentID == "" {
		fmt.Fprintf(p.d.out, "Item %s reached no candidate environment, so its Merge to master gate does not fire\n", c.itemID)
		return nil
	}

	checked, err := p.enforceContracts(ctx, c, c.buildID)
	if err != nil {
		return err
	}
	reached, err := p.exposureOf(ctx, c.buildID)
	if err != nil {
		return err
	}
	firing := gate.Firing{
		Row:             gate.MergeToMaster,
		ItemID:          c.itemID,
		BuildID:         c.buildID,
		ServiceID:       c.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(c.criteria),
		Criteria:        c.criteria,
		Measurement:     c.measurement,
		Exposure:        reached,
	}
	opened, err := p.gate.Fire(ctx, firing)
	if err != nil {
		return err
	}
	report(p.d.out, opened, c.criteria)
	reportContracts(p.d.out, checked)

	// The mechanical rejection, before a verdict is asked for.
	if check := checked.Check(); check != "" {
		closing, err := p.gate.AutoReject(ctx, opened, check, checked.Why())
		if err != nil {
			return err
		}
		c.mergeGate = recordFiring(opened, closing)
		c.autoRejected, c.autoRejectedBy = true, check
		if _, err := p.items.ReturnTo(ctx, gate.Component(gate.MergeToMaster), c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected by %s before a verdict was asked for: %s\n", check, checked.Why())
		fmt.Fprintf(p.d.out, "  close event %s written as %s; item %s goes back to %s with an attempt counted there\n",
			closing.ID, closing.Actor.Key, c.itemID, item.StageImplementation)
		return nil
	}

	verdict, feedback, closing, err := p.settle(ctx, opened, firing)
	if err != nil {
		return err
	}
	c.mergeGate = recordFiring(opened, closing)
	if verdict == gate.VerdictReject {
		c.rejected = true
		if _, err := p.items.ReturnTo(ctx, p.human, c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected: %s\nItem %s goes back to %s with an attempt counted there, and keeps its environment\n",
			feedback, c.itemID, item.StageImplementation)
		return nil
	}

	if _, err := p.items.Advance(ctx, dispatchActor, c.itemID, item.StageQueued); err != nil {
		return err
	}
	c.queued = true
	fmt.Fprintf(p.d.out, "Approved; item %s is in the merge queue\n", c.itemID)
	return nil
}

// enforceContracts is the two contract checks over one candidate, kept on the
// candidate so the merge row and the queue's re-verification report the same value
// and the test can assert over it. The build is an argument and not read off the
// candidate, because the two callers read two different builds: the merge row reads
// the one the implementation stage made, and the re-verification reads the one it
// just made from the candidate branch with master merged in.
//
// Neither baseline is written down: both are computed here, at the moment the row
// fires, and again inside the queue at re-verification. So a candidate that waited
// in the queue can fail on a baseline that moved after its own run passed, with no
// record of the earlier pass to point at — which is the cost the design states for
// computing them rather than recording them.
func (p *path) enforceContracts(ctx context.Context, c *candidate, buildID string) (contractcheck.Checked, error) {
	checked, err := p.contracts.Enforce(ctx, contractcheck.Candidate{
		ItemID:        c.itemID,
		ServiceID:     c.svc.ID,
		ServiceName:   c.svc.Name,
		BuildID:       buildID,
		EnvironmentID: c.environmentID,
	}, p.production.ID)
	if err != nil {
		return contractcheck.Checked{}, err
	}
	c.checked = &checked
	return checked, nil
}

// reportContracts prints what enforcement found, as an owner at the row would read
// it: what the build publishes and what that does to the version production runs,
// what it declares of others, who consumes it, and what still names an element it
// breaks.
func reportContracts(out io.Writer, checked contractcheck.Checked) {
	if len(checked.Publishes) == 0 && len(checked.Declares.Drafts) == 0 {
		fmt.Fprintln(out, "  this build publishes no contract and declares nothing of another service")
		return
	}
	for _, broken := range checked.Broken {
		from := "no version, so this candidate would create the contract"
		if broken.Had {
			from = "against " + broken.From.String() + ", the version production runs"
		}
		fmt.Fprintf(out, "  contract %s (%s) %s: %s\n",
			broken.Contract.Name, broken.Contract.Kind, from, broken.Change.Describe())
		if broken.Change.Moved() {
			fmt.Fprintf(out, "    it would mint %s\n", broken.Next)
		}
		for _, element := range broken.Change.Breaking {
			fmt.Fprintf(out, "    %s breaks the promise this kind of contract makes\n", element)
		}
		for _, blocking := range broken.Blocking {
			for _, consumer := range blocking.Consumers() {
				fmt.Fprintf(out, "    %s is still declared by %s\n", blocking.Element, consumer)
			}
			for _, s := range blocking.Safeguards {
				fmt.Fprintf(out, "    %s is still asserted by safeguard %s, placed by %s %s\n",
					blocking.Element, s.SafeguardID, s.Actor.Kind, s.Actor.Key)
			}
		}
	}
	if checked.Declares.CouldNotDerive() || checked.Declares.Partial() {
		fmt.Fprintf(out, "  the derivation of what it declares is %s\n", checked.Declares.Describe())
	}
	for _, declared := range checked.Declares.Drafts {
		fmt.Fprintf(out, "  it declares %s on %s.%s.%s\n",
			declared.Kind, declared.ProducerService, declared.Interface, declared.Element)
	}
	if len(checked.Affected) > 0 {
		fmt.Fprintf(out, "  %d service(s) declare they consume what it publishes: %v\n",
			len(checked.Affected), checked.Affected)
	}
	if checked.Observed > 0 {
		fmt.Fprintf(out, "  %d exchange document(s) observed on its own environment, which is what every predicate was decided against\n",
			checked.Observed)
	}
}
