// Check.Enforce against the two baselines: a producer's own diff against what
// is running, and a consumer contract against its producer's newest release.
package contractcheck_test

import (
	"context"
	"slices"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/release"
	"github.com/dulguun0225/borg/factory/window"
)

// TestABreakingDiffIsRejectedWhereAConsumerStillDeclaresTheElement, and passes once
// nothing does — which is the whole of "without the migration already shipped ahead
// of it".
func TestABreakingDiffIsRejectedWhereAConsumerStillDeclaresTheElement(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false), element("Detail", "string", false, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)

	trimmed := published(element("Status", "string", true, false))
	removing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err := g.check.Enforce(ctx, removing, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatalf("a removal of an element the consumer declares passed: %+v", checked.Broken)
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q, want the producer's own diff", checked.Check())
	}
	if !slices.Contains(checked.Affected, g.consumer.ID) {
		t.Errorf("the affected services are %v, want the consumer %s", checked.Affected, g.consumer.ID)
	}
	// The rejection names the consumer it would break, which is the whole point of
	// the graph answering who is affected.
	if !contains(checked.Why(), g.consumer.ID) {
		t.Errorf("the rejection does not name the consumer: %s", checked.Why())
	}

	// The consumer's next release stops reading it, which empties the list with
	// nobody withdrawing anything — and then the same removal passes.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)
	again := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, again, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the removal is still refused after the list emptied: %s", checked.Why())
	}
	if len(checked.Broken) != 1 || len(checked.Broken[0].Change.Breaking) != 1 {
		t.Fatalf("the diff is recorded as %+v, and a breaking change that is allowed is still breaking", checked.Broken)
	}
	if checked.Broken[0].Next != (contract.Semver{Major: 2}) {
		t.Errorf("the removal would mint %s, want a major", checked.Broken[0].Next)
	}
}

// TestAStoresForwardPromiseRefusesAPopulatedAddition, which nothing on an interface
// refuses and no list empties to allow.
func TestAStoresForwardPromiseRefusesAPopulatedAddition(t *testing.T) {
	ctx, g := newGraph(t)

	ship(t, ctx, g, g.producer, []contract.Form{stored(element("ID", "string", true, false))}, nil, window.ExitTimedOut)

	populated := candidateOf(t, ctx, g, g.producer, []contract.Form{
		stored(element("ID", "string", true, false), element("Amount", "int64", true, false)),
	}, nil, nil)
	checked, err := g.check.Enforce(ctx, populated, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a store gained an always-populated element and the build being restored does not write it")
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q", checked.Check())
	}
	if !contains(checked.Why(), "rollback restores") {
		t.Errorf("the rejection does not name the store's own consumer: %s", checked.Why())
	}

	// The same element added optional is what the first item of a store migration
	// does, and it passes.
	optional := candidateOf(t, ctx, g, g.producer, []contract.Form{
		stored(element("ID", "string", true, false), element("Amount", "int64", false, false)),
	}, nil, nil)
	checked, err = g.check.Enforce(ctx, optional, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("adding the element optional is refused too: %s", checked.Why())
	}
}

// TestAConsumerContractIsDecidedAgainstTheCandidatesOwnRun: all five kinds are
// decidable against an observed exchange, and no exchange at all is a failure rather
// than a pass.
func TestAConsumerContractIsDecidedAgainstTheCandidatesOwnRun(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateDomain, "ok|error"),
	}, window.ExitTimedOut)

	// A candidate whose run writes a value outside the domain the consumer declared:
	// the form is unchanged, so the diff says nothing, and what catches it is the
	// consumer contract decided against the run.
	outside := candidateOf(t, ctx, g, g.producer, []contract.Form{full}, nil,
		[]consumercontract.Document{{"Status": "unknown"}})
	checked, err := g.check.Enforce(ctx, outside, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a value outside the declared domain passed, and no schema diff would have caught it")
	}
	if checked.Check() != gate.AutoRejectedByConsumerContract {
		t.Errorf("the check that rejected is %q, want a consumer contract", checked.Check())
	}
	if checked.Observed != 1 {
		t.Errorf("%d exchange documents were observed, want the one written", checked.Observed)
	}

	// A candidate whose run wrote nothing has not shown that the assumption holds.
	silent := candidateOf(t, ctx, g, g.producer, []contract.Form{full}, nil, nil)
	checked, err = g.check.Enforce(ctx, silent, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a producer that wrote no exchange document passed a consumer contract it never showed holding")
	}

	// And one whose run stays inside it passes.
	inside := candidateOf(t, ctx, g, g.producer, []contract.Form{full}, nil,
		[]consumercontract.Document{{"Status": "ok"}, {"Status": "error"}})
	checked, err = g.check.Enforce(ctx, inside, g.production)
	if err != nil {
		t.Fatalf("the third Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("a run inside the declared domain is refused: %s", checked.Why())
	}
}

