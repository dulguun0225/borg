package consumercontract

import (
	"fmt"
	"go/ast"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// A Go package below the root is outside the published mirror convention. Do
// not silently certify the root's empty reading as the whole build's reading.
func rejectNestedSource(root string) error {
	return filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "vendor" || entry.Name() == "testdata") {
			return filepath.SkipDir
		}
		if !entry.IsDir() && strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			return fmt.Errorf("Go source %s is outside the root-only mirror convention", path)
		}
		return nil
	})
}

// walkNode tracks lexical scopes and visits assignment values before changing
// bindings. An unknown assignment invalidates its old trace; shadowing never
// attributes an unrelated receiver to the outer variable's mirror.
func (s *consumerSource) walkNode(node ast.Node, file string, varTypes map[string]string, returns map[string]string) {
	s.walkScoped(node, file, varTypes, map[string]string{}, returns)
}

func (s *consumerSource) walkScoped(node ast.Node, file string, types, tainted, returns map[string]string) {
	ast.Inspect(node, func(current ast.Node) bool {
		if current == nil {
			return false
		}
		walk := func(child ast.Node) { s.walkScoped(child, file, types, tainted, returns) }
		switch n := current.(type) {
		case *ast.BlockStmt:
			local, flow := cloneTypes(types), cloneTypes(tainted)
			for _, statement := range n.List {
				s.walkScoped(statement, file, local, flow, returns)
			}
			return false
		case *ast.CaseClause:
			local, flow := cloneTypes(types), cloneTypes(tainted)
			for _, expression := range n.List {
				s.walkScoped(expression, file, local, flow, returns)
			}
			for _, statement := range n.Body {
				s.walkScoped(statement, file, local, flow, returns)
			}
			return false
		case *ast.FuncLit:
			local := cloneTypes(types)
			addFieldTypes(local, n.Type.Params.List)
			s.walkScoped(n.Body, file, local, cloneTypes(tainted), returns)
			return false
		case *ast.IfStmt:
			local, flow := cloneTypes(types), cloneTypes(tainted)
			if n.Init != nil {
				s.walkScoped(n.Init, file, local, flow, returns)
			}
			s.walkScoped(n.Cond, file, local, flow, returns)
			s.walkScoped(n.Body, file, local, flow, returns)
			if n.Else != nil {
				s.walkScoped(n.Else, file, local, flow, returns)
			}
			// A prior address trace remains possible after a conditional branch.
			return false
		case *ast.ForStmt:
			local, flow := cloneTypes(types), cloneTypes(tainted)
			// A loop hides nothing: its body is walked in its own scope, and a
			// read of a mirror's field inside it is the read it would be outside.
			for _, child := range []ast.Node{n.Init, n.Cond, n.Body, n.Post} {
				if child != nil {
					s.walkScoped(child, file, local, flow, returns)
				}
			}
			return false
		case *ast.RangeStmt:
			walk(n.X)
			local := cloneTypes(types)
			// The bindings are followed only where they hide nothing this walk
			// tracks: ranging over a tracked value binds its elements under names
			// the walk cannot type, and a binding that shadows a tracked name
			// takes that name's reads out of view.
			_, overTracked := receiverType(n.X, types)
			for _, binding := range []ast.Expr{n.Key, n.Value} {
				if ident, ok := binding.(*ast.Ident); ok {
					if _, shadows := local[ident.Name]; shadows || overTracked {
						s.cannotFollow(fmt.Sprintf("range bindings in %s", file))
					}
					delete(local, ident.Name)
				}
			}
			s.walkScoped(n.Body, file, local, cloneTypes(tainted), returns)
			return false
		case *ast.SwitchStmt:
			local, flow := cloneTypes(types), cloneTypes(tainted)
			for _, child := range []ast.Node{n.Init, n.Tag, n.Body} {
				if child != nil {
					s.walkScoped(child, file, local, flow, returns)
				}
			}
			return false
		case *ast.TypeSwitchStmt:
			local, flow := cloneTypes(types), cloneTypes(tainted)
			for _, child := range []ast.Node{n.Init, n.Assign, n.Body} {
				if child != nil {
					s.walkScoped(child, file, local, flow, returns)
				}
			}
			// The binding is followed unless the value switched on is one this
			// walk tracks, whose reads the binding's own name would hide.
			if _, tracked := receiverType(typeSwitched(n.Assign), types); tracked {
				s.cannotFollow(fmt.Sprintf("type-switch bindings in %s", file))
			}
			return false
		case *ast.IncDecStmt:
			if selector, ok := n.X.(*ast.SelectorExpr); ok {
				if typ, ok := receiverType(selector.X, types); ok {
					s.recordWrite(typ+"."+selector.Sel.Name, nil)
				}
			}

		case *ast.ValueSpec:
			for _, value := range n.Values {
				walk(value)
			}
			for i, name := range n.Names {
				delete(types, name.Name)
				delete(tainted, name.Name)
				if typ, ok := identName(n.Type); ok {
					types[name.Name] = typ
				}
				if i < len(n.Values) {
					s.bind(name.Name, n.Values[i], types, tainted, returns)
				}
			}
			return false
		case *ast.AssignStmt:
			for _, value := range n.Rhs {
				walk(value)
			}
			oldTypes, oldFlow := cloneTypes(types), cloneTypes(tainted)
			for i, target := range n.Lhs {
				var value ast.Expr
				if i < len(n.Rhs) && len(n.Lhs) == len(n.Rhs) {
					value = n.Rhs[i]
				}
				switch target := target.(type) {
				case *ast.Ident:
					delete(types, target.Name)
					delete(tainted, target.Name)
					if value != nil {
						typ, found := typeOfExpr(value, returns)
						if ident, ok := value.(*ast.Ident); ok {
							typ, found = oldTypes[ident.Name]
						}
						if found {
							types[target.Name] = typ
						}
						if reason, ok := s.addressFlowsFrom(value, oldTypes); ok {
							tainted[target.Name] = reason
						}
						if ident, ok := value.(*ast.Ident); ok && oldFlow[ident.Name] != "" {
							tainted[target.Name] = oldFlow[ident.Name]
						}
					}
				case *ast.SelectorExpr:
					if typ, ok := receiverType(target.X, oldTypes); ok {
						key := typ + "." + target.Sel.Name
						if n.Tok != token.ASSIGN && n.Tok != token.DEFINE {
							value = nil
							s.reads[key] = true
						}
						s.recordWrite(key, value)
					} else {
						s.cannotFollow(fmt.Sprintf("an untraced write to %s in %s", target.Sel.Name, file))
					}
					walk(target.X)
				default:
					walk(target)
				}
			}
			return false
		case *ast.CompositeLit:
			typ, hasType := identName(n.Type)
			for _, element := range n.Elts {
				if pair, ok := element.(*ast.KeyValueExpr); ok {
					if key, ok := pair.Key.(*ast.Ident); ok && hasType {
						s.recordWrite(typ+"."+key.Name, pair.Value)
					}
					walk(pair.Value)
				} else {
					if hasType {
						s.cannotFollow(fmt.Sprintf("a positional composite literal of %s in %s", typ, file))
					}
					walk(element)
				}
			}
			return false
		case *ast.CallExpr:
			if selector, ok := n.Fun.(*ast.SelectorExpr); ok {
				if typ, ok := receiverType(selector.X, types); ok {
					s.calls[typ+"."+selector.Sel.Name] = true
				}
				s.checkDirectCall(n, selector, file, tainted)
				s.checkReflection(selector, file)
				walk(selector.X)
			} else if ident, ok := n.Fun.(*ast.Ident); ok {
				if ident.Obj == nil || ident.Obj.Kind == ast.Fun {
					s.calls[ident.Name] = true
				}
			} else {
				walk(n.Fun)
			}
			for _, arg := range n.Args {
				walk(arg)
			}
			return false
		case *ast.SelectorExpr:
			s.checkReflection(n, file)
			if typ, ok := receiverType(n.X, types); ok {
				s.reads[typ+"."+n.Sel.Name] = true
			} else if ident, ok := n.X.(*ast.Ident); !ok || s.imports[ident.Name] == "" {
				s.cannotFollow(fmt.Sprintf("an untraced read of %s in %s", n.Sel.Name, file))
			}
		case *ast.IndexExpr:
			if key, ok := n.Index.(*ast.BasicLit); ok && key.Kind == token.STRING {
				if name, err := strconv.Unquote(key.Value); err == nil {
					s.cannotFollow(fmt.Sprintf("a string-keyed access to %q in %s", name, file))
				}
			}
			if call, ok := n.Index.(*ast.CallExpr); ok {
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					if pkg, ok := selector.X.(*ast.Ident); ok && configRead(s.imports[pkg.Name], selector.Sel.Name) {
						s.cannotFollow(fmt.Sprintf("a mapping read from configuration in %s", file))
					}
				}
			}
		}
		return true
	})
}

