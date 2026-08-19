package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/dulguun0225/borg/factory/score"
)

// emptyTree is git's hash of the empty tree, which is the same in every
// repository. It is what a first item's diff is taken against: the candidate
// branch has no base, so the change is every line of it, and diffing against
// nothing is how that is stated to git.
const emptyTree = "4b825dc642cb6eb9a060e54bf8d69288fbee4904"

// measure takes the build's diff where the repository is. The score cannot read
// a repository and must not re-take a diff later — a diff re-taken against a
// master other items have merged into is not the diff the decision was made on —
// so this runs once, at the firing, and what it produces is handed to the score
// and never stored. The vector computed from it is the record.
//
// A git command that fails leaves the measurement unavailable with the reason on
// it, which the published formula turns into a human at the gate. That is the
// direction the design fixes: the gate a failure would remove is the gate that
// failure is evidence for needing.
func measure(repo string, masterExists bool) score.Measurement {
	base := emptyTree
	if masterExists {
		base = "master"
	}

	numstat, err := git(repo, "diff", "--numstat", base, "HEAD")
	if err != nil {
		return score.Measurement{Unavailable: fmt.Sprintf("the diff against %s could not be taken: %v", base, err)}
	}
	files, err := git(repo, "ls-tree", "-r", "--name-only", "HEAD")
	if err != nil {
		return score.Measurement{Unavailable: fmt.Sprintf("the build's tree could not be listed: %v", err)}
	}

	m := score.Measurement{FilesInTree: len(lines(files))}
	for _, line := range lines(numstat) {
		added, removed, path, ok := numstatLine(line)
		if !ok {
			// A binary file's numstat line carries dashes where the counts
			// belong. It is a file changed and no lines changed, which is what
			// git says about it and all the score can read.
			m.FilesChanged++
			continue
		}
		_ = path
		m.FilesChanged++
		m.LinesChanged += added + removed
	}
	return m
}

// numstatLine reads one line of git diff --numstat: lines added, lines removed,
// and the path, tab separated. It is not ok for a line whose counts are dashes,
// which is how git reports a binary file.
func numstatLine(line string) (added, removed int, path string, ok bool) {
	parts := strings.SplitN(line, "\t", 3)
	if len(parts) != 3 {
		return 0, 0, "", false
	}
	added, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, parts[2], false
	}
	removed, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, parts[2], false
	}
	return added, removed, parts[2], true
}

// lines is the non-empty lines of a command's output.
func lines(output string) []string {
	var kept []string
	for _, line := range strings.Split(output, "\n") {
		if strings.TrimSpace(line) != "" {
			kept = append(kept, line)
		}
	}
	return kept
}
