package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dulguun0225/borg/factory/deploy"
)

// What a backfill item declares, derived from its checkout the way the schema
// change a checkout declares and the forms it publishes are.
//
// A backfill item is a release whose change is data and not form: it declares no
// schema diff and opens no contract version, only the element it fills and the
// element it fills from. So neither the diff nor the schema change a build
// declares says there is a change to exercise, and this is what does — the
// candidate environment runs that change twice over the seeded store, a second
// run that changes anything being a rejection at Merge to master.
//
// The convention is one file at the root of the repository, named for the store
// contract the copy is over:
//
//	backfill.<store>.go
//
// carrying one directive line, which is the pair:
//
//	//borg:backfill <element> from <element>
//
// It is a convention about where a declaration lives, per toolchain the way
// every other derivation from a checkout is, and a second toolchain replaces it
// rather than extending it. Only the root directory is read, for the reason
// package contract's own derivation reads only the root: a service is one
// repository with one long-lived branch and the declaration sits beside the
// store file it is over.

const (
	// backfillPrefix is the file-name prefix of a backfill declaration. The rest
	// of the stem is the store contract's name, the way package contract's
	// convention names an interface in the file rather than in a type.
	backfillPrefix = "backfill."
	// backfillDirective is the line the pair is written on. Go has no struct tag
	// on a file, so the declaration is a directive comment, which is the form
	// that convention already gives an operation's deprecation mark.
	backfillDirective = "//borg:backfill "
	// backfillFrom separates the element filled from the element it is filled
	// from.
	backfillFrom = " from "
)

// errBackfillDeclaration is returned where a file follows the naming convention
// and is not something a pair can be read from: it carries no directive, or more
// than one. It is an error and not an empty answer for the reason package
// contract's derivation errors on a contract file that publishes nothing — a
// build that names the file meant to declare a backfill, and reading it as none
// would ship the copy with the double run never asked for.
var errBackfillDeclaration = errors.New("factory: a backfill file the derivation cannot read")

// declaresBackfill is the pair the checkout at root declares, and the empty
// [deploy.Backfill] where it declares none — which is every item whose change is
// form and not data.
func declaresBackfill(root string) (deploy.Backfill, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return deploy.Backfill{}, fmt.Errorf("factory: reading the checkout at %s: %w", root, err)
	}
	var declared deploy.Backfill
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		store, named := backfillStore(entry.Name())
		if !named {
			continue
		}
		if declared.Any() {
			return deploy.Backfill{}, fmt.Errorf(
				"%w: %s declares a second backfill and an item's change is one copy",
				errBackfillDeclaration, entry.Name())
		}
		declared, err = backfillIn(filepath.Join(root, entry.Name()), store)
		if err != nil {
			return deploy.Backfill{}, err
		}
	}
	return declared, nil
}

// backfillStore is the store contract a file's own name says the copy is over,
// and false for a file that is not a backfill declaration. A _test.go file is
// never one, the way it is never a contract file.
func backfillStore(file string) (string, bool) {
	if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
		return "", false
	}
	name, found := strings.CutPrefix(strings.TrimSuffix(file, ".go"), backfillPrefix)
	if !found || name == "" || strings.Contains(name, ".") {
		return "", false
	}
	return name, true
}

// backfillIn is the pair one backfill file declares. Exactly one directive: an
// item's change is one copy, so a file carrying two is two items written as one
// and a file carrying none declares nothing the double run could be asked over.
func backfillIn(path, store string) (deploy.Backfill, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return deploy.Backfill{}, fmt.Errorf("factory: reading %s: %w", path, err)
	}
	declared := deploy.Backfill{Contract: store}
	for _, line := range strings.Split(string(source), "\n") {
		pair, found := strings.CutPrefix(strings.TrimSpace(line), backfillDirective)
		if !found {
			continue
		}
		if declared.Any() {
			return deploy.Backfill{}, fmt.Errorf(
				"%w: %s declares more than one pair and an item's change is one copy",
				errBackfillDeclaration, path)
		}
		element, from, split := strings.Cut(pair, backfillFrom)
		element, from = strings.TrimSpace(element), strings.TrimSpace(from)
		if !split || element == "" || from == "" {
			return deploy.Backfill{}, fmt.Errorf(
				"%w: %s declares %q, and a backfill names the element it fills and the element it fills from",
				errBackfillDeclaration, path, pair)
		}
		declared.Element, declared.FromElement = element, from
	}
	if !declared.Any() {
		return deploy.Backfill{}, fmt.Errorf(
			"%w: %s declares no %s line", errBackfillDeclaration, path, strings.TrimSpace(backfillDirective))
	}
	return declared, nil
}
