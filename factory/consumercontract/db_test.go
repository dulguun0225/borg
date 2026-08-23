// The database tests of this package are in consumercontract_test rather than in
// consumer contract, because they open the pool through package postgres, which
// imports this one to apply its DDL, and because the one writer of these rows is
// the artifact store. deps.txt records the edges as "test consumercontract ->
// postgres contract gatepolicy".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package consumercontract_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// implementer is the actor a consumer contract version is written as: the stage
// that derived it from the build.
var implementer = record.Actor{Kind: record.KindComponent, Name: "agent.implementer"}

// by is who authored the version, which for a derived consumer contract is the
// model the implementation stage ran on.
var by = artifact.By{Authorship: artifact.AuthorshipAgent, Author: "fake-model-1"}

func newStore(t *testing.T) (context.Context, *pgxpool.Pool, *artifact.Store) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m5_cc_" + hex.EncodeToString(suffix[:])

	pool, err := postgres.Open(ctx, inSchema(t, postgres.URL(), schema))
	if err != nil {
		t.Fatalf("the database at %s is not reachable, and these tests do not skip: %v", postgres.URL(), err)
	}
	t.Cleanup(func() {
		drop, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := pool.Exec(drop, `drop schema if exists `+pgx.Identifier{schema}.Sanitize()+` cascade`); err != nil {
			t.Errorf("dropping schema %s: %v", schema, err)
		}
		pool.Close()
	})
	if _, err := pool.Exec(ctx, `create schema `+pgx.Identifier{schema}.Sanitize()); err != nil {
		t.Fatalf("creating schema %s: %v", schema, err)
	}
	if err := postgres.Apply(ctx, pool); err != nil {
		t.Fatalf("applying the schema: %v", err)
	}
	return ctx, pool, artifact.NewStore(pool)
}

func inSchema(t *testing.T, base, schema string) string {
	t.Helper()
	parsed, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parsing %s: %v", base, err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

// The consumer and the producer these tests use. The producer is named and
// resolved: a consumer contract carries both the name the build gave and the id
// it resolved to, and the empty id is a real answer.
const (
	theConsumer  = "svc_consumer"
	theProducer  = "svc_producer"
	theInterface = "health"
)

func draft(element string, kind gatepolicy.PredicateKind, argument string) consumercontract.Draft {
	return consumercontract.Draft{
		ProducerService:   "producer",
		ProducerServiceID: theProducer,
		Interface:         theInterface,
		Element:           element,
		Kind:              kind,
		Argument:          argument,
	}
}

// TestTheStoreWritesTheVersionAndItsPredicatesTogether: a consumer contract version
// is an artifact of the consumer's item and the predicates it introduces are
// written in the same call, so the two cannot disagree about what was declared.
func TestTheStoreWritesTheVersionAndItsPredicatesTogether(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	version, written, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer,
		"2 predicates derived from the build", []consumercontract.Draft{
			draft("Status", gatepolicy.PredicateRead, ""),
			draft("Status", gatepolicy.PredicatePopulated, ""),
		})
	if err != nil {
		t.Fatalf("SubmitConsumerContract: %v", err)
	}
	if version.Kind != artifact.KindConsumerContract || version.Version != 1 {
		t.Fatalf("the version is kind %q at %d, want a consumer contract at 1", version.Kind, version.Version)
	}
	if len(written) != 2 {
		t.Fatalf("the version introduced %d predicates, want two", len(written))
	}
	read, err := consumercontract.ForArtifact(ctx, pool, version.ID)
	if err != nil {
		t.Fatalf("ForArtifact: %v", err)
	}
	if len(read) != 2 {
		t.Fatalf("the version's predicates read back as %d rows", len(read))
	}
	for _, p := range read {
		if p.ServiceID != theConsumer || p.ItemID != itemID || p.ArtifactID != version.ID {
			t.Errorf("a predicate reads back as %+v", p)
		}
		if p.ProducerService != "producer" || p.ProducerServiceID != theProducer {
			t.Errorf("the producer reads back as %q / %q", p.ProducerService, p.ProducerServiceID)
		}
	}
}

// TestAPredicateTheConsumerContractCannotDecideRollsTheVersionBack: the version
// and its predicates commit together or not at all, which is what stops a consumer
// contract version existing that nothing can be decided against.
func TestAPredicateTheConsumerContractCannotDecideRollsTheVersionBack(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	if _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "one bad predicate",
		[]consumercontract.Draft{
			draft("Status", gatepolicy.PredicateRead, ""),
			draft("Status", gatepolicy.PredicateRange, "not a range"),
		}); err == nil {
		t.Fatal("a range whose ends are not numbers was accepted")
	}
	newest, found, err := artifact.NewestOfKind(ctx, pool, itemID, artifact.KindConsumerContract)
	if err != nil {
		t.Fatalf("NewestOfKind: %v", err)
	}
	if found {
		t.Fatalf("a refused predicate left the version %s behind", newest.ID)
	}
	if read, err := consumercontract.ForItems(ctx, pool, []string{itemID}); err != nil || len(read) != 0 {
		t.Fatalf("ForItems = %d rows, %v", len(read), err)
	}
}

