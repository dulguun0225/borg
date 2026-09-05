// The database tests of this package are in consumercontract_test rather than in
// consumer contract, because they open the pool through package postgres, which
// imports this one to apply its DDL, and because the one writer of these rows is
// the artifact store. deps.txt states those edges on its test line for
// consumercontract.
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package consumercontract_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// implementer is the actor a consumer contract version is written as: the stage
// that derived it from the build.
var implementer = record.Actor{Kind: record.KindComponent, Key: "agent.implementer"}

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
	token, err := lease.Acquire(ctx, pool, "test", time.Minute)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	return ctx, pool, artifact.NewStore(pool, token)
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
		Address:           "health",
		ProducerService:   "producer",
		ProducerServiceID: theProducer,
		Interface:         theInterface,
		Element:           element,
		Kind:              kind,
		Argument:          argument,
	}
}

// declared is what one extractor run produced: the drafts, by an extractor these
// tests name, with nothing it could not follow.
func declared(drafts ...consumercontract.Draft) consumercontract.Derived {
	return consumercontract.Derived{Extractor: consumercontract.GoExtractor("test"), Drafts: drafts}
}

// TestTheStoreWritesTheVersionAndItsPredicatesTogether: a consumer contract version
// is an artifact of the consumer's item and the predicates it introduces are
// written in the same call, so the two cannot disagree about what was declared.
func TestTheStoreWritesTheVersionAndItsPredicatesTogether(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	version, derivation, written, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer,
		"2 predicates derived from the build", declared(
			draft("Health.Status", gatepolicy.PredicateRead, ""),
			draft("Health.Status", gatepolicy.PredicatePopulated, ""),
		))
	if err != nil {
		t.Fatalf("SubmitConsumerContract: %v", err)
	}
	if version.Kind != artifact.KindConsumerContract || version.Version != 1 {
		t.Fatalf("the version is kind %q at %d, want a consumer contract at 1", version.Kind, version.Version)
	}
	if len(written) != 2 {
		t.Fatalf("the version introduced %d predicates, want two", len(written))
	}
	if derivation.Partial() || derivation.CouldNotDerive() {
		t.Fatalf("the derivation is %s, want complete", derivation.Describe())
	}
	if derivation.Extractor.Name != consumercontract.ExtractorName || derivation.Extractor.FactoryVersion != "test" {
		t.Errorf("the derivation names extractor %+v, and a record naming only the code is silent about the extractor",
			derivation.Extractor)
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
	if _, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "one bad predicate",
		declared(
			draft("Health.Status", gatepolicy.PredicateRead, ""),
			draft("Health.Status", gatepolicy.PredicateRange, "not a range"),
		)); err == nil {
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
	if _, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "first derivation",
		declared(
			draft("Health.Status", gatepolicy.PredicateRead, ""),
			draft("Health.Detail", gatepolicy.PredicateRead, ""),
		)); err != nil {
		t.Fatalf("the first SubmitConsumerContract: %v", err)
	}
	// The second derivation finds the build reading one field fewer, which is a
	// consumer that stopped reading it — and what stops seeing the old assertion is
	// this read, with nobody withdrawing anything.
	if _, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "second derivation",
		declared(draft("Health.Status", gatepolicy.PredicateRead, ""))); err != nil {
		t.Fatalf("the second SubmitConsumerContract: %v", err)
	}

	read, err := consumercontract.ForItems(ctx, pool, []string{itemID})
	if err != nil {
		t.Fatalf("ForItems: %v", err)
	}
	if len(read) != 1 || read[0].Element != "Health.Status" {
		t.Fatalf("the item declares %d predicate(s) %+v, want the newest version's one", len(read), read)
	}
	if named := consumercontract.NamingElement(read, theProducer, theInterface, "Health.Detail"); len(named) != 0 {
		t.Errorf("the element the second derivation dropped is still named: %+v", named)
	}
}

// TestOneVersionCannotIntroduceTheSameAssertionTwice: which is what a derivation
// that read one field through two paths would produce.
func TestOneVersionCannotIntroduceTheSameAssertionTwice(t *testing.T) {
	ctx, _, store := newStore(t)

	_, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer, "twice",
		declared(
			draft("Health.Status", gatepolicy.PredicateRead, ""),
			draft("Health.Status", gatepolicy.PredicateRead, ""),
		))
	if err == nil || !strings.Contains(err.Error(), "one_assertion_per_version") {
		t.Fatalf("the same assertion twice = %v, want the store's refusal", err)
	}
}

