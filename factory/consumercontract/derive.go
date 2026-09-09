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
// [GoConvention] is this in the words the extractor's own record carries.
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
// What is derived is what the consumer's own source does with the mirror, each
// element paired with the mirror it is read or written on through the receiver's
// type or the type of the variable the value is bound to — a field name two
// mirrors share is two elements and not one:
//
//   - a field of a message the interface returns, or of a store, that the source
//     reads declares that it is read, and what the mirror's tags say about it:
//     that it arrives populated, that its name carries a unit, that its values
//     stay inside a domain or a range;
//   - a field of a message the interface accepts that the source writes, or of a
//     store, declares that it is sent, whether it is written populated, and the
//     domain or range of what it sends; one the source does not write declares
//     that it is left out, which is what a producer breaks by making it required;
//   - an operation the source calls declares that it is called at all.
//
// A field the mirror holds and the source never touches declares nothing. That is
// what makes a consumer which stops reading an element stop declaring it, with
// nobody remembering to, and it is the whole mechanism the deprecation list rests
// on.
//
// The design's blind case is real here and is not silent where this can see it.
// Four constructs are recorded rather than passed over: a read through reflection,
// a string-keyed access, a generated accessor — a file carrying the standard
// `// Code generated ... DO NOT EDIT.` header — and a mapping read from
// configuration, source.go's [readSource] and [consumerSource.checkDirectCall]
// stating each. What none of that can see — a read through a map key this cannot
// name, or a field name the mirror shares with some other type this cannot
// resolve to a receiver — is what withdrawing a safeguard, or the producer's
// blocked removal item asking the consumer to confirm, is for.
//
// A checkout that reaches an address outside this convention entirely — a
// literal address or one read from a store, passed straight to a recognized
// network call rather than through a mirror — is could not derive, naming the
// call site: [consumerSource.checkDirectCall]. So is a checkout that makes any
// call at all and holds no mirror and no configuration file, which is the state
// an adopted service arrives in until its entries are authored; only a checkout
// that makes no call at all derives complete and empty.

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

// GoConvention is where a consumer's declaration sits for the Go toolchain and
// how it is stated, published with the extractor rather than left for a reader to
// find in this file. The mirror's own shape is [contract.DeriveFile]'s, read
// rather than restated here.
const GoConvention = "one mirror file per address, consume.<address>.go at the checkout's root, " +
	"in the shape contract.DeriveFile reads"

