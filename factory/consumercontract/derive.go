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
// derivation, and a second toolchain replaces it rather than extending it.
//
// The convention is one file name at the root of the repository:
//
//	consume.<producer service>.<interface>.go
//
// It holds exactly one exported struct type — the mirror of the interface the
// consumer reads — whose exported fields are the elements it may read. What is
// derived is the fields that the consumer's own source actually selects: a field in
// the mirror that nothing reads is not declared. That is what makes a consumer
// which stops reading an element stop declaring it, with nobody remembering to,
// and it is the whole mechanism the deprecation list rests on.
//
// A `borg` struct tag on a mirror field says what else is asserted about it:
// `populated`, `unit=millis`, `domain=ok|error`, `range=0..100`. A field that is
// read declares the read predicate whether or not it has a tag, and a field that
// is not read declares nothing however many tags it carries.
//
// Both of the design's blind cases are real here and neither is silent. A read this
// misses is one made through reflection, through a map, or through a name the parse
// cannot see as a selector — an unprotected assumption, which is what a safeguard's
// predicate covers. A read it invents is a field name the mirror shares with some
// other type in the consumer's own code, since the resolution is syntactic and
// nothing here type-checks — which is what withdrawing a safeguard, or the
// producer's blocked removal item asking the consumer to confirm, is for.

// consumePrefix is the file-name prefix of a mirror.
const consumePrefix = "consume."

// The tag words a mirror field may carry beside contract's own. Each names the
// predicate kind it declares, and the three that take an argument carry it after
// an equals sign.
const (
	tagUnit   = "unit"
	tagDomain = "domain"
	tagRange  = "range"
)

// ErrDerivation is returned where a file follows the naming convention and is not
// something a consumer contract can be derived from: it does not parse, or it
// holds no exported struct type or more than one.
//
// It is an error and not an empty consumer contract, for the reason contract's own
// derivation refuses the same shape: a build that names a mirror file and declares
// nothing from it is a build whose author meant to declare something, and reading
// it as no consumer contract would silently drop the protection the consumer was
// owed.
var ErrDerivation = errors.New("consumercontract: a mirror file the derivation cannot read")

// ErrNotAnAllowedPredicateKind is returned for a tag word naming a kind of
// assertion that is not in the list of allowed predicate kinds in force. A
// consumer picks from the list and cannot invent a kind of assertion at
// consumer contract time, and this is that rule at the derivation.
var ErrNotAnAllowedPredicateKind = errors.New("consumercontract: that kind of assertion is not in the list of allowed predicate kinds in force")

// FileName is the file one consumer's mirror is derived from, which is what an
// agent is told to write and what a test writes directly.
func FileName(producerService, interfaceName string) string {
	return consumePrefix + producerService + "." + interfaceName + ".go"
}

// Derive is every predicate the checkout at root declares, in the order of the
// producer, the interface, the element, and the kind. allowed is the list of
// allowed predicate kinds in force, which is what limits the kinds a
// consumer contract may draw from; a tag naming a kind outside it is
// [ErrNotAnAllowedPredicateKind], and one naming a kind inside it that this
// factory has no decider for is [gatepolicy.ErrPredicateKindUnknown] — which is
// where a list wide enough to admit an undecidable assertion is actually
// refused.
//
// A checkout with no mirror file declares nothing, which is every service that
// consumes nothing and is not an error. Only the root directory is read, which is
// the same limit contract's derivation has and for the same reason.
func Derive(root string, allowed []string) ([]Draft, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading the checkout at %s: %w", root, err)
	}
	mirrors := map[string]mirror{}
	var order []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		producer, interfaceName, ok := named(entry.Name())
		if !ok {
			continue
		}
		fields, err := mirrorFields(filepath.Join(root, entry.Name()))
		if err != nil {
			return nil, err
		}
		key := producer + "." + interfaceName
		mirrors[key] = mirror{producer: producer, interfaceName: interfaceName, fields: fields}
		order = append(order, key)
	}
	if len(mirrors) == 0 {
		return nil, nil
	}
	slices.Sort(order)

	read, err := selectors(root)
	if err != nil {
		return nil, err
	}

	var drafts []Draft
	for _, key := range order {
		m := mirrors[key]
		for _, field := range m.fields {
			if !read[field.name] {
				continue
			}
			kinds, err := field.declares(allowed)
			if err != nil {
				return nil, fmt.Errorf("consumercontract: %s of %s: %w", field.name, key, err)
			}
			for _, k := range kinds {
				drafts = append(drafts, Draft{
					ProducerService: m.producer,
					Interface:       m.interfaceName,
					Element:         field.name,
					Kind:            k.kind,
					Argument:        k.argument,
				})
			}
		}
	}
	return drafts, nil
}

