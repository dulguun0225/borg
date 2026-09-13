package consumercontract_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/record"
)

// An empty or unreadable newest version supersedes old predicates too. Equal
// timestamps and arbitrary ids cannot make the record readers disagree.
func TestNewestEmptyOrUnreadableDerivationSupersedesPredicates(t *testing.T) {
	for _, unreadable := range []bool{false, true} {
		t.Run(map[bool]string{false: "empty", true: "unreadable"}[unreadable], func(t *testing.T) {
			ctx, pool, store := newStore(t)
			item := record.NewID("it")
			_, first, _, err := store.SubmitConsumerContract(ctx, implementer, by, item, theConsumer, "old", declared(draft("Health.Status", gatepolicy.PredicateRead, "")), theManifest)
			if err != nil {
				t.Fatal(err)
			}
			newer := declared()
			if unreadable {
				newer.Cause = consumercontract.CauseExtractionFailed
				newer.Reported = "unreadable source"
			}
			_, second, _, err := store.SubmitConsumerContract(ctx, implementer, by, item, theConsumer, "new", newer, theManifest)
			if err != nil {
				t.Fatal(err)
			}
			// Reverse lexical id order and force equal timestamps while preserving the
			// insertion sequence. This reproduces both formerly ambiguous reads.
			_, err = pool.Exec(ctx, `update `+consumercontract.DerivationTable+` set at=$1, id=case when id=$2 then 'ccd_z' else 'ccd_a' end where item_id=$3`, first.At, first.ID, item)
			if err != nil {
				t.Fatal(err)
			}
			got, err := consumercontract.ForItems(ctx, pool, []string{item})
			if err != nil || len(got) != 0 {
				t.Fatalf("ForItems = %+v, %v", got, err)
			}
			newest, found, err := consumercontract.NewestDerivation(ctx, pool, item)
			if err != nil || !found || newest.ArtifactID != second.ArtifactID {
				t.Fatalf("NewestDerivation = %+v, %v, %v", newest, found, err)
			}
			all, err := consumercontract.DerivationsForItems(ctx, pool, []string{item})
			if err != nil || len(all) != 1 || all[0].ArtifactID != second.ArtifactID {
				t.Fatalf("DerivationsForItems = %+v, %v", all, err)
			}
			standing, err := consumercontract.StandingCouldNotDerive(ctx, pool)
			want := 0
			if unreadable {
				want = 1
			}
			if err != nil || len(standing) != want {
				t.Fatalf("StandingCouldNotDerive = %+v, %v", standing, err)
			}
			// A complete empty version lifts an unreadable record even at the same
			// timestamp; it cannot leave the earlier cause standing.
			_, third, _, err := store.SubmitConsumerContract(ctx, implementer, by, item, theConsumer, "complete", declared(), theManifest)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = pool.Exec(ctx, `update `+consumercontract.DerivationTable+` set at=$1 where id=$2`, first.At, third.ID); err != nil {
				t.Fatal(err)
			}
			standing, err = consumercontract.StandingCouldNotDerive(ctx, pool)
			if err != nil || len(standing) != 0 {
				t.Fatalf("old unreadable version stood: %+v, %v", standing, err)
			}
		})
	}
}

func TestDerivationRequiresExtractorAndFactoryVersions(t *testing.T) {
	ctx, _, store := newStore(t)
	for _, field := range []string{"name", "extractor version", "factory version"} {
		for _, failed := range []bool{false, true} {
			d := declared()
			switch field {
			case "name":
				d.Extractor.Name = ""
			case "extractor version":
				d.Extractor.Version = ""
			case "factory version":
				d.Extractor.FactoryVersion = ""
			}
			if failed {
				d.Cause = consumercontract.CauseExtractionFailed
				d.Reported = "failed"
			}
			_, _, _, err := store.SubmitConsumerContract(ctx, implementer, by, record.NewID("it"), theConsumer, "incomplete", d, theManifest)
			if !errors.Is(err, consumercontract.ErrDerivationIncomplete) {
				t.Fatalf("missing %s, failed %v: %v", field, failed, err)
			}
		}
	}
}

// Backfilling the sequence preserves the deterministic historical ordering;
// applying the schema again cannot reset it below a stored derivation.
func TestDerivationOrderingUpgradesAndReapplies(t *testing.T) {
	ctx, pool, store := newStore(t)
	item := record.NewID("it")
	_, first, _, err := store.SubmitConsumerContract(ctx, implementer, by, item, theConsumer, "first", declared(draft("Health.Status", gatepolicy.PredicateRead, "")), theManifest)
	if err != nil {
		t.Fatal(err)
	}
	_, second, _, err := store.SubmitConsumerContract(ctx, implementer, by, item, theConsumer, "second", declared(), theManifest)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(ctx, `alter table `+consumercontract.DerivationTable+` drop column recorded_order`); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		for _, statement := range consumercontract.DDL {
			if _, err = pool.Exec(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		newest, found, err := consumercontract.NewestDerivation(ctx, pool, item)
		if err != nil || !found || newest.ArtifactID != second.ArtifactID {
			t.Fatalf("upgraded newest=%+v %v %v", newest, found, err)
		}
	}
	_, third, _, err := store.SubmitConsumerContract(ctx, implementer, by, item, theConsumer, "third", declared(), theManifest)
	if err != nil {
		t.Fatal(err)
	}
	// Even a backwards clock cannot make a newly recorded version older.
	if _, err = pool.Exec(ctx, `update `+consumercontract.DerivationTable+` set at=$1 where id=$2`, first.At, third.ID); err != nil {
		t.Fatal(err)
	}
	newest, found, err := consumercontract.NewestDerivation(ctx, pool, item)
	if err != nil || !found || newest.ArtifactID != third.ArtifactID {
		t.Fatalf("newest after upgrade=%+v %v %v", newest, found, err)
	}
}
