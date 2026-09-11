package consumercontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// What the consumer's own source does with a mirror, which is the whole of what
// makes a consumer contract derived rather than stated: a name here is a name the
// consumer's code touches, and a mirror field that never appears is a field the
// consumer neither reads nor writes.
//
// A read, a write, or an operation call is keyed by the element's own name —
// Message.Field or Receiver.Operation, the same name the form gives a field and
// the receiver's own type prefixed to an operation — never by the bare field or
// operation name alone. Pairing it that way takes the receiver's type: a struct
// literal's own type for a value built on the spot, and the type of the variable
// a value is bound to otherwise, read from a `var` declaration, a function's
// receiver or parameters, an assignment from another struct literal, or an
// assignment from a call to an operation this checkout's mirrors declare. A field
// or operation name two mirrors share is two names once qualified this way, and a
// read or a call against one no longer reads as one against the other. A call
// with no selector at all — a bare identifier, the shape both a plain function
// this checkout declares at package level and a mirror's own package-level
// operation take — is kept unqualified: there is no receiver there to qualify it
// with, and the two cannot collide, since a mirror's own declaration and a local
// function of the same bare name would already be one declaration to the
// compiler.
//
// The mirror files themselves are not walked: what is derived is what the
// consumer's own source does with the mirror, and the mirror is the shape being
// read against, not a use of it.
//
// It is syntactic and reads no type information beyond what one file's own text
// carries, so a receiver whose type this cannot trace — a value returned by a
// function this checkout does not declare, or reached through more than one
// selector — is a read this misses rather than one it invents. A read through a
// map key or through reflection appears as no selector at all, which is the
// other blind case; this records both a generated accessor and a mapping read
// from configuration as constructs it met and could not follow, along with those
// two, rather than passing any of the four over silently.
//
// A call resolved through an import to some other package's function — never a
// mirror's own operation, which is called through a local receiver or a bare
// local name and never through an import — reaches outside the mirror
// convention entirely, and takes an address where one of its arguments is a
// string literal shaped as a URL or host:port, or a variable this file traced
// back to a store read or a configuration read: none of those addresses is
// traceable to a mirror's configured entry, whatever package the call reaches
// into. [consumerSource.checkDirectCall] is that check, resolving the callee's
// package through the file's own imports rather than the identifier a call
// happens to spell it with, so an alias cannot hide one; [ClientPackages] and
// [clientCall] are kept only for a call into a recognized network client with
// no such argument to trace — net.Dial given a plain variable, say.

// consumerSource is what one checkout's own source does: the names it reads, the
// names it writes, the operations it calls, the constructs this extractor met and
// could not follow, and the first call it found reaching an address outside the
// mirror convention entirely.
type consumerSource struct {
	reads      map[string]bool
	writes     map[string]bool
	calls      map[string]bool
	unfollowed []string
	// directCall is what a call outside the mirror convention names, once one
	// is found. Such a call is could not derive for the whole checkout, so the
	// first one found is enough.
	directCall string
	// imports is the current file's own import paths by the alias it names
	// them under — the name after `as`, or the last element of the path where
	// it names none — set fresh per file by [consumerSource.walk] and read by
	// [consumerSource.checkDirectCall], so a call is resolved against the file
	// it is actually in rather than against every file's imports at once.
	imports map[string]string
	// storeTypes is the message type name of every mirror this checkout holds
	// as a store, read by [consumerSource.addressFlowsFrom] to trace a value
	// assigned from a field read on one of them back to a store read.
	storeTypes map[string]bool
}

// generatedFile is the standard header a generated Go file carries. A file
// carrying it is not the consumer's own hand-written usage — the accessor
// methods it declares are read here as a construct this extractor could not
// follow rather than as reads and writes it might invent from the wrong file.
var generatedFile = regexp.MustCompile(`^// Code generated .* DO NOT EDIT\.$`)

// ClientPackages is every package this extractor recognizes as a network
// client by the import path a checkout names for it, rather than the local
// alias a file gives it: any call into one reaches an address outside the
// mirror convention entirely, because none of them reach a producer through
// an operation a mirror declares. A second toolchain's list would replace
// this one, the way every other convention here does.
var ClientPackages = []string{"net", "net/http", "net/rpc", "google.golang.org/grpc"}

