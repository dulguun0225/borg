package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The fixture below is built by concatenation rather than as one raw
// string so that no physical line of this test's own source begins with
// "//" — if it did, tracecheck's own self-check would read the fixture as
// a real comment in refs_test.go and try to resolve it from here rather
// than from the fake "pkg/doc.go" the test means it for.
func TestExtractFileFindsBothKinds(t *testing.T) {
	content := []byte("// See [the record](../../README.md#running-it) and\n" +
		"// ../../roadmap.md#m1--one-change-ships directly, but not\n" +
		"// [an anchor](#same-file) or [openrouter](https://openrouter.ai/keys),\n" +
		"// and not a bare mention of CLAUDE.md with no slash.\n")
	refs := ExtractFile("pkg/doc.go", content)

	var targets []string
	for _, ref := range refs {
		targets = append(targets, ref.Target)
	}
	want := []string{
		"../../README.md#running-it",
		"../../roadmap.md#m1--one-change-ships",
	}
	for _, w := range want {
		if !slices.Contains(targets, w) {
			t.Errorf("ExtractFile found %v, want it to contain %q", targets, w)
		}
	}
	if slices.Contains(targets, "CLAUDE.md") {
		t.Errorf("ExtractFile found %v, want no bare mention with no slash", targets)
	}
	if slices.ContainsFunc(targets, func(s string) bool { return s == "#same-file" || s == "https://openrouter.ai/keys" }) {
		t.Errorf("ExtractFile found %v, want no bare anchor or http(s) target", targets)
	}
}

func TestExtractLineDoesNotDoubleCountAMarkdownLinkTarget(t *testing.T) {
	refs := extractLine("README.md", 1, "See [`../roadmap.md`](../roadmap.md#m1--one-change-ships).")
	count := 0
	for _, ref := range refs {
		if ref.Target == "../roadmap.md#m1--one-change-ships" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("extractLine found the link target %d times, want 1", count)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Code":                          "code",
		"M1 — One change ships":         "m1--one-change-ships",
		"What defines it: seam 1":       "what-defines-it-seam-1",
		"Already-hyphenated, and comma": "already-hyphenated-and-comma",
	}
	for heading, want := range cases {
		if got := slugify(heading); got != want {
			t.Errorf("slugify(%q) = %q, want %q", heading, got, want)
		}
	}
}

func TestCheckAcceptsAWorkingTree(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "guide.md"), "# Code\n\nSome text.\n\n## A section\n")
	mustWrite(t, filepath.Join(dir, "pkg", "doc.go"), "// nothing\n")

	refs := []Reference{
		{File: filepath.Join(dir, "pkg", "doc.go"), Line: 1, Target: "../guide.md#code"},
		{File: filepath.Join(dir, "pkg", "doc.go"), Line: 1, Target: "../guide.md#a-section"},
		{File: filepath.Join(dir, "pkg", "doc.go"), Line: 1, Target: "../"},
	}
	if found := Check(refs); len(found) != 0 {
		t.Fatalf("Check found %v, want nothing", found)
	}
}

func TestCheckFindsWhatDoesNotHold(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "guide.md"), "# Code\n")
	mustWrite(t, filepath.Join(dir, "pkg", "doc.go"), "// nothing\n")

	cases := map[string]struct {
		ref  Reference
		want string
	}{
		"a missing file": {
			ref:  Reference{File: filepath.Join(dir, "pkg", "doc.go"), Line: 3, Target: "../nowhere.md"},
			want: "does not exist",
		},
		"an anchor matching no heading": {
			ref:  Reference{File: filepath.Join(dir, "pkg", "doc.go"), Line: 5, Target: "../guide.md#no-such-heading"},
			want: "names no heading matching",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			found := Check([]Reference{c.ref})
			if len(found) != 1 {
				t.Fatalf("Check found %v, want exactly one defect", found)
			}
			if !strings.Contains(found[0], c.want) {
				t.Fatalf("Check found %q, want it to contain %q", found[0], c.want)
			}
		})
	}
}

func TestCheckSkipsTheAnchorOnANonMarkdownTarget(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "deps.txt"), "record\n")
	mustWrite(t, filepath.Join(dir, "pkg", "doc.go"), "// nothing\n")

	refs := []Reference{
		{File: filepath.Join(dir, "pkg", "doc.go"), Line: 1, Target: "../deps.txt#anything"},
	}
	if found := Check(refs); len(found) != 0 {
		t.Fatalf("Check found %v, want nothing — a non-Markdown target's anchor is not checked", found)
	}
}

func TestUncitedFindsADocGoWithNoReference(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "pkg", "doc.go"), "package pkg\n")
	mustWrite(t, filepath.Join(dir, "other", "doc.go"), "package other\n")
	mustWrite(t, filepath.Join(dir, "other", "file.go"), "package other\n")

	files := []string{
		filepath.Join(dir, "pkg", "doc.go"),
		filepath.Join(dir, "other", "doc.go"),
		filepath.Join(dir, "other", "file.go"),
	}
	refs := []Reference{
		{File: filepath.Join(dir, "pkg", "doc.go"), Line: 1, Target: "../guide.md"},
	}

	found := Uncited(files, refs)
	want := []string{filepath.Join(dir, "other", "doc.go")}
	if !slices.Equal(found, want) {
		t.Fatalf("Uncited found %v, want %v — a doc.go that contributed a reference passes, a doc.go with none is reported, and a non-doc.go file with none stays passing", found, want)
	}
}

func TestUncitedOrdersFindingsAsFilesGivesThem(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "b", "doc.go"), "package b\n")
	mustWrite(t, filepath.Join(dir, "a", "doc.go"), "package a\n")

	files := []string{
		filepath.Join(dir, "b", "doc.go"),
		filepath.Join(dir, "a", "doc.go"),
	}

	found := Uncited(files, nil)
	want := []string{
		filepath.Join(dir, "b", "doc.go"),
		filepath.Join(dir, "a", "doc.go"),
	}
	if !slices.Equal(found, want) {
		t.Fatalf("Uncited found %v, want %v in the order files gives them", found, want)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}
