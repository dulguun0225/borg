package criterion_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/record"
)

// checkout writes a temp directory shaped like a checkout of a build the Go
// extractor covers: a go.mod at the root, then files by name with content as
// given. It returns the directory.
func checkout(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example\n\ngo 1.24\n"), 0o644); err != nil {
		t.Fatalf("writing go.mod: %v", err)
	}
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

// encoded is one encoding of a criterion decided on the candidate
// environment, written the way the implementation role is asked to write one.
func encoded(id string) string {
	return "func Test_" + id + "_candidate_environment(t *testing.T) {}\n"
}

// derive is [criterion.Derive] with the error handled, since every check below
// is over what it produced.
func derive(t *testing.T, dir string) criterion.Derivation {
	t.Helper()
	derived, err := criterion.Derive(dir)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	return derived
}

// TestEncodingsReadsTestFilesOnly is what counts as an encoding: a criterion
// id in a _test.go file, once per id, and nothing from any other file.
func TestEncodingsReadsTestFilesOnly(t *testing.T) {
	named := record.NewID(criterion.IDPrefix)
	elsewhere := record.NewID(criterion.IDPrefix)
	embedded := record.NewID(criterion.IDPrefix)

	dir := checkout(t, map[string]string{
		// The id appears twice and is returned once.
		"pkg/a_test.go": "package pkg\n\n// encodes " + named + "_candidate_environment\n" + encoded(named),
		// Not a _test.go file, so its id is not an encoding.
		"pkg/b.go": "package pkg\n\n// mentions " + elsewhere + "\n",
		// A longer identifier containing the shape does not name it: the id
		// sits directly after a letter.
		"pkg/c_test.go": "package pkg\n\nvar x = \"prefix" + embedded + "\"\n",
	})

	found, err := criterion.Encodings(dir)
	if err != nil {
		t.Fatalf("Encodings: %v", err)
	}
	if len(found) != 1 || found[0].CriterionID != named {
		t.Fatalf("Encodings = %v, want exactly [%s]", found, named)
	}
	if found[0].Place != criterion.PlaceCandidateEnvironment {
		t.Errorf("the encoding declares %q, want %s", found[0].Place, criterion.PlaceCandidateEnvironment)
	}
}

// TestAnEncodingDeclaresWhereItIsDecided is the split the author declares: not
// every encoding needs the environment, and the marker after the id is how the
// build says which of the two decides it.
func TestAnEncodingDeclaresWhereItIsDecided(t *testing.T) {
	unit := record.NewID(criterion.IDPrefix)
	onEnvironment := record.NewID(criterion.IDPrefix)
	dir := checkout(t, map[string]string{
		"a_test.go": "package main\n\nfunc Test_" + unit + "_build(t *testing.T) {}\n" + encoded(onEnvironment),
	})

	found, err := criterion.Encodings(dir)
	if err != nil {
		t.Fatalf("Encodings: %v", err)
	}
	places := map[string]criterion.Place{}
	for _, e := range found {
		places[e.CriterionID] = e.Place
	}
	if places[unit] != criterion.PlaceBuild {
		t.Errorf("the unit encoding declares %q, want %s", places[unit], criterion.PlaceBuild)
	}
	if places[onEnvironment] != criterion.PlaceCandidateEnvironment {
		t.Errorf("the environment encoding declares %q, want %s", places[onEnvironment], criterion.PlaceCandidateEnvironment)
	}
}

// TestATestNameNamesTheCriterion is the form the implementation role is asked
// for and the one a real model wrote: the id in the test's name, where the
// character before it is the underscore after `Test`. Nothing else in the file
// mentions the id, so this passes only if a name alone counts.
func TestATestNameNamesTheCriterion(t *testing.T) {
	named := record.NewID(criterion.IDPrefix)
	dir := checkout(t, map[string]string{
		"a_test.go": "package main\n\n" + encoded(named),
	})

	found, err := criterion.Encodings(dir)
	if err != nil {
		t.Fatalf("Encodings: %v", err)
	}
	if len(found) != 1 || found[0].CriterionID != named {
		t.Fatalf("Encodings = %v, want exactly [%s] — a test's name names it", found, named)
	}
}

// TestALongerHexadecimalRunIsNotAnID is the other edge: an id is exactly
// thirty-two hexadecimal characters, so a longer run's first thirty-two are
// not one, and reading them out would name a criterion nothing has.
func TestALongerHexadecimalRunIsNotAnID(t *testing.T) {
	named := record.NewID(criterion.IDPrefix)
	dir := checkout(t, map[string]string{
		"a_test.go": "package main\n\n// " + named + "abc\n" + encoded(named),
	})

	found, err := criterion.Encodings(dir)
	if err != nil {
		t.Fatalf("Encodings: %v", err)
	}
	if len(found) != 1 || found[0].CriterionID != named {
		t.Fatalf("Encodings = %v, want exactly [%s] — the longer run is not an id", found, named)
	}
}

