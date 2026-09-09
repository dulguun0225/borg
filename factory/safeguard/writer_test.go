package safeguard_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestOnlyTheWriterWrites is a static check that C2210 holds: the record's
// one writer is [safeguard.Writer], reached through no other path. It parses
// this package's own source and fails if Insert, InsertWithdrawal or
// ApproveWithdrawal is declared as a package-level function rather than a
// method of *Writer — the shape a package function of the same name and a
// method can otherwise both take without a build ever refusing it.
func TestOnlyTheWriterWrites(t *testing.T) {
	writes := map[string]bool{"Insert": true, "InsertWithdrawal": true, "ApproveWithdrawal": true}

	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", nil, 0)
	if err != nil {
		t.Fatalf("parsing package safeguard: %v", err)
	}
	pkg, ok := pkgs["safeguard"]
	if !ok {
		t.Fatalf("no package named safeguard in .; found %v", pkgs)
	}
	for name, file := range pkg.Files {
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil {
				// fn.Recv != nil is a method, which is the one path this
				// record's write is reached through.
				continue
			}
			if writes[fn.Name.Name] {
				t.Errorf("%s declares %s as a package-level function; the record's one writer is *Writer",
					name, fn.Name.Name)
			}
		}
	}
}
