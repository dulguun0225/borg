package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// namedOtherwise lists the packages whose directory name, however split
// into words, is no phrase the design uses, each with the design phrase it
// stands for. A package here is one the design names in other words, or one
// that is infrastructure the design does not name. A new entry is argued in
// the commit that adds it; renaming the package is the other way out.
var namedOtherwise = map[string]string{
	"clientdist":      "the client's build output the binary embeds; infrastructure",
	"postgres":        "the store; infrastructure the design does not name",
	"contractcheck":   "the check a consumer contract's predicates are decided by",
	"factorysettings": "the factory-wide settings record",
	"localtarget":     "a deploy target that runs a release as a local process",
	"secretref":       "a secret by reference, behind the one resolver",
	"targetseam":      "the named seam between the deployer and a deploy target",
	"wayin":           "the way in",
}

var wordPattern = regexp.MustCompile(`[a-z0-9]+`)

// segmentBreak matches a sentence or line boundary: DesignPhrases never
// builds a word run across one.
var segmentBreak = regexp.MustCompile(`[.!?;:\n]`)

// functionWords lists the articles, prepositions, conjunctions, pronouns,
// and auxiliaries a package name may not be split into: a design phrase
// built from one of these words as a whole component is a coincidence of
// ordinary prose, not a name for a concept, so CheckPackageNames rejects any
// split naming one.
var functionWords = map[string]bool{
	"a": true, "an": true, "the": true, "and": true, "or": true, "but": true,
	"nor": true, "of": true, "to": true, "in": true, "on": true, "at": true,
	"by": true, "for": true, "from": true, "with": true, "without": true,
	"into": true, "onto": true, "over": true, "under": true, "as": true,
	"is": true, "are": true, "was": true, "were": true, "be": true,
	"been": true, "being": true, "does": true, "do": true, "did": true,
	"not": true, "no": true, "it": true, "its": true, "this": true,
	"that": true, "these": true, "those": true, "one": true, "all": true,
	"any": true, "each": true, "every": true, "some": true, "than": true,
	"then": true, "so": true, "if": true, "when": true, "where": true,
	"which": true, "who": true, "what": true, "how": true,
}

// PackageDirs returns every directory directly under root that holds a .go
// file, by base name, sorted, skipping cmd/ and client/. A command is named
// for what it does and not for a design concept, and the client is not a Go
// package.
func PackageDirs(root string) ([]string, error) {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "cmd" || e.Name() == "client" {
			continue
		}
		files, err := filepath.Glob(filepath.Join(root, e.Name(), "*.go"))
		if err != nil {
			return nil, err
		}
		if len(files) > 0 {
			dirs = append(dirs, e.Name())
		}
	}
	sort.Strings(dirs)
	return dirs, nil
}

// DesignPhrases reads every .md under endGoal and returns every run of one,
// two, or three consecutive words, lowercased, joined by one space. A word
// is a run of letters and digits; punctuation and Markdown syntax separate
// words and are dropped. The text is first split into segments at '.', '!',
// '?', ';', ':', and newline, and a run is never built across a segment
// break, so a run cannot splice the end of one sentence to the start of the
// next.
func DesignPhrases(endGoal string) (map[string]bool, error) {
	phrases := make(map[string]bool)
	err := filepath.WalkDir(endGoal, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, segment := range segmentBreak.Split(strings.ToLower(string(content)), -1) {
			words := wordPattern.FindAllString(segment, -1)
			for i := range words {
				for n := 1; n <= 3 && i+n <= len(words); n++ {
					phrases[strings.Join(words[i:i+n], " ")] = true
				}
			}
		}
		return nil
	})
	return phrases, err
}

// CheckPackageNames reports each name that, with a space inserted at no,
// one, or two points, matches no design phrase and is not in namedOtherwise.
func CheckPackageNames(names []string, phrases map[string]bool) []string {
	var found []string
	for _, name := range names {
		if _, ok := namedOtherwise[name]; ok {
			continue
		}
		if !isDesignPhrase(name, phrases) {
			found = append(found, fmt.Sprintf("%s: a package named for no phrase the design uses, and not in namedOtherwise", name))
		}
	}
	return found
}

// isDesignPhrase reports whether name, with a space inserted at no, one, or
// two points, matches a phrase DesignPhrases found. A split is rejected even
// where the phrase it forms is one DesignPhrases found, if any one of its
// components is a functionWords entry: a package name built partly or wholly
// from an article, a preposition, or another function word names nothing,
// however ordinary the sentence it was lifted from.
func isDesignPhrase(name string, phrases map[string]bool) bool {
	if phrases[name] && !anyFunctionWord(name) {
		return true
	}
	for i := 1; i < len(name); i++ {
		if phrases[name[:i]+" "+name[i:]] && !anyFunctionWord(name[:i], name[i:]) {
			return true
		}
		for j := i + 1; j < len(name); j++ {
			if phrases[name[:i]+" "+name[i:j]+" "+name[j:]] && !anyFunctionWord(name[:i], name[i:j], name[j:]) {
				return true
			}
		}
	}
	return false
}

// anyFunctionWord reports whether any of parts is a functionWords entry.
func anyFunctionWord(parts ...string) bool {
	for _, p := range parts {
		if functionWords[p] {
			return true
		}
	}
	return false
}
