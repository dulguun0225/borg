package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

// Reference is one target named from inside end-goal/: File and Line are
// where it was found, and Target is the text named, path and anchor together,
// exactly as written.
type Reference struct {
	File   string
	Line   int
	Target string
}

var (
	linkPattern    = regexp.MustCompile(`\]\(([^)]+)\)`)
	pathPattern    = regexp.MustCompile(`[\w./-]+\.md(#[\w-]*)?`)
	headingPattern = regexp.MustCompile(`^#{1,3} (.*)$`)

	// numberedName is the design's file naming: two digits, a hyphen, and a
	// slug. A bare mention of one with no slash is a path and not prose.
	numberedName = regexp.MustCompile(`^\d\d-`)
)

// unslashedExceptions are the two names excepted from designNames: they exist
// across the repository, not only under end-goal/, so a bare mention of
// either is prose and never a path.
var unslashedExceptions = map[string]bool{
	"README.md": true,
	"CLAUDE.md": true,
}

// ExtractFile reads every reference out of one file's content, line by
// line. In a .go file only a line that is a comment on its own —
// trimmed, it starts with "//" — is read, so a generic function's
// "Name[T any](" and a "postgres://" URL never reach the patterns below;
// in a .md file every line is read. designNames is the base name of every
// .md file under end-goal/, other than README.md and CLAUDE.md: a bare
// unslashed name found there is read as a reference too.
func ExtractFile(file string, content []byte, designNames map[string]bool) []Reference {
	goSource := filepath.Ext(file) == ".go"

	var refs []Reference
	for i, line := range lines(content) {
		if goSource && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		refs = append(refs, extractLine(file, i+1, line, designNames)...)
	}
	return refs
}

// lines splits content on newlines. It is here rather than a bufio.Scanner
// because this repository hard-wraps nothing outside its instruction files —
// one paragraph is one line, and the longest in end-goal/ is already 4KB —
// and a Scanner stops at a line longer than 64KB. Stopping is what makes that
// a defect worth avoiding rather than a limit worth stating: a check that
// reads half a file and reports nothing passes silently, which is the one way
// this command can be wrong and useless at once.
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

func extractLine(file string, lineNum int, line string, designNames map[string]bool) []Reference {
	var refs []Reference
	var covered []span

	for _, m := range linkPattern.FindAllStringSubmatchIndex(line, -1) {
		covered = append(covered, span{linkStart(line, m[0]), m[1]})
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
		if !strings.Contains(path, "/") && !numberedName.MatchString(path) && !isBareDesignName(path, designNames) {
			continue
		}
		refs = append(refs, Reference{File: file, Line: lineNum, Target: target})
	}
	return refs
}

// isBareDesignName reports whether path is a bare, unslashed name read as a
// reference because a file of that name exists under end-goal/, other than
// README.md and CLAUDE.md, whose names exist across the repository and so
// name nothing in particular by themselves.
func isBareDesignName(path string, designNames map[string]bool) bool {
	if unslashedExceptions[path] {
		return false
	}
	return designNames[path]
}

// linkStart returns where a Markdown link's covered span should begin.
// closeBracket is the position linkPattern's match starts at — the "]"
// right before "(" — and the scan runs backward from just before it for
// the nearest "[" with no "]" between it and closeBracket: that "[" is the
// link's own opening bracket, and covering from there keeps a numbered
// filename in the link's text from being read a second time as a bare
// path. A "]" met first, before any "[", means the bracket pair closing
// right before closeBracket belongs to something nested in the text, not
// to the link itself, so the scan stops there and widens no further. A
// link wrapped from the previous line has no "[" on this line at all —
// extraction reads one line at a time, so the text before it is invisible
// here — and closeBracket is returned unchanged: the target itself needs
// no widening to be read, so the link is still found from its closing
// half alone.
func linkStart(line string, closeBracket int) int {
	for i := closeBracket - 1; i >= 0; i-- {
		switch line[i] {
		case '[':
			return i
		case ']':
			return closeBracket
		}
	}
	return closeBracket
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
			if errors.Is(err, fs.ErrNotExist) {
				found = append(found, fmt.Sprintf("%s:%d: %s resolves to %s, which does not exist", ref.File, ref.Line, ref.Target, resolved))
			} else {
				found = append(found, fmt.Sprintf("%s:%d: %s: reading %s: %v", ref.File, ref.Line, ref.Target, resolved, err))
			}
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

// Uncited returns every doc.go path, and every screen README path
// [isScreenReadme] names, in files that contributed no Reference, in the
// order files gives them. A doc.go with nothing cited out of it has no
// "What defines it" line, or one a Go comment never carries a path from; a
// screen README with nothing cited out of it has no such line either — this
// is the client's counterpart to a doc.go, so it is held to the same rule.
// main holds only what [isCommand] names to this result: a command doc.go
// cites a file rather than a claim, so a command's doc.go with no
// reference at all is still a defect this way. A non-command doc.go or a
// screen README is held to a different rule instead, whether it cites a
// claim from claims.txt, which [Unclaimed] answers. Either way, this is
// what tracecheck's own doc.go claims to check and Check alone does not.
func Uncited(files []string, refs []Reference) []string {
	cited := make(map[string]bool)
	for _, ref := range refs {
		cited[ref.File] = true
	}

	var uncited []string
	for _, file := range files {
		if (filepath.Base(file) == "doc.go" || isScreenReadme(file)) && !cited[file] {
			uncited = append(uncited, file)
		}
	}
	return uncited
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
