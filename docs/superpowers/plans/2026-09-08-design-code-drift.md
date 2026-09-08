# Design and Code Drift Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Hold `factory/` to `end-goal/` by tracing code to numbered design claims, failing the build on mechanical drift, and running a cold judgment where a machine cannot tell.

**Architecture:** A sidecar inventory `end-goal/claims.txt` quotes one design sentence per claim under a stable id. `cmd/tracecheck` gains readers for the inventory, for claim citations in `doc.go` and screen `README.md` files, and for package names, and fails on the mismatches the spec lists. A new `drift-reviewer` agent judges one package against its cited sentences, dispatched from the consistency pass and before a code commit.

**Tech Stack:** Go (standard library only, tests in the existing temp-dir fixture style), bash in the consistency-pass fence, Markdown agent definitions.

**Spec:** `docs/superpowers/specs/2026-09-08-design-code-drift-design.md`

## Global Constraints

- Every Go file stays under 500 lines, tests included. Split by subject when one would pass it.
- No reflection outside tests, no `init`, no generated code, no dispatch keyed by string.
- Prose follows the root `CLAUDE.md` writing style: established meaning, no invented terms, no figurative phrasing, concise.
- `tracecheck` runs from `factory/`; `end-goal/` is `../end-goal` and the roadmap is `../roadmap.md` from there.
- Nothing is committed until the owner says so. Every task ends with a commit point the owner may take.
- Agent dispatches: Opus for judgment, Sonnet for execution. No batch of more than six subagents; each batch finishes before the next starts.
- The design wins a conflict between the document and the code.

---

### Task 1: The claims inventory reader

**Files:**
- Create: `factory/cmd/tracecheck/claims.go`
- Create: `factory/cmd/tracecheck/claims_test.go`

**Interfaces:**
- Produces: `type Claim struct { ID, File, Status, Milestone, Sentence string; Line int }`, `func ParseClaims(content []byte) ([]Claim, []string)`, `func CheckClaims(claims []Claim, endGoal string, roadmap []byte) []string`, `func normalizeSpace(s string) string`. Later tasks consume `Claim` and `normalizeSpace`.

- [ ] **Step 1: Write the failing tests**

```go
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseClaimsReadsFourFields(t *testing.T) {
	content := []byte("# header\n\nC0001\thow/01-a.md\tbuilt\tA gate sits at a boundary.\n" +
		"C0002\thow/02-b.md\tunbuilt M9\tA report enters through one channel.\n" +
		"C0003\twhat/05-c.md\tstated\tOne customer per install.\n")
	claims, findings := ParseClaims(content)
	if len(findings) != 0 {
		t.Fatalf("findings %v, want none", findings)
	}
	if len(claims) != 3 {
		t.Fatalf("parsed %d claims, want 3", len(claims))
	}
	if claims[1].Status != "unbuilt" || claims[1].Milestone != "M9" {
		t.Errorf("claim 2 status %q milestone %q, want unbuilt M9", claims[1].Status, claims[1].Milestone)
	}
	if claims[2].Line != 5 {
		t.Errorf("claim 3 line %d, want 5", claims[2].Line)
	}
}

func TestParseClaimsReportsMalformedLines(t *testing.T) {
	content := []byte("C0001\thow/01-a.md\tbuilt\tA sentence.\n" + // valid
		"C0002\thow/01-a.md\tbuilt\n" + // three fields
		"X0003\thow/01-a.md\tbuilt\tA sentence.\n" + // bad id
		"C0004\thow/01-a.md\tshipped\tA sentence.\n" + // bad status
		"C0005\thow/01-a.md\tunbuilt\tA sentence.\n" + // unbuilt with no milestone
		"C0001\thow/01-a.md\tbuilt\tA sentence.\n") // duplicate of line 1
	claims, findings := ParseClaims(content)
	if len(findings) != 5 {
		t.Fatalf("findings %v, want 5", findings)
	}
	if len(claims) != 1 {
		t.Fatalf("claims %v, want the one valid line", claims)
	}
}

func TestCheckClaimsFindsEachSentenceOnce(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "how")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := "# A\n\nA gate sits at a\nboundary. Twice said. Twice said.\n"
	if err := os.WriteFile(filepath.Join(dir, "01-a.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	roadmap := []byte("# Roadmap\n\n## M9 — Reports\n\ntext\n")
	claims := []Claim{
		{ID: "C0001", File: "how/01-a.md", Status: "built", Sentence: "A gate sits at a boundary.", Line: 1},
		{ID: "C0002", File: "how/01-a.md", Status: "built", Sentence: "Twice said.", Line: 2},
		{ID: "C0003", File: "how/01-a.md", Status: "built", Sentence: "Never said.", Line: 3},
		{ID: "C0004", File: "how/99-missing.md", Status: "built", Sentence: "A gate sits at a boundary.", Line: 4},
		{ID: "C0005", File: "how/01-a.md", Status: "unbuilt", Milestone: "M9", Sentence: "A gate sits at a boundary.", Line: 5},
		{ID: "C0006", File: "how/01-a.md", Status: "unbuilt", Milestone: "M42", Sentence: "A gate sits at a boundary.", Line: 6},
	}
	findings := CheckClaims(claims, root, roadmap)
	want := []string{"C0002", "C0003", "C0004", "C0006"}
	if len(findings) != len(want) {
		t.Fatalf("findings %v, want %d", findings, len(want))
	}
	for i, w := range want {
		if !strings.Contains(findings[i], w) {
			t.Errorf("finding %d is %q, want it to name %s", i, findings[i], w)
		}
	}
}

func TestNormalizeSpace(t *testing.T) {
	if got := normalizeSpace("a  b\n c\t d"); got != "a b c d" {
		t.Errorf("normalizeSpace = %q", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run from `factory/`: `go test ./cmd/tracecheck/ -run 'TestParseClaims|TestCheckClaims|TestNormalizeSpace' -v`
Expected: FAIL, undefined: ParseClaims, CheckClaims, normalizeSpace, Claim.

- [ ] **Step 3: Write claims.go**

```go
package main

