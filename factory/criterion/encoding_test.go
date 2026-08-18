package criterion_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/record"
)

// checkout writes a temp directory shaped like a checkout of a build: files
// by name, content as given. It returns the directory.
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("making %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatalf("writing %s: %v", name, err)
		}
	}
	return dir
}

// TestEncodingsReadsTestFilesOnly is what counts as an encoding: a criterion
// id in a _test.go file, once per id, and nothing from any other file.
func TestEncodingsReadsTestFilesOnly(t *testing.T) {
	named := record.NewID(criterion.IDPrefix)
	elsewhere := record.NewID(criterion.IDPrefix)
	embedded := record.NewID(criterion.IDPrefix)

	dir := checkout(t, map[string]string{
		// The id appears twice and is returned once.
		"pkg/a_test.go": "package pkg\n\n// encodes " + named + "\nfunc Test_" + named + "(t *testing.T) {}\n",
		// Not a _test.go file, so its id is not an encoding.
		"pkg/b.go": "package pkg\n\n// mentions " + elsewhere + "\n",
		// A longer identifier containing the shape does not name it: the
		// regexp's word boundaries refuse it.
		"pkg/c_test.go": "package pkg\n\nvar x = \"prefix" + embedded + "\"\n",
	})

	ids, err := criterion.Encodings(dir)
	if err != nil {
		t.Fatalf("Encodings: %v", err)
	}
	if len(ids) != 1 || ids[0] != named {
		t.Fatalf("Encodings = %v, want exactly [%s]", ids, named)
	}
}

// TestCheckEncodingsPasses is the case with nothing to reject: every
// criterion in force is named, and every id named is in force.
func TestCheckEncodingsPasses(t *testing.T) {
	inForce := []criterion.Criterion{{ID: record.NewID(criterion.IDPrefix)}, {ID: record.NewID(criterion.IDPrefix)}}
	dir := checkout(t, map[string]string{
		"a_test.go": "// " + inForce[0].ID + "\n// " + inForce[1].ID + "\n",
	})
	if err := criterion.CheckEncodings(dir, inForce); err != nil {
		t.Fatalf("CheckEncodings = %v, want nil", err)
	}
}

// TestACriterionWithNoEncodingIsNamed is the first rejection direction: in
// force, and nothing in the build names it.
func TestACriterionWithNoEncodingIsNamed(t *testing.T) {
	encoded := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	unencoded := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	dir := checkout(t, map[string]string{
		"a_test.go": "// " + encoded.ID + "\n",
	})

	err := criterion.CheckEncodings(dir, []criterion.Criterion{encoded, unencoded})
	var notEncoded *criterion.NotEncodedError
	if !errors.As(err, &notEncoded) {
		t.Fatalf("CheckEncodings = %v, want a *criterion.NotEncodedError", err)
	}
	if notEncoded.CriterionID != unencoded.ID {
		t.Errorf("the error names %s, want %s", notEncoded.CriterionID, unencoded.ID)
	}
}

// TestAnEncodingOfNothingInForceIsNamed is the second direction: the build
// names a criterion the service does not promise.
func TestAnEncodingOfNothingInForceIsNamed(t *testing.T) {
	inForce := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	stray := record.NewID(criterion.IDPrefix)
	dir := checkout(t, map[string]string{
		"a_test.go": "// " + inForce.ID + "\n// " + stray + "\n",
	})

	err := criterion.CheckEncodings(dir, []criterion.Criterion{inForce})
	var notInForce *criterion.NotInForceError
	if !errors.As(err, &notInForce) {
		t.Fatalf("CheckEncodings = %v, want a *criterion.NotInForceError", err)
	}
	if notInForce.CriterionID != stray {
		t.Errorf("the error names %s, want %s", notInForce.CriterionID, stray)
	}
}
