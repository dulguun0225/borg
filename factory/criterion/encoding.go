package criterion

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// encodingID is the shape of a criterion id in test code: the [IDPrefix], an
// underscore, and the 32 hexadecimal characters [record.NewID] produces,
// followed by the place the encoding declares. Submatch 1 is the id; submatch
// 2 is a hexadecimal character following it, which [Encodings] reads as a
// longer run of hexadecimal and not an id; submatch 3 is the place marker,
// `_build` or `_candidate_environment`, and is empty where the encoding
// declares none.
//
// What the two edges are for. An id directly after a letter or a digit does
// not count, so a longer identifier that happens to contain the shape is not
// a naming — `"prefixcr_<id>"` names nothing. An underscore before it does
// count, and that is not a nicety: a Go test's name begins with `Test`, so the
// id inside one is always preceded by a word character, and requiring a word
// boundary there made `func Test_cr_<id>` unrecognisable — the exact form the
// implementation role's system prompt asks for. That was a contradiction
// between what an agent is told and what the build accepts, and the fake model
// in the end-to-end test wrote the id in a comment, where a boundary holds, so
// nothing failed until a real model named it in a test's name.
//
// The place marker is an underscore and a word for the same reason: it has to
// be writable inside a Go identifier, so that `func Test_cr_<id>_build` and
// `func Test_cr_<id>_candidate_environment` are the two ways an encoding says
// where it is decided.
var encodingID = regexp.MustCompile(`(?:^|[^0-9A-Za-z])(cr_[0-9a-f]{32})([0-9a-f]?)(_build|_candidate_environment)?`)

// Encoding is one encoding as the build declares it: the criterion it names,
// every place its namings declared, and the one place that is its declaration.
// Place is empty where the namings declared none or declared both, which
// [CheckEncodings] rejects.
type Encoding struct {
	CriterionID string
	Place       Place
	Declared    []Place
}

// Derivation is what reading the encodings out of a build produced: which
// toolchain's extractor ran, what it covered, the encodings it found, and the
// reason it could not derive them at all. It is a record and not a list,
// because "no encodings were found" and "nothing was visible" call for
// opposite responses — a build no extractor covers is could not derive, and a
// caller reading that as an empty list would reject every criterion in force
// for a build it never read.
type Derivation struct {
	// Toolchain is the extractor that ran, and is empty where none did.
	Toolchain string
	// Coverage is what that extractor read, in the words the build's readers
	// are shown.
	Coverage string
	// Encodings is what it found, empty where it could not derive.
	Encodings []Encoding
	// CouldNotDerive is why nothing was derived, and is empty where the
	// extraction ran.
	CouldNotDerive string
}

// Derive reads the encodings out of a checkout of the build. Go is the one
// toolchain with an extractor, recognised by a go.mod at the root of the
// checkout; every other build is could not derive, a record naming that no
// extractor covers it. Which toolchains have one is a fact of the factory's
// version, the arrangement a consumer contract's derivation already has.
func Derive(dir string) (Derivation, error) {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return Derivation{
			CouldNotDerive: "no extractor covers this build: the factory ships one for Go, recognised by a go.mod at the root of the checkout",
		}, nil
	}
	found, err := Encodings(dir)
	if err != nil {
		return Derivation{}, err
	}
	return Derivation{
		Toolchain: "go",
		Coverage:  "the criterion ids named in the _test.go files under the checkout",
		Encodings: found,
	}, nil
}

