package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

// rulesFile is where the allowed graph is, relative to the working directory.
const rulesFile = "deps.txt"

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	file, err := os.Open(rulesFile)
	if err != nil {
		return fmt.Errorf("depscheck: %w", err)
	}
	defer file.Close()

	rules, err := ParseRules(file)
	if err != nil {
		return fmt.Errorf("depscheck: %w", err)
	}

	packages, err := listPackages()
	if err != nil {
		return fmt.Errorf("depscheck: %w", err)
	}

	found := Check(rules, packages)
	if len(found) == 0 {
		return nil
	}
	return errors.New("depscheck: the build and deps.txt disagree:\n\t" + strings.Join(found, "\n\t"))
}

// listed is the part of "go list -json" this command reads.
type listed struct {
	ImportPath   string
	Module       struct{ Path string }
	Imports      []string
	TestImports  []string
	XTestImports []string
}

// listPackages asks the build what every package of this module imports. It
// runs go list rather than parsing the source, so what is checked is what the
// build actually compiles, build tags and all.
func listPackages() ([]Package, error) {
	command := exec.Command("go", "list", "-deps=false", "-json", "./...")
	command.Stderr = os.Stderr
	out, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("running %s: %w", command, err)
	}

	var packages []Package
	decoder := json.NewDecoder(bytes.NewReader(out))
	for {
		var l listed
		if err := decoder.Decode(&l); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			return nil, fmt.Errorf("reading the output of %s: %w", command, err)
		}
		path, inModule := relative(l.Module.Path, l.ImportPath)
		if !inModule {
			continue
		}
		packages = append(packages, Package{
			Path:        path,
			Imports:     internal(l.Module.Path, path, l.Imports),
			TestImports: internal(l.Module.Path, path, append(append([]string(nil), l.TestImports...), l.XTestImports...)),
		})
	}
	return packages, nil
}

// internal keeps the imports that are packages of this module and drops the
// importing package itself, which an external test package imports as a
// matter of course.
func internal(module, self string, imports []string) []string {
	var kept []string
	for _, imported := range imports {
		path, inModule := relative(module, imported)
		if !inModule || path == self {
			continue
		}
		kept = append(kept, path)
	}
	return kept
}

// relative turns an import path into the path deps.txt writes, which is
// relative to the module.
//
// The module's own path is ".", a package this module does not have and is not
// expected to grow — every package here is a feature slice in a directory. The
// case is handled anyway because the alternative is not an error but a silence:
// a package this function called foreign is skipped rather than reported, so a
// root package added later would have its imports checked by nothing.
func relative(module, path string) (string, bool) {
	if path == module {
		return ".", true
	}
	return strings.CutPrefix(path, module+"/")
}
