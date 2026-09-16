package criterion

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

// Emission is the part of a build derivation the emission gate checks: the
// JSON names on the emitted record, the hazardous operations it emits a count
// for, and why that shape could not be derived when it could not be read.
type Emission struct {
	Names               []string
	HazardousOperations []string
	CouldNotDerive      string
}

// EmissionStoppedError is a number the previous build emitted but this build
// stopped emitting.
type EmissionStoppedError struct {
	Name string
}

func (e *EmissionStoppedError) Error() string {
	return fmt.Sprintf("criterion: the build stopped emitting field %q", e.Name)
}

// EmissionUnreadableError is a number the build emits that the shipped health
// monitor shape does not read.
type EmissionUnreadableError struct {
	Name string
}

func (e *EmissionUnreadableError) Error() string {
	return fmt.Sprintf("criterion: the build emission names unreadable field %q", e.Name)
}

// HazardousOperationMissingError is an area operation the build does not
// count in its emission.
type HazardousOperationMissingError struct {
	Operation string
}

func (e *HazardousOperationMissingError) Error() string {
	return fmt.Sprintf("criterion: the build emission does not count hazardous operation %q", e.Operation)
}

// CheckEmission compares the build-derived emission with the previous build's
// emission and the names the health monitor can read. hazardousOperation is
// empty for an area with no such operation.
func CheckEmission(derived Derivation, previous Emission, readableNames []string, hazardousOperation string) error {
	if derived.CouldNotDerive != "" {
		return &CouldNotDeriveError{Reason: derived.CouldNotDerive}
	}
	if derived.Emission.CouldNotDerive != "" {
		return &CouldNotDeriveError{Reason: derived.Emission.CouldNotDerive}
	}
	readable := make(map[string]bool, len(readableNames))
	for _, name := range readableNames {
		readable[name] = true
	}
	declared := make(map[string]bool, len(derived.Emission.Names))
	var defects []error
	for _, name := range derived.Emission.Names {
		if declared[name] {
			continue
		}
		declared[name] = true
		if !readable[name] {
			defects = append(defects, &EmissionUnreadableError{Name: name})
		}
	}
	for _, name := range previous.Names {
		if readable[name] && !declared[name] {
			defects = append(defects, &EmissionStoppedError{Name: name})
		}
	}
	if hazardousOperation != "" && !slices.Contains(derived.Emission.HazardousOperations, hazardousOperation) {
		defects = append(defects, &HazardousOperationMissingError{Operation: hazardousOperation})
	}
	return errors.Join(defects...)
}

func deriveEmission(dir string) (Emission, error) {
	files, err := goFiles(dir)
	if err != nil {
		return Emission{}, err
	}
	parsed := make([]*ast.File, 0, len(files))
	constants := map[string]string{}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return Emission{CouldNotDerive: fmt.Sprintf("could not parse %s: %v", path, err)}, nil
		}
		parsed = append(parsed, file)
		collectStringConstants(file, constants)
	}

	var shapes [][]string
	for _, file := range parsed {
		shapes = append(shapes, emissionShapes(file)...)
	}
	if len(shapes) == 0 {
		return Emission{}, nil
	}
	if len(shapes) > 1 {
		return Emission{CouldNotDerive: "more than one emitted record shape was recognised"}, nil
	}
	result := Emission{Names: shapes[0]}
	for _, file := range parsed {
		for _, operation := range hazardousOperations(file, constants) {
			if !slices.Contains(result.HazardousOperations, operation) {
				result.HazardousOperations = append(result.HazardousOperations, operation)
			}
		}
	}
	return result, nil
}

func goFiles(dir string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("criterion: reading Go files under %s: %w", dir, err)
	}
	return files, nil
}

func collectStringConstants(file *ast.File, constants map[string]string) {
	for _, declaration := range file.Decls {
		gen, ok := declaration.(*ast.GenDecl)
		if !ok || gen.Tok.String() != "const" {
			continue
		}
		for _, spec := range gen.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for n, name := range value.Names {
				if n < len(value.Values) {
					if resolved, ok := stringValue(value.Values[n], constants); ok {
						constants[name.Name] = resolved
					}
				}
			}
		}
	}
}

func emissionShapes(file *ast.File) [][]string {
	var shapes [][]string
	ast.Inspect(file, func(node ast.Node) bool {
		typeSpec, ok := node.(*ast.TypeSpec)
		if !ok {
			return true
		}
		structType, ok := typeSpec.Type.(*ast.StructType)
		if !ok {
			return true
		}
		candidate := jsonNames(structType)
		if !isEmissionRecord(candidate) {
			return true
		}
		shapes = append(shapes, candidate)
		return true
	})
	return shapes
}

func jsonNames(structType *ast.StructType) []string {
	var names []string
	for _, field := range structType.Fields.List {
		if field.Tag == nil {
			continue
		}
		name, ok := jsonName(field.Tag.Value)
		if ok && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

func jsonName(tag string) (string, bool) {
	value, err := strconv.Unquote(tag)
	if err != nil {
		return "", false
	}
	for _, field := range strings.Fields(value) {
		if strings.HasPrefix(field, "json:\"") && strings.HasSuffix(field, "\"") {
			name := strings.TrimSuffix(strings.TrimPrefix(field, "json:\""), "\"")
			return strings.Split(name, ",")[0], true
		}
	}
	return "", false
}

func isEmissionRecord(names []string) bool {
	for _, required := range []string{"version", "kind", "time", "service", "build", "deploy", "target", "operation"} {
		if !slices.Contains(names, required) {
			return false
		}
	}
	return true
}

func hazardousOperations(file *ast.File, constants map[string]string) []string {
	var operations []string
	ast.Inspect(file, func(node ast.Node) bool {
		literal, ok := node.(*ast.CompositeLit)
		if !ok {
			return true
		}
		fields := keyedStrings(literal, constants)
		if fields["kind"] != "hazardous_operation" || fields["operation"] == "" {
			return true
		}
		if !slices.Contains(operations, fields["operation"]) {
			operations = append(operations, fields["operation"])
		}
		return true
	})
	return operations
}

func keyedStrings(literal *ast.CompositeLit, constants map[string]string) map[string]string {
	values := map[string]string{}
	for _, element := range literal.Elts {
		keyed, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key := ""
		switch expression := keyed.Key.(type) {
		case *ast.Ident:
			key = expression.Name
		case *ast.SelectorExpr:
			key = expression.Sel.Name
		}
		if key == "" {
			continue
		}
		if value, ok := stringValue(keyed.Value, constants); ok {
			values[strings.ToLower(key)] = value
		}
	}
	return values
}

func stringValue(expression ast.Expr, constants map[string]string) (string, bool) {
	switch value := expression.(type) {
	case *ast.BasicLit:
		if value.Kind == token.STRING {
			resolved, err := strconv.Unquote(value.Value)
			return resolved, err == nil
		}
	case *ast.Ident:
		resolved, ok := constants[value.Name]
		return resolved, ok
	case *ast.SelectorExpr:
		resolved, ok := constants[value.Sel.Name]
		return resolved, ok
	}
	return "", false
}
