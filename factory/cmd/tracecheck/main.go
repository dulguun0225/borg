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
		findings = append(findings, fmt.Sprintf("%s: a doc.go carrying no reference at all", file))
	}
	if len(findings) == 0 {
		return nil
	}
	return errors.New("tracecheck: a reference points at nothing, or a doc.go carries none:\n\t" + strings.Join(findings, "\n\t"))
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
			if d.Name() == ".git" {
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
