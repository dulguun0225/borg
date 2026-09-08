package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

const (
	endGoalDir  = "../end-goal"
	claimsPath  = "../end-goal/claims.txt"
	roadmapPath = "../roadmap.md"
)

func run() error {
	files, err := walkFiles(".")
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}

	designNames, err := designFileNames(endGoalDir)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}

	var refs []Reference
	var cites []Citation
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("tracecheck: %w", err)
		}
		refs = append(refs, ExtractFile(file, content, designNames)...)
		cites = append(cites, ExtractCitations(file, content)...)
	}

	claimsContent, err := os.ReadFile(claimsPath)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	roadmap, err := os.ReadFile(roadmapPath)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	claims, findings := ParseClaims(claimsContent)

	findings = append(findings, Check(refs)...)
	findings = append(findings, CheckClaims(claims, endGoalDir, roadmap)...)
	findings = append(findings, CheckCitations(claims, cites)...)
	coverage, err := CheckCoverage(claims, endGoalDir)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	findings = append(findings, coverage...)
	for _, file := range Unclaimed(files, cites) {
		findings = append(findings, fmt.Sprintf("%s: cites no claim from claims.txt", file))
	}
	for _, file := range Uncited(files, refs) {
		if !isCommand(file) {
			continue
		}
		findings = append(findings, fmt.Sprintf("%s: a command's doc.go carrying no reference at all", file))
	}
	for _, path := range MissingScreenReadmes(".", files) {
		findings = append(findings, fmt.Sprintf("%s: a screen README that does not exist", path))
	}

	dirs, err := PackageDirs(".")
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	phrases, err := DesignPhrases(endGoalDir)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	findings = append(findings, CheckPackageNames(dirs, phrases)...)

	if len(findings) == 0 {
		return nil
	}
	return errors.New("tracecheck: the code and the design do not hold to each other:\n\t" + strings.Join(findings, "\n\t"))
}

// designFileNames walks root and returns the base name of every .md file
// under it, other than README.md and CLAUDE.md, which exist across the
// repository rather than naming one design file. It is built once and
// passed into ExtractFile so a bare unslashed mention of one of these names
// is read as a reference.
func designFileNames(root string) (map[string]bool, error) {
	names := make(map[string]bool)
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		names[d.Name()] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	delete(names, "README.md")
	delete(names, "CLAUDE.md")
	return names, nil
}

// walkFiles returns every *.go and *.md file under root, in the order
// filepath.WalkDir visits them.
func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			// node_modules is the client's package tree, whose Markdown is
			// not this repository's and whose links point at nothing here.
			// test-results is the browser run's output, written by a failing
			// run and never committed, and its Markdown is the same kind of
			// thing: not this repository's, and pointing at nothing here.
			if d.Name() == ".git" || d.Name() == "node_modules" || d.Name() == "test-results" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".md":
			files = append(files, path)
		}
		return nil
	})
	return files, err
}
