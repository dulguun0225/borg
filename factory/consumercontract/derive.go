package consumercontract

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/gatepolicy"
)

// The derivation. A consumer's assumptions are read out of its build and never
// entered by hand, and how much of a consumer's reading is visible is a property
// of that interface's toolchain rather than of the factory — so this file is Go's
// extractor, and a second toolchain ships a second one rather than extending this.
//
// The convention is one file name at the root of the repository:
//
//	consume.<address>.go
//
// where <address> is an entry of the configuration file address.go reads. The
// mirror is written the way the producer's own contract file is written — the same
// messages, fields, operations and tags — so its form is derived through
// [contract.DeriveFile] and there is one convention rather than two.
//
// What is derived is what the consumer's own source does with the mirror:
//
//   - a field of a message the interface returns, or of a store, that the source
//     reads declares that it is read, and what the mirror's tags say about it:
//     that it arrives populated, that its name carries a unit, that its values
//     stay inside a domain or a range;
//   - a field of a message the interface accepts that the source writes declares
//     that it is sent, and the domain or range of what it sends; one the source
//     does not write declares that it is left out, which is what a producer
//     breaks by making it required;
//   - an operation the source calls declares that it is called at all.
//
// A field the mirror holds and the source never touches declares nothing. That is
// what makes a consumer which stops reading an element stop declaring it, with
// nobody remembering to, and it is the whole mechanism the deprecation list rests
// on.
//
// Both of the design's blind cases are real here and neither is silent. A read
// this misses is one made through reflection, through a map, or through a name the
// parse cannot see as a selector; the two it can see itself it records as
// constructs it could not follow, which makes the record partial. A read it invents
// is a field name the mirror shares with some other type in the consumer's own
// code, since the resolution is syntactic and nothing here type-checks — which is
// what withdrawing a safeguard, or the producer's blocked removal item asking the
// consumer to confirm, is for.

// consumePrefix is the file-name prefix of a mirror.
const consumePrefix = "consume."

// tagUnit is the one tag word this extractor reads that a form does not carry:
// the unit belongs to an element's name, so a form has no field for one and a
// consumer asserting a unit says which it expects.
const tagUnit = "unit"

// Toolchain and ExtractorName are what this extractor is, and ExtractorVersion is
// which one it is. The version moves when what this file derives changes, because
// an upgrade that ships a changed extractor derives again for every release in
// force on the toolchain and that is the fact it compares.
const (
	Toolchain        = "go"
	ExtractorName    = "go/ast"
	ExtractorVersion = "1"
)

// GoExtractor is this extractor as a record names one. The factory version is the
// caller's: an extractor ships with the factory, so a derivation is a function of
// the code and of the factory version.
func GoExtractor(factoryVersion string) Extractor {
	return Extractor{
		Name: ExtractorName, Version: ExtractorVersion,
		Toolchain: Toolchain, FactoryVersion: factoryVersion,
	}
}

// ErrNotAnAllowedPredicateKind is returned for an assertion whose kind is not in
// the list of allowed predicate kinds in force. A consumer picks from the list and
// cannot invent a kind of assertion at consumer contract time, and this is that
// rule at the derivation.
var ErrNotAnAllowedPredicateKind = errors.New("consumercontract: that kind of assertion is not in the list of allowed predicate kinds in force")

// FileName is the file one mirror is written in, which is what an agent is told to
// write and what a test writes directly.
func FileName(address string) string { return consumePrefix + address + ".go" }

