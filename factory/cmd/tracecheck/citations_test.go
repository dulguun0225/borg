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
		{ID: "C0002", Status: "built", Line: 2},                    // uncited
		{ID: "C0003", Status: "unbuilt", Milestone: "M9", Line: 3}, // cited
		{ID: "C0004", Status: "stated", Line: 4},                   // cited
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

// TestMainKeepsOnlyTheCommandDocGosFromUncited stands in for run, which is
// not unit-testable: it composes Uncited's result with isCommand itself,
// rather than calling a function this package could test whole. Uncited
// returns every doc.go with no reference at all, command and non-command
// alike; main then keeps only the ones isCommand names, holding a
// non-command doc.go to Unclaimed instead.
func TestMainKeepsOnlyTheCommandDocGosFromUncited(t *testing.T) {
	files := []string{"cmd/depscheck/doc.go", "gate/doc.go"}
	found := Uncited(files, nil)
	if !slices.Equal(found, files) {
		t.Fatalf("Uncited found %v, want both %v", found, files)
	}
	if !isCommand(files[0]) {
		t.Errorf("isCommand(%q) = false, want true", files[0])
	}
	if isCommand(files[1]) {
		t.Errorf("isCommand(%q) = true, want false", files[1])
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