type mirror struct {
	producer      string
	interfaceName string
	fields        []mirrorField
}

type mirrorField struct {
	name string
	tags []string
}

type assertion struct {
	kind     gatepolicy.PredicateKind
	argument string
}

// declares is what one mirror field asserts: the read predicate, because the field
// is read, and one more per tag word. The read comes first, so a reader of a
// consumer contract sees the assumption the derivation found before the ones the
// build stated.
func (f mirrorField) declares(allowed []string) ([]assertion, error) {
	asserted := []assertion{{kind: gatepolicy.PredicateRead}}
	for _, word := range f.tags {
		name, argument, _ := strings.Cut(word, "=")
		switch name {
		case contract.TagPopulated:
			asserted = append(asserted, assertion{kind: gatepolicy.PredicatePopulated})
		case tagUnit, tagDomain, tagRange:
			asserted = append(asserted, assertion{kind: gatepolicy.PredicateKind(name), argument: argument})
		default:
			// A word this derivation does not know is not an error: the same field
			// may carry tags for other tools, and contract's own derivation ignores
			// what it does not read for the same reason.
			continue
		}
	}
	for _, a := range asserted {
		if !slices.Contains(allowed, string(a.kind)) {
			return nil, fmt.Errorf("%w: %s", ErrNotAnAllowedPredicateKind, a.kind)
		}
		if _, err := gatepolicy.DecidablePredicate(string(a.kind)); err != nil {
			return nil, err
		}
		if err := checkArgument(a.kind, a.argument); err != nil {
			return nil, err
		}
	}
	return asserted, nil
}

// named is the producer's service and the interface a file's own name says, and
// false for a file that is not a mirror. A _test.go file is never one: what the
// service reads is what its code reads, and a test is not part of it.
func named(file string) (string, string, bool) {
	if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
		return "", "", false
	}
	rest, found := strings.CutPrefix(strings.TrimSuffix(file, ".go"), consumePrefix)
	if !found {
		return "", "", false
	}
	producer, interfaceName, found := strings.Cut(rest, ".")
	if !found || producer == "" || interfaceName == "" || strings.Contains(interfaceName, ".") {
		return "", "", false
	}
	return producer, interfaceName, true
}

// mirrorFields is the exported fields of the one exported struct type in a mirror
// file, with the `borg` tag words of each.
func mirrorFields(path string) ([]mirrorField, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, source, parser.SkipObjectResolution)
	if err != nil {
		return nil, fmt.Errorf("%w: %s does not parse: %w", ErrDerivation, path, err)
	}

	var structType *ast.StructType
	found := 0
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
			if s, ok := typeSpec.Type.(*ast.StructType); ok {
				structType = s
				found++
			}
		}
	}
	if found != 1 {
		return nil, fmt.Errorf("%w: %s holds %d exported struct types and a mirror holds one",
			ErrDerivation, path, found)
	}

	var fields []mirrorField
	for _, field := range structType.Fields.List {
		var words []string
		if field.Tag != nil {
			words = contract.TagWords(field.Tag.Value)
		}
		for _, ident := range field.Names {
			if ident.IsExported() {
				fields = append(fields, mirrorField{name: ident.Name, tags: words})
			}
		}
	}
	return fields, nil
}

// selectors is every field name the consumer's own source selects, anywhere at
// the root of its repository. It is the whole of what makes a consumer contract
// derived rather than stated: a name here is a name the consumer's code reads,
// and a mirror field that never appears is a field the consumer does not read.
//
// It is syntactic. `x.Status` counts wherever it appears and whatever x is, so a
// field name the mirror shares with another type reads as a read of the mirror's —
// a read invented, which is one of the two blind cases the design names. A read
// through a map key or through reflection appears as no selector at all, which is
// the other.
func selectors(root string) (map[string]bool, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("consumercontract: reading the checkout at %s: %w", root, err)
	}
	read := map[string]bool{}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(root, name), nil, parser.SkipObjectResolution)
		if err != nil {
			// A file that does not parse is not something to read reads out of,
			// and it is not this derivation's to refuse either: the build has to
			// compile one step earlier, and a mirror that does not parse is
			// refused above by name.
			continue
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			if selector, ok := node.(*ast.SelectorExpr); ok && selector.Sel != nil {
				read[selector.Sel.Name] = true
			}
			return true
		})
	}
	return read, nil
}