// clientCall is a call this extractor recognizes in a package it does not
// flag wholesale, by the package's own import path and the call's name.
// database/sql exports far more than a network client's address, and only
// Open takes the data source.
var clientCall = map[string]string{"database/sql": "Open"}

// addressLiteral is the shape of a string literal this extractor reads as an
// address regardless of the call it is passed to: a URL's scheme, or a
// host:port pair.
var addressLiteral = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*://|^[a-zA-Z0-9_.-]+:[0-9]+$`)

// readSource walks every file at the root of the consumer's repository that is
// not a mirror and not a test, and reports what it does. returns is the return
// type of every operation this checkout's mirrors declare, by the operation's own
// name, which is what pairs a value the source binds to a call's result with the
// mirror the call reaches. storeTypes is the message type name of every mirror
// this checkout holds as a store, which is what
// [consumerSource.addressFlowsFrom] traces a field read back to.
func readSource(root string, returns map[string]string, storeTypes map[string]bool) (consumerSource, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return consumerSource{}, fmt.Errorf("reading the checkout at %s: %v", root, err)
	}
	source := consumerSource{
		reads: map[string]bool{}, writes: map[string]bool{}, calls: map[string]bool{},
		storeTypes: storeTypes,
	}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, ok := named(name); ok {
			// The mirror is the shape being read against, not a use of it.
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(root, name), nil, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			// A file that does not parse is not something to read a consumer's
			// assumptions out of, and it is not this extractor's to refuse
			// either: the build has to compile one step earlier, and a mirror
			// that does not parse is refused by name.
			continue
		}
		if isGenerated(parsed) {
			source.cannotFollow(fmt.Sprintf("a generated accessor in %s", name))
			continue
		}
		source.walk(parsed, name, returns)
	}
	slices.Sort(source.unfollowed)
	return source, nil
}

// isGenerated reports whether a file carries the standard generated-file header.
func isGenerated(parsed *ast.File) bool {
	for _, group := range parsed.Comments {
		for _, c := range group.List {
			if generatedFile.MatchString(strings.TrimSpace(c.Text)) {
				return true
			}
		}
	}
	return false
}

// walk is one file: what it writes, what it calls, what it reads, and what it
// does that this extractor cannot follow. Each top-level declaration gets its own
// variable-type scope, seeded from the file's package-level vars and, for a
// function, its receiver and parameters — a variable's type is tracked only
// inside the declaration that names it.
func (s *consumerSource) walk(parsed *ast.File, file string, returns map[string]string) {
	s.imports = importsOf(parsed)
	packageLevel := map[string]string{}
	for _, decl := range parsed.Decls {
		generic, ok := decl.(*ast.GenDecl)
		if !ok || generic.Tok != token.VAR {
			continue
		}
		for _, spec := range generic.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if typeName, ok := identName(valueSpec.Type); ok {
				for _, ident := range valueSpec.Names {
					packageLevel[ident.Name] = typeName
				}
			}
		}
	}
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			s.walkNode(decl, file, cloneTypes(packageLevel), returns)
			continue
		}
		local := cloneTypes(packageLevel)
		if fn.Recv != nil {
			addFieldTypes(local, fn.Recv.List)
		}
		if fn.Type.Params != nil {
			addFieldTypes(local, fn.Type.Params.List)
		}
		s.walkNode(fn.Body, file, local, returns)
	}
}