// TestForItemsReadsTheNewestVersionOfEachItem: a stage attempted twice authors two
// consumer contract versions, and what the item declares is the later one — the
// same rule the artifact store's version chain already sets.
func TestForItemsReadsTheNewestVersionOfEachItem(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	if _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "first derivation",
		[]consumercontract.Draft{
			draft("Status", gatepolicy.PredicateRead, ""),
			draft("Detail", gatepolicy.PredicateRead, ""),
		}); err != nil {
		t.Fatalf("the first SubmitConsumerContract: %v", err)
	}
	// The second derivation finds the build reading one field fewer, which is a
	// consumer that stopped reading it — and what stops seeing the old assertion is
	// this read, with nobody withdrawing anything.
	if _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "second derivation",
		[]consumercontract.Draft{draft("Status", gatepolicy.PredicateRead, "")}); err != nil {
		t.Fatalf("the second SubmitConsumerContract: %v", err)
	}

	read, err := consumercontract.ForItems(ctx, pool, []string{itemID})
	if err != nil {
		t.Fatalf("ForItems: %v", err)
	}
	if len(read) != 1 || read[0].Element != "Status" {
		t.Fatalf("the item declares %d predicate(s) %+v, want the newest version's one", len(read), read)
	}
	if named := consumercontract.NamingElement(read, theProducer, theInterface, "Detail"); len(named) != 0 {
		t.Errorf("the element the second derivation dropped is still named: %+v", named)
	}
}

// TestOneVersionCannotIntroduceTheSameAssertionTwice: which is what a derivation
// that read one field through two paths would produce.
func TestOneVersionCannotIntroduceTheSameAssertionTwice(t *testing.T) {
	ctx, _, store := newStore(t)

	_, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer, "twice",
		[]consumercontract.Draft{
			draft("Status", gatepolicy.PredicateRead, ""),
			draft("Status", gatepolicy.PredicateRead, ""),
		})
	if err == nil || !strings.Contains(err.Error(), "one_assertion_per_version") {
		t.Fatalf("the same assertion twice = %v, want the store's refusal", err)
	}
}

// TestAConsumerDeclaringAgainstAnUnpublishedInterfaceCarriesTheNameAlone: a
// consumer may declare against an interface no release has published yet, and the
// empty producer id is that answer rather than a refusal.
func TestAConsumerDeclaringAgainstAnUnpublishedInterfaceCarriesTheNameAlone(t *testing.T) {
	ctx, pool, store := newStore(t)

	unresolved := draft("Status", gatepolicy.PredicateRead, "")
	unresolved.ProducerServiceID = ""
	version, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer,
		"against nothing published", []consumercontract.Draft{unresolved})
	if err != nil {
		t.Fatalf("SubmitConsumerContract: %v", err)
	}
	read, err := consumercontract.ForArtifact(ctx, pool, version.ID)
	if err != nil {
		t.Fatalf("ForArtifact: %v", err)
	}
	if len(read) != 1 || read[0].ProducerServiceID != "" || read[0].ProducerService != "producer" {
		t.Fatalf("the predicate reads back as %+v", read)
	}
}

// TestAgainstProducerAndConsumerServicesEverReadTheGraph: the two reads the risk
// score's context factor and the in-force query are each a use of.
func TestAgainstProducerAndConsumerServicesEverReadTheGraph(t *testing.T) {
	ctx, pool, store := newStore(t)

	for _, consumer := range []string{theConsumer, "svc_other", theProducer} {
		if _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), consumer,
			"one predicate", []consumercontract.Draft{draft("Status", gatepolicy.PredicateRead, "")}); err != nil {
			t.Fatalf("SubmitConsumerContract for %s: %v", consumer, err)
		}
	}
	against, err := consumercontract.AgainstProducer(ctx, pool, theProducer)
	if err != nil {
		t.Fatalf("AgainstProducer: %v", err)
	}
	if len(against) != 3 {
		t.Fatalf("%d predicates name the producer, want the three written", len(against))
	}
	ever, err := consumercontract.ConsumerServicesEver(ctx, pool, theProducer)
	if err != nil {
		t.Fatalf("ConsumerServicesEver: %v", err)
	}
	if len(ever) != 3 {
		t.Fatalf("%d services have declared against the producer, want three — including the producer itself, whose own past is a consumer", len(ever))
	}
	if len(consumercontract.ItemsOf(against)) != 3 {
		t.Errorf("the items behind those predicates are %v", consumercontract.ItemsOf(against))
	}
}

// TestTheStoreRefusesAroundTheWriter inserts by raw SQL, so what it exercises is
// the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesAroundTheWriter(t *testing.T) {
	ctx, pool, _ := newStore(t)

	insert := `insert into ` + consumercontract.Table + ` (id, actor_kind, actor_name, at, item_id, service_id,
		artifact_id, producer_service, producer_service_id, interface_name, element, kind, argument)
		values ($1, 'component', 'agent.implementer', $2, $3, $4, $5, $6, '', $7, $8, $9, '')`
	for _, refused := range []struct {
		name                                                              string
		item, service, artifactID, producer, interfaceName, element, kind string
		constraint                                                        string
	}{
		{"no item", "", "svc_a", "art_a", "producer", "health", "Status", "read", "item_id_present"},
		{"no service", "it_a", "", "art_a", "producer", "health", "Status", "read", "service_id_present"},
		{"no version", "it_a", "svc_a", "", "producer", "health", "Status", "read", "artifact_id_present"},
		{"no producer name", "it_a", "svc_a", "art_a", "", "health", "Status", "read", "producer_service_present"},
		{"no interface", "it_a", "svc_a", "art_a", "producer", "", "Status", "read", "interface_name_present"},
		{"no element", "it_a", "svc_a", "art_a", "producer", "health", "", "read", "element_present"},
		{"no kind", "it_a", "svc_a", "art_a", "producer", "health", "Status", "", "kind_present"},
	} {
		_, err := pool.Exec(ctx, insert, record.NewID(consumercontract.IDPrefix), record.Now(),
			refused.item, refused.service, refused.artifactID, refused.producer,
			refused.interfaceName, refused.element, refused.kind)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting with %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}
}
