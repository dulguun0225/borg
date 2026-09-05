package consumercontract

import (
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

// What the consumer's own source does with a mirror, which is the whole of what
// makes a consumer contract derived rather than stated: a name here is a name the
// consumer's code touches, and a mirror field that never appears is a field the
// consumer neither reads nor writes.
//
// It is syntactic. `x.Status` counts wherever it appears and whatever x is, so a
// field name the mirror shares with another type reads as a touch of the mirror's
// — a read invented, which is one of the two blind cases the design names. A read
// through a map key or through reflection appears as no selector at all, which is
// the other, and the two of those this parse can see itself it records as
// constructs it could not follow rather than passing over them silently.

// consumerSource is what one checkout's own source does: the names it reads, the
// names it writes, the operations it calls, and the constructs this extractor met
// and could not follow.
type consumerSource struct {
	reads      map[string]bool
	writes     map[string]bool
	calls      map[string]bool
	unfollowed []string
}

// readSource walks every file at the root of the consumer's repository that is not
// a mirror and not a test, and reports what it does.
func readSource(root string) (consumerSource, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return consumerSource{}, fmt.Errorf("reading the checkout at %s: %v", root, err)
	}
	source := consumerSource{reads: map[string]bool{}, writes: map[string]bool{}, calls: map[string]bool{}}
	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, filepath.Join(root, name), nil, parser.SkipObjectResolution)
		if err != nil {
			// A file that does not parse is not something to read a consumer's
			// assumptions out of, and it is not this extractor's to refuse
			// either: the build has to compile one step earlier, and a mirror
			// that does not parse is refused by name.
			continue
		}
		source.walk(parsed, name)
	}
	slices.Sort(source.unfollowed)
	return source, nil
}

// walk is one file: what it writes, what it calls, what it reads, and what it does
// that this extractor cannot follow.
func (s *consumerSource) walk(parsed *ast.File, file string) {
	written := map[ast.Node]bool{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch n := node.(type) {
		case *ast.AssignStmt:
			for _, target := range n.Lhs {
				if selector, ok := target.(*ast.SelectorExpr); ok && selector.Sel != nil {
					s.writes[selector.Sel.Name] = true
					written[selector] = true
				}
			}
		case *ast.CompositeLit:
			for _, element := range n.Elts {
				pair, ok := element.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				if key, ok := pair.Key.(*ast.Ident); ok {
					s.writes[key.Name] = true
				}
			}
		case *ast.CallExpr:
			if selector, ok := n.Fun.(*ast.SelectorExpr); ok && selector.Sel != nil {
				s.calls[selector.Sel.Name] = true
				written[selector] = true
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
				s.reads[n.Sel.Name] = true
			}
		case *ast.IndexExpr:
			if key, ok := n.Index.(*ast.BasicLit); ok && key.Kind == token.STRING {
				name, err := strconv.Unquote(key.Value)
				if err == nil {
					s.cannotFollow(fmt.Sprintf("a string-keyed access to %q in %s", name, file))
				}
			}
		}
		return true
	})
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
