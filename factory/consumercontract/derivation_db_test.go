// The derivation record beside a consumer contract's predicates: the record a
// run that could not derive leaves, what a partial one says the extractor could
// not follow, and the second derivation an upgraded extractor writes beside the
// first. They share newStore and the helpers of db_test.go.
package consumercontract_test

import (
	"errors"
	"strings"
	"testing"

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
	version, derivation, written, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "no extractor covers this build", could, "")
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
			_, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer, name, d, "")
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
	if _, derivation, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "one construct it could not follow", partial, ""); err != nil || !derivation.Partial() {
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
	first, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, itemID, theConsumer, "the first extractor", declared(draft("Health.Status", gatepolicy.PredicateRead, "")), "")
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
