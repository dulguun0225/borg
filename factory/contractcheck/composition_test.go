// Check.ComposedFrom: a candidate's environment is composed from the producers
// its build's consumer contract names, and theirs through their current
// releases' consumer contracts.
package contractcheck_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/service"
	"github.com/dulguun0225/borg/factory/window"
)

// TestACandidatesEnvironmentIsComposedFromWhatItsConsumerContractNames: the
// composition is a walk over the one field that holds the edge between two
// services, and it goes one hop further than the candidate's own build — a
// producer's own current release names producers of its own, and the candidate's
// run reaches them through it.
func TestACandidatesEnvironmentIsComposedFromWhatItsConsumerContractNames(t *testing.T) {
	ctx, g := newGraph(t)

	// A third service under the producer, so the walk has somewhere transitive
	// to go: the consumer reaches the producer, and the producer reaches this.
	beneath, err := service.NewWriter(g.pool, g.token).Create(ctx, theActor, "beneath", t.TempDir(), g.project)
	if err != nil {
		t.Fatalf("writing the third service: %v", err)
	}

	ship(t, ctx, g, beneath, []contract.Form{published(element("Depth", "string", true, false))},
		nil, window.ExitTimedOut)
	// The producer's own current release declares against it, which is the hop
	// the candidate reaches through its producer.
	ship(t, ctx, g, g.producer, []contract.Form{published(element("Status", "string", true, false))},
		[]consumercontract.Draft{draft(beneath, theInterface, "Depth", gatepolicy.PredicateRead, "")},
		window.ExitTimedOut)

	// The candidate is the consumer's, and its own consumer contract names the
	// producer alone.
	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: newIntent(t, ctx, g), ServiceID: g.consumer.ID, Branch: "item/" + record.NewID("in"),
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the candidate's item: %v", err)
	}
	if _, _, _, err := g.store.SubmitConsumerContract(ctx, theActor, theBy, it.ID, g.consumer.ID,
		"derived from the build", consumercontract.Derived{
			Extractor: consumercontract.GoExtractor("test"),
			Drafts:    []consumercontract.Draft{draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, "")},
		}, ""); err != nil {
		t.Fatalf("submitting the candidate's consumer contract: %v", err)
	}

	composed, err := g.check.ComposedFrom(ctx, it.ID, g.consumer.ID, g.production)
	if err != nil {
		t.Fatalf("ComposedFrom: %v", err)
	}
	if len(composed) != 2 {
		t.Fatalf("the composition is %+v, want the producer and the service beneath it", composed)
	}
	first, second := composed[0], composed[1]
	if first.ServiceID != g.producer.ID || first.Through != "" || first.ReleaseID == "" {
		t.Errorf("the first entry is %+v, want the producer at its current release named by the candidate itself", first)
	}
	if len(first.Addresses) != 1 || first.Addresses[0] != theInterface {
		t.Errorf("the first entry's addresses are %v, want the entry the call site reads its address from", first.Addresses)
	}
	if second.ServiceID != beneath.ID || second.Through != g.producer.ID || second.ReleaseID == "" {
		t.Errorf("the second entry is %+v, want the service beneath, reached through the producer", second)
	}
}

// TestACandidateComposesNothingForItsOwnStoreOrAProducerRunningNothing: a
// service declares against its own store exactly as against another service's
// interface, and its own store is not a producer to put in place beside it. A
// producer running nothing is an entry with no release, which the caller reports
// rather than composing a hole.
func TestACandidateComposesNothingForItsOwnStoreOrAProducerRunningNothing(t *testing.T) {
	ctx, g := newGraph(t)

	it, err := g.items.Create(ctx, theActor, item.New{
		IntentID: newIntent(t, ctx, g), ServiceID: g.consumer.ID, Branch: "item/" + record.NewID("in"),
	}, "", "", nil)
	if err != nil {
		t.Fatalf("decomposing the candidate's item: %v", err)
	}
	if _, _, _, err := g.store.SubmitConsumerContract(ctx, theActor, theBy, it.ID, g.consumer.ID,
		"derived from the build", consumercontract.Derived{
			Extractor: consumercontract.GoExtractor("test"),
			Drafts: []consumercontract.Draft{
				draft(g.consumer, theStore, "Row", gatepolicy.PredicateRead, ""),
				draft(g.producer, theInterface, "Status", gatepolicy.PredicateRead, ""),
			},
		}, ""); err != nil {
		t.Fatalf("submitting the candidate's consumer contract: %v", err)
	}

	composed, err := g.check.ComposedFrom(ctx, it.ID, g.consumer.ID, g.production)
	if err != nil {
		t.Fatalf("ComposedFrom: %v", err)
	}
	if len(composed) != 1 || composed[0].ServiceID != g.producer.ID {
		t.Fatalf("the composition is %+v, want the producer alone — its own store is not one", composed)
	}
	if composed[0].ReleaseID != "" {
		t.Errorf("the producer is running nothing and the entry names release %s", composed[0].ReleaseID)
	}
}
