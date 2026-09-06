package securitypredicate

import (
	"fmt"
	"os"
	"strings"
)

// Checkout is what a derivation runs over: the build's own checkout on disk. It
// is the same thing the exposure extractor and the consumer contract extractor
// are given, because a security predicate is decided against the build too.
type Checkout struct {
	Dir string
}

// Result is one kind of the list decided against one build.
type Result struct {
	Kind Kind
	// Held is whether the build satisfies it. A kind nothing decided is not here
	// at all: what an underived kind produces is [Decided.CouldNotDerive].
	Held bool
	// Why is what a human at the merge row reads, on a kind that did not hold.
	Why string
}

// Decided is what one toolchain's derivations made of one build: the list they
// were run from, what each kind decided, and why none could be decided at all.
//
// Nothing writes this. It is computed at the row that reads it, the way the two
// contract baselines are, and the gate stores what it came to on the open and
// close events.
type Decided struct {
	List    List
	Results []Result
	// CouldNotDerive is why no result could be produced: the factory ships no
	// list for the build's toolchain, the derivation could not read the
	// checkout, or a kind in the list is one this toolchain's derivations do not
	// cover. It is empty where every kind was decided, and a list with no kind
	// leaves it empty too — nothing was left underived.
	CouldNotDerive string
}

// CouldNotBeDerived reports whether nothing could be decided, which puts a human
// at the merge row rather than rejecting: a derivation that could not derive
// says that nothing decided, which is what an undecided criterion already says.
func (d Decided) CouldNotBeDerived() bool { return d.CouldNotDerive != "" }

// Rejected is every kind the build does not satisfy, which is what rejects at
// the merge row on the terms an undecided criterion does.
func (d Decided) Rejected() []Result {
	var rejected []Result
	for _, result := range d.Results {
		if !result.Held {
			rejected = append(rejected, result)
		}
	}
	return rejected
}

// Why is what the rejection says, in the words a human reads on the close event.
func (d Decided) Why() string {
	var said []string
	for _, result := range d.Rejected() {
		said = append(said, fmt.Sprintf("the build does not satisfy %s: %s", result.Kind, result.Why))
	}
	return strings.Join(said, "; ")
}

// Decide runs one derivation per kind of the list over the checkout. It is the
// factory's own list decided against the build, per toolchain, and it decides
// rather than rejects: what the caller does with a kind that did not hold is
// hand it to the gate, which is the one thing that closes a firing.
//
// A list for no toolchain, a checkout no derivation can read, and a kind the
// toolchain's derivations do not cover are one outcome and not three: could not
// derive, which puts a human at the row. A kind decided against nothing that
// passed would say the build satisfies something nobody read.
func Decide(list List, checkout Checkout) Decided {
	decided := Decided{List: list}
	if list.Toolchain == "" {
		decided.CouldNotDerive = "the factory ships no security-predicate list for this build's toolchain"
		return decided
	}
	if len(list.Kinds) == 0 {
		return decided
	}
	if err := readable(checkout.Dir); err != nil {
		decided.CouldNotDerive = fmt.Sprintf("the %s derivations could not read the checkout: %v", list.Toolchain, err)
		return decided
	}
	for _, kind := range list.Kinds {
		result, covered := decide(list.Toolchain, kind, checkout)
		if !covered {
			decided.Results, decided.CouldNotDerive = nil, fmt.Sprintf(
				"the %s derivations cover no predicate %s, which the list this factory version ships names",
				list.Toolchain, kind)
			return decided
		}
		decided.Results = append(decided.Results, result)
	}
	return decided
}

// decide is one kind against one build, through the derivation set of the
// toolchain the list is for, and false where that set does not cover the kind.
func decide(toolchain string, kind Kind, checkout Checkout) (Result, bool) {
	if toolchain == ToolchainGo {
		return decideGo(kind, checkout)
	}
	return Result{Kind: kind}, false
}

// decideGo is the Go toolchain's derivation set: what deciding one kind against
// a Go checkout produces, and false for a kind it does not cover.
//
// It covers no kind, because [Go] ships none. A kind added to that list is
// decided here in the same commit, and one added without a derivation here reads
// as could not derive rather than as satisfied.
func decideGo(kind Kind, _ Checkout) (Result, bool) {
	return Result{Kind: kind}, false
}

// readable is whether a derivation can read the checkout at all, which is the
// one thing every derivation needs before any of them runs.
func readable(dir string) error {
	if dir == "" {
		return fmt.Errorf("the build names no checkout")
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	return nil
}