// TestAConsumerDeclaringAgainstAnUnpublishedInterfaceCarriesTheNameAlone: a
// consumer may declare against an interface no release has published yet, and the
// empty producer id is that answer rather than a refusal.
func TestAConsumerDeclaringAgainstAnUnpublishedInterfaceCarriesTheNameAlone(t *testing.T) {
	ctx, pool, store := newStore(t)

	unresolved := draft("Health.Status", gatepolicy.PredicateRead, "")
	unresolved.ProducerServiceID = ""
	version, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer,
		"against nothing published", declared(unresolved))
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
		if _, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), consumer,
			"one predicate", declared(draft("Health.Status", gatepolicy.PredicateRead, ""))); err != nil {
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

	insert := `insert into ` + consumercontract.Table + ` (id, format_version, actor_kind, actor_key, actor_key_basis, at, item_id, service_id,
		artifact_id, address, producer_service, producer_service_id, interface_name, element, kind, argument)
		values ($1, $2, 'component', 'agent.implementer', '', $3, $4, $5, $6, $7, $8, '', $9, $10, $11, '')`
	for _, refused := range []struct {
		name                                                                       string
		item, service, artifactID, address, producer, interfaceName, element, kind string
		constraint                                                                 string
	}{
		{"no item", "", "svc_a", "art_a", "health", "producer", "health", "Status", "read", "item_id_present"},
		{"no service", "it_a", "", "art_a", "health", "producer", "health", "Status", "read", "service_id_present"},
		{"no version", "it_a", "svc_a", "", "health", "producer", "health", "Status", "read", "artifact_id_present"},
		{"no address", "it_a", "svc_a", "art_a", "", "producer", "health", "Status", "read", "address_present"},
		{"no producer name", "it_a", "svc_a", "art_a", "health", "", "health", "Status", "read", "producer_service_present"},
		{"no interface", "it_a", "svc_a", "art_a", "health", "producer", "", "Status", "read", "interface_name_present"},
		{"no element", "it_a", "svc_a", "art_a", "health", "producer", "health", "", "read", "element_present"},
		{"no kind", "it_a", "svc_a", "art_a", "health", "producer", "health", "Status", "", "kind_present"},
	} {
		_, err := pool.Exec(ctx, insert, record.NewID(consumercontract.IDPrefix), consumercontract.FormatVersion, record.Now(),
			refused.item, refused.service, refused.artifactID, refused.address, refused.producer,
			refused.interfaceName, refused.element, refused.kind)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting with %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}
}

// TestACouldNotDeriveIsARecordAndNotAnEmptyList: "no consumer reads this" and "no
// consumer's read was visible" call for opposite responses, and a record that
// cannot tell them apart licenses the wrong one silently.
func TestACouldNotDeriveIsARecordAndNotAnEmptyList(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	could := consumercontract.Derived{
		Extractor: consumercontract.Extractor{Toolchain: "rust"},
		Cause:     consumercontract.CauseNoExtractor,
	}
	version, derivation, written, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer,
		"no extractor covers this build", could)
	if err != nil {
		t.Fatalf("SubmitConsumerContract for a build nobody can read: %v", err)
	}
	if len(written) != 0 || !derivation.CouldNotDerive() {
		t.Fatalf("the derivation is %s with %d predicates", derivation.Describe(), len(written))
	}
	if derivation.Cause != consumercontract.CauseNoExtractor {
		t.Errorf("the cause is %q, and the two call for different responses", derivation.Cause)
	}
	read, found, err := consumercontract.DerivationFor(ctx, pool, version.ID)
	if err != nil || !found {
		t.Fatalf("DerivationFor = found %v, %v", found, err)
	}
	if !read.CouldNotDerive() || read.Extractor.Toolchain != "rust" {
		t.Fatalf("the derivation reads back as %+v", read)
	}
	// A service with no consumer contract at all and one nobody could read are
	// different answers, and this is where a reader tells them apart.
	if _, found, err := consumercontract.DerivationFor(ctx, pool, "art_nothing"); err != nil || found {
		t.Fatalf("a version with no derivation = found %v, %v", found, err)
	}
}

// TestADerivationRefusesWhatItCannotMean: a could-not-derive record carrying
// predicates, an extraction that failed reporting nothing, and a cause that is
// neither.
func TestADerivationRefusesWhatItCannotMean(t *testing.T) {
	ctx, _, store := newStore(t)

	for name, d := range map[string]consumercontract.Derived{
		"could not derive and declares something": {
			Extractor: consumercontract.Extractor{Toolchain: "go"},
			Cause:     consumercontract.CauseNoExtractor,
			Drafts:    []consumercontract.Draft{draft("Health.Status", gatepolicy.PredicateRead, "")},
		},
		"an extraction that failed reporting nothing": {
			Extractor: consumercontract.GoExtractor("test"), Cause: consumercontract.CauseExtractionFailed,
		},
		"a cause that is neither": {
			Extractor: consumercontract.GoExtractor("test"), Cause: "tired",
		},
		"neither an extractor nor a cause": {
			Extractor: consumercontract.Extractor{Toolchain: "go"},
		},
		"no toolchain at all": {
			Extractor: consumercontract.Extractor{Name: "go/ast"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			_, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer, name, d)
			if !errors.Is(err, consumercontract.ErrDerivationIncomplete) {
				t.Fatalf("%s = %v, want ErrDerivationIncomplete", name, err)
			}
		})
	}
}