// TestTheTwoBaselinesAreDifferent: a producer's own diff runs against what is
// running, and a consumer contract against what its producer's newest
// release publishes.
func TestTheTwoBaselinesAreDifferent(t *testing.T) {
	ctx, g := newGraph(t)

	// The producer's release 1 is running. Release 2 has merged and is not
	// deployed, and it removed the element — so what runs still has it and the newest
	// does not.
	full := published(element("Status", "string", true, false), element("Detail", "string", false, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)
	merged, err := g.items.Create(ctx, theActor, item.New{
		IntentID: record.NewID("in"), ServiceID: g.producer.ID, Branch: "item/merged",
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the merged item: %v", err)
	}
	bl, err := g.builds.Create(ctx, theActor, build.Draft{
		ItemID: merged.ID, ServiceID: g.producer.ID, CommitHash: record.NewID("commit"), ArtifactDigest: record.NewID("digest"),
		ShippedBundleIdentity: "bundle-test",
	})
	if err != nil {
		t.Fatalf("writing the build: %v", err)
	}
	trimmed := published(element("Status", "string", true, false))
	if _, err := g.releases.MintWith(ctx, theActor,
		release.Minting{ServiceID: g.producer.ID, BuildID: bl.ID, Commit: bl.CommitHash, ItemID: merged.ID},
		func(ctx context.Context, tx pgx.Tx, r release.Release) error {
			_, err := contract.PublishAll(ctx, tx, theActor, g.producer.ID, r.ID, r.Number,
				merged.ID, []contract.Form{trimmed})
			return err
		}); err != nil {
		t.Fatalf("minting the merged release: %v", err)
	}

	// A consumer candidate that newly declares the element the producer has already
	// removed on master fails at its own gate, because a consumer contract is
	// checked against the version its producer's newest release publishes.
	consuming := candidateOf(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Detail", gatepolicy.PredicateRead, ""),
	}, nil)
	checked, err := g.check.Enforce(ctx, consuming, g.production)
	if err != nil {
		t.Fatalf("Enforce on the consumer: %v", err)
	}
	if checked.Passed() {
		t.Fatal("a consumer newly declaring an element its producer's newest release does not publish passed")
	}
	if len(checked.Unmet) != 1 || checked.Unmet[0].Draft.Element != "Detail" {
		t.Fatalf("the unmet consumer contracts are %+v", checked.Unmet)
	}

	// The producer's own next candidate diffs against what is running, which still
	// has the element: it is running release 1.
	producing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, producing, g.production)
	if err != nil {
		t.Fatalf("Enforce on the producer: %v", err)
	}
	if len(checked.Broken) != 1 {
		t.Fatalf("the producer's diff produced %+v", checked.Broken)
	}
	if !checked.Broken[0].Had || checked.Broken[0].From != contract.FirstVersion {
		t.Fatalf("the producer's diff ran against %+v, want the version release 1 publishes",
			checked.Broken[0].From)
	}
	if len(checked.Broken[0].Change.Removed) != 1 {
		t.Errorf("the diff against what is running removed %v, want the element", checked.Broken[0].Change.Removed)
	}
}

// TestACandidateCreatingAContractBreaksNothing: an interface a rejected candidate
// publishes points at nothing, so a candidate that would create one has no form to
// diff against.
func TestACandidateCreatingAContractBreaksNothing(t *testing.T) {
	ctx, g := newGraph(t)

	first := candidateOf(t, ctx, g, g.producer,
		[]contract.Form{published(element("Status", "string", true, false))}, nil, ok())
	checked, err := g.check.Enforce(ctx, first, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("a candidate that would create a contract is refused: %s", checked.Why())
	}
	if len(checked.Broken) != 1 || checked.Broken[0].Had {
		t.Fatalf("the diff read a baseline for a contract nothing has published: %+v", checked.Broken)
	}
	if checked.Broken[0].Next != contract.FirstVersion {
		t.Errorf("it would mint %s, want the first version", checked.Broken[0].Next)
	}
}

