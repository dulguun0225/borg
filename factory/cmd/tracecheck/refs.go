package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Reference is one target named from inside the tree: File and Line are
// where it was found, and Target is the text named, path and anchor
// together, exactly as written.
type Reference struct {
	File   string
	Line   int
	Target string
}

var (
	linkPattern    = regexp.MustCompile(`\]\(([^)]+)\)`)
	pathPattern    = regexp.MustCompile(`[\w./-]+\.md(#[\w-]*)?`)
	headingPattern = regexp.MustCompile(`^#{1,3} (.*)$`)
)

// ExtractFile reads every reference out of one file's content, line by
// line. In a .go file only a line that is a comment on its own —
// trimmed, it starts with "//" — is read, so a generic function's
// "Name[T any](" and a "postgres://" URL never reach the patterns below;
// in a .md file every line is read.
func ExtractFile(file string, content []byte) []Reference {
	goSource := filepath.Ext(file) == ".go"

	var refs []Reference
	for i, line := range lines(content) {
		if goSource && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		refs = append(refs, extractLine(file, i+1, line)...)
	}
	return refs
}

// lines splits content on newlines. It is here rather than a bufio.Scanner
// because this repository hard-wraps nothing outside its instruction files —
// one paragraph is one line, and the longest in the tree is already 4KB — and
// a Scanner stops at a line longer than 64KB. Stopping is what makes that a
// defect worth avoiding rather than a limit worth stating: a check that reads
// half a file and reports nothing passes silently, which is the one way this
// command can be wrong and useless at once.
func lines(content []byte) []string {
	split := strings.Split(string(content), "\n")
	for i, line := range split {
		split[i] = strings.TrimSuffix(line, "\r")
	}
	return split
}

// span is a byte range within one line, used to keep a bare-path match
// from double-reading text a Markdown link's target already covered.
type span struct{ start, end int }

func extractLine(file string, lineNum int, line string) []Reference {
	var refs []Reference
	var covered []span

	for _, m := range linkPattern.FindAllStringSubmatchIndex(line, -1) {
		covered = append(covered, span{m[0], m[1]})
		target := line[m[2]:m[3]]
		if target == "" || strings.HasPrefix(target, "#") ||
			strings.HasPrefix(target, "http:") || strings.HasPrefix(target, "https:") {
			continue
		}
		refs = append(refs, Reference{File: file, Line: lineNum, Target: target})
	}

	for _, m := range pathPattern.FindAllStringIndex(line, -1) {
		if withinAny(m[0], m[1], covered) {
			continue
		}
		target := line[m[0]:m[1]]
		path, _, _ := splitTarget(target)
		if !strings.Contains(path, "/") {
			continue
		}
		refs = append(refs, Reference{File: file, Line: lineNum, Target: target})
	}
	return refs
}

func withinAny(start, end int, covered []span) bool {
	for _, c := range covered {
		if start < c.end && end > c.start {
			return true
		}
	}
	return false
}

// splitTarget separates a target into its path and, where it has one, its
// anchor.
func splitTarget(target string) (path, anchor string, hasAnchor bool) {
	if i := strings.IndexByte(target, '#'); i >= 0 {
		return target[:i], target[i+1:], true
	}
	return target, "", false
}

// Check resolves every reference against the filesystem and returns what
// does not hold, one line per defect, in the order the references were
// found.
func Check(refs []Reference) []string {
	var found []string
	slugCache := make(map[string]map[string]bool)

	for _, ref := range refs {
		path, anchor, hasAnchor := splitTarget(ref.Target)
		resolved := filepath.Join(filepath.Dir(ref.File), path)

		info, err := os.Stat(resolved)
		if err != nil {
			found = append(found, fmt.Sprintf("%s:%d: %s resolves to %s, which does not exist", ref.File, ref.Line, ref.Target, resolved))
			continue
		}
		if !hasAnchor || info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".md") {
			continue
		}

		slugs, cached := slugCache[resolved]
		if !cached {
			var err error
			slugs, err = headingSlugs(resolved)
			if err != nil {
				found = append(found, fmt.Sprintf("%s:%d: reading %s: %v", ref.File, ref.Line, resolved, err))
				continue
			}
			slugCache[resolved] = slugs
		}
		if !slugs[anchor] {
			found = append(found, fmt.Sprintf("%s:%d: %s names no heading matching #%s in %s", ref.File, ref.Line, ref.Target, anchor, resolved))
		}
	}
	return found
}

// headingSlugs reads a Markdown file and returns the slug of every heading
// it has.
func headingSlugs(path string) (map[string]bool, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	slugs := make(map[string]bool)
	for _, line := range lines(content) {
		m := headingPattern.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		slugs[slugify(m[1])] = true
	}
	return slugs, nil
}

// slugify turns a heading into its anchor: lowercase, every letter, digit,
// space, and hyphen kept, everything else dropped, and each space turned
// to a hyphen.
func slugify(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}