// walkNode is one declaration or one function body, with the variable types in
// scope for it.
func (s *consumerSource) walkNode(node ast.Node, file string, varTypes map[string]string, returns map[string]string) {
	written := map[ast.Node]bool{}
	// tainted is, for a variable this declaration assigns, what
	// [consumerSource.addressFlowsFrom] traced its value back to, scoped to this
	// declaration the way varTypes is.
	tainted := map[string]string{}
	ast.Inspect(node, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for i, target := range n.Lhs {
				if ident, ok := target.(*ast.Ident); ok && i < len(n.Rhs) {
					if typeName, ok := typeOfExpr(n.Rhs[i], returns); ok {
						varTypes[ident.Name] = typeName
					}
					if reason, ok := s.addressFlowsFrom(n.Rhs[i], varTypes); ok {
						tainted[ident.Name] = reason
					}
				}
				if selector, ok := target.(*ast.SelectorExpr); ok && selector.Sel != nil {
					if typeName, ok := receiverType(selector.X, varTypes); ok {
						s.writes[typeName+"."+selector.Sel.Name] = true
					}
					written[selector] = true
				}
			}
		case *ast.CompositeLit:
			typeName, hasType := identName(n.Type)
			for _, element := range n.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := pair.Key.(*ast.Ident); ok && hasType {
					s.writes[typeName+"."+key.Name] = true
				}
			}
		case *ast.CallExpr:
			if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel != nil {
				if typeName, ok := receiverType(selector.X, varTypes); ok {
					s.calls[typeName+"."+selector.Sel.Name] = true
				}
				written[selector] = true
				s.checkDirectCall(n, selector, file, tainted)
			}
			if ident, ok := n.Fun.(*ast.Ident); ok {
				s.calls[ident.Name] = true
			}
		case *ast.SelectorExpr:
			if n.Sel == nil {
				return true
			}
			if ident, ok := n.X.(*ast.Ident); ok && ident.Name == "reflect" {
				s.cannotFollow(fmt.Sprintf("a read through reflection in %s", file))
			}
			if !written[n] {
				if typeName, ok := receiverType(n.X, varTypes); ok {
					s.reads[typeName+"."+n.Sel.Name] = true
				}
			}
		case *ast.IndexExpr:
			if key, ok := n.Index.(*ast.BasicLit); ok && key.Kind == token.STRING {
				name, err := strconv.Unquote(key.Value)
				if err == nil {
					s.cannotFollow(fmt.Sprintf("a string-keyed access to %q in %s", name, file))
				}
			}
			if call, ok := n.Index.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok && selector.Sel != nil {
					if pkg, ok := selector.X.(*ast.Ident); ok && configRead(pkg.Name, selector.Sel.Name) {
						s.cannotFollow(fmt.Sprintf("a mapping read from configuration in %s", file))
					}
				}
			}
		}
		return true
	})
}

// checkDirectCall records the first call resolved through an import — never a
// mirror's own operation, which this file calls through a local receiver or a
// bare local name — that reaches outside the mirror convention entirely: a
// call taking an address as one of its arguments, whatever package it reaches
// into, or, absent such an argument to trace, a call into a network client
// package this checkout imports ([ClientPackages] wholesale, or Open of
// database/sql). Either is could not derive for the whole checkout, so the
// first one found is enough. The package is resolved through the file's own
// imports rather than matched on the identifier text alone, so an alias
// cannot hide one.
func (s *consumerSource) checkDirectCall(call *ast.CallExpr, selector *ast.SelectorExpr, file string, tainted map[string]string) {
	if s.directCall != "" {
		return
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || selector.Sel == nil {
		return
	}
	path, imported := s.imports[pkg.Name]
	if !imported {
		return
	}
	if address, found := addressArgument(call, tainted); found {
		s.directCall = fmt.Sprintf(
			"a call to %s.%s in %s takes %s, which is not traceable to a mirror's configured entry",
			pkg.Name, selector.Sel.Name, file, address)
		return
	}
	if !slices.Contains(ClientPackages, path) && clientCall[path] != selector.Sel.Name {
		return
	}
	s.directCall = fmt.Sprintf(
		"a call to %s.%s in %s reaches into the network client package %s, which is not through a mirror's configured entry",
		pkg.Name, selector.Sel.Name, file, path)
}

// addressArgument is the first argument of call this extractor reads as an
// address — a string literal shaped as a URL or host:port, or a variable
// [consumerSource.addressFlowsFrom] traced back to one — and false where no
// argument traces to either.
func addressArgument(call *ast.CallExpr, tainted map[string]string) (string, bool) {
	for _, arg := range call.Args {
		if lit, ok := arg.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			if value, err := strconv.Unquote(lit.Value); err == nil && addressLiteral.MatchString(value) {
				return fmt.Sprintf("the literal %q", value), true
			}
		}
		if ident, ok := arg.(*ast.Ident); ok {
			if reason, found := tainted[ident.Name]; found {
				return reason, true
			}
		}
	}
	return "", false
}

