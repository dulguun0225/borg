// The extractor: what one Go checkout declares, which producer each predicate is
// over, and what it records about itself. It reaches no database. Deciding one
// predicate is decide_test.go and the records are db_test.go.
package consumercontract_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/consumercontract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// allowed is the list of allowed predicate kinds in force in these tests: the
// nine kinds the factory owns, which is the unauthored value and what an owner
// extends.
var allowed = gatepolicy.AllowedPredicateKindNames()

// extractor is this factory's Go extractor at some factory version, which is what
// every record it writes names.
var extractor = consumercontract.GoExtractor("test")

func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

// mirror is the consumer's mirror of the producer's health interface, written the
// way the producer's own contract file is written.
const mirror = `package main

// Health is what the producer returns.
type Health struct {
	Status         string ` + "`borg:\"populated,domain=ok|error\"`" + `
	SendTimeMillis int64  ` + "`borg:\"unit=millis\"`" + `
	Detail         string
	Unread         string ` + "`borg:\"populated\"`" + `
}

// Ask is what the consumer sends.
type Ask struct {
	Deep   bool
	Reason string ` + "`borg:\"domain=slow|error\"`" + `
}

// Fetch is the operation.
func Fetch(ask Ask) Health { return Health{} }
`

const uses = `package main

import "fmt"

func report() {
	ask := Ask{Reason: "slow"}
	h := Fetch(ask)
	fmt.Println(h.Status, h.SendTimeMillis, h.Detail)
}

func main() { report() }
`

// derived is what one checkout declares, keyed element/kind, with the failure
// reported where the extraction could not run.
func derived(t *testing.T, dir string) (consumercontract.Derived, map[string]string) {
	t.Helper()
	d, err := consumercontract.Derive(dir, allowed, extractor)
	if err != nil {
		t.Fatalf("deriving: %v", err)
	}
	got := map[string]string{}
	for _, draft := range d.Drafts {
		got[draft.Element+"/"+string(draft.Kind)] = draft.Argument
	}
	return d, got
}

func TestDeriveDeclaresBothSidesOfWhatTheCodeDoes(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\n",
		consumercontract.FileName("health"): mirror,
		"main.go":                           uses,
	})
	d, got := derived(t, dir)
	if d.CouldNotDerive() {
		t.Fatalf("the derivation could not run: %s", d.Describe())
	}
	for _, draft := range d.Drafts {
		if draft.ProducerService != "producer" || draft.Interface != "health" || draft.Address != "health" {
			t.Fatalf("a draft names %s.%s through %q, want producer.health through the entry",
				draft.ProducerService, draft.Interface, draft.Address)
		}
	}
	for _, want := range []string{
		// What it receives.
		"Health.Status/read", "Health.Status/populated", "Health.Status/domain",
		"Health.SendTimeMillis/read", "Health.SendTimeMillis/unit", "Health.Detail/read",
		// What it sends.
		"Fetch/called", "Ask.Reason/sent", "Ask.Reason/sent_domain", "Ask.Deep/sent",
	} {
		if _, found := got[want]; !found {
			t.Fatalf("%s was not derived; derived %v", want, got)
		}
	}
	if got["Health.Status/domain"] != "ok|error" {
		t.Fatalf("the domain of Health.Status is %q", got["Health.Status/domain"])
	}
	if got["Health.SendTimeMillis/unit"] != "millis" {
		t.Fatalf("the unit of Health.SendTimeMillis is %q", got["Health.SendTimeMillis/unit"])
	}
	if got["Ask.Reason/sent"] != consumercontract.Sent {
		t.Errorf("Ask.Reason is %q, and the code writes it", got["Ask.Reason/sent"])
	}
	// An element the consumer does not write is one it leaves out, which is what
	// a producer breaks by making the element required.
	if got["Ask.Deep/sent"] != consumercontract.LeftOut {
		t.Errorf("Ask.Deep is %q, and nothing in the consumer writes it", got["Ask.Deep/sent"])
	}
	// The field nothing reads declares nothing, however many tags it carries.
	// That is what makes a consumer which stops reading an element stop declaring
	// it with nobody remembering to.
	for key := range got {
		if strings.HasPrefix(key, "Health.Unread") {
			t.Fatalf("%s was derived, and nothing in the consumer reads that field", key)
		}
	}
}

func TestAStoreConsumerDeclaresItsWritesAsWellAsItsReads(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "ledger self ledger store\n",
		consumercontract.FileName("ledger"): "package main\n\ntype Ledger struct {\n\tID string `borg:\"populated\"`\n\tAmount int64 `borg:\"range=0..100\"`\n}\n",
		"main.go": `package main

import "fmt"

func main() {
	row := Ledger{Amount: 3}
	row.ID = "a"
	fmt.Println(row.ID)
}
`,
	})
	d, got := derived(t, dir)
	if d.CouldNotDerive() {
		t.Fatalf("the derivation could not run: %s", d.Describe())
	}
	for _, want := range []string{"Ledger.ID/sent", "Ledger.Amount/sent", "Ledger.Amount/sent_range", "Ledger.ID/read"} {
		if _, found := got[want]; !found {
			t.Fatalf("%s was not derived; a store's consumer writes as well as reads. derived %v", want, got)
		}
	}
	if got["Ledger.Amount/sent_range"] != "0..100" {
		t.Errorf("the range written is %q", got["Ledger.Amount/sent_range"])
	}
}

