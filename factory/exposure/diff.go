package exposure

import (
	"strconv"
	"strings"
)

// change is one line the diff added or removed, with where it is. The file is
// the path on the head side for an added line and on the base side for a removed
// one, and the line is that side's number — which is what an entry names, a
// human reading the list beside the diff needing to find the line.
type change struct {
	File    string
	Line    int
	Text    string
	Removed bool
}

// parse reads the output of git diff -U0 into the added and removed lines it
// carries. It reads the two headers it needs and skips everything else: the file
// name from "+++ b/<path>" and "--- a/<path>", and the two line numbers from the
// hunk header "@@ -<from>,<count> +<to>,<count> @@".
//
// A diff with no context is what makes this a parse of lines and not of files: a
// context line is indistinguishable from an unchanged one, and every line here is
// one side or the other.
func parse(diff string) []change {
	var changes []change
	var base, head string
	var baseLine, headLine int
	for _, line := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "):
			base = pathOf(strings.TrimPrefix(line, "--- "))
		case strings.HasPrefix(line, "+++ "):
			head = pathOf(strings.TrimPrefix(line, "+++ "))
		case strings.HasPrefix(line, "@@"):
			baseLine, headLine = hunk(line)
		case strings.HasPrefix(line, "+") && head != "":
			changes = append(changes, change{File: head, Line: headLine, Text: line[1:]})
			headLine++
		case strings.HasPrefix(line, "-") && base != "":
			changes = append(changes, change{File: base, Line: baseLine, Text: line[1:], Removed: true})
			baseLine++
		}
	}
	return changes
}

// pathOf strips the a/ or b/ prefix git puts on a diff's file names, and answers
// empty for /dev/null, which is a file that is not on that side at all.
func pathOf(name string) string {
	name = strings.TrimSpace(name)
	if name == "/dev/null" {
		return ""
	}
	if after, found := strings.CutPrefix(name, "a/"); found {
		return after
	}
	if after, found := strings.CutPrefix(name, "b/"); found {
		return after
	}
	return name
}

// hunk is the first line number on each side of one hunk header, and nought for
// a header this cannot read — a line numbered nought says the file and not the
// line, which is the honest answer where the header was not understood.
func hunk(header string) (base, head int) {
	fields := strings.Fields(header)
	for _, field := range fields {
		switch {
		case strings.HasPrefix(field, "-"):
			base = number(field[1:])
		case strings.HasPrefix(field, "+"):
			head = number(field[1:])
		}
	}
	return base, head
}

// number is the count before the comma of a hunk header's side.
func number(field string) int {
	if comma := strings.IndexByte(field, ','); comma >= 0 {
		field = field[:comma]
	}
	n, err := strconv.Atoi(field)
	if err != nil {
		return 0
	}
	return n
}
