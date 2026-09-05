// Tests of a decomposition that spans two services: the contract a
// producer publishes and the consumer contract derived from what its
// consumer reads.
package main

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/contract"
)

// TestOneIntentBecomesTwoItemsAndTheContractArrivesWithTheRelease is the first
// episode: Decomposition fires over a set for the first time, the producer's release
// publishes the contract at 1.0.0 inside the mint that gave it its number, the
// consumer's environment is composed from what the producer is running, and the
// consumer's release derives a consumer contract naming what its code reads.
func TestOneIntentBecomesTwoItemsAndTheContractArrivesWithTheRelease(t *testing.T) {
	ctx, d, out := newContractPath(t)
	res := pair(t, ctx, d, out)

	if len(res.decompositions) != 1 || len(res.decompositions[0].itemIDs) != 2 {
		t.Fatalf("the run decomposed %+v, want one intent and two items", res.decompositions)
	}
	set := res.decompositions[0]
	if !set.decided || !set.approved {
		t.Fatalf("the Decomposition row decided=%v approved=%v over a set of two", set.decided, set.approved)
	}
	if set.fired.opening == "" || set.fired.closing == "" {
		t.Fatalf("the Decomposition firing left %+v, want two rows", set.fired)
	}
	if !strings.Contains(out.String(), "the diff factors are unavailable here") {
		t.Errorf("the row does not say why its vector has holes in it:\n%s", out)
	}

	// One intent, two items, two services — and the intent is what joins them,
	// which is work that spans services needing no record type of its own.
	var producer, consumer *candidate
	for _, c := range res.candidates {
		if c.svc.Name == theService {
			producer = c
		}
		if c.svc.Name == theSecondService {
			consumer = c
		}
	}
	if producer == nil || consumer == nil {
		t.Fatalf("the run produced %d candidates and not one per service", len(res.candidates))
	}
	if producer.intentID != consumer.intentID {
		t.Fatalf("the two items name intents %s and %s, and one request produced both",
			producer.intentID, consumer.intentID)
	}
	if len(consumer.waitsOn) != 1 || consumer.waitsOn[0] != producer.itemID {
		t.Fatalf("the consumer's item waits on %v, want the producer's item %s", consumer.waitsOn, producer.itemID)
	}

	// The producer's release published the contract, and the queue wrote it inside
	// the mint that gave that release its number.
	if !producer.merged || producer.deployID == "" {
		t.Fatalf("the producer merged=%v deployed=%q", producer.merged, producer.deployID)
	}
	if len(producer.published) != 1 {
		t.Fatalf("the producer's release published %d contracts, want the one its build declares", len(producer.published))
	}
	published := producer.published[0]
	if !published.Created || !published.Moved || published.Version.Semver != contract.FirstVersion {
		t.Fatalf("the contract published as %+v, want a created contract at %s", published, contract.FirstVersion)
	}
	if published.Version.ReleaseID != producer.releaseID {
		t.Errorf("the version names release %s, and the release minted with it is %s",
			published.Version.ReleaseID, producer.releaseID)
	}
	if published.Version.ReleaseNumber != producer.releaseNumber {
		t.Errorf("the version carries number %d and the release is %d",
			published.Version.ReleaseNumber, producer.releaseNumber)
	}
	form, err := contract.FormOf(ctx, d.pool, published.Contract, published.Version.ID)
	if err != nil {
		t.Fatalf("FormOf: %v", err)
	}
	if len(form.Elements) != 3 {
		t.Fatalf("the form has %d elements: %+v", len(form.Elements), form.Elements)
	}
	status, _ := form.Element("Health.Status")
	if !status.Populated {
		t.Error("Status is not always populated, and the source tags it populated")
	}

	// The consumer's environment was composed from the producer's current release,
	// which is the first composition any run in this repository has performed with
	// something in it.
	if len(consumer.composedFrom) != 1 || consumer.composedFrom[0].ReleaseID != producer.releaseID {
		t.Fatalf("the consumer's environment was composed from %+v, want the producer's release %s",
			consumer.composedFrom, producer.releaseID)
	}

	// And its release derived a consumer contract naming what its code reads.
	if consumer.consumerContractArtifactID == "" {
		t.Fatal("the consumer's build derived no consumer contract, and its code reads two of the producer's elements")
	}
	predicates, err := consumercontract.ForArtifact(ctx, d.pool, consumer.consumerContractArtifactID)
	if err != nil {
		t.Fatalf("ForArtifact: %v", err)
	}
	kinds := map[string]bool{}
	for _, p := range predicates {
		kinds[p.Element+"/"+string(p.Kind)] = true
		if p.ProducerServiceID != producer.svc.ID {
			t.Errorf("a predicate names producer %q, want the demo service's id", p.ProducerServiceID)
		}
	}
	for _, want := range []string{"Health.Status/read", "Health.Status/populated", "Health.Status/domain", "Health.Detail/read"} {
		if !kinds[want] {
			t.Errorf("%s was not derived; the consumer contract is %v", want, kinds)
		}
	}
	if kinds["Health.Detail/populated"] {
		t.Error("Detail was declared populated, and the mirror tags it with nothing")
	}
}
