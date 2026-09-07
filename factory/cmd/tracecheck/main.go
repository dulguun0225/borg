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

func run() error {
	files, err := walkFiles(".")
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}

	var refs []Reference
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("tracecheck: %w", err)
		}
		refs = append(refs, ExtractFile(file, content)...)
	}

	var findings []string
	findings = append(findings, Check(refs)...)
	for _, file := range Uncited(files, refs) {
		if isScreenReadme(file) {
			findings = append(findings, fmt.Sprintf("%s: a screen README carrying no reference at all", file))
			continue
		}
		findings = append(findings, fmt.Sprintf("%s: a doc.go carrying no reference at all", file))
	}
	for _, path := range MissingScreenReadmes(".", files) {
		findings = append(findings, fmt.Sprintf("%s: a screen README that does not exist", path))
	}
	if len(findings) == 0 {
		return nil
	}
	return errors.New("tracecheck: a reference points at nothing, a doc.go or a screen README carries none, or a screen README does not exist:\n\t" + strings.Join(findings, "\n\t"))
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
			if d.Name() == ".git" || d.Name() == "node_modules" {
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
