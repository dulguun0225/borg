package main

import (
	"path/filepath"
	"slices"
)

// screenNames are the four screens factory/client/src/app/ holds, each named
// in CLAUDE.md's "The map" counterpart for the client: a directory of one of
// these names holds a README.md read as that screen's doc.go — [Unclaimed]
// holds it to citing a claim the way it holds a package doc.go, and
// [MissingScreenReadmes] finds a screen with none at all.
var screenNames = []string{"work", "ops", "factory", "people"}

// isScreenReadme reports whether path is one of the four screen READMEs:
// a README.md directly inside a directory named for one of screenNames,
// itself inside .../client/src/app/. It checks the path's last five
// components rather than the whole path, so it matches a real run's
// relative path and a test's temp-directory-prefixed one alike.
func isScreenReadme(path string) bool {
	if filepath.Base(path) != "README.md" {
		return false
	}
	screenDir := filepath.Dir(path)
	if !slices.Contains(screenNames, filepath.Base(screenDir)) {
		return false
	}
	appDir := filepath.Dir(screenDir)
	if filepath.Base(appDir) != "app" {
		return false
	}
	srcDir := filepath.Dir(appDir)
	if filepath.Base(srcDir) != "src" {
		return false
	}
	return filepath.Base(filepath.Dir(srcDir)) == "client"
}

// MissingScreenReadmes returns, for each screen name with no README.md among
// files, the path tracecheck expected it at: root joined with client, src,
// app, the screen name, and README.md, in that order.
func MissingScreenReadmes(root string, files []string) []string {
	present := make(map[string]bool)
	for _, file := range files {
		if isScreenReadme(file) {
			present[filepath.Clean(file)] = true
		}
	}

	var missing []string
	for _, screen := range screenNames {
		path := filepath.Join(root, "client", "src", "app", screen, "README.md")
		if !present[filepath.Clean(path)] {
			missing = append(missing, path)
		}
	}
	return missing
}
