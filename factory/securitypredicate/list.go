package securitypredicate

// ToolchainGo is the one toolchain this factory version ships a list for, which
// is the toolchain its extractors already read.
const ToolchainGo = "go"

// Kind is one security predicate: what the factory's own list asserts about a
// build. A consumer picks its predicates from the list of allowed predicate
// kinds gate policy owns; the factory picks these from this list, which it owns
// itself and an owner may only extend.
type Kind string

// List is the security-predicate list for one toolchain: the kinds in it, the
// toolchain whose derivations decide them, and the version of the factory that
// shipped them. The version is here because the list is content of the product
// and moves with a release of it, the way an extractor does, so a record naming
// only what was decided would be silent about what decided it.
type List struct {
	Toolchain string
	// FactoryVersion is which version of the factory shipped these kinds. The
	// install event, which is what versions shipped content, is not built; this
	// is the version the binary carries.
	FactoryVersion string
	// Kinds is what the list holds. An owner may only extend it, and nothing
	// authors an extension yet.
	Kinds []Kind
}

// Lists is every list this factory version ships, one per toolchain it covers.
// The list is shipped content, so which toolchains have one is a fact of the
// factory's version and is published rather than discovered at a merge row.
//
// A second toolchain is a second file in this package and a second line here.
func Lists(factoryVersion string) []List {
	return []List{Go(factoryVersion)}
}

// Go is the list this factory version ships for the Go toolchain.
//
// It holds no kind. What a security predicate asserts is not stated anywhere
// this package implements, so shipping one would be this package deciding the
// product's content; a kind added here is added to [decideGo] in the same
// commit, and until one is, this list decides nothing and leaves nothing
// underived.
func Go(factoryVersion string) List {
	return List{Toolchain: ToolchainGo, FactoryVersion: factoryVersion}
}

// ForToolchain is the list this factory version ships for one toolchain, and the
// zero list with false where it ships none — which [Decide] reads as could not
// derive, the outcome a toolchain whose derivation cannot run has.
func ForToolchain(toolchain, factoryVersion string) (List, bool) {
	for _, list := range Lists(factoryVersion) {
		if list.Toolchain == toolchain {
			return list, true
		}
	}
	return List{}, false
}
