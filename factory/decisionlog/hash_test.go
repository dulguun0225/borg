package decisionlog

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/record"
)

func fixedRow() Row {
	return Row{
		Seq:           2,
		ID:            "dl_00112233445566778899aabbccddeeff",
		FormatVersion: "decision/1",
		Actor:         record.Actor{Kind: record.KindComponent, Key: "gate.merge_to_master", Basis: record.BasisClaimed},
		At:            "2026-08-17T00:00:00.000000000Z",
		Shape:         ShapeDecision,
		Payload:       `{"verdict":"pass"}`,
		PolicyVersion: "policy-1",
		ScoreVersion:  "score-1",
		Part:          PartOpen,
		PrevHash:      "0000000000000000000000000000000000000000000000000000000000000001",
	}
}

// TestChainHashIsFixed is what stops the serialisation changing by accident.
// A change to the field order, the framing, or a stored format version
// changes this value, and changing it is changing what every stored hash
// means.
func TestChainHashIsFixed(t *testing.T) {
	const want = "192516cf838ab9bad1957f1f062e2c8908cf8951edc3c4389fe9ea7a6aefebb3"
	if got := fixedRow().ChainHash(); got != want {
		t.Fatalf("ChainHash() = %q, want %q", got, want)
	}
}

// TestChainHashCoversEveryField changes one field at a time and requires the
// hash to move. A field the hash does not cover is a field that can be
// edited in the store without Verify noticing.
func TestChainHashCoversEveryField(t *testing.T) {
	base := fixedRow().ChainHash()
	changes := map[string]func(*Row){
		"FormatVersion":  func(r *Row) { r.FormatVersion = "page_event/1" },
		"Seq":            func(r *Row) { r.Seq = 3 },
		"ID":             func(r *Row) { r.ID = "dl_ffeeddccbbaa99887766554433221100" },
		"Actor.Kind":     func(r *Row) { r.Actor.Kind = record.KindHuman },
		"Actor.Key":      func(r *Row) { r.Actor.Key = "person:abc" },
		"Actor.Basis":    func(r *Row) { r.Actor.Basis = record.BasisVerified },
		"At":             func(r *Row) { r.At = "2026-08-17T00:00:00.000000001Z" },
		"Shape":          func(r *Row) { r.Shape = ShapeWait },
		"Payload":        func(r *Row) { r.Payload = `{"verdict":"fail"}` },
		"PolicyVersion":  func(r *Row) { r.PolicyVersion = "policy-2" },
		"ScoreVersion":   func(r *Row) { r.ScoreVersion = "score-2" },
		"Part":           func(r *Row) { r.Part = PartClose },
		"Closes":         func(r *Row) { r.Closes = "dl_ffeeddccbbaa99887766554433221100" },
		"Verdict":        func(r *Row) { r.Verdict = "approve" },
		"Reason":         func(r *Row) { r.Reason = "because" },
		"OpenedInWorkAt": func(r *Row) { r.OpenedInWorkAt = "2026-08-17T00:00:00.000000000Z" },
		"SelfApproval":   func(r *Row) { r.SelfApproval = true },
		"PrevHash":       func(r *Row) { r.PrevHash = strings.Repeat("a", 64) },
	}
	for field, change := range changes {
		t.Run(field, func(t *testing.T) {
			row := fixedRow()
			change(&row)
			if row.ChainHash() == base {
				t.Fatalf("changing %s did not change the hash", field)
			}
		})
	}
}

// TestChainHashCannotBeForgedAcrossFields is what the length prefix is for.
// Moving a character from one field into the next leaves the concatenation
// identical and has to leave the hash different.
func TestChainHashCannotBeForgedAcrossFields(t *testing.T) {
	one := fixedRow()
	one.Actor.Key = "gate.merge_to_master"
	one.Payload = "x"

	other := fixedRow()
	other.Actor.Key = "gate.merge_to_masterx"
	other.Payload = ""

	if one.ChainHash() == other.ChainHash() {
		t.Fatal("two rows differing only in where a field ends hash the same")
	}
}

// TestAdvisoryLockKeyIsDerivedFromTheName recomputes the constant from the
// name schema.go says it comes from.
func TestAdvisoryLockKeyIsDerivedFromTheName(t *testing.T) {
	sum := sha256.Sum256([]byte("borg/factory/decisionlog"))
	want := int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
	if AdvisoryLockKey != want {
		t.Fatalf("AdvisoryLockKey = %#x, the name hashes to %#x", AdvisoryLockKey, want)
	}
	if AdvisoryLockKey <= 0 {
		t.Fatalf("AdvisoryLockKey = %d, want a positive value", AdvisoryLockKey)
	}
}

// TestDDLListsEveryShape keeps the CHECK constraint and [Shapes] from
// disagreeing, the way TestConstraintsListEveryKind does for actor kinds.
func TestDDLListsEveryShape(t *testing.T) {
	ddl := strings.Join(DDL, "\n")
	const open = "shape in ("
	i := strings.Index(ddl, open)
	if i < 0 {
		t.Fatalf("the DDL has no %q list", open)
	}
	rest := ddl[i+len(open):]
	j := strings.Index(rest, ")")
	if j < 0 {
		t.Fatalf("the %q list is not closed", open)
	}
	listed := strings.FieldsFunc(rest[:j], func(r rune) bool { return r == ',' || r == '\n' || r == '\t' })
	if len(listed) != len(Shapes) {
		t.Fatalf("the constraint lists %d shapes, Shapes has %d: %v", len(listed), len(Shapes), listed)
	}
	for n, s := range Shapes {
		if got, want := strings.TrimSpace(listed[n]), "'"+string(s)+"'"; got != want {
			t.Errorf("the constraint lists %s where Shapes has %s", got, want)
		}
	}
}

// TestFormatVersionsMatchDDL keeps [Formats] and format_version_matches_shape
// from disagreeing: every pair Formats declares is in the DDL text, and the
// DDL declares no more pairs than Formats has.
func TestFormatVersionsMatchDDL(t *testing.T) {
	ddl := strings.Join(DDL, "\n")
	for fv, shape := range Formats {
		want := fmt.Sprintf("format_version = '%s' and shape = '%s'", fv, shape)
		if !strings.Contains(ddl, want) {
			t.Errorf("the DDL does not pair format version %q with shape %q", fv, shape)
		}
	}
	if got, want := strings.Count(ddl, "format_version = '"), len(Formats); got != want {
		t.Errorf("the DDL pairs %d format versions, Formats has %d", got, want)
	}
}

// TestShapesAndFormatsAgree is what keeps [Shapes] and [Formats] describing
// the same ten shapes: every shape has at least one format version naming
// it, and every format version names a shape in [Shapes].
func TestShapesAndFormatsAgree(t *testing.T) {
	named := make(map[Shape]bool, len(Shapes))
	for fv, shape := range Formats {
		found := false
		for _, s := range Shapes {
			if s == shape {
				found = true
			}
		}
		if !found {
			t.Errorf("format version %q names shape %q, which is not in Shapes", fv, shape)
		}
		named[shape] = true
	}
	for _, s := range Shapes {
		if !named[s] {
			t.Errorf("shape %q has no format version in Formats", s)
		}
	}
}
