package consumercontract

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/dulguun0225/borg/factory/contract"
)

// mirrorMeta is the unit each of a mirror's fields asserts, by the element name
// the form gives it; the return type of each exported operation with exactly
// one plain result, by the operation's own name; and the receiver type of each
// exported operation declared as a method, by the operation's own name. The
// unit is the one thing a form does not carry — it belongs to an element's
// name — so it is read off the mirror's own tags; the return type is what
// pairs a value the source binds to a call's result with the mirror the call
// reaches, and the receiver type is what pairs a call made through it with
// this mirror rather than another declaring the same operation name, since a
// form does not carry either.
func mirrorMeta(path string) (units map[string]string, returns map[string]string, receivers map[string]string, err error) {
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("%s does not parse: %v", path, err)
	}
	units = map[string]string{}
	returns = map[string]string{}
	receivers = map[string]string{}
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
			if !d.Name.IsExported() {
				continue
			}
			if d.Recv != nil && len(d.Recv.List) == 1 {
				recvType := d.Recv.List[0].Type
				if star, ok := recvType.(*ast.StarExpr); ok {
					recvType = star.X
				}
				if ident, ok := recvType.(*ast.Ident); ok {
					receivers[d.Name.Name] = ident.Name
				}
			}
			if d.Type.Results == nil || len(d.Type.Results.List) != 1 {
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
	return units, returns, receivers, nil
}