// GoExtractor is this extractor as a record names one. The factory version is the
// caller's: an extractor ships with the factory, so a derivation is a function of
// the code and of the factory version.
func GoExtractor(factoryVersion string) Extractor {
	return Extractor{
		Name: ExtractorName, Version: ExtractorVersion,
		Toolchain: Toolchain, FactoryVersion: factoryVersion, Convention: GoConvention,
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
// A mirror whose address the configuration file does not hold, a configuration
// file that is missing while a mirror names an address, and a mirror this
// extractor cannot read are all could not derive: a record, not an empty list,
// because "no consumer reads this" and "no consumer's read was visible" call for
// opposite responses. So is a checkout that reaches an address outside a mirror
// entirely, and a checkout with no mirror file that makes any call at all — the
// state an adopted service arrives in until its entries are authored. Only a
// checkout with no mirror file that makes no call either derives complete and
// empty, which is every service that consumes nothing.
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

	// The first pass derives every mirror's own form, before any source file is
	// read: an element the source binds to a call's result is paired with the
	// mirror through that operation's return type, which this extractor has to
	// know before it can read what the source does with it.
	type mirror struct {
		address string
		entry   Entry
		form    contract.Form
		units   map[string]string
	}
	var loaded []mirror
	returns := map[string]string{}
	if len(mirrors) > 0 {
		addresses, found, err := Entries(root)
		if err != nil {
			return failed(extractor, err.Error()), nil
		}
		if !found {
			return failed(extractor, fmt.Sprintf("%d mirror(s) name an address and the checkout holds no %s",
				len(mirrors), ConfigurationFile)), nil
		}
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
			units, operationReturns, err := mirrorMeta(path)
			if err != nil {
				return failed(extractor, err.Error()), nil
			}
			for name, typeName := range operationReturns {
				returns[name] = typeName
			}
			loaded = append(loaded, mirror{address, entry, form, units})
		}
	}

	source, err := readSource(root, returns)
	if err != nil {
		return failed(extractor, err.Error()), nil
	}
	if source.directCall != "" {
		return failed(extractor, source.directCall), nil
	}
	derived.Unfollowed = source.unfollowed

	if len(mirrors) == 0 {
		if len(source.calls) > 0 {
			return failed(extractor, fmt.Sprintf(
				"the checkout makes %d call(s) and holds no mirror and no %s naming a producer",
				len(source.calls), ConfigurationFile)), nil
		}
		return derived, nil
	}

	for _, m := range loaded {
		drafts, err := declared(m.entry, m.form, m.units, source, allowed)
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
// does with it. Reads and writes are looked up by the element's own name, which
// [readSource] already qualifies by the type the source read or wrote it on — a
// field name two mirrors share is two names here and pairs with only its own.
//
// seen dedupes the same predicate offered twice: a store element both read and
// written can otherwise be asserted populated once for each side, and this
// package writes a predicate exactly once.
func declared(entry Entry, form contract.Form, units map[string]string, source consumerSource,
	allowed []string) ([]Draft, error) {
	var drafts []Draft
	seen := map[string]bool{}
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
		key := element + "\x00" + string(kind) + "\x00" + argument
		if seen[key] {
			return nil
		}
		seen[key] = true
		drafts = append(drafts, Draft{
			Address: entry.Address, ProducerService: entry.ProducerService,
			Interface: entry.Interface, Element: element, Kind: kind, Argument: argument,
		})
		return nil
	}

	for _, e := range form.Elements {
		switch e.Kind {
		case contract.ElementOperation:
			if !source.calls[e.Name] {
				continue
			}
			if err := add(e.Name, gatepolicy.PredicateCalled, ""); err != nil {
				return nil, err
			}
		case contract.ElementField:
			written := source.writes[e.Name]
			read := source.reads[e.Name]
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

// sendsInside is what the consumer asserts about a value it writes: whether it is
// written populated, and the domain and the range the mirror states — the write
// side of the same tags [receives] reads on the side the source shows populated.
func sendsInside(add func(string, gatepolicy.PredicateKind, string) error, e contract.Element) error {
	if e.Populated {
		if err := add(e.Name, gatepolicy.PredicatePopulated, ""); err != nil {
			return err
		}
	}
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

// mirrorMeta is the unit each of a mirror's fields asserts, by the element name
// the form gives it, and the return type of each exported operation with exactly
// one plain result, by the operation's own name. The unit is the one thing a form
// does not carry — it belongs to an element's name — so it is read off the
// mirror's own tags; the return type is what pairs a value the source binds to a
// call's result with the mirror the call reaches, since a form does not carry
// that either.
func mirrorMeta(path string) (units map[string]string, returns map[string]string, err error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, fmt.Errorf("%s does not parse: %v", path, err)
	}
	units = map[string]string{}
	returns = map[string]string{}
	for _, decl := range parsed.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			if d.Tok != token.TYPE {
				continue
			}
			for _, spec := range d.Specs {
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
		case *ast.FuncDecl:
			if !d.Name.IsExported() || d.Type.Results == nil || len(d.Type.Results.List) != 1 {
				continue
			}
			result := d.Type.Results.List[0]
			if len(result.Names) > 0 {
				continue
			}
			resultType := result.Type
			if star, ok := resultType.(*ast.StarExpr); ok {
				resultType = star.X
			}
			if ident, ok := resultType.(*ast.Ident); ok {
				returns[d.Name.Name] = ident.Name
			}
		}
	}
	return units, returns, nil
}
