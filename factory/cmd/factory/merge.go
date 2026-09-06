package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/securitypredicate"
)

// mergeGate is the Merge to master row: where the verdict on the candidate is
// given. What it reads is the candidate's own run — the acceptance criteria decided
// against the candidate environment with undecided read the way a failure is, the
// encoding defect [path.checkEncodings] found over that same run and carried on
// the candidate rather than stopping it, every consumer contract in force decided
// against that same run, the producer's own contract diff against the version
// production is running, and the factory's own list of security predicates
// decided against the same build.
//
// All five reject on their own terms before anyone gives a verdict, which is
// what the design says of this row: the row fires so the vector exists and the
// rejection is readable against it, and then the factory's own reject closes it. A
// human who was going to approve is not overruled — there is nothing left to
// approve, and a schema diff is not a judgment they could have made differently.
// The criteria go first and the encoding defect next: an acceptance criterion the
// candidate's own run did not pass is what the row exists to read, and a
// criterion in force with no encoding or an encoding naming one the build
// withdraws is read right after it, before what a contract or a security
// predicate found. An encoding whose derivation could not be made puts a human
// at the row instead of rejecting, the way a security predicate's own
// could-not-derive already does.
//
// Approving admits the candidate to the merge queue, which is the stage the item
// advances to; rejecting sends the item back with an attempt counted where it
// goes — which is what the rejection carries as feedback to the implementer.
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
	predicates := p.decideSecurityPredicates(c)
	firing := gate.Firing{
		Row:             gate.MergeToMaster,
		ItemID:          c.itemID,
		BuildID:         c.buildID,
		ServiceID:       c.svc.ID,
		AreaID:          p.areaID,
		EnvironmentID:   p.production.ID,
		CriteriaInForce: len(c.criteria),
		Criteria:        c.criteria,
		CouldNotDerive:  couldNotDerive(c, predicates),
		Measurement:     c.measurement,
		Exposure:        reached,
	}
	opened, err := p.gate.Fire(ctx, firing)
	if err != nil {
		return err
	}
	report(p.d.out, opened, c.criteria)
	reportContracts(p.d.out, checked)
	reportSecurityPredicates(p.d.out, predicates)

	// The mechanical rejection, before a verdict is asked for, in the order
	// [gate.MechanicalChecks] lists: the criteria first — what the row exists to
	// read — then the encoding defect, then the contract checks, then a security
	// predicate on the terms the contract checks do.
	check, why := "", ""
	if blocking := blockingCriteria(c.criteria); len(blocking) > 0 {
		check, why = gate.AutoRejectedByCriterion, describeCriteriaRejection(blocking)
	}
	if check == "" && c.encodingDefect != "" {
		check, why = gate.AutoRejectedByEncoding, c.encodingDefect
	}
	if check == "" {
		check, why = checked.Check(), checked.Why()
	}
	if check == "" && len(predicates.Rejected()) > 0 {
		check, why = gate.AutoRejectedBySecurityPredicate, predicates.Why()
	}
	if check != "" {
		closing, err := p.gate.AutoReject(ctx, opened, check, why)
		if err != nil {
			return err
		}
		c.mergeGate = recordFiring(opened, closing)
		c.autoRejected, c.autoRejectedBy = true, check
		c.mergeRejectReason = why
		if _, err := p.items.ReturnTo(ctx, gate.Component(gate.MergeToMaster), c.itemID, item.StageImplementation); err != nil {
			return err
		}
		fmt.Fprintf(p.d.out, "Rejected by %s before a verdict was asked for: %s\n", check, why)
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
		c.mergeRejectReason = feedback
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

// mergeUntilQueued is [path.mergeGate] fired again for a candidate it rejected,
// against a build made against what was found wrong, until it approves or the
// implementer's own attempt limit escalates: a mechanical rejection
// (c.autoRejected) or a human's reject (c.rejected) at the Merge to master row
// both send the item back to Implementation with an attempt counted there, per
// ../../../end-goal/how-the-factory-works/03-gates/06-going-back-up.md, and this is what
// builds it again rather than leaving it there for good.
//
// What is not looped here is the merge queue's own rejection at
// re-verification, which [path.returnRejected] already sends back with an
// attempt counted: that happens inside [path.runQueue], after every candidate
// of the layer has been through this loop, and building it again is not part of
// this milestone.
func (p *path) mergeUntilQueued(ctx context.Context, c *candidate) error {
	for {
		if err := p.mergeGate(ctx, c); err != nil {
			return err
		}
		if !c.autoRejected && !c.rejected {
			return nil
		}
		reason := c.mergeRejectReason
		c.sentBack = agent.Returned{Reason: reason, Version: c.commit}
		c.resetForRebuild()
		fmt.Fprintf(p.d.out, "Item %s goes back to implementation against what the Merge to master row found wrong: %s\n",
			c.itemID, reason)
		if err := p.implementationStage(ctx, c); err != nil {
			return err
		}
		if err := p.candidateEnvironment(ctx, c); err != nil {
			return err
		}
	}
}

// decideSecurityPredicates is the factory's own list of security predicates
// decided against this candidate's build. The design decides it at the candidate
// run beside the criteria; this decides it here, over the same checkout the run
// was performed on, because the row that reads the answer is this one and no
// record carries it between the two.
//
// Which toolchain a service's build is is not a field of any record, so every
// service is read as Go, the way the consumer contract extractor already reads
// one. A toolchain the factory ships no list for is the zero list, which reads
// as could not derive.
func (p *path) decideSecurityPredicates(c *candidate) securitypredicate.Decided {
	list, _ := securitypredicate.ForToolchain(securitypredicate.ToolchainGo, factoryVersion)
	return securitypredicate.Decide(list, securitypredicate.Checkout{Dir: c.svc.Repository})
}

// blockingCriteria is every result in criteria whose outcome stops the item at
// this row, per [criterion.Outcome.Blocks]: failed, or undecided, unless the
// criterion is unreliable — in which case its failure blocks nothing and
// counts no attempt, and this returns none of it.
func blockingCriteria(criteria []gate.CriterionResult) []gate.CriterionResult {
	var blocking []gate.CriterionResult
	for _, result := range criteria {
		if result.Outcome.Blocks(result.Unreliable) {
			blocking = append(blocking, result)
		}
	}
	return blocking
}

// describeCriteriaRejection lists each blocking criterion by id and outcome, so
// a reader of the close event sees what the candidate's own run did not pass
// without following back to the criterion table.
func describeCriteriaRejection(blocking []gate.CriterionResult) string {
	parts := make([]string, 0, len(blocking))
	for _, result := range blocking {
		parts = append(parts, fmt.Sprintf("%s %s", result.CriterionID, result.Outcome))
	}
	return strings.Join(parts, ", ")
}

// couldNotDerive is what the firing carries where the encodings or the security
// predicates could not be decided: the derivations the merge row names, in that
// order, which put a human there rather than rejecting.
func couldNotDerive(c *candidate, predicates securitypredicate.Decided) []string {
	var derivations []string
	if c.encodingCouldNotDerive {
		derivations = append(derivations, gate.CouldNotDeriveEncoding)
	}
	if predicates.CouldNotBeDerived() {
		derivations = append(derivations, gate.CouldNotDeriveSecurityPredicate)
	}
	return derivations
}

// reportSecurityPredicates prints what the factory's own list came to, as an
// owner at the row would read it: which list ran, how many kinds it holds, and
// why nothing could be decided where nothing was.
func reportSecurityPredicates(out io.Writer, predicates securitypredicate.Decided) {
	if predicates.CouldNotBeDerived() {
		fmt.Fprintf(out, "  the security predicates could not be derived: %s\n", predicates.CouldNotDerive)
		return
	}
	fmt.Fprintf(out, "  %d security predicate(s) of the %s list shipped with factory version %s decided against this build\n",
		len(predicates.Results), predicates.List.Toolchain, predicates.List.FactoryVersion)
	for _, rejected := range predicates.Rejected() {
		fmt.Fprintf(out, "    %s does not hold: %s\n", rejected.Kind, rejected.Why)
	}
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
			for _, consumer := range blocking.Unreadable.Partial {
				fmt.Fprintf(out, "    %s is held by %s, whose derivation is partial\n", blocking.Element, consumer)
			}
			for _, consumer := range blocking.Unreadable.CouldNotDerive {
				fmt.Fprintf(out, "    %s is held by %s, whom nobody could derive at all\n", blocking.Element, consumer)
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
