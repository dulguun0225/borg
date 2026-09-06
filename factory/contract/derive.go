package contract

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
)

// The derivation. What a service publishes is read out of its checkout and never
// entered by hand, and how much of a build is visible is a property of its
// toolchain rather than of the factory — so this file is Go's derivation and a
// second toolchain replaces it rather than extending it.
//
// The convention is two file names at the root of the repository:
//
//	contract.<name>.go  one published interface, named <name>
//	store.<name>.go     one store contract, named <name>
//
// The interface's name is the file's and not a type's, because the file is
// already what says the kind, and a name in both places would be two spellings
// able to disagree.
//
// What the file declares is the form's elements, of the four kinds a form has:
//
//   - an exported func or method is an operation, named as it is named, and its
//     type is the source text of its signature — so a signature redeclared is a
//     retype;
//   - each of that operation's parameters is an argument, named Operation.name,
//     in input position and required, a caller having no way not to pass one;
//   - an exported struct type is a message, named as it is named;
//   - each of that message's exported fields is a field, named Message.Field.
//
// A message a signature names among its parameters is in input position and so
// are its fields; every other message is in output position. Everything in a
// store file is in store position, a store being read and written by the same
// service.
//
// Each type is the source text of its type expression, compared for equality and
// never interpreted. A `borg` struct tag says the rest: `populated` for an element
// that is always there, `required` for one a caller has to supply, `deprecated`
// for the mark, `domain=a|b` and `range=0..100` for what the element accepts, and
// `notnull` and `unique` for the two other constraints a store element carries. A
// field with no tag is optional, unmarked, unconstrained, and accepts anything,
// which is the safe direction — an element nobody said was always populated cannot
// be weakened. An operation carries the mark in a `//borg:deprecated` line of its
// doc comment, Go having no tag on a func.
//
// What this cannot see: a form built at run time, a type declared somewhere else
// and aliased here, and anything a generator would have produced. It parses source
// and does not type-check it, so a form the compiler would reject is still a form —
// which nothing acts on, the build having had to compile one step earlier.

const (
	// interfacePrefix is the file-name prefix of a published interface.
	interfacePrefix = "contract."
	// storePrefix is the file-name prefix of a store contract.
	storePrefix = "store."
	// tagKey is the struct tag this derivation reads.
	tagKey = "borg"
	// TagPopulated marks an element as always populated.
	TagPopulated = "populated"
	// TagRequired marks an element a caller has to supply.
	TagRequired = "required"
	// TagDeprecated is the deprecation mark, which is a change to the form like
	// any other and mints a version.
	TagDeprecated = "deprecated"
	// TagDomain is the set of names an element accepts, written domain=a|b.
	TagDomain = "domain"
	// TagRange is the two numbers an element accepts, written range=0..100.
	TagRange = "range"
	// TagNotNull and TagUnique are the two store constraints that are neither a
	// domain nor a range.
	TagNotNull = "notnull"
	TagUnique  = "unique"
	// deprecatedDirective is the deprecation mark on an operation, which carries
	// no struct tag.
	deprecatedDirective = "borg:" + TagDeprecated
	// messageType is the type text of a message element. A message's own type
	// never moves; what moves is its fields, each of which is an element of its
	// own.
	messageType = "struct"
)

// ErrDerivation is returned where a file follows the naming convention and is not
// something a form can be derived from: it does not parse, it declares no exported
// operation and no exported struct type, an argument of an operation has no name,
// or a tag's domain or range cannot be read.
//
// It is an error and not an empty form, because a build that names a contract file
// and publishes nothing from it is a build whose author meant to publish
// something. The alternative — reading it as no contract — would silently retire an
// interface consumers depend on.
var ErrDerivation = errors.New("contract: a contract file the derivation cannot read")

// FileName is the file one form is derived from, which is what an agent is told to
// write and what a test writes directly.
func FileName(kind Kind, name string) string {
	if kind == KindStore {
		return storePrefix + name + ".go"
	}
	return interfacePrefix + name + ".go"
}

// Derive is every form the checkout at root publishes, ordered by name. A
// checkout with no contract file publishes none, which is every service that
// publishes nothing and is not an error.
//
// Only the root directory is read. A service is one repository with one long-lived
// branch and this convention puts a contract beside the code that serves it, so a
// contract file in a subdirectory is not one — which is a limit of the convention
// and is stated where an agent is told about it.
func Derive(root string) ([]Form, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("contract: reading the checkout at %s: %w", root, err)
	}
	var forms []Form
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		kind, name, ok := named(entry.Name())
		if !ok {
			continue
		}
		form, err := DeriveFile(filepath.Join(root, entry.Name()), kind, name)
		if err != nil {
			return nil, err
		}
		forms = append(forms, form)
	}
	slices.SortFunc(forms, func(a, b Form) int { return strings.Compare(a.Name, b.Name) })
	return forms, nil
}

// named is the kind and the name a file's own name says, and false for a file
// that is not a contract file. A _test.go file is never one: a contract is what the
// service publishes, and a test is not part of it.
func named(file string) (Kind, string, bool) {
	if !strings.HasSuffix(file, ".go") || strings.HasSuffix(file, "_test.go") {
		return "", "", false
	}
	stem := strings.TrimSuffix(file, ".go")
	for _, prefix := range []struct {
		text string
		kind Kind
	}{{interfacePrefix, KindInterface}, {storePrefix, KindStore}} {
		name, found := strings.CutPrefix(stem, prefix.text)
		if found && name != "" && !strings.Contains(name, ".") {
			return prefix.kind, name, true
		}
	}
	return "", "", false
}

