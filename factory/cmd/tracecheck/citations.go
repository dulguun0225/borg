package main

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// Citation is one claim id named from a doc.go comment line or a screen
// README: File and Line are where, ID is the claim.
type Citation struct {
	File string
	Line int
	ID   string
}

var citationPattern = regexp.MustCompile(`\bC\d{4}\b`)

// citesClaims reports whether file is one the claim rule holds: a doc.go
// outside cmd/, or one of the four screen READMEs. Only these are read for
// citations, so a "C0001" in a test or a constant is never one.
func citesClaims(file string) bool {
	if isScreenReadme(file) {
		return true
	}
	return filepath.Base(file) == "doc.go" && !isCommand(file)
}

// isCommand reports whether file sits under a cmd/ directory. A command
// implements the root CLAUDE.md's rules rather than the design, so it cites
// files and not claims.
func isCommand(file string) bool {
	return slices.Contains(strings.Split(filepath.ToSlash(file), "/"), "cmd")
}

// ExtractCitations reads every claim id out of one file. In a doc.go only a
// line that is a comment on its own is read, the rule ExtractFile applies;
// in a screen README every line is read. Any other file yields nothing.
func ExtractCitations(file string, content []byte) []Citation {
	if !citesClaims(file) {
		return nil
	}
	goSource := filepath.Ext(file) == ".go"
	var cites []Citation
	for i, line := range lines(content) {
		if goSource && !strings.HasPrefix(strings.TrimSpace(line), "//") {
			continue
		}
		for _, id := range citationPattern.FindAllString(line, -1) {
			cites = append(cites, Citation{File: file, Line: i + 1, ID: id})
		}
	}
	return cites
}

// CheckCitations holds the inventory and the code to each other: a cited id
// must be a claim; a built claim must be cited somewhere; an unbuilt or
// stated claim must be cited nowhere. Citation findings come first in
// citation order, then claim findings in claim order.
func CheckCitations(claims []Claim, cites []Citation) []string {
	byID := make(map[string]Claim, len(claims))
	for _, c := range claims {
		byID[c.ID] = c
	}
	citedBy := make(map[string][]Citation)
	var found []string
	for _, cite := range cites {
		if _, ok := byID[cite.ID]; !ok {
			found = append(found, fmt.Sprintf("%s:%d: cites %s, which is no claim in claims.txt", cite.File, cite.Line, cite.ID))
			continue
		}
		citedBy[cite.ID] = append(citedBy[cite.ID], cite)
	}
	for _, c := range claims {
		cited := citedBy[c.ID]
		switch {
		case c.Status == "built" && len(cited) == 0:
			found = append(found, fmt.Sprintf("claims.txt:%d: %s is built and nothing cites it", c.Line, c.ID))
		case c.Status != "built" && len(cited) > 0:
			found = append(found, fmt.Sprintf("claims.txt:%d: %s is %s and %s:%d cites it", c.Line, c.ID, c.Status, cited[0].File, cited[0].Line))
		}
	}
	return found
}

// Unclaimed returns every file citesClaims holds that contributed no
// Citation, in the order files gives them.
func Unclaimed(files []string, cites []Citation) []string {
	cited := make(map[string]bool)
	for _, c := range cites {
		cited[c.File] = true
	}
	var out []string
	for _, f := range files {
		if citesClaims(f) && !cited[f] {
			out = append(out, f)
		}
	}
	return out
}
