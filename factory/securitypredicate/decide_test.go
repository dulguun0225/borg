// The factory's own list of security predicates and what deciding it against a
// build produces. Nothing here needs a database: the list is content of the
// product and a derivation reads a checkout.
package securitypredicate_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/securitypredicate"
)

// TestTheListIsPublishedAsAFactOfTheFactoryVersion: which toolchains have a
// security-predicate list is a fact of the version that shipped it, readable
// before a service is adopted rather than at its first merge row.
func TestTheListIsPublishedAsAFactOfTheFactoryVersion(t *testing.T) {
	lists := securitypredicate.Lists("v-test")
	if len(lists) != 1 || lists[0].Toolchain != securitypredicate.ToolchainGo {
		t.Fatalf("this version ships %+v, want one list for %s", lists, securitypredicate.ToolchainGo)
	}
	if lists[0].FactoryVersion != "v-test" {
		t.Errorf("the list names factory version %q, want the one that shipped it", lists[0].FactoryVersion)
	}
	if len(lists[0].Kinds) != 0 {
		t.Errorf("the Go list ships %v; the design names no security predicate, so shipping one is not this package's to decide",
			lists[0].Kinds)
	}

	if _, covered := securitypredicate.ForToolchain(securitypredicate.ToolchainGo, "v-test"); !covered {
		t.Error("ForToolchain does not find the list this version ships for Go")
	}
	list, covered := securitypredicate.ForToolchain("rust", "v-test")
	if covered || list.Toolchain != "" {
		t.Errorf("ForToolchain over an uncovered toolchain = %+v, %v; want the zero list", list, covered)
	}
}

// TestAToolchainWithNoListReadsAsCouldNotDerive, which is what puts a human at
// the merge row instead of a build passing a list nobody ran.
func TestAToolchainWithNoListReadsAsCouldNotDerive(t *testing.T) {
	list, _ := securitypredicate.ForToolchain("rust", "v-test")
	decided := securitypredicate.Decide(list, securitypredicate.Checkout{Dir: t.TempDir()})
	if !decided.CouldNotBeDerived() {
		t.Fatalf("a build whose toolchain has no list decided %+v, want could not derive", decided)
	}
	if len(decided.Rejected()) != 0 {
		t.Errorf("it rejected %+v as well, and a derivation that could not run rejects nothing", decided.Rejected())
	}
}

// TestAShippedListWithNoKindDecidesNothingAndLeavesNothingUnderived: an empty
// list is not a derivation that failed, so it puts no human at the row and
// rejects nobody.
func TestAShippedListWithNoKindDecidesNothingAndLeavesNothingUnderived(t *testing.T) {
	decided := securitypredicate.Decide(securitypredicate.Go("v-test"), securitypredicate.Checkout{Dir: t.TempDir()})
	if decided.CouldNotBeDerived() {
		t.Errorf("the shipped list could not be derived: %s", decided.CouldNotDerive)
	}
	if len(decided.Results) != 0 || len(decided.Rejected()) != 0 {
		t.Errorf("the shipped list decided %+v over a build it holds no predicate about", decided.Results)
	}
}

// TestAKindNoDerivationCoversReadsAsCouldNotDerive: a kind added to the list
// without a derivation for it says that nothing decided, never that the build
// satisfies it.
func TestAKindNoDerivationCoversReadsAsCouldNotDerive(t *testing.T) {
	extended := securitypredicate.Go("v-test")
	extended.Kinds = append(extended.Kinds, "a kind no derivation covers")

	decided := securitypredicate.Decide(extended, securitypredicate.Checkout{Dir: t.TempDir()})
	if !decided.CouldNotBeDerived() {
		t.Fatalf("a kind no derivation covers decided %+v, want could not derive", decided)
	}
	if !strings.Contains(decided.CouldNotDerive, "a kind no derivation covers") {
		t.Errorf("the reason does not name the kind: %s", decided.CouldNotDerive)
	}
	if len(decided.Results) != 0 {
		t.Errorf("it produced results as well: %+v", decided.Results)
	}
}

// TestACheckoutNoDerivationCanReadIsCouldNotDeriveAndNotAPass.
func TestACheckoutNoDerivationCanReadIsCouldNotDeriveAndNotAPass(t *testing.T) {
	extended := securitypredicate.Go("v-test")
	extended.Kinds = append(extended.Kinds, "a kind no derivation covers")

	missing := filepath.Join(t.TempDir(), "no-such-checkout")
	decided := securitypredicate.Decide(extended, securitypredicate.Checkout{Dir: missing})
	if !decided.CouldNotBeDerived() || !strings.Contains(decided.CouldNotDerive, "could not read the checkout") {
		t.Fatalf("a checkout that is not there decided %+v, want could not derive", decided)
	}

	file := filepath.Join(t.TempDir(), "a-file")
	if err := os.WriteFile(file, []byte("not a checkout"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	decided = securitypredicate.Decide(extended, securitypredicate.Checkout{Dir: file})
	if !decided.CouldNotBeDerived() {
		t.Fatalf("a checkout that is a file decided %+v, want could not derive", decided)
	}
}
