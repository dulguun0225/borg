package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Claim is one line of end-goal/claims.txt: one sentence of the design under
// a stable id, the file it is in, and whether code is expected to implement
// it. Line is where in claims.txt it sits, for the finding that names it.
type Claim struct {
	ID        string
	File      string
	Status    string // built, unbuilt, or stated
	Milestone string // set only where Status is unbuilt
	Sentence  string
	Line      int
}

var (
	claimIDPattern   = regexp.MustCompile(`^C\d{4}$`)
	milestonePattern = regexp.MustCompile(`^M\d+$`)
	// milestoneHeading matches a roadmap milestone heading, "## M9 — ...".
	milestoneHeading = regexp.MustCompile(`^## (M\d+)\b`)
	spaceRun         = regexp.MustCompile(`\s+`)
)

// ParseClaims reads claims.txt. A line beginning with "#" or empty is
// skipped. Each other line is id, file, status, sentence, tab-separated; the
// status is "built", "stated", or "unbuilt Mn", and the file must end in
// ".md" and hold no ".." path element. A malformed line is a finding and is
// not returned as a claim; a repeated id is a finding on its second line.
func ParseClaims(content []byte) ([]Claim, []string) {
	var claims []Claim
	var findings []string
	seen := make(map[string]int)

	for i, line := range lines(content) {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n := i + 1
		fields := strings.Split(line, "\t")
		if len(fields) != 4 {
			findings = append(findings, fmt.Sprintf("claims.txt:%d: %d fields, want 4", n, len(fields)))
			continue
		}
		c := Claim{ID: fields[0], File: fields[1], Sentence: normalizeSpace(fields[3]), Line: n}
		if !strings.HasSuffix(c.File, ".md") || hasDotDotElement(c.File) {
			findings = append(findings, fmt.Sprintf("claims.txt:%d: file %q is not a .md path under end-goal/", n, c.File))
			continue
		}
		if !claimIDPattern.MatchString(c.ID) {
			findings = append(findings, fmt.Sprintf("claims.txt:%d: id %q is not C and four digits", n, c.ID))
			continue
		}
		if first, dup := seen[c.ID]; dup {
			findings = append(findings, fmt.Sprintf("claims.txt:%d: id %s repeats line %d", n, c.ID, first))
			continue
		}
		status, milestone, _ := strings.Cut(fields[2], " ")
		switch {
		case status == "built" && milestone == "", status == "stated" && milestone == "":
		case status == "unbuilt" && milestonePattern.MatchString(milestone):
		default:
			findings = append(findings, fmt.Sprintf("claims.txt:%d: status %q is not built, stated, or unbuilt Mn", n, fields[2]))
			continue
		}
		c.Status, c.Milestone = status, milestone
		if c.Sentence == "" {
			findings = append(findings, fmt.Sprintf("claims.txt:%d: empty sentence", n))
			continue
		}
		seen[c.ID] = n
		claims = append(claims, c)
	}
	return claims, findings
}

// hasDotDotElement reports whether p, split on "/", holds a ".." element.
func hasDotDotElement(p string) bool {
	for _, part := range strings.Split(p, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// CheckClaims decides each claim against the tree: its file exists under
// endGoal, its sentence occurs in that file exactly once after whitespace is
// normalized, and an unbuilt claim's milestone is a "## Mn" heading in
// roadmap. A claim whose file cannot be read skips its milestone check, so a
// run can grow findings after a path is fixed. One line per defect, in claim
// order.
func CheckClaims(claims []Claim, endGoal string, roadmap []byte) []string {
	var found []string
	milestones := make(map[string]bool)
	for _, line := range lines(roadmap) {
		if m := milestoneHeading.FindStringSubmatch(line); m != nil {
			milestones[m[1]] = true
		}
	}
	texts := make(map[string]string)

	for _, c := range claims {
		path := filepath.Join(endGoal, c.File)
		text, read := texts[path]
		if !read {
			content, err := os.ReadFile(path)
			if err != nil {
				if errors.Is(err, fs.ErrNotExist) {
					found = append(found, fmt.Sprintf("claims.txt:%d: %s names %s, which does not exist", c.Line, c.ID, c.File))
				} else {
					found = append(found, fmt.Sprintf("claims.txt:%d: %s: reading %s: %v", c.Line, c.ID, c.File, err))
				}
				continue
			}
			text = normalizeSpace(string(content))
			texts[path] = text
		}
		switch n := strings.Count(text, c.Sentence); n {
		case 1:
		case 0:
			found = append(found, fmt.Sprintf("claims.txt:%d: %s's sentence is not in %s", c.Line, c.ID, c.File))
		default:
			found = append(found, fmt.Sprintf("claims.txt:%d: %s's sentence occurs %d times in %s, want once", c.Line, c.ID, n, c.File))
		}
		if c.Status == "unbuilt" && !milestones[c.Milestone] {
			found = append(found, fmt.Sprintf("claims.txt:%d: %s names milestone %s, which is no heading in roadmap.md", c.Line, c.ID, c.Milestone))
		}
	}
	return found
}

// CheckCoverage returns every .md file under how-the-factory-works/ and
// what-the-factory-does/ inside endGoal, other than a README.md, that no
// claim names. A design file with no claim is a file no code can be held
// to. The consistency pass states the same rule as one of its gates; this
// is the build's copy of it.
func CheckCoverage(claims []Claim, endGoal string) ([]string, error) {
	claimed := make(map[string]bool, len(claims))
	for _, c := range claims {
		claimed[filepath.ToSlash(c.File)] = true
	}
	var found []string
	for _, section := range []string{"how-the-factory-works", "what-the-factory-does"} {
		err := filepath.WalkDir(filepath.Join(endGoal, section), func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || filepath.Ext(path) != ".md" || d.Name() == "README.md" {
				return err
			}
			rel, err := filepath.Rel(endGoal, path)
			if err != nil {
				return err
			}
			if !claimed[filepath.ToSlash(rel)] {
				found = append(found, fmt.Sprintf("%s: a design file no claim in claims.txt names", rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return found, nil
}

// normalizeSpace collapses every run of whitespace to one space and trims
// the ends, so a sentence wrapped across lines in a Markdown file matches the
// one-line quote of it in claims.txt.
func normalizeSpace(s string) string {
	return strings.TrimSpace(spaceRun.ReplaceAllString(s, " "))
}
