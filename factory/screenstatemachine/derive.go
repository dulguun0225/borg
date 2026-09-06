package screenstatemachine

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
)

// The Go extractor. How much of a screen's behaviour is visible is a property
// of the toolchain rather than of the factory, so this file is Go's extractor
// and a second toolchain ships a second one rather than extending this.
//
// The convention is one file at the root of the repository per screen:
//
//	screen.<the screen's id>.go
//
// holding the screen's own transition function,
//
//	func Transition(from, event string) string
//
// a switch on the first parameter whose cases are the states written as string
// literals, each holding a switch on the second parameter whose cases are the
// events written as string literals, each of those returning the destination as
// a string literal — a state of this machine, or the id of the screen it leaves
// to. A trailing return of a parameter is the fall-through and admits nothing.
//
// Everything else in that function is a construct this extractor met and could
// not follow, and one such construct is could not derive for that screen: a
// value read from a table, a state returned by a call, a handler assigned at run
// time, a destination returned for every state at once. That is the direction
// the design fixes — reject only a transition it can show the implementation
// admits — and a partial extraction reads as none rather than as clean.

// Toolchain and ExtractorName are what this extractor is, and ExtractorVersion
// is which one it is. The version moves when what this file derives changes.
const (
	Toolchain        = "go"
	ExtractorName    = "go/ast"
	ExtractorVersion = "1"
)

// transitionFunc is the name of the function a screen's file holds.
const transitionFunc = "Transition"

// GoExtractor is this extractor as a record names one. The factory version is
// the caller's: an extractor ships with the factory, so a derivation is a
// function of the code and of the factory version.
func GoExtractor(factoryVersion string) Extractor {
	return Extractor{
		Name: ExtractorName, Version: ExtractorVersion,
		Toolchain: Toolchain, FactoryVersion: factoryVersion,
	}
}

// FileName is the file one screen's transition function is written in, which is
// what the implementation role is told to write and what a test writes directly.
func FileName(screen string) string { return "screen." + screen + ".go" }

// DeriveTransitions is what this extractor makes of a checkout at dir, one
// [ScreenDerivation] per machine in force: the transitions it can show the
// implementation admits, or the cause it could not derive them.
//
// Go is the one toolchain with an extractor, recognised by a go.mod at the root
// of the checkout; every other build is could not derive for every screen,
// naming that no extractor covers it.
func DeriveTransitions(dir string, inForce []Machine, extractor Extractor) Derivation {
	derived := Derivation{Extractor: extractor}
	_, err := os.Stat(filepath.Join(dir, "go.mod"))
	covered := err == nil
	for _, m := range inForce {
		if !covered {
			derived.Screens = append(derived.Screens,
				ScreenDerivation{Screen: m.Screen, Cause: CauseNoExtractor})
			continue
		}
		derived.Screens = append(derived.Screens, deriveScreen(dir, m.Screen))
	}
	return derived
}

// deriveScreen is one screen: its file, its transition function, and what that
// function admits.
func deriveScreen(dir, screen string) ScreenDerivation {
	path := filepath.Join(dir, FileName(screen))
	if _, err := os.Stat(path); err != nil {
		return failed(screen, fmt.Sprintf("the checkout holds no %s", FileName(screen)))
	}
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return failed(screen, fmt.Sprintf("%s does not parse: %v", FileName(screen), err))
	}
	fn := transitionFunctionIn(parsed)
	if fn == nil {
		return failed(screen, fmt.Sprintf("%s declares no func %s(from, event string) string",
			FileName(screen), transitionFunc))
	}
	from, event, ok := parameterNames(fn)
	if !ok {
		return failed(screen, fmt.Sprintf("%s's %s does not take two string parameters",
			FileName(screen), transitionFunc))
	}

	read := &reader{fset: fset, file: FileName(screen), from: from, event: event}
	read.body(fn.Body)
	if len(read.constructs) > 0 {
		return ScreenDerivation{Screen: screen, Cause: CauseConstructNotFollowed, Constructs: read.constructs}
	}
	return ScreenDerivation{Screen: screen, Transitions: read.transitions}
}

// failed is a could-not-derive record for an extraction that ran and failed on
// one screen, with what the extractor reported.
func failed(screen, reported string) ScreenDerivation {
	return ScreenDerivation{Screen: screen, Cause: CauseExtractionFailed, Reported: reported}
}

// transitionFunctionIn is the screen's transition function, and nil where the
// file declares none.
func transitionFunctionIn(parsed *ast.File) *ast.FuncDecl {
	for _, decl := range parsed.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if ok && fn.Recv == nil && fn.Name != nil && fn.Name.Name == transitionFunc && fn.Body != nil {
			return fn
		}
	}
	return nil
}

// parameterNames is the names of the two parameters the transition function
// takes, in order, and false where it does not take two.
func parameterNames(fn *ast.FuncDecl) (string, string, bool) {
	var names []string
	if fn.Type.Params == nil {
		return "", "", false
	}
	for _, field := range fn.Type.Params.List {
		for _, ident := range field.Names {
			names = append(names, ident.Name)
		}
	}
	if len(names) != 2 {
		return "", "", false
	}
	return names[0], names[1], true
}

