// The database tests of this package are in contract_test rather than in
// contract, because they open the pool through package postgres, which imports
// this one to apply its DDL. deps.txt records the edge as "test contract ->
// postgres".
//
// None of these tests skips when the database is unreachable. The milestone is
// demonstrated by them running, so an unreachable database fails the run.
package contract_test

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

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/postgres"
	"github.com/dulguun0225/borg/factory/record"
)

// queue is the one writer of contracts and their versions, the way doc.go names
// it: the merge queue, writing inside the transaction that mints the release.
var queue = record.Actor{Kind: record.KindComponent, Key: "merge_queue", Basis: record.BasisClaimed}

func newStore(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := t.Context()

	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatalf("naming the test schema: %v", err)
	}
	schema := "m5_con_" + hex.EncodeToString(suffix[:])

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
	return ctx, pool
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

// theService is the publisher every test here uses, and theInterface the name its
// build gives what it publishes.
const (
	theService   = "svc_publisher"
	theInterface = "health"
)

// publish writes one form at one release number, in a transaction of its own —
// which is how its one caller writes it, inside the transaction that mints the
// release.
func publish(t *testing.T, ctx context.Context, pool *pgxpool.Pool, number int64, form contract.Form) contract.Published {
	t.Helper()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	published, err := contract.Publish(ctx, tx, queue, contract.Publication{
		ServiceID:     theService,
		ReleaseID:     record.NewID("rel"),
		ReleaseNumber: number,
		ItemID:        record.NewID("it"),
		Form:          form,
	})
	if err != nil {
		t.Fatalf("Publish at release %d: %v", number, err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return published
}

// form is one form of that kind. A store's elements are in store position and an
// interface's in output position, which is what the two helpers together say: the
// position is a fact about where the element sits and not something a test picks.
func form(kind contract.Kind, elements ...contract.Element) contract.Form {
	made := make([]contract.Element, 0, len(elements))
	for _, e := range elements {
		if kind == contract.KindStore {
			e.Position = contract.PositionStore
		}
		made = append(made, e)
	}
	return contract.Form{Name: theInterface, Kind: kind, Elements: made}
}

func element(name, kind string, populated, deprecated bool) contract.Element {
	return contract.Element{
		Name: name, Kind: contract.ElementField, Position: contract.PositionOutput,
		Type: kind, Populated: populated, Deprecated: deprecated,
	}
}

// TestTheFirstReleaseCreatesTheContractAndItsFirstVersion: a contract exists only
// from the merge that first published it, and the contract row and that release's
// first version are one write.
func TestTheFirstReleaseCreatesTheContractAndItsFirstVersion(t *testing.T) {
	ctx, pool := newStore(t)

	published := publish(t, ctx, pool, 1, form(contract.KindInterface,
		element("Status", "string", true, false),
		element("Detail", "string", false, false),
	))
	if !published.Created || !published.Moved {
		t.Fatalf("the first publication created=%v moved=%v, want both", published.Created, published.Moved)
	}
	if published.Version.Semver != contract.FirstVersion {
		t.Errorf("the first version is %s, want %s", published.Version.Semver, contract.FirstVersion)
	}
	if len(published.Change.Breaking) != 0 {
		t.Errorf("a contract's first form breaks %v, and there is no earlier build to break", published.Change.Breaking)
	}

	con, found, err := contract.ByName(ctx, pool, theService, theInterface)
	if err != nil || !found {
		t.Fatalf("ByName = found %v, %v", found, err)
	}
	if con.Kind != contract.KindInterface {
		t.Errorf("the contract's kind is %q", con.Kind)
	}
	read, err := contract.FormOf(ctx, pool, con, published.Version.ID)
	if err != nil {
		t.Fatalf("FormOf: %v", err)
	}
	if len(read.Elements) != 2 || read.Name != theInterface || read.Kind != contract.KindInterface {
		t.Fatalf("the form reads back as %+v", read)
	}
	status, _ := read.Element("Status")
	if !status.Populated || status.Type != "string" {
		t.Errorf("Status reads back as %+v", status)
	}

	// The release names the versions it publishes, and that is the inbound edge
	// rather than a column: the version names the release.
	versions, err := contract.VersionsForRelease(ctx, pool, published.Version.ReleaseID)
	if err != nil {
		t.Fatalf("VersionsForRelease: %v", err)
	}
	if len(versions) != 1 || versions[0].ID != published.Version.ID {
		t.Fatalf("the release publishes %d version(s), want the one it minted", len(versions))
	}
	if versions[0].ReleaseNumber != 1 {
		t.Errorf("the version carries release number %d, want the one the same write minted", versions[0].ReleaseNumber)
	}
}

// TestAReleaseWhoseFormIsUnchangedPublishesNoVersion: most releases publish no new
// contract version at all, which is what keeps the version tracking the form rather
// than the release.
func TestAReleaseWhoseFormIsUnchangedPublishesNoVersion(t *testing.T) {
	ctx, pool := newStore(t)

	one := form(contract.KindInterface, element("Status", "string", true, false))
	first := publish(t, ctx, pool, 1, one)
	second := publish(t, ctx, pool, 2, one)
	if second.Moved || second.Created {
		t.Fatalf("an unchanged form moved=%v created=%v", second.Moved, second.Created)
	}
	if second.Version.ID != first.Version.ID {
		t.Errorf("the version in force is %s, want the one below it %s", second.Version.ID, first.Version.ID)
	}
	versions, err := contract.VersionsOf(ctx, pool, first.Contract.ID)
	if err != nil {
		t.Fatalf("VersionsOf: %v", err)
	}
	if len(versions) != 1 {
		t.Errorf("the contract has %d versions after two releases, want the one the form moved at", len(versions))
	}
}

// TestTheVersionMovesMajorOnABreakAndMinorOtherwise, and the version in force at a
// release is the newest minted at or below it.
func TestTheVersionMovesMajorOnABreakAndMinorOtherwise(t *testing.T) {
	ctx, pool := newStore(t)

	publish(t, ctx, pool, 1, form(contract.KindInterface,
		element("Status", "string", true, false),
		element("Detail", "string", false, false),
	))
	// An addition and a mark: a minor, and a consumer sees a bump for an
	// annotation nothing breaks on.
	minor := publish(t, ctx, pool, 2, form(contract.KindInterface,
		element("Status", "string", true, false),
		element("Detail", "string", false, true),
		element("DetailText", "string", false, false),
	))
	if minor.Version.Semver != (contract.Semver{Major: 1, Minor: 1}) {
		t.Fatalf("an addition and a mark minted %s, want 1.1.0", minor.Version.Semver)
	}
	// A removal: a major.
	major := publish(t, ctx, pool, 3, form(contract.KindInterface,
		element("Status", "string", true, false),
		element("DetailText", "string", false, false),
	))
	if major.Version.Semver != (contract.Semver{Major: 2}) {
		t.Fatalf("a removal minted %s, want 2.0.0", major.Version.Semver)
	}
	if len(major.Change.Breaking) != 1 || major.Change.Breaking[0] != "Detail" {
		t.Errorf("the removal breaks %v, want Detail", major.Change.Breaking)
	}

	// The version in force at a release is the newest at or below its number,
	// which is what a producer's own diff against what is running reads.
	for _, at := range []struct {
		number int64
		want   contract.Semver
	}{{1, contract.FirstVersion}, {2, contract.Semver{Major: 1, Minor: 1}},
		{3, contract.Semver{Major: 2}}, {9, contract.Semver{Major: 2}}} {
		v, found, err := contract.VersionAt(ctx, pool, major.Contract.ID, at.number)
		if err != nil || !found {
			t.Fatalf("VersionAt(%d) = found %v, %v", at.number, found, err)
		}
		if v.Semver != at.want {
			t.Errorf("the version in force at release %d is %s, want %s", at.number, v.Semver, at.want)
		}
	}
	if _, found, err := contract.VersionAt(ctx, pool, major.Contract.ID, 0); err != nil || found {
		t.Errorf("a release below the contract's first has a version: found %v, %v", found, err)
	}
}

// TestTheKindDoesNotChangeBetweenVersions: two versions disagreeing about whether
// the thing is a store would enforce two promises on one interface, which is the
// whole reason a contract is a record.
func TestTheKindDoesNotChangeBetweenVersions(t *testing.T) {
	ctx, pool := newStore(t)

	publish(t, ctx, pool, 1, form(contract.KindInterface, element("Status", "string", true, false)))

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = contract.Publish(ctx, tx, queue, contract.Publication{
		ServiceID: theService, ReleaseID: record.NewID("rel"), ReleaseNumber: 2,
		ItemID: record.NewID("it"),
		Form:   form(contract.KindStore, element("Status", "string", true, false)),
	})
	if !errors.Is(err, contract.ErrKindChanged) {
		t.Fatalf("publishing the same name as a store = %v, want ErrKindChanged", err)
	}
}

// TestOneReleasePublishesOneVersionOfAContract: the store refuses a second, which
// is what stops one merge minting two versions of one contract.
func TestOneReleasePublishesOneVersionOfAContract(t *testing.T) {
	ctx, pool := newStore(t)

	first := publish(t, ctx, pool, 1, form(contract.KindInterface, element("Status", "string", true, false)))

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	_, err = contract.Publish(ctx, tx, queue, contract.Publication{
		ServiceID: theService, ReleaseID: first.Version.ReleaseID, ReleaseNumber: 1,
		ItemID: record.NewID("it"),
		Form:   form(contract.KindInterface, element("Other", "string", false, false)),
	})
	if err == nil || !strings.Contains(err.Error(), "one_version_per_release") {
		t.Fatalf("a second version under one release = %v, want the store's refusal", err)
	}
}

// TestAPublicationMissingSomethingIsRefused: every publication names a service, a
// release with a number, and a form that validates. The item is not among them —
// TestAVersionIsMintedForAReleaseNamingNoItem is that rule.
func TestAPublicationMissingSomethingIsRefused(t *testing.T) {
	ctx, pool := newStore(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	good := contract.Publication{
		ServiceID: theService, ReleaseID: "rel_a", ReleaseNumber: 1, ItemID: "it_a",
		Form: form(contract.KindInterface, element("Status", "string", true, false)),
	}
	for name, broken := range map[string]func(contract.Publication) contract.Publication{
		"no service":      func(p contract.Publication) contract.Publication { p.ServiceID = ""; return p },
		"no release":      func(p contract.Publication) contract.Publication { p.ReleaseID = ""; return p },
		"no number":       func(p contract.Publication) contract.Publication { p.ReleaseNumber = 0; return p },
		"no form name":    func(p contract.Publication) contract.Publication { p.Form.Name = ""; return p },
		"an unknown kind": func(p contract.Publication) contract.Publication { p.Form.Kind = "queue"; return p },
	} {
		if _, err := contract.Publish(ctx, tx, queue, broken(good)); err == nil {
			t.Errorf("publishing with %s was accepted", name)
		}
	}
	if _, err := contract.Publish(ctx, tx, record.Actor{}, good); !errors.Is(err, record.ErrKindUnknown) {
		t.Errorf("publishing with no actor = %v, want ErrKindUnknown", err)
	}
}

// TestPublishAllIsEveryFormOneReleaseDeclares: a release publishes as many
// contracts as its build declares, each diffed against its own version in force.
func TestPublishAllIsEveryFormOneReleaseDeclares(t *testing.T) {
	ctx, pool := newStore(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	published, err := contract.PublishAll(ctx, tx, queue, theService, "rel_a", 1, "it_a", []contract.Form{
		{Name: "health", Kind: contract.KindInterface, Elements: []contract.Element{element("Status", "string", true, false)}},
		{Name: "ledger", Kind: contract.KindStore, Elements: []contract.Element{element("ID", "string", true, false)}},
	})
	if err != nil {
		t.Fatalf("PublishAll: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(published) != 2 {
		t.Fatalf("PublishAll wrote %d contracts, want two", len(published))
	}
	all, err := contract.OfService(ctx, pool, theService)
	if err != nil {
		t.Fatalf("OfService: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("the service publishes %d contracts", len(all))
	}
	kinds := map[string]contract.Kind{}
	for _, con := range all {
		kinds[con.Name] = con.Kind
	}
	if kinds["health"] != contract.KindInterface || kinds["ledger"] != contract.KindStore {
		t.Errorf("the kinds read back as %v", kinds)
	}
}

// TestNothingIsWrittenWhereTheTransactionRollsBack: the publish runs inside the
// mint's transaction, so a failure after it leaves neither the number nor the
// version — there is no state where a number stands and its versions are missing.
func TestNothingIsWrittenWhereTheTransactionRollsBack(t *testing.T) {
	ctx, pool := newStore(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if _, err := contract.Publish(ctx, tx, queue, contract.Publication{
		ServiceID: theService, ReleaseID: "rel_a", ReleaseNumber: 1, ItemID: "it_a",
		Form: form(contract.KindInterface, element("Status", "string", true, false)),
	}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if _, found, err := contract.ByName(ctx, pool, theService, theInterface); err != nil || found {
		t.Fatalf("a rolled-back publication left a contract: found %v, %v", found, err)
	}
}

// TestAVersionIsMintedForAReleaseNamingNoItem: the queue mints a release over an
// accepted commit naming the build and no item, and writes the contract versions
// its interfaces publish in the same write, as it does at a fast-forward. The
// release is the key there and the item is what a version has where a gate decided
// one.
func TestAVersionIsMintedForAReleaseNamingNoItem(t *testing.T) {
	ctx, pool := newStore(t)

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	published, err := contract.Publish(ctx, tx, queue, contract.Publication{
		ServiceID: theService, ReleaseID: "rel_accepted", ReleaseNumber: 1,
		Form: form(contract.KindInterface, element("Status", "string", true, false)),
	})
	if err != nil {
		t.Fatalf("publishing for a release naming no item: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if published.Version.ItemID != "" {
		t.Errorf("the version names item %q, want none", published.Version.ItemID)
	}
	versions, err := contract.VersionsForRelease(ctx, pool, "rel_accepted")
	if err != nil || len(versions) != 1 {
		t.Fatalf("VersionsForRelease = %d version(s), %v", len(versions), err)
	}
	if versions[0].ItemID != "" {
		t.Errorf("the version reads back naming item %q", versions[0].ItemID)
	}
}

// TestAnElementReadsBackWithEverythingTheDiffReads: the position, the kind, what
// it accepts, and the two store constraints all survive the round trip, because a
// diff run over a form read out of the store has to be the diff run over the form
// that was derived.
func TestAnElementReadsBackWithEverythingTheDiffReads(t *testing.T) {
	ctx, pool := newStore(t)

	published := publish(t, ctx, pool, 1, contract.Form{
		Name: theInterface, Kind: contract.KindStore,
		Elements: []contract.Element{{
			Name: "Ledger.Status", Kind: contract.ElementField, Position: contract.PositionStore,
			Type: "string", Required: true, Populated: true,
			Domain: []string{"ok", "error"}, NotNull: true, Unique: true,
		}, {
			Name: "Ledger.Amount", Kind: contract.ElementField, Position: contract.PositionStore,
			Type: "int64", Range: &contract.Range{Low: 0, High: 100},
		}},
	})
	read, err := contract.FormOf(ctx, pool, published.Contract, published.Version.ID)
	if err != nil {
		t.Fatalf("FormOf: %v", err)
	}
	status, _ := read.Element("Ledger.Status")
	if status.Kind != contract.ElementField || status.Position != contract.PositionStore {
		t.Errorf("Ledger.Status reads back as a %q in position %q", status.Kind, status.Position)
	}
	if !status.Required || !status.NotNull || !status.Unique {
		t.Errorf("Ledger.Status reads back as %+v, want required, not null and unique", status)
	}
	if len(status.Domain) != 2 || status.Domain[0] != "ok" {
		t.Errorf("the domain reads back as %v", status.Domain)
	}
	amount, _ := read.Element("Ledger.Amount")
	if amount.Range == nil || *amount.Range != (contract.Range{Low: 0, High: 100}) {
		t.Errorf("the range reads back as %v", amount.Range)
	}
	if status.Range != nil {
		t.Errorf("an element that accepts any number reads back with the range %v", status.Range)
	}
	// The form read back is the form that was written, which is what makes a
	// diff against the store the same diff as one against the derivation.
	if contract.Diff(read, read).Moved() {
		t.Error("a form diffed against itself moved")
	}
}