// TestAPartialDerivationHoldsTheElementAtTheMergeRow: the deprecation list reads
// a partial record the way it reads a could-not-derive one — the consumer stays
// on the list of every marked element of every producer contract the record
// names — and the list the producer's own diff waits on is that same list, so a
// removal reaching this row by any route waits on what the detector waits on.
func TestAPartialDerivationHoldsTheElementAtTheMergeRow(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false), element("Detail", "string", false, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)

	// The consumer declares Status alone, and the extractor met a read it could
	// not follow. Nothing derived names Detail, and the record's silence on it
	// shows nothing.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut, "a read through reflection")

	trimmed := published(element("Status", "string", true, false))
	removing := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err := g.check.Enforce(ctx, removing, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatalf("a removal passed while a consumer's derivation is partial: %+v", checked.Broken)
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q, want the producer's own diff", checked.Check())
	}
	if !slices.Contains(checked.Unreadable.Partial, g.consumer.ID) {
		t.Errorf("the partial consumers are %v, want the consumer %s", checked.Unreadable.Partial, g.consumer.ID)
	}
	if !contains(checked.Why(), "partial") || !contains(checked.Why(), g.consumer.ID) {
		t.Errorf("the rejection does not say the consumer's derivation is partial: %s", checked.Why())
	}

	// The same element's deprecation list gives the same answer, which is the
	// point: the two spellings are one list.
	marked := markedDetail(t, ctx, g)
	if marked.Empty() {
		t.Error("the deprecation list on the same element is empty while the diff's list is not")
	}
	if !slices.Contains(marked.Unreadable.Partial, g.consumer.ID) {
		t.Errorf("the deprecation list's partial consumers are %v, want the consumer", marked.Unreadable.Partial)
	}

	// The consumer's next release derives completely, and the same removal passes:
	// what lifts a partial record is a derivation that followed everything.
	ship(t, ctx, g, g.consumer, nil, []consumercontract.Draft{
		draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
	}, window.ExitTimedOut)
	again := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, again, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the removal is still refused after the derivation was complete: %s", checked.Why())
	}
}

// markedDetail ships a producer release that marks Detail deprecated and returns
// the deprecation list entry for it, so one test can compare the two lists the
// design says are one.
func markedDetail(t *testing.T, ctx context.Context, g graph) contractcheck.Marked {
	t.Helper()
	ship(t, ctx, g, g.producer, []contract.Form{
		published(element("Status", "string", true, false), element("Detail", "string", false, true)),
	}, nil, window.ExitTimedOut)
	list, err := g.check.Deprecated(ctx)
	if err != nil {
		t.Fatalf("Deprecated: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("the marked elements are %+v, want Detail alone", list)
	}
	return list[0]
}

// TestAConsumerNobodyCouldDeriveHoldsEveryProducersElement: nothing bounds what
// an unreadable consumer consumes, so a marked element's list can no longer be
// known empty and no removal ships mechanically while the record stands.
func TestAConsumerNobodyCouldDeriveHoldsEveryProducersElement(t *testing.T) {
	ctx, g := newGraph(t)

	full := published(element("Status", "string", true, false), element("Detail", "string", false, false))
	ship(t, ctx, g, g.producer, []contract.Form{full}, nil, window.ExitTimedOut)

	// A consumer with a release and no consumer contract at all: nothing yet
	// holds the producer's element.
	rel, _ := ship(t, ctx, g, g.consumer, nil, nil, window.ExitTimedOut)
	trimmed := published(element("Status", "string", true, false))
	before := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err := g.check.Enforce(ctx, before, g.production)
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if !checked.Passed() {
		t.Fatalf("the removal is refused with nothing naming the element: %s", checked.Why())
	}

	// The consumer's build is derived again and no extractor covers its
	// toolchain. It declares nothing and is on every producer's list all the same.
	if _, _, _, err := g.store.SubmitConsumerContract(ctx, theActor, theBy, rel.ItemID, g.consumer.ID,
		"no extractor covers this build", consumercontract.Derived{
			Extractor: consumercontract.Extractor{Toolchain: "rust", FactoryVersion: "test"},
			Cause:     consumercontract.CauseNoExtractor,
		}, ""); err != nil {
		t.Fatalf("submitting the could-not-derive record: %v", err)
	}

	after := candidateOf(t, ctx, g, g.producer, []contract.Form{trimmed}, nil, ok())
	checked, err = g.check.Enforce(ctx, after, g.production)
	if err != nil {
		t.Fatalf("the second Enforce: %v", err)
	}
	if checked.Passed() {
		t.Fatal("the removal passed while a consumer nobody could derive stands")
	}
	if checked.Check() != gate.AutoRejectedByContractDiff {
		t.Errorf("the check that rejected is %q, want the producer's own diff", checked.Check())
	}
	if !slices.Contains(checked.Unreadable.CouldNotDerive, g.consumer.ID) {
		t.Errorf("the unreadable consumers are %v, want the consumer %s",
			checked.Unreadable.CouldNotDerive, g.consumer.ID)
	}
	if !contains(checked.Why(), "nobody could derive") {
		t.Errorf("the rejection does not say nobody could derive the consumer: %s", checked.Why())
	}
}