// Derive is what this extractor makes of the checkout at root: the predicates it
// found, the constructs it could not follow, or the cause it could not derive at
// all. allowed is the list of allowed predicate kinds in force, and an assertion
// outside it is [ErrNotAnAllowedPredicateKind] — the one thing here that is the
// build's fault rather than the extractor's.
//
// A checkout with no mirror file declares nothing and derives completely, which is
// every service that consumes nothing. A mirror whose address the configuration
// file does not hold, a configuration file that is missing while a mirror names an
// address, and a mirror this extractor cannot read are all could not derive: a
// record, not an empty list, because "no consumer reads this" and "no consumer's
// read was visible" call for opposite responses.
//
// Only the root directory is read, which is the same limit contract's derivation
// has and for the same reason.
func Derive(root string, allowed []string, extractor Extractor) (Derived, error) {
	derived := Derived{Extractor: extractor}
	entries, err := os.ReadDir(root)
	if err != nil {
		return failed(extractor, fmt.Sprintf("reading the checkout at %s: %v", root, err)), nil
	}
	var mirrors []string
	for _, entry := range entries {
		if !entry.IsDir() {
			if address, ok := named(entry.Name()); ok {
				mirrors = append(mirrors, address)
			}
		}
	}
	slices.Sort(mirrors)
	if len(mirrors) == 0 {
		return derived, nil
	}

	addresses, found, err := Entries(root)
	if err != nil {
		return failed(extractor, err.Error()), nil
	}
	if !found {
		return failed(extractor, fmt.Sprintf("%d mirror(s) name an address and the checkout holds no %s",
			len(mirrors), ConfigurationFile)), nil
	}

	source, err := readSource(root)
	if err != nil {
		return failed(extractor, err.Error()), nil
	}
	derived.Unfollowed = source.unfollowed

	for _, address := range mirrors {
		entry, held := addresses[address]
		if !held {
			return failed(extractor, fmt.Sprintf("the address %s is in no entry of %s", address, ConfigurationFile)), nil
		}
		if entry.Outside {
			// A call through an address outside the factory is covered by
			// nothing, which is what the design says of such a call.
			continue
		}
		kind := contract.KindInterface
		if entry.Store {
			kind = contract.KindStore
		}
		path := filepath.Join(root, FileName(address))
		form, err := contract.DeriveFile(path, kind, entry.Interface)
		if err != nil {
			return failed(extractor, err.Error()), nil
		}
		units, err := mirrorUnits(path)
		if err != nil {
			return failed(extractor, err.Error()), nil
		}
		drafts, err := declared(entry, form, units, source, allowed)
		if err != nil {
			return Derived{}, err
		}
		derived.Drafts = append(derived.Drafts, drafts...)
	}
	return derived, nil
}

// failed is a could-not-derive record for an extraction that ran and failed, with
// what the extractor reported. The other cause — no extractor for the toolchain —
// is the caller's: this file is an extractor, so it cannot be the one that is
// missing.
func failed(extractor Extractor, reported string) Derived {
	return Derived{Extractor: extractor, Cause: CauseExtractionFailed, Reported: reported}
}

// declared is what one mirror's form declares, given what the consumer's source
// does with it.
func declared(entry Entry, form contract.Form, units map[string]string, source consumerSource,
	allowed []string) ([]Draft, error) {
	var drafts []Draft
	add := func(element string, kind gatepolicy.PredicateKind, argument string) error {
		if !slices.Contains(allowed, string(kind)) {
			return fmt.Errorf("%w: %s", ErrNotAnAllowedPredicateKind, kind)
		}
		if _, err := gatepolicy.DecidablePredicate(string(kind)); err != nil {
			return err
		}
		if err := checkArgument(kind, argument); err != nil {
			return err
		}
		drafts = append(drafts, Draft{
			Address: entry.Address, ProducerService: entry.ProducerService,
			Interface: entry.Interface, Element: element, Kind: kind, Argument: argument,
		})
		return nil
	}

	for _, e := range form.Elements {
		simple := simpleName(e.Name)
		switch e.Kind {
		case contract.ElementOperation:
			if !source.calls[simple] {
				continue
			}
			if err := add(e.Name, gatepolicy.PredicateCalled, ""); err != nil {
				return nil, err
			}
		case contract.ElementField:
			written := source.writes[simple]
			read := source.reads[simple]
			switch {
			case e.Position == contract.PositionInput:
				// What the consumer sends. An element it does not write is one
				// it leaves out, which is what a producer breaks by making the
				// element required.
				argument := LeftOut
				if written {
					argument = Sent
				}
				if err := add(e.Name, gatepolicy.PredicateSent, argument); err != nil {
					return nil, err
				}
				if !written {
					continue
				}
				if err := sendsInside(add, e); err != nil {
					return nil, err
				}
			case e.Position == contract.PositionStore && written:
				// A store's consumer writes as well as reads, a rollback making
				// the restored build the store's writer again.
				if err := add(e.Name, gatepolicy.PredicateSent, Sent); err != nil {
					return nil, err
				}
				if err := sendsInside(add, e); err != nil {
					return nil, err
				}
				if read {
					if err := add(e.Name, gatepolicy.PredicateRead, ""); err != nil {
						return nil, err
					}
					if err := receives(add, e, units); err != nil {
						return nil, err
					}
				}
			case read:
				if err := add(e.Name, gatepolicy.PredicateRead, ""); err != nil {
					return nil, err
				}
				if err := receives(add, e, units); err != nil {
					return nil, err
				}
			}
		}
	}
	return drafts, nil
}