// addressFlowsFrom is what a value assigned from expr traces back to when it
// is an address this checkout did not write as a literal: a call recognized
// as a configuration read, or a read of a field on a mirror this checkout
// holds as a store — [consumerSource.storeTypes]. Anything else is untraced,
// which is every value this extractor cannot follow back to either.
func (s *consumerSource) addressFlowsFrom(expr ast.Expr, varTypes map[string]string) (string, bool) {
	switch e := expr.(type) {
	case *ast.CallExpr:
		if selector, ok := e.Fun.(*ast.SelectorExpr); ok && selector.Sel != nil {
			if pkg, ok := selector.X.(*ast.Ident); ok && configRead(pkg.Name, selector.Sel.Name) {
				return "a value read from configuration", true
			}
		}
	case *ast.SelectorExpr:
		if e.Sel == nil {
			return "", false
		}
		if typeName, ok := receiverType(e.X, varTypes); ok && s.storeTypes[typeName] {
			return "a value read from the store", true
		}
	}
	return "", false
}

// importsOf is the import path a file names for each of its own aliases: the
// name after `as`, or the last element of the path where a file names none.
// [consumerSource.checkDirectCall] resolves a call's package through this
// rather than through the identifier text a call happens to spell it with.
func importsOf(parsed *ast.File) map[string]string {
	aliases := map[string]string{}
	for _, imp := range parsed.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		alias := path
		if slash := strings.LastIndexByte(path, '/'); slash >= 0 {
			alias = path[slash+1:]
		}
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		aliases[alias] = path
	}
	return aliases
}

// configRead is whether a call is a recognized read of configuration whose value
// used as a map key is a field name this extractor cannot see.
func configRead(pkg, fn string) bool {
	return pkg == "os" && (fn == "Getenv" || fn == "LookupEnv")
}

// identName is the name of a plain identifier type expression, and false for
// anything else — a map type, an anonymous struct, or a qualified name from
// another package, none of which this extractor resolves to a mirror's message.
func identName(expr ast.Expr) (string, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	return ident.Name, true
}

// receiverType is the type a receiver expression is bound to, and false where
// this cannot trace it: a plain identifier looked up in the variable types in
// scope, and nothing else.
func receiverType(expr ast.Expr, varTypes map[string]string) (string, bool) {
	ident, ok := expr.(*ast.Ident)
	if !ok {
		return "", false
	}
	typeName, found := varTypes[ident.Name]
	return typeName, found
}

// typeOfExpr is the type a value bound to a variable takes, where this can trace
// it: a struct literal's own type, one level of address-of read through, or the
// return type of a call to an operation returns names.
func typeOfExpr(expr ast.Expr, returns map[string]string) (string, bool) {
	if unary, ok := expr.(*ast.UnaryExpr); ok && unary.Op == token.AND {
		expr = unary.X
	}
	switch e := expr.(type) {
	case *ast.CompositeLit:
		return identName(e.Type)
	case *ast.CallExpr:
		ident, ok := e.Fun.(*ast.Ident)
		if !ok {
			return "", false
		}
		typeName, found := returns[ident.Name]
		return typeName, found
	}
	return "", false
}

// addFieldTypes records the type of every named field in a receiver or a
// parameter list, reading through one level of pointer.
func addFieldTypes(types map[string]string, fields []*ast.Field) {
	for _, field := range fields {
		fieldType := field.Type
		if star, ok := fieldType.(*ast.StarExpr); ok {
			fieldType = star.X
		}
		typeName, ok := identName(fieldType)
		if !ok {
			continue
		}
		for _, ident := range field.Names {
			types[ident.Name] = typeName
		}
	}
}

// cloneTypes is a fresh copy of a variable-type scope, so one declaration's
// assignments never leak into another's.
func cloneTypes(types map[string]string) map[string]string {
	clone := make(map[string]string, len(types))
	for name, typeName := range types {
		clone[name] = typeName
	}
	return clone
}

// cannotFollow records one construct the extractor met and could not follow, once
// however many times it met it. A record whose list is not empty is partial, and
// the deprecation list reads a partial record the way it reads a could-not-derive
// one.
func (s *consumerSource) cannotFollow(what string) {
	if !slices.Contains(s.unfollowed, what) {
		s.unfollowed = append(s.unfollowed, what)
	}
}