func (s *consumerSource) bind(name string, value ast.Expr, types, tainted, returns map[string]string) {
	if typ, ok := typeOfExpr(value, returns); ok {
		types[name] = typ
	}
	if reason, ok := s.addressFlowsFrom(value, types); ok {
		tainted[name] = reason
	}
}

func (s *consumerSource) checkReflection(selector *ast.SelectorExpr, file string) {
	if ident, ok := selector.X.(*ast.Ident); ok && ident.Obj == nil && s.imports[ident.Name] == "reflect" {
		s.cannotFollow(fmt.Sprintf("a read through reflection in %s", file))
	}
}

func (s *consumerSource) recordWrite(key string, value ast.Expr) {
	s.writes[key] = true
	s.values[key] = append(s.values[key], value)
}

// typeSwitched is the expression a type switch asserts on: x in both
// `switch v := x.(type)` and `switch x.(type)`, and nil where the statement
// takes neither shape.
func typeSwitched(assign ast.Stmt) ast.Expr {
	var expr ast.Expr
	switch a := assign.(type) {
	case *ast.AssignStmt:
		if len(a.Rhs) == 1 {
			expr = a.Rhs[0]
		}
	case *ast.ExprStmt:
		expr = a.X
	}
	if assertion, ok := expr.(*ast.TypeAssertExpr); ok {
		return assertion.X
	}
	return nil
}