// Encodings is every distinct criterion id named anywhere in a _test.go file
// under dir — dir being a checkout of the build — with the place its namings
// declare. An encoding is code picked out by the criterion id it names, so
// naming the id is the whole of how the build says a criterion is encoded, and
// the marker after the id is how it says where it is decided. Each id appears
// once, in the order the walk first finds it, which is lexical by path.
//
// An id whose namings declare no place, or declare both, comes back with an
// empty [Encoding.Place]: which of the two decides a criterion is the
// encoding's own declaration, and two declarations are not one.
//
// What the shape-match costs: an id in a comment or a string counts the same
// as one in a test's name, because the check reads text and not what the test
// does. The gate this feeds rests on the encoding running, not on this list
// being more than where the ids are.
func Encodings(dir string) ([]Encoding, error) {
	var ids []string
	declared := make(map[string][]Place)
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range encodingID.FindAllSubmatch(text, -1) {
			// A hexadecimal character after the thirty-second is a longer run
			// of hexadecimal, and its first 32 characters are not an id — every
			// id is exactly that long, so reading one out of a longer run would
			// name a criterion nothing in force has.
			if len(match[2]) > 0 {
				continue
			}
			id := string(match[1])
			if _, seen := declared[id]; !seen {
				ids = append(ids, id)
				declared[id] = nil
			}
			if marker := string(match[3]); marker != "" {
				place := Place(strings.TrimPrefix(marker, "_"))
				if !slices.Contains(declared[id], place) {
					declared[id] = append(declared[id], place)
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("criterion: reading the encodings under %s: %w", dir, err)
	}

	found := make([]Encoding, 0, len(ids))
	for _, id := range ids {
		e := Encoding{CriterionID: id, Declared: declared[id]}
		if len(e.Declared) == 1 {
			e.Place = e.Declared[0]
		}
		found = append(found, e)
	}
	return found, nil
}

// NotEncodedError is a criterion in force with no encoding naming it: the
// build decides nothing about a promise the service makes.
type NotEncodedError struct {
	CriterionID string
}

func (e *NotEncodedError) Error() string {
	return fmt.Sprintf("criterion: %s is in force and no encoding in the build names it", e.CriterionID)
}

// NotInForceError is an encoding naming a criterion not in force: the build
// decides a promise the service does not make.
type NotInForceError struct {
	CriterionID string
}

func (e *NotInForceError) Error() string {
	return fmt.Sprintf("criterion: an encoding in the build names %s, which is not in force", e.CriterionID)
}

// WithdrawnError is an encoding naming a criterion that same build withdraws.
// Without it the item that withdraws a criterion leaves its encoding in
// master, deciding a promise the service no longer makes.
type WithdrawnError struct {
	CriterionID string
}

func (e *WithdrawnError) Error() string {
	return fmt.Sprintf("criterion: an encoding in the build names %s, which the build withdraws", e.CriterionID)
}

// PlaceNotDeclaredError is an encoding that does not say which of the two
// places decides it. Not every encoding needs the environment, and which one
// runs it is the author's declaration: an encoding declaring none is one the
// build runner and the deployer would each have to guess about.
type PlaceNotDeclaredError struct {
	CriterionID string
	Declared    []Place
}

func (e *PlaceNotDeclaredError) Error() string {
	if len(e.Declared) > 1 {
		return fmt.Sprintf("criterion: the encoding of %s declares %d places, and one encoding is decided in one", e.CriterionID, len(e.Declared))
	}
	return fmt.Sprintf("criterion: the encoding of %s declares neither the build nor the candidate environment", e.CriterionID)
}

// CouldNotDeriveError is a build whose encodings no extractor could read. A
// partial or absent extraction reads as could not derive and never as no
// encodings, so the gate resolves a human here rather than passing a build
// nothing was read from.
type CouldNotDeriveError struct {
	Reason string
}

func (e *CouldNotDeriveError) Error() string {
	return "criterion: the encodings could not be derived: " + e.Reason
}

// CheckEncodings rejects in every direction the Implementation gate rejects
// in, one error per id: a criterion in force with no encoding naming it is a
// [*NotEncodedError], an encoding naming a criterion the build withdraws is a
// [*WithdrawnError], an encoding naming a criterion that is not in force and
// not withdrawn is a [*NotInForceError], and an encoding that declares no
// place, or two, is a [*PlaceNotDeclaredError]. A derivation that could not be
// made is a [*CouldNotDeriveError] and nothing else, there being no list to
// check either way.
//
// Every defect is returned rather than the first found, because a caller
// acting on one at a time would rebuild once per id. withdrawn is what
// [Withdrawn] returned for the same build.
func CheckEncodings(derived Derivation, inForce []Criterion, withdrawn []string) error {
	if derived.CouldNotDerive != "" {
		return &CouldNotDeriveError{Reason: derived.CouldNotDerive}
	}
	encoded := make(map[string]Encoding, len(derived.Encodings))
	for _, e := range derived.Encodings {
		encoded[e.CriterionID] = e
	}
	inForceSet := make(map[string]bool, len(inForce))
	for _, c := range inForce {
		inForceSet[c.ID] = true
	}
	withdrawnSet := make(map[string]bool, len(withdrawn))
	for _, id := range withdrawn {
		withdrawnSet[id] = true
	}

	var defects []error
	for _, c := range inForce {
		if _, ok := encoded[c.ID]; !ok {
			defects = append(defects, &NotEncodedError{CriterionID: c.ID})
		}
	}
	for _, e := range derived.Encodings {
		switch {
		case withdrawnSet[e.CriterionID]:
			defects = append(defects, &WithdrawnError{CriterionID: e.CriterionID})
		case !inForceSet[e.CriterionID]:
			defects = append(defects, &NotInForceError{CriterionID: e.CriterionID})
		}
		if e.Place == "" {
			defects = append(defects, &PlaceNotDeclaredError{CriterionID: e.CriterionID, Declared: e.Declared})
		}
	}
	return errors.Join(defects...)
}
