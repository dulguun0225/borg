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
// underscore, and the 32 hexadecimal characters [record.NewID] produces. The
// word boundaries keep a longer identifier that happens to contain one from
// counting as naming it.
var encodingID = regexp.MustCompile(`\bcr_[0-9a-f]{32}\b`)

// Encodings is every distinct criterion id named anywhere in a _test.go file
// under dir — dir being a checkout of the build. An encoding is code picked
// out by the criterion id it names, so naming the id is the whole of how the
// build says a criterion is encoded. Each id appears once, in the order the
// walk first finds it, which is lexical by path.
//
// What the shape-match costs: an id in a comment or a string counts the same
// as one in a test's name or body, because the check reads text and not what
// the test does. The gate this feeds rests on the encoding running, not on
// this list being more than where the ids are.
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
		for _, match := range encodingID.FindAll(text, -1) {
			if id := string(match); !seen[id] {
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
