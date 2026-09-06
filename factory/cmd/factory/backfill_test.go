// What a backfill item's checkout declares, derived here the way the schema
// change a checkout declares and the forms it publishes are.
package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/deploy"
)

// TestABackfillItemDeclaresThePairItCopiesBetween: a backfill item is a release
// whose change is data and not form, so neither the diff nor the schema change a
// build declares says there is a change to exercise — what says it is the pair
// the item declares, and the candidate environment runs the change twice over
// the seeded store on the strength of it.
func TestABackfillItemDeclaresThePairItCopiesBetween(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, "store.ledger.go", "package ledger\n")

	none, err := declaresBackfill(repo)
	if err != nil {
		t.Fatalf("declaresBackfill over a checkout with no backfill file: %v", err)
	}
	if none.Any() {
		t.Errorf("a checkout with no backfill file declares %+v, want none", none)
	}

	write(t, repo, backfillFileName("ledger"),
		"// The migration's third item.\n//borg:backfill Ledger.AmountMinor from Ledger.Amount\npackage ledger\n")
	declared, err := declaresBackfill(repo)
	if err != nil {
		t.Fatalf("declaresBackfill: %v", err)
	}
	want := deploy.Backfill{Contract: "ledger", Element: "Ledger.AmountMinor", FromElement: "Ledger.Amount"}
	if declared != want {
		t.Errorf("the checkout declares %+v, want %+v", declared, want)
	}
}

// TestABackfillFileThatDeclaresNoPairIsAnError: a file named for the convention
// and declaring nothing is a build whose author meant to declare a backfill.
// Reading it as no backfill would ship the copy with the double run never asked
// for, which is the one thing the exercise exists to establish.
func TestABackfillFileThatDeclaresNoPairIsAnError(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, backfillFileName("ledger"), "package ledger\n")
	if _, err := declaresBackfill(repo); !errors.Is(err, errBackfillDeclaration) {
		t.Errorf("a backfill file declaring no pair = %v, want errBackfillDeclaration", err)
	}

	write(t, repo, backfillFileName("ledger"),
		"//borg:backfill Ledger.AmountMinor from Ledger.Amount\n"+
			"//borg:backfill Ledger.Currency from Ledger.CurrencyCode\npackage ledger\n")
	if _, err := declaresBackfill(repo); !errors.Is(err, errBackfillDeclaration) {
		t.Errorf("a backfill file declaring two pairs = %v, want errBackfillDeclaration", err)
	}
}

func write(t *testing.T, repo, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repo, name), []byte(content), 0o644); err != nil {
		t.Fatalf("writing %s: %v", name, err)
	}
}

// backfillFileName is the file the convention names, spelled here from the
// prefix the derivation reads so that the two cannot drift apart.
func backfillFileName(store string) string {
	return backfillPrefix + store + ".go"
}

// TestTheBackfillFileNameIsTheStoresOwn keeps the spelling above honest.
func TestTheBackfillFileNameIsTheStoresOwn(t *testing.T) {
	if !strings.HasPrefix(backfillFileName("ledger"), "backfill.") {
		t.Errorf("the backfill file is %q, want it named for the store it is over", backfillFileName("ledger"))
	}
}