// reader is one pass over one transition function: what it admits, and what it
// does that this extractor cannot follow.
type reader struct {
	fset        *token.FileSet
	file        string
	from, event string
	transitions []DerivedTransition
	constructs  []string
}

// cannotFollow records one construct the extractor met and could not follow,
// once however many times it met it.
func (r *reader) cannotFollow(at ast.Node, what string) {
	where := fmt.Sprintf("%s:%d — %s", r.file, r.fset.Position(at.Pos()).Line, what)
	if !slices.Contains(r.constructs, where) {
		r.constructs = append(r.constructs, where)
	}
}

// body reads the transition function's own statements: the switch on the state,
// and a trailing return of a parameter, which is the fall-through and admits
// nothing.
func (r *reader) body(block *ast.BlockStmt) {
	for _, statement := range block.List {
		switch s := statement.(type) {
		case *ast.SwitchStmt:
			r.states(s)
		case *ast.ReturnStmt:
			if len(s.Results) == 1 {
				if _, isIdent := s.Results[0].(*ast.Ident); isIdent {
					continue
				}
			}
			r.cannotFollow(s, "a destination returned for every state and event at once")
		default:
			r.cannotFollow(statement, "a statement beside the switch on the state")
		}
	}
}

// states reads the outer switch: its tag is the state, and each case is one
// state written as a string literal.
func (r *reader) states(outer *ast.SwitchStmt) {
	if outer.Init != nil || !isIdent(outer.Tag, r.from) {
		r.cannotFollow(outer, "a switch on something other than the state")
		return
	}
	for _, statement := range outer.Body.List {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			continue
		}
		if len(clause.List) == 0 {
			r.cannotFollow(clause, "a default case, which stands for every state the machine does not name")
			continue
		}
		states, ok := r.literals(clause)
		if !ok {
			continue
		}
		if len(clause.Body) != 1 {
			r.cannotFollow(clause, "a case holding something beside the switch on the event")
			continue
		}
		inner, ok := clause.Body[0].(*ast.SwitchStmt)
		if !ok {
			r.cannotFollow(clause.Body[0], "a case holding something beside the switch on the event")
			continue
		}
		r.events(states, inner)
	}
}

// events reads the inner switch for one or more states: its tag is the event,
// and each case returns the destination as a string literal.
func (r *reader) events(states []string, inner *ast.SwitchStmt) {
	if inner.Init != nil || !isIdent(inner.Tag, r.event) {
		r.cannotFollow(inner, "a switch on something other than the event")
		return
	}
	for _, statement := range inner.Body.List {
		clause, ok := statement.(*ast.CaseClause)
		if !ok {
			continue
		}
		if len(clause.List) == 0 {
			r.cannotFollow(clause, "a default case, which stands for every event the machine does not name")
			continue
		}
		events, ok := r.literals(clause)
		if !ok {
			continue
		}
		if len(clause.Body) != 1 {
			r.cannotFollow(clause, "a case doing something beside returning the destination")
			continue
		}
		returned, ok := clause.Body[0].(*ast.ReturnStmt)
		if !ok || len(returned.Results) != 1 {
			r.cannotFollow(clause.Body[0], "a case doing something beside returning the destination")
			continue
		}
		to, ok := stringLiteral(returned.Results[0])
		if !ok {
			r.cannotFollow(returned, "a destination this extractor cannot read off the source")
			continue
		}
		for _, from := range states {
			for _, event := range events {
				r.transitions = append(r.transitions, DerivedTransition{From: from, Event: event, To: to})
			}
		}
	}
}

// literals is a case clause's values as string literals, and false where any of
// them is not one — a case on a constant, on a variable, or on a call.
func (r *reader) literals(clause *ast.CaseClause) ([]string, bool) {
	var values []string
	for _, expression := range clause.List {
		value, ok := stringLiteral(expression)
		if !ok {
			r.cannotFollow(expression, "a case value this extractor cannot read off the source")
			return nil, false
		}
		values = append(values, value)
	}
	return values, true
}

// isIdent reports whether the expression is exactly the named identifier.
func isIdent(expression ast.Expr, name string) bool {
	ident, ok := expression.(*ast.Ident)
	return ok && ident.Name == name
}

// stringLiteral is the text of a string literal, and false for anything else.
// The quotes are stripped by hand rather than through strconv, because a
// destination is a plain word and an escape in one is a name this extractor
// would be guessing at.
func stringLiteral(expression ast.Expr) (string, bool) {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING || len(literal.Value) < 2 {
		return "", false
	}
	text := literal.Value
	if text[0] != '"' || text[len(text)-1] != '"' {
		return "", false
	}
	trimmed := text[1 : len(text)-1]
	for _, r := range trimmed {
		if r == '\\' {
			return "", false
		}
	}
	return trimmed, true
}
