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
	"strconv"
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
// Each holds exactly one exported struct type, whatever that type is called, and
// its exported fields are the elements. The interface's name is the file's and not
// the type's, because the file is already what says the kind, and a name in both
// places would be two spellings able to disagree.
//
// Each field's type is the source text of its type expression, compared for
// equality and never interpreted. A `borg` struct tag says the rest: `populated`
// for an element that is always there, `deprecated` for the mark. A field with no
// tag is optional and unmarked, which is the safe direction — an element nobody
// said was always populated cannot be weakened.
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
	// TagDeprecated is the deprecation mark, which is a change to the form like
	// any other and mints a version.
	TagDeprecated = "deprecated"
)

// ErrDerivation is returned where a file follows the naming convention and is not
// something a form can be derived from: it does not parse, it holds no exported
// struct type or more than one, or a field of that type has no name.
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
		form, err := deriveFile(filepath.Join(root, entry.Name()), kind, name)
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

// deriveFile is the one form a contract file publishes.
func deriveFile(path string, kind Kind, name string) (Form, error) {
	source, err := os.ReadFile(path)
	if err != nil {
		return Form{}, fmt.Errorf("contract: reading %s: %w", path, err)
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, source, parser.SkipObjectResolution)
	if err != nil {
		return Form{}, fmt.Errorf("%w: %s does not parse: %w", ErrDerivation, path, err)
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
		return Form{}, fmt.Errorf("%w: %s holds %d exported struct types and a contract file holds one",
			ErrDerivation, path, found)
	}

	form := Form{Name: name, Kind: kind}
	for _, field := range structType.Fields.List {
		if len(field.Names) == 0 {
			return Form{}, fmt.Errorf("%w: %s has an embedded field, which names no element", ErrDerivation, path)
		}
		populated, deprecated := readTag(field.Tag)
		for _, ident := range field.Names {
			if !ident.IsExported() {
				continue
			}
			form.Elements = append(form.Elements, Element{
				Name:       ident.Name,
				Type:       sourceOf(source, fset, field.Type),
				Populated:  populated,
				Deprecated: deprecated,
			})
		}
	}
	form = form.Sorted()
	if err := form.Validate(); err != nil {
		return Form{}, fmt.Errorf("%w: %s: %w", ErrDerivation, path, err)
	}
	return form, nil
}

// readTag is what a field's `borg` tag says: whether the element is always
// populated, and whether it is marked deprecated. A tag naming anything else is
// ignored rather than refused — this derivation reads two words and a build may
// carry tags for other tools on the same field.
func readTag(tag *ast.BasicLit) (populated, deprecated bool) {
	if tag == nil {
		return false, false
	}
	for _, word := range TagWords(tag.Value) {
		switch word {
		case TagPopulated:
			populated = true
		case TagDeprecated:
			deprecated = true
		}
	}
	return populated, deprecated
}

// TagWords is the comma-separated words of one field's `borg` tag, and none where
// the field has no tag or no `borg` key. It is exported because package
// declaration reads the same tag on a consumer's mirror, with words of its own,
// and two packages parsing one tag two ways would be two spellings of one
// convention.
func TagWords(quoted string) []string {
	if quoted == "" {
		return nil
	}
	literal, err := strconv.Unquote(quoted)
	if err != nil {
		return nil
	}
	value, ok := reflectTagLookup(literal, tagKey)
	if !ok {
		return nil
	}
	var words []string
	for _, word := range strings.Split(value, ",") {
		if word = strings.TrimSpace(word); word != "" {
			words = append(words, word)
		}
	}
	return words
}

// reflectTagLookup is the one value of a struct tag, read the way the language
// defines a tag rather than by asking the reflect package: a space-separated list
// of key:"value" pairs. It is written out here because reading a tag through
// reflect.StructTag would mean holding a type at run time, and what this has is
// the tag's text out of the source.
func reflectTagLookup(tag, key string) (string, bool) {
	for tag != "" {
		i := 0
		for i < len(tag) && tag[i] == ' ' {
			i++
		}
		tag = tag[i:]
		if tag == "" {
			break
		}
		i = 0
		for i < len(tag) && tag[i] > ' ' && tag[i] != ':' && tag[i] != '"' {
			i++
		}
		if i+1 >= len(tag) || tag[i] != ':' || tag[i+1] != '"' {
			break
		}
		name := tag[:i]
		tag = tag[i+1:]
		i = 1
		for i < len(tag) && tag[i] != '"' {
			if tag[i] == '\\' {
				i++
			}
			i++
		}
		if i >= len(tag) {
			break
		}
		quoted := tag[:i+1]
		tag = tag[i+1:]
		if name != key {
			continue
		}
		value, err := strconv.Unquote(quoted)
		if err != nil {
			return "", false
		}
		return value, true
	}
	return "", false
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
