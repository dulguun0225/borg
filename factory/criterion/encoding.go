package criterion

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// encodingID is the shape of a criterion id in test code: the [IDPrefix], an
// underscore, and the 32 hexadecimal characters [record.NewID] produces.
// Submatch 1 is the id; submatch 2 is a hexadecimal character following it,
// which [Encodings] reads as a longer run of hexadecimal and not an id.
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
var encodingID = regexp.MustCompile(`(?:^|[^0-9A-Za-z])(cr_[0-9a-f]{32})([0-9a-f]?)`)

// Encodings is every distinct criterion id named anywhere in a _test.go file
// under dir — dir being a checkout of the build. An encoding is code picked
// out by the criterion id it names, so naming the id is the whole of how the
// build says a criterion is encoded. Each id appears once, in the order the
// walk first finds it, which is lexical by path.
//
// What the shape-match costs: an id in a comment or a string counts the same
// as one in a test's name, because the check reads text and not what the test
// does. The gate this feeds rests on the encoding running, not on this list
// being more than where the ids are.
func Encodings(dir string) ([]string, error) {
	var ids []string
	seen := make(map[string]bool)
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
			if id := string(match[1]); !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("criterion: reading the encodings under %s: %w", dir, err)
	}
	return ids, nil
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

// CheckEncodings rejects in both directions, one error per id: a criterion
// in force with no encoding naming it is a [*NotEncodedError], and an
// encoding naming a criterion not in force is a [*NotInForceError]. Every
// defect is returned rather than the first found, because a caller acting on
// one at a time would rebuild once per id.
func CheckEncodings(dir string, inForce []Criterion) error {
	encoded, err := Encodings(dir)
	if err != nil {
		return err
	}
	encodedSet := make(map[string]bool, len(encoded))
	for _, id := range encoded {
		encodedSet[id] = true
	}
	inForceSet := make(map[string]bool, len(inForce))
	for _, c := range inForce {
		inForceSet[c.ID] = true
	}

	var defects []error
	for _, c := range inForce {
		if !encodedSet[c.ID] {
			defects = append(defects, &NotEncodedError{CriterionID: c.ID})
		}
	}
	for _, id := range encoded {
		if !inForceSet[id] {
			defects = append(defects, &NotInForceError{CriterionID: id})
		}
	}
	return errors.Join(defects...)
}