func TestWhichProducerIsTheEntryPairedWithTheCallSite(t *testing.T) {
	for name, of := range map[string]struct {
		files      map[string]string
		couldNot   bool
		predicates bool
	}{
		"an address in no entry": {files: map[string]string{
			"consumes.txt":                      "other producer health\n",
			consumercontract.FileName("health"): mirror, "main.go": uses,
		}, couldNot: true},
		"no configuration file at all": {files: map[string]string{
			consumercontract.FileName("health"): mirror, "main.go": uses,
		}, couldNot: true},
		"an entry naming no service": {files: map[string]string{
			"consumes.txt":                      "health\n",
			consumercontract.FileName("health"): mirror, "main.go": uses,
		}, couldNot: true},
		"an address outside the factory": {files: map[string]string{
			"consumes.txt":                      "health outside\n",
			consumercontract.FileName("health"): mirror, "main.go": uses,
		}},
	} {
		t.Run(name, func(t *testing.T) {
			d, got := derived(t, checkout(t, of.files))
			if d.CouldNotDerive() != of.couldNot {
				t.Fatalf("%s derived %s, want could not derive = %v", name, d.Describe(), of.couldNot)
			}
			if d.CouldNotDerive() {
				if d.Cause != consumercontract.CauseExtractionFailed || d.Reported == "" {
					t.Fatalf("the cause is %q reporting %q, and the reader at the gate has to know which",
						d.Cause, d.Reported)
				}
				return
			}
			if len(got) != 0 && !of.predicates {
				t.Fatalf("%s derived %v, and a call through an address outside the factory is covered by nothing",
					name, got)
			}
		})
	}
}

func TestAServiceThatConsumesNothingDerivesCompletely(t *testing.T) {
	d, got := derived(t, checkout(t, map[string]string{"main.go": "package main\n\nfunc main() {}\n"}))
	if d.CouldNotDerive() || d.Partial() || len(got) != 0 {
		t.Fatalf("a service with no mirror derived %s with %v — that is an empty list and not a could not derive",
			d.Describe(), got)
	}
}

func TestAConstructTheExtractorCannotFollowMakesTheRecordPartial(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\n",
		consumercontract.FileName("health"): mirror,
		"main.go": `package main

import "reflect"

func main() {
	by := map[string]string{"Status": "ok"}
	_ = by["Status"]
	_ = reflect.TypeOf(by)
}
`,
	})
	d, got := derived(t, dir)
	if !d.Partial() {
		t.Fatalf("the record is complete and the extractor met %d construct(s) it could not follow", len(d.Unfollowed))
	}
	if len(d.Unfollowed) != 2 {
		t.Fatalf("the unfollowed constructs are %v, want the reflection and the string-keyed access", d.Unfollowed)
	}
	// The read through a map is one of the two blind cases, and what it costs is
	// an unprotected assumption — which is what the record now says out loud.
	for key := range got {
		if strings.HasPrefix(key, "Health.Status/") {
			t.Fatalf("%s was derived, and the only read of it is a map key", key)
		}
	}
}

func TestAMirrorTheExtractorCannotReadIsCouldNotDerive(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\n",
		consumercontract.FileName("health"): "package main\n\ntype A struct{",
	})
	d, _ := derived(t, dir)
	if d.Cause != consumercontract.CauseExtractionFailed || d.Reported == "" {
		t.Fatalf("a mirror that does not parse derived %s, want could not derive naming what the extractor reported",
			d.Describe())
	}
}

func TestDeriveRefusesAKindOutsideTheListInForce(t *testing.T) {
	dir := checkout(t, map[string]string{
		"consumes.txt":                      "health producer health\n",
		consumercontract.FileName("health"): mirror,
		"main.go":                           uses,
	})
	// A list an owner narrowed to the read predicate alone is not a state gate
	// policy can reach — a safeguard may only extend it — but it is what this
	// check is about: a consumer picks from the list and cannot invent a kind.
	_, err := consumercontract.Derive(dir, []string{string(gatepolicy.PredicateRead)}, extractor)
	if !errors.Is(err, consumercontract.ErrNotAnAllowedPredicateKind) {
		t.Fatalf("deriving against a list of one kind returned %v, want ErrNotAnAllowedPredicateKind", err)
	}
}

// TestWhichToolchainsHaveAnExtractorIsPublished: which toolchains have an
// extractor is a fact of the factory's version and is readable before a service
// is adopted, rather than discovered at that service's first removal. A
// toolchain nothing covers is the could-not-derive cause that lifts when an
// extractor ships.
func TestWhichToolchainsHaveAnExtractorIsPublished(t *testing.T) {
	shipped := consumercontract.Extractors("test")
	if len(shipped) == 0 {
		t.Fatal("this factory version publishes no extractor at all")
	}
	for _, e := range shipped {
		if e.Toolchain == "" || e.Name == "" || e.Version == "" || e.FactoryVersion != "test" {
			t.Errorf("the published extractor %+v does not name a toolchain, a name, a version and the factory version", e)
		}
	}

	covered, found := consumercontract.ExtractorFor(consumercontract.Toolchain, "test")
	if !found || covered != consumercontract.GoExtractor("test") {
		t.Errorf("ExtractorFor(%q) = %+v, %v; want this factory's Go extractor",
			consumercontract.Toolchain, covered, found)
	}
	if _, found := consumercontract.ExtractorFor("rust", "test"); found {
		t.Error("a toolchain no extractor covers reads as covered")
	}
}