// TestAPartialRecordSaysWhatTheExtractorCouldNotFollow: a record whose list is
// empty is complete and one whose list is not is partial, and the deprecation
// list reads a partial record the way it reads a could-not-derive one.
func TestAPartialRecordSaysWhatTheExtractorCouldNotFollow(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	partial := declared(draft("Health.Status", gatepolicy.PredicateRead, ""))
	partial.Unfollowed = []string{"a read through reflection in main.go"}
	if _, derivation, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer,
		"one construct it could not follow", partial); err != nil || !derivation.Partial() {
		t.Fatalf("SubmitConsumerContract = %v, and the record is partial: %v", err, derivation.Partial())
	}
	read, found, err := consumercontract.NewestDerivation(ctx, pool, itemID)
	if err != nil || !found {
		t.Fatalf("NewestDerivation = found %v, %v", found, err)
	}
	if !read.Partial() || len(read.Unfollowed) != 1 {
		t.Fatalf("the record reads back as %+v, want partial with the one construct", read)
	}
	for _, of := range []struct {
		items []string
		want  int
	}{{[]string{itemID}, 1}, {nil, 0}} {
		derivations, err := consumercontract.DerivationsForItems(ctx, pool, of.items)
		if err != nil || len(derivations) != of.want {
			t.Fatalf("DerivationsForItems(%v) = %d, %v", of.items, len(derivations), err)
		}
	}
}

// TestDeriveAgainWritesBesideTheEarlierRecord: an upgrade whose shipped extractor
// for a toolchain changed derives again for every release in force on that
// toolchain, beside the earlier record and never over it, and a release's contract
// in force is its derivation by the newest extractor.
func TestDeriveAgainWritesBesideTheEarlierRecord(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	first, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer,
		"the first extractor", declared(draft("Health.Status", gatepolicy.PredicateRead, "")))
	if err != nil {
		t.Fatalf("SubmitConsumerContract: %v", err)
	}

	// The install's first-start step is what calls this, and it is not built. What
	// it does is write a second consumer contract version and derive again into
	// it, in one transaction.
	newer := consumercontract.Derived{
		Extractor: consumercontract.Extractor{
			Name: consumercontract.ExtractorName, Version: "2",
			Toolchain: consumercontract.Toolchain, FactoryVersion: "test+1",
		},
		Drafts: []consumercontract.Draft{
			draft("Health.Status", gatepolicy.PredicateRead, ""),
			draft("Health.Detail", gatepolicy.PredicateRead, ""),
		},
	}
	second := record.NewID("art")
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	of := consumercontract.Of{ItemID: itemID, ServiceID: theConsumer, ArtifactID: second}
	if _, _, err := consumercontract.DeriveAgain(ctx, tx, implementer, of, newer); err != nil {
		t.Fatalf("DeriveAgain: %v", err)
	}
	// The same extractor again would write a record saying what the record says.
	same := newer
	same.Extractor.Version = "2"
	if _, _, err := consumercontract.DeriveAgain(ctx, tx, implementer, of, same); !errors.Is(err, consumercontract.ErrExtractorUnchanged) {
		t.Fatalf("deriving again with the same extractor = %v, want ErrExtractorUnchanged", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}

	// The earlier record still stands, and the newest is the one in force.
	if _, found, err := consumercontract.DerivationFor(ctx, pool, first.ID); err != nil || !found {
		t.Fatalf("the earlier derivation = found %v, %v — the new record is written beside it", found, err)
	}
	newest, found, err := consumercontract.NewestDerivation(ctx, pool, itemID)
	if err != nil || !found {
		t.Fatalf("NewestDerivation = found %v, %v", found, err)
	}
	if newest.Extractor.Version != "2" || newest.ArtifactID != second {
		t.Fatalf("the derivation in force is %+v, want the newest extractor's", newest)
	}
}

// TestDDLListsEveryCause: a cause the store would refuse is one this package can
// write, and the two lists have to agree.
func TestDDLListsEveryCause(t *testing.T) {
	joined := strings.Join(consumercontract.DDL, "")
	for _, cause := range consumercontract.Causes {
		if !strings.Contains(joined, "'"+string(cause)+"'") {
			t.Errorf("the schema does not list cause %q", cause)
		}
	}
}
