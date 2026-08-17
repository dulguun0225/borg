package main

import (
	"bufio"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
)

// Rules is the graph deps.txt allows: for each package, what it may import,
// and what it may import from its tests alone.
type Rules struct {
	allowed     map[string]map[string]bool
	testAllowed map[string]map[string]bool
}

// Package is one package of this module as the build reports it. Path is
// relative to the module, and both import lists hold paths in the same form,
// with imports outside the module already dropped.
type Package struct {
	Path        string
	Imports     []string
	TestImports []string
}

// ParseRules reads deps.txt. A line is a package and, after "->", the packages
// it may import; a line beginning with "test" states an edge allowed from that
// package's tests alone. Everything from a '#' to the end of a line is a
// comment, and a blank line is skipped.
func ParseRules(r io.Reader) (*Rules, error) {
	rules := &Rules{
		allowed:     make(map[string]map[string]bool),
		testAllowed: make(map[string]map[string]bool),
	}
	scanner := bufio.NewScanner(r)
	for line := 1; scanner.Scan(); line++ {
		text := scanner.Text()
		if i := strings.IndexByte(text, '#'); i >= 0 {
			text = text[:i]
		}
		fields := strings.Fields(text)
		if len(fields) == 0 {
			continue
		}

		test := fields[0] == "test"
		if test {
			fields = fields[1:]
			if len(fields) == 0 {
				return nil, fmt.Errorf("deps.txt line %d: \"test\" names no package", line)
			}
		}
		name := fields[0]
		imports, err := parseImports(fields[1:], name, line)
		if err != nil {
			return nil, err
		}

		into := rules.allowed
		if test {
			into = rules.testAllowed
			if _, listed := rules.allowed[name]; !listed {
				return nil, fmt.Errorf("deps.txt line %d: the test line for %s comes before its own line, or it has none", line, name)
			}
			if len(imports) == 0 {
				return nil, fmt.Errorf("deps.txt line %d: the test line for %s allows nothing", line, name)
			}
		}
		if _, repeated := into[name]; repeated {
			return nil, fmt.Errorf("deps.txt line %d: %s is listed twice", line, name)
		}
		into[name] = imports
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("deps.txt: %w", err)
	}
	return rules, nil
}

func parseImports(fields []string, name string, line int) (map[string]bool, error) {
	imports := make(map[string]bool)
	if len(fields) == 0 {
		return imports, nil
	}
	if fields[0] != "->" {
		return nil, fmt.Errorf("deps.txt line %d: %s is followed by %q, expected \"->\"", line, name, fields[0])
	}
	for _, imported := range fields[1:] {
		imports[imported] = true
	}
	if len(imports) == 0 {
		return nil, fmt.Errorf("deps.txt line %d: %s is followed by \"->\" and nothing", line, name)
	}
	return imports, nil
}

// Check returns every way the packages disagree with the rules, in a stable
// order, and nothing for a tree that agrees.
func Check(rules *Rules, packages []Package) []string {
	var found []string
	built := make(map[string]bool, len(packages))

	for _, pkg := range slices.SortedFunc(slices.Values(packages), func(a, b Package) int {
		return strings.Compare(a.Path, b.Path)
	}) {
		built[pkg.Path] = true
		allowed, listed := rules.allowed[pkg.Path]
		if !listed {
			found = append(found, fmt.Sprintf("%s is not in deps.txt", pkg.Path))
			continue
		}
		for _, imported := range slices.Sorted(slices.Values(pkg.Imports)) {
			if !allowed[imported] {
				found = append(found, fmt.Sprintf("%s imports %s, which deps.txt does not allow", pkg.Path, imported))
			}
		}
		for _, imported := range slices.Sorted(slices.Values(pkg.TestImports)) {
			if !allowed[imported] && !rules.testAllowed[pkg.Path][imported] {
				found = append(found, fmt.Sprintf("the tests of %s import %s, which deps.txt does not allow", pkg.Path, imported))
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(rules.allowed)) {
		if !built[name] {
			found = append(found, fmt.Sprintf("deps.txt lists %s, which is not a package of this module", name))
		}
	}
	return found
}
