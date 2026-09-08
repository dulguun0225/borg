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
		"C0006\thow/../secret.md\tbuilt\tA sentence.\n" + // file escapes end-goal/
		"C0001\thow/01-a.md\tbuilt\tA sentence.\n") // duplicate of line 1
	claims, findings := ParseClaims(content)
	if len(findings) != 6 {
		t.Fatalf("findings %v, want 6", findings)
	}
	if len(claims) != 1 {
		t.Fatalf("claims %v, want the one valid line", claims)
	}
}

func TestParseClaimsReportsAnEmptySentence(t *testing.T) {
	content := []byte("C0001\thow/01-a.md\tbuilt\t   \n")
	claims, findings := ParseClaims(content)
	if len(claims) != 0 {
		t.Fatalf("claims %v, want none — the sentence is only spaces", claims)
	}
	if len(findings) != 1 || !strings.Contains(findings[0], "empty sentence") {
		t.Fatalf("findings %v, want one finding naming empty sentence", findings)
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

func TestCheckClaimsReportsReadErrorNotMissing(t *testing.T) {
	root := t.TempDir()
	// A directory at the claim's file path: it exists, but os.ReadFile
	// fails on it with an error that is not fs.ErrNotExist.
	dirAsFile := filepath.Join(root, "how", "01-a.md")
	if err := os.MkdirAll(dirAsFile, 0o755); err != nil {
		t.Fatal(err)
	}
	claims := []Claim{
		{ID: "C0001", File: "how/01-a.md", Status: "built", Sentence: "text", Line: 1},
	}
	findings := CheckClaims(claims, root, []byte(""))
	if len(findings) != 1 {
		t.Fatalf("findings %v, want 1", findings)
	}
	if !strings.Contains(findings[0], "reading") {
		t.Errorf("finding %q, want it to say reading", findings[0])
	}
	if strings.Contains(findings[0], "does not exist") {
		t.Errorf("finding %q should not say does not exist", findings[0])
	}
}

func TestCheckCoverageNamesUnclaimedDesignFiles(t *testing.T) {
	root := t.TempDir()
	for _, f := range []string{"how-the-factory-works/README.md", "how-the-factory-works/03-gates/01-a.md", "how-the-factory-works/03-gates/02-b.md", "what-the-factory-does/01-c.md", "records.md"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, f)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, f), []byte("# x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	claims := []Claim{{ID: "C0001", File: "how-the-factory-works/03-gates/01-a.md"}}
	found, err := CheckCoverage(claims, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"how-the-factory-works/03-gates/02-b.md", "what-the-factory-does/01-c.md"}
	if len(found) != len(want) {
		t.Fatalf("found %v, want %d", found, len(want))
	}
	for i, w := range want {
		if !strings.Contains(found[i], w) {
			t.Errorf("finding %d is %q, want it to name %s", i, found[i], w)
		}
	}
}

func TestNormalizeSpace(t *testing.T) {
	if got := normalizeSpace("a  b\n c\t d"); got != "a b c d" {
		t.Errorf("normalizeSpace = %q", got)
	}
}