// sendsInside is what the consumer asserts about the values it sends: the domain
// and the range the mirror states.
func sendsInside(add func(string, gatepolicy.PredicateKind, string) error, e contract.Element) error {
	if len(e.Domain) > 0 {
		if err := add(e.Name, gatepolicy.PredicateSentDomain, contract.DomainText(e.Domain)); err != nil {
			return err
		}
	}
	if e.Range != nil {
		return add(e.Name, gatepolicy.PredicateSentRange, e.Range.Text())
	}
	return nil
}

// receives is what the consumer asserts about what it reads: that the element
// arrives populated, that its name carries a unit, and the domain and range its
// values stay inside.
func receives(add func(string, gatepolicy.PredicateKind, string) error, e contract.Element,
	units map[string]string) error {
	if e.Populated {
		if err := add(e.Name, gatepolicy.PredicatePopulated, ""); err != nil {
			return err
		}
	}
	if unit := units[e.Name]; unit != "" {
		if err := add(e.Name, gatepolicy.PredicateUnit, unit); err != nil {
			return err
		}
	}
	if len(e.Domain) > 0 {
		if err := add(e.Name, gatepolicy.PredicateDomain, contract.DomainText(e.Domain)); err != nil {
			return err
		}
	}
	if e.Range != nil {
		return add(e.Name, gatepolicy.PredicateRange, e.Range.Text())
	}
	return nil
}

// simpleName is an element's name without what it belongs to, which is what a
// selector in the consumer's source spells: the form names a field Message.Field
// and the source writes x.Field.
func simpleName(name string) string {
	if _, after, found := strings.Cut(name, "."); found {
		return after
	}
	return name
}

// named is the address a file's own name says, and false for a file that is not a
// mirror. A _test.go file is never one: what the service reads is what its code
// reads, and a test is not part of it.
func named(file string) (string, bool) {
	if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
		return "", false
	}
	address, found := strings.CutPrefix(strings.TrimSuffix(file, ".go"), consumePrefix)
	if !found || address == "" || strings.Contains(address, ".") {
		return "", false
	}
	return address, true
}

// mirrorUnits is the unit each of a mirror's fields asserts, by the element name
// the form gives it. The unit is the one thing a form does not carry — it belongs
// to an element's name — so it is read off the mirror's own tags.
func mirrorUnits(path string) (map[string]string, error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%s does not parse: %v", path, err)
	}
	units := map[string]string{}
	for _, decl := range parsed.Decls {
		generic, ok := decl.(*ast.GenDecl)
		if !ok || generic.Tok != token.TYPE {
			continue
		}
		for _, spec := range generic.Specs {
			typeSpec, ok := spec.(*ast.TypeSpec)
			if !ok || !typeSpec.Name.IsExported() {
				continue
			}
			structType, ok := typeSpec.Type.(*ast.StructType)
			if !ok {
				continue
			}
			for _, field := range structType.Fields.List {
				if field.Tag == nil {
					continue
				}
				for _, word := range contract.TagWords(field.Tag.Value) {
					name, argument, found := strings.Cut(word, "=")
					if !found || name != tagUnit || argument == "" {
						continue
					}
					for _, ident := range field.Names {
						if ident.IsExported() {
							units[typeSpec.Name.Name+"."+ident.Name] = argument
						}
					}
				}
			}
		}
	}
	return units, nil
}