import (
	"fmt"
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
// status is "built", "stated", or "unbuilt Mn". A malformed line is a finding
// and is not returned as a claim; a repeated id is a finding on its second
// line.
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

// CheckClaims decides each claim against the tree: its file exists under
// endGoal, its sentence occurs in that file exactly once after whitespace is
// normalized, and an unbuilt claim's milestone is a "## Mn" heading in
// roadmap. One line per defect, in claim order.
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
				found = append(found, fmt.Sprintf("claims.txt:%d: %s names %s, which does not exist", c.Line, c.ID, c.File))
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

// normalizeSpace collapses every run of whitespace to one space and trims
// the ends, so a sentence wrapped across lines in a Markdown file matches the
// one-line quote of it in claims.txt.
func normalizeSpace(s string) string {
	return strings.TrimSpace(spaceRun.ReplaceAllString(s, " "))
}
```

Note: in the test above, C0004 names a missing file and so produces one finding and no sentence check; C0006 has its sentence found once and one finding for the milestone. C0005 passes. That gives four findings in the order C0002, C0003, C0004, C0006.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/tracecheck/ -run 'TestParseClaims|TestCheckClaims|TestNormalizeSpace' -v`
Expected: PASS.

- [ ] **Step 5: Commit point**

```bash
git add factory/cmd/tracecheck/claims.go factory/cmd/tracecheck/claims_test.go
git commit -m "tracecheck: read end-goal/claims.txt and check each sentence against its file"
```

---

### Task 2: Claim citations in doc.go and screen READMEs

**Files:**
- Create: `factory/cmd/tracecheck/citations.go`
- Create: `factory/cmd/tracecheck/citations_test.go`

**Interfaces:**
- Consumes: `Claim` from Task 1, `isScreenReadme` from `client.go`, `lines` from `refs.go`.
- Produces: `type Citation struct { File string; Line int; ID string }`, `func ExtractCitations(file string, content []byte) []Citation`, `func CheckCitations(claims []Claim, cites []Citation) []string`, `func Unclaimed(files []string, cites []Citation) []string`, `func isCommand(file string) bool`, `func citesClaims(file string) bool`.

- [ ] **Step 1: Write the failing tests**

```go
package main

import (
	"slices"
	"strings"
	"testing"
)

func TestExtractCitationsReadsIdsFromCommentLinesOfDocGo(t *testing.T) {
	content := []byte("// Package gate. C0001 and C0002 are what defines it.\n" +
		"package gate\n" +
		"var x = \"C0003\" // C0004\n")
	cites := ExtractCitations("gate/doc.go", content)
	var ids []string
	for _, c := range cites {
		ids = append(ids, c.ID)
	}
	slices.Sort(ids)
	if !slices.Equal(ids, []string{"C0001", "C0002"}) {
		t.Errorf("ids %v, want C0001 C0002 only", ids)
	}
}

func TestExtractCitationsReadsEveryLineOfAScreenReadme(t *testing.T) {
	cites := ExtractCitations("client/src/app/work/README.md", []byte("## What defines it\n\nC0007, C0008.\n"))
	if len(cites) != 2 {
		t.Errorf("cites %v, want 2", cites)
	}
}

func TestExtractCitationsIgnoresFilesThatDoNotCiteClaims(t *testing.T) {
	if got := ExtractCitations("gate/gate.go", []byte("// C0001\n")); len(got) != 0 {
		t.Errorf("a .go file that is not doc.go yielded %v", got)
	}
	if got := ExtractCitations("client/src/app/api/README.md", []byte("C0001\n")); len(got) != 0 {
		t.Errorf("a README that is not a screen's yielded %v", got)
	}
}

func TestCheckCitationsBothDirections(t *testing.T) {
	claims := []Claim{
		{ID: "C0001", Status: "built", Line: 1},
		{ID: "C0002", Status: "built", Line: 2},   // uncited
		{ID: "C0003", Status: "unbuilt", Milestone: "M9", Line: 3}, // cited
		{ID: "C0004", Status: "stated", Line: 4},  // cited
	}
	cites := []Citation{
		{File: "gate/doc.go", Line: 10, ID: "C0001"},
		{File: "gate/doc.go", Line: 11, ID: "C0003"},
		{File: "gate/doc.go", Line: 12, ID: "C0004"},
		{File: "gate/doc.go", Line: 13, ID: "C0099"}, // unknown
	}
	findings := CheckCitations(claims, cites)
	for _, w := range []string{"C0099", "C0002", "C0003", "C0004"} {
		if !slices.ContainsFunc(findings, func(f string) bool { return strings.Contains(f, w) }) {
			t.Errorf("findings %v name no %s", findings, w)
		}
	}
	if len(findings) != 4 {
		t.Errorf("findings %v, want 4", findings)
	}
}

func TestUnclaimedExemptsCommands(t *testing.T) {
	files := []string{"gate/doc.go", "cmd/depscheck/doc.go", "client/src/app/ops/README.md", "gate/gate.go"}
	cites := []Citation{{File: "gate/doc.go", Line: 1, ID: "C0001"}}
	got := Unclaimed(files, cites)
	if !slices.Equal(got, []string{"client/src/app/ops/README.md"}) {
		t.Errorf("Unclaimed = %v, want the ops README alone", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./cmd/tracecheck/ -run 'TestExtractCitations|TestCheckCitations|TestUnclaimed' -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write citations.go**

```go
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
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./cmd/tracecheck/ -run 'TestExtractCitations|TestCheckCitations|TestUnclaimed' -v`
Expected: PASS.

- [ ] **Step 5: Commit point**

```bash
git add factory/cmd/tracecheck/citations.go factory/cmd/tracecheck/citations_test.go
git commit -m "tracecheck: read claim citations and hold the inventory and the code to each other"
```

---

### Task 3: A bare numbered filename is a reference

**Files:**
- Modify: `factory/cmd/tracecheck/refs.go:78-88` (the bare-path loop in `extractLine`)
- Modify: `factory/cmd/tracecheck/refs_test.go` (add one test)
- Modify: `factory/gate/doc.go:167-170` and `factory/agent/doc.go:168` (write the full path for each bare sibling name)

**Interfaces:**
- Consumes: `extractLine`, `splitTarget`, `Reference`.
- Produces: no new names. A bare `.md` name with no slash that starts with two digits and a hyphen is now a `Reference` resolved against the file's directory.

- [ ] **Step 1: Write the failing test** (append to `refs_test.go`)

```go
func TestExtractLineReadsANumberedBareName(t *testing.T) {
	refs := extractLine("gate/doc.go", 1, "// The tasks gate is 04-tasks.md, and CLAUDE.md is not a path.")
	var targets []string
	for _, r := range refs {
		targets = append(targets, r.Target)
	}
	if !slices.Contains(targets, "04-tasks.md") {
		t.Errorf("targets %v, want 04-tasks.md read as a reference", targets)
	}
	if slices.Contains(targets, "CLAUDE.md") {
		t.Errorf("targets %v, want no CLAUDE.md", targets)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./cmd/tracecheck/ -run TestExtractLineReadsANumberedBareName -v`
Expected: FAIL, "want 04-tasks.md read as a reference".

- [ ] **Step 3: Change the slash rule in refs.go**

Add beside the other patterns:

```go
	// numberedName is the design's file naming: two digits, a hyphen, and a
	// slug. A bare mention of one with no slash is a path and not prose.
	numberedName = regexp.MustCompile(`^\d\d-`)
```

Replace in `extractLine`:

```go
		path, _, _ := splitTarget(target)
		if !strings.Contains(path, "/") {
			continue
		}
```

with:

```go
		path, _, _ := splitTarget(target)
		if !strings.Contains(path, "/") && !numberedName.MatchString(path) {
			continue
		}
```

- [ ] **Step 4: Run all tracecheck tests**

Run: `go test ./cmd/tracecheck/`
Expected: PASS.

- [ ] **Step 5: Run tracecheck on the real tree and read the new findings**

Run from `factory/`: `go run ./cmd/tracecheck`
Expected: findings in `gate/doc.go` (three: `09-a-role-prompt-or-a-skill.md`, `10-a-safeguards-withdrawal.md`, `11-a-halts-withdrawal.md`) and `agent/doc.go` (`04-tasks.md`), each "resolves to ..., which does not exist". Anything else that appears is a fifth blind reference; fix it the same way.

- [ ] **Step 6: Fix the four references**

In `factory/gate/doc.go` and `factory/agent/doc.go`, replace each bare name with its full path under `../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/`. Verify each target exists with `ls` before writing it.

- [ ] **Step 7: Update tracecheck's doc.go**

In `factory/cmd/tracecheck/doc.go`, in the paragraph beginning "A bare relative path is read only where it contains a "/"", add: `or where it is a bare name beginning with two digits and a hyphen, the design's file naming, so a sibling named without its path is read rather than skipped`. In the "What it does not see" paragraph, delete the final clause about "a same-directory reference with no "/"" and end the sentence at the preceding clause.

- [ ] **Step 8: Verify green**

Run: `go run ./cmd/tracecheck && go test ./cmd/tracecheck/`
Expected: no output from tracecheck, tests PASS.

- [ ] **Step 9: Commit point**

```bash
git add factory/cmd/tracecheck factory/gate/doc.go factory/agent/doc.go
git commit -m "tracecheck: a bare numbered filename is a reference, and four that pointed at nothing are fixed"
```

---

### Task 4: Package names are design phrases

**Files:**
- Create: `factory/cmd/tracecheck/names.go`
- Create: `factory/cmd/tracecheck/names_test.go`

**Interfaces:**
- Produces: `func PackageDirs(root string) ([]string, error)`, `func DesignPhrases(endGoal string) (map[string]bool, error)`, `func CheckPackageNames(names []string, phrases map[string]bool) []string`, `var namedOtherwise map[string]string`.

- [ ] **Step 1: Write the failing tests**

```go
package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDesignPhrasesHoldsUpToThreeWords(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.md"), []byte("The merge queue holds a screen state\nmachine."), 0o644); err != nil {
		t.Fatal(err)
	}
	phrases, err := DesignPhrases(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"merge", "merge queue", "screen state machine", "state machine"} {
		if !phrases[p] {
			t.Errorf("phrases lack %q", p)
		}
	}
	if phrases["merge queue holds a"] {
		t.Error("phrases hold a four-word run")
	}
}

func TestCheckPackageNamesSplitsAName(t *testing.T) {
	phrases := map[string]bool{"merge queue": true, "gate": true, "screen state machine": true}
	findings := CheckPackageNames([]string{"mergequeue", "gate", "screenstatemachine", "widget"}, phrases)
	if len(findings) != 1 || !slices.ContainsFunc(findings, func(f string) bool { return f == "widget: a package named for no phrase the design uses, and not in namedOtherwise" }) {
		t.Errorf("findings %v, want widget alone", findings)
	}
}

func TestCheckPackageNamesAcceptsTheAllowance(t *testing.T) {
	if got := CheckPackageNames([]string{"postgres"}, map[string]bool{}); len(got) != 0 {
		t.Errorf("postgres is in namedOtherwise and was reported: %v", got)
	}
}

func TestPackageDirsSkipsCmdAndClient(t *testing.T) {
	root := t.TempDir()
	for _, d := range []string{"gate", "cmd/factory", "client/src", "clientdist", "empty"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range []string{"gate/gate.go", "cmd/factory/main.go", "clientdist/embed.go", "client/src/x.ts"} {
		if err := os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := PackageDirs(root)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"clientdist", "gate"}) {
		t.Errorf("PackageDirs = %v, want clientdist gate", got)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./cmd/tracecheck/ -run 'TestDesignPhrases|TestCheckPackageNames|TestPackageDirs' -v`
Expected: FAIL, undefined.

- [ ] **Step 3: Write names.go**

```go
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
}

var wordPattern = regexp.MustCompile(`[a-z0-9]+`)

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
// words and are dropped.
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
		words := wordPattern.FindAllString(strings.ToLower(string(content)), -1)
		for i := range words {
			for n := 1; n <= 3 && i+n <= len(words); n++ {
				phrases[strings.Join(words[i:i+n], " ")] = true
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

func isDesignPhrase(name string, phrases map[string]bool) bool {
	if phrases[name] {
		return true
	}
	for i := 1; i < len(name); i++ {
		if phrases[name[:i]+" "+name[i:]] {
			return true
		}
		for j := i + 1; j < len(name); j++ {
			if phrases[name[:i]+" "+name[i:j]+" "+name[j:]] {
				return true
			}
		}
	}
	return false
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./cmd/tracecheck/ -run 'TestDesignPhrases|TestCheckPackageNames|TestPackageDirs' -v`
Expected: PASS.

- [ ] **Step 5: Confirm the allowance is exactly the real tree's remainder**

Write a throwaway `main_test.go`-free check: from `factory/`, run

```bash
go run ./cmd/tracecheck 2>&1 | grep 'a package named' || echo none
```

This will not yet run the names check (Task 5 wires it). Skip to Task 5 step 4 for the real-tree confirmation; the seven entries above were computed against the design on 2026-09-08 and are expected to be exactly the remainder.

- [ ] **Step 6: Commit point**

```bash
git add factory/cmd/tracecheck/names.go factory/cmd/tracecheck/names_test.go
git commit -m "tracecheck: a package is named for a phrase the design uses"
```

---

### Task 5: Wire the checks into main.go and update the map

**Files:**
- Modify: `factory/cmd/tracecheck/main.go:19-50` (`run`)
- Modify: `factory/cmd/tracecheck/doc.go` (what it checks, the file list, the error list)
- Modify: `factory/README.md:17` (tracecheck's row) and `:93` (the screen README sentence)

**Interfaces:**
- Consumes: everything Tasks 1 to 4 produce.

- [ ] **Step 1: Rewrite `run`**

```go
const (
	endGoalDir  = "../end-goal"
	claimsPath  = "../end-goal/claims.txt"
	roadmapPath = "../roadmap.md"
)

func run() error {
	files, err := walkFiles(".")
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}

	var refs []Reference
	var cites []Citation
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("tracecheck: %w", err)
		}
		refs = append(refs, ExtractFile(file, content)...)
		cites = append(cites, ExtractCitations(file, content)...)
	}

	claimsContent, err := os.ReadFile(claimsPath)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	roadmap, err := os.ReadFile(roadmapPath)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	claims, findings := ParseClaims(claimsContent)

	findings = append(findings, Check(refs)...)
	findings = append(findings, CheckClaims(claims, endGoalDir, roadmap)...)
	findings = append(findings, CheckCitations(claims, cites)...)
	for _, file := range Unclaimed(files, cites) {
		findings = append(findings, fmt.Sprintf("%s: cites no claim from claims.txt", file))
	}
	for _, file := range Uncited(files, refs) {
		if !isCommand(file) {
			continue
		}
		findings = append(findings, fmt.Sprintf("%s: a command's doc.go carrying no reference at all", file))
	}
	for _, path := range MissingScreenReadmes(".", files) {
		findings = append(findings, fmt.Sprintf("%s: a screen README that does not exist", path))
	}

	dirs, err := PackageDirs(".")
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	phrases, err := DesignPhrases(endGoalDir)
	if err != nil {
		return fmt.Errorf("tracecheck: %w", err)
	}
	findings = append(findings, CheckPackageNames(dirs, phrases)...)

	if len(findings) == 0 {
		return nil
	}
	return errors.New("tracecheck: the code and the design do not hold to each other:\n\t" + strings.Join(findings, "\n\t"))
}
```

`Uncited` stays for commands only; a non-command doc.go is held by `Unclaimed` instead. Update `Uncited`'s comment in `refs.go` to say the command rule now applies to `cmd/` alone.

- [ ] **Step 2: Rewrite the doc.go**

Rewrite `factory/cmd/tracecheck/doc.go` so that it states, in this order: what fails the build, as one list matching the spec's section 2 (sentence not found once; file, status, or milestone invalid; unknown cited id; built uncited; unbuilt or stated cited; non-command doc.go or screen README citing no claim; command doc.go with no reference; reference pointing at nothing; anchor matching no heading; screen README missing; package named for no design phrase); the file map (`claims.go`, `citations.go`, `names.go`, `refs.go`, `client.go`, `main.go`, and their tests); the extraction rules as today with Task 3's change; what it does not see; who may write what (nothing); and "What defines it" unchanged. Keep it under 120 lines. Keep the existing writing style.

- [ ] **Step 3: Update `factory/README.md`**

Row 17 becomes:

```
| [`cmd/tracecheck`](../../../factory/cmd/tracecheck) | Failing the build where the code and the design do not hold to each other: a claim in `../end-goal/claims.txt` whose sentence is no longer in its file, a `doc.go` or screen `README.md` citing no claim or citing one the inventory lacks, a built claim nothing cites, an unbuilt or stated claim something cites, a reference into a Markdown file that points at nothing, and a package named for no phrase the design uses. |
```

In line 93, after "reads each screen's `README.md` the way it reads a `doc.go`", the failing conditions become "a screen directory with no `README.md`, one citing no claim, or one whose reference points at nothing".

- [ ] **Step 4: Build and run**

Run from `factory/`: `go vet ./... && go build ./... && go test ./cmd/tracecheck/ && go run ./cmd/tracecheck; echo exit=$?`
Expected: vet, build, tests clean. tracecheck fails: `claims.txt` does not exist yet, so the error is `open ../end-goal/claims.txt`. That is the expected state until Task 8. Then temporarily create an empty `../end-goal/claims.txt` with only a comment line, rerun, and confirm the findings are exactly: every non-command doc.go and the four screen READMEs "cites no claim", and no "a package named" line. Delete the temporary file after.

- [ ] **Step 5: Commit point**

```bash
git add factory/cmd/tracecheck factory/README.md
git commit -m "tracecheck: holds the code to the claims inventory and package names to the design's words"
```

CI runs tracecheck on every push, so this commit fails CI until Task 9 lands. Push nothing between here and Task 9, or push the whole sequence at once.

---

### Task 6: The consistency pass and CI

**Files:**
- Modify: `end-goal/CLAUDE.md:6-9` ("What this is" opening), `:233-360` (the fence), and the prose after it
- Modify: `.github/workflows/factory.yml` (one step)
- Modify: `factory/README.md` (mention that `tools/consistency-commands.sh` is the document's check, if not already)

- [ ] **Step 1: Describe `claims.txt` in "What this is"**

Replace the first sentence of the section:

> `end-goal/` is Markdown files plus one that is not: `terms.txt`, the inventory of every name the document introduces, read only by the consistency pass.

with:

> `end-goal/` is Markdown files plus two that are not. `terms.txt` is the inventory of every name the document introduces, read only by the consistency pass. `claims.txt` is the inventory of every claim the document makes that code answers to: one line per claim, tab-separated, with an id (`C` and four digits, never reused), the file, a status (`built`, `unbuilt Mn` naming a `roadmap.md` milestone, or `stated` for a statement no code is expected to implement), and one sentence of the file quoted verbatim. Both are read by `factory/cmd/tracecheck`, which is where a claim's sentence moving fails the build, and neither holds a reason.

Use links in the form the file already uses for `terms.txt`.

- [ ] **Step 2: Add three commands to the fence**

Append inside the bash fence, before the closing fence and after the prose-form check:

```bash
# every claims.txt line has four fields — expect no output
grep -v '^#' end-goal/claims.txt | grep -v '^$' | awk -F'\t' 'NF!=4{print "claims.txt:" FNR ": " NF " fields"}'

# every design file outside a README states at least one claim — expect no output
comm -23 <(find end-goal/how-the-factory-works end-goal/what-the-factory-does -name '*.md' ! -name README.md | sed 's#^end-goal/##' | sort) <(grep -v '^#' end-goal/claims.txt | grep -v '^$' | cut -f2 | sort -u)

# the claims whose sentence this edit changed, and the package or screen directories citing each — read for the drift step below
git diff -U0 HEAD -- end-goal/claims.txt | grep '^[-+]C[0-9]' | sed 's/^[-+]//' | cut -f1 | sort -u | while read -r id; do printf '%s:' "$id"; grep -rlw "$id" factory --include=doc.go --include=README.md | xargs -r -n1 dirname | sort -u | tr '\n' ' '; echo; done

# every claim's sentence is still in its file and every citation holds — run from factory/, expect no output
(cd factory && go run ./cmd/tracecheck)
```

- [ ] **Step 3: Say which are gates**

In the paragraph beginning "Two commands here are gates and the rest are readings", change to "Four commands here are gates" and add: the four-field check and the every-file-states-a-claim check are gates for the same reason the inventory check is: a file with no claim is a file no code can be held to, and it is found at the edit that adds the file, where it costs one line.

- [ ] **Step 4: CI step**

In `.github/workflows/factory.yml`, after the `go run ./cmd/tracecheck` step:

```yaml
      # The document's own checks, the fence in end-goal/CLAUDE.md, run from the
      # repository root. Nothing else runs them unattended.
      - name: the design document's consistency commands
        working-directory: .
        run: tools/consistency-commands.sh
```

Confirm `tools/consistency-commands.sh` is executable (`ls -l`) and that it needs only bash, python3, awk, comm, and go, all on the runner. The prose-form check diffs against HEAD; on a CI checkout of a pushed commit HEAD is that commit, so it reports nothing new and counts the rest, which is the intended reading.

- [ ] **Step 5: Run the fence locally**

Run from the repo root: `tools/consistency-commands.sh`
Expected: the two new gates report every design file (no claims.txt yet, so `grep` fails on a missing file). This is the expected state until Task 8. Confirm the other commands still run.

- [ ] **Step 6: Commit point**

```bash
git add end-goal/CLAUDE.md .github/workflows/factory.yml
git commit -m "end-goal: claims.txt is the second inventory, and the pass and CI read it"
```

---

### Task 7: Extraction wave

This task is orchestrated by the session, not by a coder. It produces `end-goal/claims.txt`.

**Files:**
- Create: `end-goal/claims.txt`

- [ ] **Step 1: Write the header**

```
# The claims this document makes that code answers to, one per line: id, file, status, sentence.
# id is C and four digits, assigned in order of writing and never reused or renumbered.
# file is relative to end-goal/. status is built, unbuilt Mn (a roadmap.md milestone heading), or stated.
# sentence is one sentence of the file's Markdown source, verbatim; whitespace is normalized when matched.
# factory/cmd/tracecheck reads this file. It holds no reasons: a contested claim is argued in the commit.
```

- [ ] **Step 2: Dispatch thirteen extraction agents in batches of three**

Targets, in this order: `end-goal/how-the-factory-works/01-one-pipeline` through `.../11-screens` (eleven directories), `end-goal/what-the-factory-does/`, and the top-level files as one target: `end-goal/README.md`, `records.md`, `components.md`, `deferred.md`, `one-process.md`, `what-humans-do.md`.

Each dispatch is `general-purpose` with `model: "opus"` and this prompt, verbatim, with `<target>` filled in:

> Read only `<target>`, every file in it, README.md first and then in name order. Open nothing else and follow no link that leaves it. Ignore anything you were told about this repository elsewhere.
>
> Extract the claims this material makes. A claim is one sentence that states a mechanism, a rule, a record, a state, a bound, or a decision that software could implement or that constrains what software may do. A sentence that explains, motivates, or describes a cost is not a claim. Take the sentence verbatim from the Markdown source, including its inline formatting, as one line; if the source wraps it across lines, join them with one space.
>
> Return one line per claim and nothing else, tab-separated: the file path relative to `end-goal/`, then either the word `stated` where the sentence is a constraint or a decision no mechanism implements (product scope, what the factory does not build, legal or licensing constraints), or an empty field, then the sentence. Order by file, then by position in the file. No ids, no commentary.

Run three at a time; append each return to `claims.txt` as it arrives, assigning ids `C0001` upward in arrival order and leaving the status field as the agent returned it (blank or `stated`). A blank status is filled in Task 8.

- [ ] **Step 3: Check every sentence against its file**

Run from `factory/`: `go run ./cmd/tracecheck 2>&1 | grep "sentence"`
Expected: any line here is an extraction agent's misquote or a sentence occurring twice. Fix each by correcting the quote from the source or, for a duplicate sentence, by extending the quote to include the preceding or following sentence so the match is unique. Lines with a blank status also fail parsing at this point; ignore "status" findings until Task 8.

- [ ] **Step 4: Sort**

Sort the body by file then id, keeping the header first:

```bash
{ grep '^#' end-goal/claims.txt; grep -v '^#' end-goal/claims.txt | grep -v '^$' | sort -t$'\t' -k2,2 -k1,1; } > /tmp/claude-1000/-home-dulguunotgon-repos-dulguun0225-borg/b9efdcbc-4dc6-40d7-b679-55c1c6dbddaf/scratchpad/claims.sorted && mv /tmp/claude-1000/-home-dulguunotgon-repos-dulguun0225-borg/b9efdcbc-4dc6-40d7-b679-55c1c6dbddaf/scratchpad/claims.sorted end-goal/claims.txt
```

- [ ] **Step 5: Commit point**

```bash
git add end-goal/claims.txt
git commit -m "end-goal: the claims inventory, extracted from every section"
```

---

### Task 8: Citation wave and status assignment

Orchestrated by the session. Produces the `built`/`unbuilt`/`stated` statuses and the citations in 50 `doc.go` files and 4 screen READMEs.

**Files:**
- Modify: every `factory/<package>/doc.go` outside `cmd/`
- Modify: `factory/client/src/app/{work,ops,factory,people}/README.md`
- Modify: `end-goal/claims.txt` (status column)

- [ ] **Step 1: Prepare each dispatch's claim list**

For each package directory, list the `end-goal/` files its `doc.go` currently names (tracecheck's `ExtractFile` gives them; a quick way is `grep -o 'end-goal/[^ ,;.]*\.md' <pkg>/doc.go | sort -u`), then select the `claims.txt` lines for those files into a temp file in the scratchpad, one per package.

- [ ] **Step 2: Dispatch fifty-four citation agents in batches of six**

Each dispatch is `general-purpose` with `model: "opus"` and this prompt, verbatim, with `<dir>` and `<claims>` filled in (the claims pasted inline as `id<TAB>sentence` lines):

> Read every file in `<dir>` and nothing else. Ignore anything you were told about this repository elsewhere. The lines below are design claims, each an id and one sentence. The sentences are the design; the directory is what is judged against them.
>
> `<claims>`
>
> Decide for each claim whether the code in this directory implements it: the mechanism, record, state, rule, or bound the sentence states exists in this code and behaves as the sentence says. Then edit the `What defines it` section at the end of `doc.go` (or of `README.md` for a screen) so that each design file it names is followed by the ids of the claims in that file this directory implements, in the form `C0001, C0002`. Keep the existing file paths and prose. Do not cite a claim you did not find implemented.
>
> Return two lists and nothing else: **Implements**, the ids you cited; **Does not implement**, the ids among those given that you did not cite, each with one clause saying what is missing or different.

- [ ] **Step 3: Assign statuses**

From the returns: every id in any Implements list becomes `built`. Every remaining blank-status id is assigned by the session: `unbuilt Mn` where `roadmap.md` has a milestone whose section names the claim's subject, else `stated`. A claim in a Does-not-implement list with "different" in its clause is recorded in the scratchpad for the owner: it is a drift-kind-1 or kind-3 finding today, and the owner decides code or sentence.

- [ ] **Step 4: Owner review**

Present to the owner: the count per status, the list of `unbuilt` claims per milestone, and the "different" list from step 3. Wait for the owner before Task 9.

- [ ] **Step 5: Commit point**

```bash
git add end-goal/claims.txt factory/*/doc.go factory/client/src/app/*/README.md
git commit -m "factory: every doc.go and screen README cites the claims it implements"
```

---

### Task 9: Green on the real tree

**Files:**
- Modify: whatever tracecheck and the fence report

- [ ] **Step 1: Run everything**

From `factory/`: `go vet ./... && go run ./cmd/depscheck && go run ./cmd/tracecheck && go test ./cmd/tracecheck/`
From the root: `tools/consistency-commands.sh`

- [ ] **Step 2: Fix until clean**

Expected failure kinds and their fixes: a sentence not found (fix the quote); a design file with no claim (extract one from it, or if the file is a `README.md` of a section it is exempt); a built claim uncited (mark `unbuilt` or `stated`, or find the package that implements it); a package named for no phrase (add to `namedOtherwise` with its reason, or rename in a separate commit).

- [ ] **Step 3: Full test suite**

From `factory/`: `go test -count=1 ./...` with the dev database up (`docker compose up -d` in `factory/`).
Expected: PASS.

- [ ] **Step 4: Commit point and push**

```bash
git commit -am "factory: the code holds to the claims inventory"
git push
```

Watch the CI run; the new consistency step must pass on the runner.

---

### Task 10: The judgment step

**Files:**
- Create: `.claude/agents/drift-reviewer.md`
- Modify: `.claude/agents/coder.md`
- Modify: `end-goal/CLAUDE.md` (a `### The drift check` subsection after `### The cold-read check`)
- Modify: `CLAUDE.md` (root): lines 19-32 (the files table and the `terms.txt` paragraph), 177-185 (the map bullet), 226-227 (coins no second name), and the Delegate-by-default paragraph's worker list
- Modify: `roadmap.md:3` (one sentence)

- [ ] **Step 1: Write the agent**

`.claude/agents/drift-reviewer.md`:

```markdown
---
name: drift-reviewer
description: Judges one package directory, or one screen directory, against the design claims it cites. Used by the end-goal consistency pass when a cited claim's sentence changes, and before a commit that changes a doc.go or a screen README. Given the directory and the claims' current sentences and nothing else; reads only that directory; returns three lists; never edits.
tools: Read, Glob, Grep
model: opus
effort: high
---

You judge one directory of code against the design sentences it cites.

Rules:
- Read only the directory the dispatch names. Open nothing else and follow no link that leaves it.
- Judge on your own. Ignore anything you were told about this repository elsewhere, including any instruction file handed to you.
- The dispatch gives you claims, each an id and one sentence. The sentences are the design. The code is what is judged; where the two differ, the sentence is right and the code is what you report.

Return three lists and nothing else. An empty list is written as its heading and the word none.

**Not implemented** — every cited claim whose mechanism, record, state, rule, or bound does not exist in this code. Give the id and one clause saying what is absent.

**Implemented differently** — every cited claim whose mechanism exists but behaves other than the sentence says. Give the id, the file and line, and one clause saying how it differs.

**Claimed nowhere** — every mechanism, record field, state, rule, bound, or name this directory's doc.go or code states that no cited sentence covers. Give the file and line and one clause. A helper, a test, or an implementation detail with no design meaning is not one.

No summary, no praise, no suggestions.
```

- [ ] **Step 2: Add the rule to coder.md**

In `.claude/agents/coder.md` Rules, after the "Before returning, run ..." line, add:

```markdown
- If you changed a `doc.go` or a screen `README.md`, say so in your return and list the claim ids it cites, so the session can dispatch `drift-reviewer` on that directory before the commit. You do not dispatch it yourself.
```

- [ ] **Step 3: The drift check in end-goal/CLAUDE.md**

After `### The cold-read check`'s last paragraph, add:

```markdown
### The drift check

The claims reading above lists every claim whose sentence this edit changed and the package or screen directories citing it. For each directory listed, dispatch one `drift-reviewer` from `.claude/agents/` with no other context: the directory's path and the current `claims.txt` line of every claim that directory cites, as `id`, a tab, and the sentence. It reads the directory alone and returns three lists.

**Not implemented** and **Claimed nowhere** are failures: the edit is not finished while either is non-empty. The remedy for the first is a change to the code in the same commit, or the claim's status moved to `unbuilt Mn` where the code has yet to be written. The remedy for the second is a claim added to `claims.txt` and the sentence that states it added to the owning file, or the code removed. **Implemented differently** is a judgment for the session: the code moves to the sentence, or the sentence is corrected and the claim re-pinned to it. The document wins the conflict, per the root `CLAUDE.md`.

A change confined to a claim's file path or status, with the sentence unchanged, does not fire the check for that claim: nothing the reviewer judges has moved. What the check costs is one Opus read per citing directory per moved sentence, bounded by a claim being one sentence.
```

- [ ] **Step 4: Root CLAUDE.md edits**

Four edits, each replacing the text at the line given:

(a) After the `terms.txt` paragraph (line 28 onward), add a paragraph:

> `end-goal/claims.txt` is not a third either: it lists every claim the document makes that code answers to, one sentence each under a stable id, with the file and whether code implements it yet. `factory/cmd/tracecheck` reads it and fails the build on a sentence no longer in its file, a claim cited by no code while marked built, and code citing no claim. It holds no reasons.

(b) The map bullet (line 177): replace "and the path of the Markdown file that defines what it implements — an `end-goal/` file for anything the design names. `cmd/tracecheck` fails the build on a `doc.go` with no such reference and on a reference that points at nothing." with "the path of each `end-goal/` file that defines what it implements, and after each path the ids of the `end-goal/claims.txt` claims in that file the package implements. `cmd/tracecheck` fails the build on a `doc.go` citing no claim or citing one the inventory lacks, on a built claim no `doc.go` cites, on a reference that points at nothing, and on a package whose name is no phrase the design uses. A `doc.go` under `cmd/` implements this file's rules rather than the design, cites files and no claims, and is held to the reference rule alone."

(c) Lines 226-227: replace with "Code coins no second name for a thing the design document names: a package is named for a phrase some `end-goal/` file uses, which `cmd/tracecheck` checks by splitting the directory name into words, and the seven packages named otherwise are listed in its `names.go` with the design phrase each stands for. Below the package name the rule is held in review and by `drift-reviewer`, whose third list is a name or mechanism the design does not have."

(d) Add a paragraph to the Code section after the client counterparts list:

> An edit to a `doc.go` or a screen `README.md` is followed, before its commit, by one dispatch of `drift-reviewer` from `.claude/agents/` on that directory, given the directory and the current sentence of every claim it cites and nothing else. Its **Not implemented** and **Claimed nowhere** lists must be empty before the commit; its **Implemented differently** list is decided in the session, and the design wins. `coder` reports the directories it changed so the session can run this.

(e) In the Delegate-by-default paragraph, extend "Workers that judge — `cold-reader`, `discipline-reviewer`, `reviewer` — run on Opus" to include `drift-reviewer`.

- [ ] **Step 5: roadmap.md**

Append to the first paragraph (line 3), after "the commit history is the record of what has been built.": "The claims a milestone will build are the `end-goal/claims.txt` lines whose status names it, and a milestone's completion is those lines turning `built` in the commits that cite them."

- [ ] **Step 6: Run the consistency pass**

From the root: `tools/consistency-commands.sh`
Expected: no gate output. The link check reads the root `CLAUDE.md`? No: it skips `CLAUDE.md` by name; verify `end-goal/CLAUDE.md`'s own links still resolve. Then, per `end-goal/CLAUDE.md`, dispatch `cold-reader` on `end-goal/CLAUDE.md` alone, since this edit changed prose there.

- [ ] **Step 7: Dispatch drift-reviewer once as a live test**

Pick `factory/window` and dispatch `drift-reviewer` with its cited claims. Confirm it returns three lists in the shape above. Record anything in the first or third list for the owner.

- [ ] **Step 8: Commit point**

```bash
git add .claude/agents/drift-reviewer.md .claude/agents/coder.md end-goal/CLAUDE.md CLAUDE.md roadmap.md
git commit -m "the drift check: a cold judgment of a package against the sentences it cites"
```

---

## Self-review against the spec

- Section 1 (inventory): Task 1 parses and checks; Task 7 writes it; Task 6 step 1 describes it. Covered.
- Section 2 (machine check): sentence once, file/status/milestone (Task 1); unknown id, built uncited, unbuilt/stated cited, citing no claim with `cmd/` exempt (Task 2); bare name (Task 3); package name with allowance in `names.go` (Task 4); fence commands and CI step (Task 6); tests per failure (Tasks 1 to 4). Covered.
- Section 3 (judgment): agent (Task 10 step 1), design-edit trigger via the fence reading plus the drift check subsection (Task 6 step 2, Task 10 step 3), code-edit rule (Task 10 step 4d, coder.md). Covered.
- Section 4 (bootstrap): Tasks 5 to 9 in the spec's order; the check is wired before the inventory exists and CI is pushed only at Task 9. Covered.
- Section 5 (where written): root CLAUDE.md (10.4), end-goal/CLAUDE.md (6.1, 6.2, 10.3), factory/README.md (5.3), roadmap.md (10.5), agents (10.1, 10.2), names.go (Task 4). Covered.
- Type consistency: `Claim` fields, `Citation`, `ParseClaims`, `CheckClaims`, `ExtractCitations`, `CheckCitations`, `Unclaimed`, `isCommand`, `citesClaims`, `PackageDirs`, `DesignPhrases`, `CheckPackageNames`, `namedOtherwise` are named identically across Tasks 1, 2, 4, 5.
