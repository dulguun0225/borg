// The derivation record beside a consumer contract's predicates: the record a
// run that could not derive leaves, what a partial one says the extractor could
// not follow, and the second derivation an upgraded extractor writes beside the
// first. They share newStore and the helpers of db_test.go.
package consumercontract_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

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
	version, derivation, written, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "no extractor covers this build", could, theManifest)
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

	// The score's context factor reads the whole install: nothing bounds what an
	// unreadable consumer consumes, so one standing record is what makes that
	// factor unknowable rather than zero.
	standing, err := consumercontract.StandingCouldNotDerive(ctx, pool)
	if err != nil {
		t.Fatalf("StandingCouldNotDerive: %v", err)
	}
	if len(standing) != 1 || standing[0].ItemID != itemID || !standing[0].CouldNotDerive() {
		t.Fatalf("the consumers nobody could read are %+v, want the one just written", standing)
	}

	// A later derivation by an extractor that can read the build supersedes it,
	// and the factor is computable again.
	readable := consumercontract.Derived{
		Extractor: consumercontract.Extractor{
			Name: consumercontract.ExtractorName, Version: "2",
			Toolchain: "rust", FactoryVersion: "test+1",
		},
		Drafts: []consumercontract.Draft{draft("Health.Status", gatepolicy.PredicateRead, "")},
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	of := consumercontract.Of{ItemID: itemID, ServiceID: theConsumer, ArtifactID: record.NewID("art")}
	if _, _, err := consumercontract.DeriveAgain(ctx, tx, implementer, of, readable); err != nil {
		t.Fatalf("DeriveAgain with an extractor that covers the toolchain: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if standing, err = consumercontract.StandingCouldNotDerive(ctx, pool); err != nil || len(standing) != 0 {
		t.Fatalf("the consumers nobody could read are %+v (%v), want none once one was read", standing, err)
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
			_, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer, name, d, theManifest)
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
	if _, derivation, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "one construct it could not follow", partial, theManifest); err != nil || !derivation.Partial() {
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
	first, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "the first extractor", declared(draft("Health.Status", gatepolicy.PredicateRead, "")), theManifest)
	if err != nil {
		t.Fatalf("SubmitConsumerContract: %v", err)
	}

	// The install's first-start step is the caller, and the path it takes is the
	// artifact store's: one call writes the second consumer contract version and
	// derives again into it. The version is nobody's — the extractor derived it —
	// so it carries the release that shipped that extractor and the event that
	// entered it, and it reads no manifest.
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
	second, derivation, written, err := store.DeriveConsumerContractAgain(ctx, artifact.FactoryStart,
		itemID, theConsumer, "the build as the newer extractor reads it", newer, "factory/test+1")
	if err != nil {
		t.Fatalf("DeriveConsumerContractAgain: %v", err)
	}
	if second.Version != 2 || second.Supersedes != first.ID {
		t.Fatalf("the second version is %+v, want version 2 superseding %s", second, first.ID)
	}
	if second.Authorship != "" || second.Author != "" || second.EnteredBy != artifact.EnteredByUpgradeFirstStart {
		t.Errorf("the version is authored by %q %q and entered by %q, want nobody and an upgrade's first start",
			second.Authorship, second.Author, second.EnteredBy)
	}
	if second.ShippedBundleIdentity != "factory/test+1" || second.InputManifestID != "" {
		t.Errorf("the version names bundle %q and manifest %q, want the release that shipped the extractor and no manifest",
			second.ShippedBundleIdentity, second.InputManifestID)
	}
	if len(written) != 2 || derivation.ArtifactID != second.ID {
		t.Fatalf("the derivation is %+v with %d predicates, want the second version's two", derivation, len(written))
	}

	// The same extractor again would write a record saying what the record says,
	// and the version it would have been written on goes back with it.
	if _, _, _, err := store.DeriveConsumerContractAgain(ctx, artifact.FactoryStart, itemID, theConsumer,
		"the same extractor", newer, "factory/test+1"); !errors.Is(err, consumercontract.ErrExtractorUnchanged) {
		t.Fatalf("deriving again with the same extractor = %v, want ErrExtractorUnchanged", err)
	}
	head, found, err := artifact.NewestOfKind(ctx, pool, itemID, artifact.KindConsumerContract)
	if err != nil || !found || head.ID != second.ID {
		t.Fatalf("the newest version is %+v, %v, %v; want the refused derivation to have left %s standing",
			head, found, err, second.ID)
	}

	// Only the factory's own start derives again: the call authors nothing, so
	// the actor is the whole of who wrote the row.
	if _, _, _, err := store.DeriveConsumerContractAgain(ctx, implementer, itemID, theConsumer,
		"a component deriving on its own", newer, "factory/test+1"); !errors.Is(err, artifact.ErrNotTheFactorysStart) {
		t.Errorf("deriving again as the implementation stage = %v, want ErrNotTheFactorysStart", err)
	}

	// The earlier record still stands, and the newest is the one in force.
	if _, found, err := consumercontract.DerivationFor(ctx, pool, first.ID); err != nil || !found {
		t.Fatalf("the earlier derivation = found %v, %v — the new record is written beside it", found, err)
	}
	newest, found, err := consumercontract.NewestDerivation(ctx, pool, itemID)
	if err != nil || !found {
		t.Fatalf("NewestDerivation = found %v, %v", found, err)
	}
	if newest.Extractor.Version != "2" || newest.ArtifactID != second.ID {
		t.Fatalf("the derivation in force is %+v, want the newest extractor's", newest)
	}
}

// TestTheExtractorPublishesItsConvention: a reader of the derivation sees the
// convention the extractor applied, published with the extractor and read back
// with the rest of the record.
func TestTheExtractorPublishesItsConvention(t *testing.T) {
	ctx, pool, store := newStore(t)

	itemID := record.NewID("it")
	_, derivation, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer,
		"the convention travels with the record", declared(draft("Health.Status", gatepolicy.PredicateRead, "")), theManifest)
	if err != nil {
		t.Fatalf("SubmitConsumerContract: %v", err)
	}
	if derivation.Extractor.Convention == "" {
		t.Fatal("the derivation names no convention, and a reader cannot see what the extractor applied")
	}
	read, found, err := consumercontract.NewestDerivation(ctx, pool, itemID)
	if err != nil || !found {
		t.Fatalf("NewestDerivation = found %v, %v", found, err)
	}
	if read.Extractor.Convention != consumercontract.GoConvention {
		t.Fatalf("the convention reads back as %q, want %q", read.Extractor.Convention, consumercontract.GoConvention)
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