// TestABuildNoExtractorCoversIsCouldNotDerive: the derivation is a record and
// not a list, because "no encodings were found" and "nothing was visible" call
// for opposite responses. A build with no go.mod is the second, and the check
// rejects on it rather than on every criterion in force.
func TestABuildNoExtractorCoversIsCouldNotDerive(t *testing.T) {
	dir := t.TempDir()
	derived, err := criterion.Derive(dir)
	if err != nil {
		t.Fatalf("Derive: %v", err)
	}
	if derived.CouldNotDerive == "" {
		t.Fatalf("Derive over a build no extractor covers = %+v, want a could-not-derive reason", derived)
	}
	if derived.Toolchain != "" || len(derived.Encodings) != 0 {
		t.Errorf("a could-not-derive derivation names %+v, want no toolchain and no encodings", derived)
	}

	inForce := []criterion.Criterion{{ID: record.NewID(criterion.IDPrefix)}}
	err = criterion.CheckEncodings(derived, inForce, nil)
	var couldNot *criterion.CouldNotDeriveError
	if !errors.As(err, &couldNot) {
		t.Fatalf("CheckEncodings over a could-not-derive derivation = %v, want a *criterion.CouldNotDeriveError", err)
	}
	var notEncoded *criterion.NotEncodedError
	if errors.As(err, &notEncoded) {
		t.Error("a build nothing was read from was rejected for a criterion having no encoding")
	}
}

// TestDeriveNamesTheToolchainAndWhatItCovered: every reader of a derivation
// reads the coverage with it, so the record says which extractor ran.
func TestDeriveNamesTheToolchainAndWhatItCovered(t *testing.T) {
	id := record.NewID(criterion.IDPrefix)
	derived := derive(t, checkout(t, map[string]string{"a_test.go": "package main\n\n" + encoded(id)}))
	if derived.Toolchain != "go" || derived.Coverage == "" {
		t.Errorf("Derive = %+v, want the go extractor and what it covered", derived)
	}
	if derived.CouldNotDerive != "" {
		t.Errorf("Derive over a Go build could not derive: %s", derived.CouldNotDerive)
	}
}

// TestCheckEncodingsPasses is the case with nothing to reject: every
// criterion in force is named, every id named is in force, and each declares
// where it is decided.
func TestCheckEncodingsPasses(t *testing.T) {
	inForce := []criterion.Criterion{{ID: record.NewID(criterion.IDPrefix)}, {ID: record.NewID(criterion.IDPrefix)}}
	dir := checkout(t, map[string]string{
		"a_test.go": encoded(inForce[0].ID) + encoded(inForce[1].ID),
	})
	if err := criterion.CheckEncodings(derive(t, dir), inForce, nil); err != nil {
		t.Fatalf("CheckEncodings = %v, want nil", err)
	}
}

// TestACriterionWithNoEncodingIsNamed is the first rejection direction: in
// force, and nothing in the build names it.
func TestACriterionWithNoEncodingIsNamed(t *testing.T) {
	present := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	unencoded := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	dir := checkout(t, map[string]string{"a_test.go": encoded(present.ID)})

	err := criterion.CheckEncodings(derive(t, dir), []criterion.Criterion{present, unencoded}, nil)
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
	dir := checkout(t, map[string]string{"a_test.go": encoded(inForce.ID) + encoded(stray)})

	err := criterion.CheckEncodings(derive(t, dir), []criterion.Criterion{inForce}, nil)
	var notInForce *criterion.NotInForceError
	if !errors.As(err, &notInForce) {
		t.Fatalf("CheckEncodings = %v, want a *criterion.NotInForceError", err)
	}
	if notInForce.CriterionID != stray {
		t.Errorf("the error names %s, want %s", notInForce.CriterionID, stray)
	}
}

// TestAnEncodingOfWhatTheBuildWithdrawsIsNamed is the third direction: the
// item that withdraws a criterion would otherwise leave its encoding in
// master, deciding a promise the service no longer makes.
func TestAnEncodingOfWhatTheBuildWithdrawsIsNamed(t *testing.T) {
	withdrawn := record.NewID(criterion.IDPrefix)
	dir := checkout(t, map[string]string{"a_test.go": encoded(withdrawn)})

	err := criterion.CheckEncodings(derive(t, dir), nil, []string{withdrawn})
	var takenDown *criterion.WithdrawnError
	if !errors.As(err, &takenDown) {
		t.Fatalf("CheckEncodings = %v, want a *criterion.WithdrawnError", err)
	}
	if takenDown.CriterionID != withdrawn {
		t.Errorf("the error names %s, want %s", takenDown.CriterionID, withdrawn)
	}
}

// TestAnEncodingDeclaringNoPlaceIsRejected: which of the two places decides an
// encoding is the author's declaration, and one declaring neither, or both, is
// one the build runner and the deployer would each have to guess about.
func TestAnEncodingDeclaringNoPlaceIsRejected(t *testing.T) {
	silent := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	both := criterion.Criterion{ID: record.NewID(criterion.IDPrefix)}
	dir := checkout(t, map[string]string{
		"a_test.go": "// " + silent.ID + "\nfunc Test_" + both.ID + "_build(t *testing.T) {}\n" + encoded(both.ID),
	})

	err := criterion.CheckEncodings(derive(t, dir), []criterion.Criterion{silent, both}, nil)
	var undeclared *criterion.PlaceNotDeclaredError
	if !errors.As(err, &undeclared) {
		t.Fatalf("CheckEncodings = %v, want a *criterion.PlaceNotDeclaredError", err)
	}
	// Both are rejected, and every defect is returned rather than the first
	// found, so both ids are in what came back.
	for _, id := range []string{silent.ID, both.ID} {
		if !strings.Contains(err.Error(), id) {
			t.Errorf("CheckEncodings = %v, which does not name %s", err, id)
		}
	}
}
