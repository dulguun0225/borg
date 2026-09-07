package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestIsScreenReadmeAcceptsTheFourScreens(t *testing.T) {
	dir := t.TempDir()
	for _, screen := range screenNames {
		path := filepath.Join(dir, "client", "src", "app", screen, "README.md")
		if !isScreenReadme(path) {
			t.Errorf("isScreenReadme(%q) = false, want true", path)
		}
	}
}

func TestIsScreenReadmeRejectsAReadmeOutsideTheFour(t *testing.T) {
	dir := t.TempDir()
	cases := []string{
		filepath.Join(dir, "client", "README.md"),
		filepath.Join(dir, "client", "src", "app", "api", "README.md"),
		filepath.Join(dir, "client", "src", "app", "state", "README.md"),
		filepath.Join(dir, "pkg", "README.md"),
		filepath.Join(dir, "client", "src", "app", "work", "doc.go"),
	}
	for _, path := range cases {
		if isScreenReadme(path) {
			t.Errorf("isScreenReadme(%q) = true, want false", path)
		}
	}
}

func TestUncitedFindsAScreenReadmeWithNoReference(t *testing.T) {
	dir := t.TempDir()
	readme := filepath.Join(dir, "client", "src", "app", "work", "README.md")
	mustWrite(t, readme, "# Work\n\nNo reference here.\n")

	files := []string{readme}
	found := Uncited(files, nil)
	want := []string{readme}
	if !slices.Equal(found, want) {
		t.Fatalf("Uncited found %v, want %v — a screen README with no reference is a finding naming the file", found, want)
	}
}

func TestUncitedAcceptsAScreenReadmeWithAResolvingReference(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "end-goal", "screens.md"), "# Screens\n")
	readme := filepath.Join(dir, "client", "src", "app", "work", "README.md")
	content := "# Work\n\n" +
		"## What defines it\n\n" +
		"[Screens](../../../../end-goal/screens.md#screens).\n"
	mustWrite(t, readme, content)

	refs := ExtractFile(readme, []byte(content))
	if len(refs) == 0 {
		t.Fatalf("ExtractFile found no reference in %s, want the Markdown link found", readme)
	}
	if found := Check(refs); len(found) != 0 {
		t.Fatalf("Check found %v, want the reference to resolve", found)
	}
	if found := Uncited([]string{readme}, refs); len(found) != 0 {
		t.Fatalf("Uncited found %v, want a screen README with a resolving reference to pass", found)
	}
}

func TestUncitedIgnoresAReadmeOutsideTheFourScreens(t *testing.T) {
	dir := t.TempDir()
	cases := []string{
		filepath.Join(dir, "client", "README.md"),
		filepath.Join(dir, "client", "src", "app", "api", "README.md"),
	}
	for _, readme := range cases {
		mustWrite(t, readme, "# Not a screen\n\nNo reference here.\n")
		if found := Uncited([]string{readme}, nil); len(found) != 0 {
			t.Errorf("Uncited(%q) found %v, want nothing — only the four screens are held to this rule", readme, found)
		}
	}
}

func TestMissingScreenReadmesFindsAScreenDirectoryWithNone(t *testing.T) {
	dir := t.TempDir()
	// Three of the four screens have a README.md; "people" has none.
	var files []string
	for _, screen := range []string{"work", "ops", "factory"} {
		path := filepath.Join(dir, "client", "src", "app", screen, "README.md")
		mustWrite(t, path, "# "+screen+"\n")
		files = append(files, path)
	}

	found := MissingScreenReadmes(dir, files)
	want := []string{filepath.Join(dir, "client", "src", "app", "people", "README.md")}
	if !slices.Equal(found, want) {
		t.Fatalf("MissingScreenReadmes found %v, want %v", found, want)
	}
}

func TestMissingScreenReadmesFindsNothingWhenAllFourExist(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for _, screen := range screenNames {
		path := filepath.Join(dir, "client", "src", "app", screen, "README.md")
		mustWrite(t, path, "# "+screen+"\n")
		files = append(files, path)
	}

	if found := MissingScreenReadmes(dir, files); len(found) != 0 {
		t.Fatalf("MissingScreenReadmes found %v, want nothing — all four screens have a README.md", found)
	}
}