// DeriveFile is the one form one contract file publishes. It is exported because
// a consumer's mirror of an interface is written the way the interface's own
// contract file is written, and package consumercontract derives the mirror's form
// through this rather than keeping a second parse of one convention.
func DeriveFile(path string, kind Kind, name string) (Form, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return Form{}, fmt.Errorf("contract: reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, source, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		return Form{}, fmt.Errorf("%w: %s does not parse: %w", ErrDerivation, path, err)
	}

	structs, order := structTypes(parsed)
	operations := operationDecls(parsed)
	if len(structs) == 0 && len(operations) == 0 {
		return Form{}, fmt.Errorf("%w: %s declares no exported operation and no exported struct type",
			ErrDerivation, path)
	}

	form := Form{Name: name, Kind: kind}
	sent := map[string]bool{}
	for _, decl := range operations {
		elements, inputs, err := operation(source, fset, decl, kind, path)
		if err != nil {
			return Form{}, err
		}
		form.Elements = append(form.Elements, elements...)
		for _, in := range inputs {
			sent[in] = true
		}
	}
	for _, typeName := range order {
		position := PositionOutput
		switch {
		case kind == KindStore:
			position = PositionStore
		case sent[typeName]:
			position = PositionInput
		}
		elements, err := message(source, fset, typeName, structs[typeName], position, path)
		if err != nil {
			return Form{}, err
		}
		form.Elements = append(form.Elements, elements...)
	}

	form = form.Sorted()
	if err := form.Validate(); err != nil {
		return Form{}, fmt.Errorf("%w: %s: %w", ErrDerivation, path, err)
	}
	return form, nil
}

// structTypes is the exported struct types of one file, by name and in the order
// they are declared.
func structTypes(parsed *ast.File) (map[string]*ast.StructType, []string) {
	types := map[string]*ast.StructType{}
	var order []string
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
				types[typeSpec.Name.Name] = s
				order = append(order, typeSpec.Name.Name)
			}
		}
	}
	return types, order
}

// operationDecls is the exported func and method declarations of one file, in the
// order they are declared. Each is one operation of the interface.
func operationDecls(parsed *ast.File) []*ast.FuncDecl {
	var decls []*ast.FuncDecl
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Name.IsExported() {
			decls = append(decls, fn)
		}
	}
	return decls
}

// operation is the elements one operation contributes — itself and one argument
// per parameter — and the names of the types its parameters carry, which is what
// puts a message in input position.
func operation(source []byte, fset *token.FileSet, decl *ast.FuncDecl, kind Kind, path string) ([]Element, []string, error) {
	elements := []Element{{
		Name:      decl.Name.Name,
		Kind:      ElementOperation,
		Position:  positionOf(kind, PositionOutput),
		Type:      sourceOf(source, fset, decl.Type),
		Populated: true,
		Marked:    directed(decl.Doc, deprecatedDirective),
	}}
	var carried []string
	if decl.Type.Params == nil {
		return elements, nil, nil
	}
	for _, param := range decl.Type.Params.List {
		if len(param.Names) == 0 {
			return nil, nil, fmt.Errorf("%w: %s has an argument with no name in %s",
				ErrDerivation, decl.Name.Name, path)
		}
		carried = append(carried, typeNames(param.Type)...)
		for _, ident := range param.Names {
			elements = append(elements, Element{
				Name:     decl.Name.Name + "." + ident.Name,
				Kind:     ElementArgument,
				Position: positionOf(kind, PositionInput),
				Type:     sourceOf(source, fset, param.Type),
				Required: true,
			})
		}
	}
	return elements, carried, nil
}

// message is the elements one struct type contributes: itself, and one field per
// exported field.
func message(source []byte, fset *token.FileSet, name string, structType *ast.StructType,
	position Position, path string) ([]Element, error) {
	elements := []Element{{
		Name: name, Kind: ElementMessage, Position: position, Type: messageType, Populated: true,
	}}
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			return nil, fmt.Errorf("%w: %s has an embedded field, which names no element", ErrDerivation, path)
		}
		read, err := readTag(field.Tag)
		if err != nil {
			return nil, fmt.Errorf("%w: %s of %s: %w", ErrDerivation, name, path, err)
		}
		for _, ident := range field.Names {
			if !ident.IsExported() {
				continue
			}
			e := read
			e.Name = name + "." + ident.Name
			e.Kind = ElementField
			e.Position = position
			e.Type = sourceOf(source, fset, field.Type)
			elements = append(elements, e)
		}
	}
	return elements, nil
}

// positionOf is the position an element of an interface would be in, and store
// position on a store: a store is read and written by the same service, so
// nothing in one is in input or output position.
func positionOf(kind Kind, position Position) Position {
	if kind == KindStore {
		return PositionStore
	}
	return position
}

// typeNames is the names a type expression mentions, which is how a parameter
// carrying a message is paired with the message. A pointer, a slice, and a map
// are read through to what they hold.
func typeNames(expr ast.Expr) []string {
	var names []string
	ast.Inspect(expr, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			names = append(names, ident.Name)
		}
		return true
	})
	return names
}

// directed reports whether a doc comment carries one directive line.
func directed(doc *ast.CommentGroup, directive string) bool {
	if doc == nil {
		return false
	}
	for _, line := range doc.List {
		if strings.TrimSpace(strings.TrimPrefix(line.Text, "//")) == directive {
			return true
		}
	}
	return false
}

// sourceOf is the source text of one expression, which is how an element's type
// reaches the form. It is the bytes the build itself holds, so `int64` and `int64`
// are one type and `int64` and `int` are two, with nothing here deciding which
// pairs a consumer survives.
func sourceOf(source []byte, fset *token.FileSet, expr ast.Expr) string {
	from, to := fset.Position(expr.Pos()).Offset, fset.Position(expr.End()).Offset
	if from < 0 || to > len(source) || from >= to {
		return ""
	}
	return string(source[from:to])
}
