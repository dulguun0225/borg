package screenstatemachine

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

// What drives each screen into the states its machine declares. It is code in
// the build like the encoding of a criterion, and it is derived from the build
// on the same terms: picked out by the screen and the state it names, in a
// marker of fixed shape.

// driverMarker is the shape of a driver's naming in source: the screen's id —
// [IDPrefix], an underscore and the 32 hexadecimal characters [record.NewID]
// produces — a colon, and the state. Submatch 1 is the screen; submatch 2 is a
// hexadecimal character following the id, which [Drivers] reads as a longer run
// of hexadecimal and not an id; submatch 3 is the state.
//
// An id directly after a letter or a digit does not count, so a longer
// identifier that happens to contain the shape is not a naming. An underscore
// before it does count, for the reason package criterion's own marker states: a
// driver's name is a Go identifier, so the id inside one is preceded by a word
// character.
var driverMarker = regexp.MustCompile(`(?:^|[^0-9A-Za-z])(ssm_[0-9a-f]{32})([0-9a-f]?):([A-Za-z0-9_-]+)`)

// Driver is one driver as the build declares it: the screen it drives and the
// state it drives that screen into.
type Driver struct {
	Screen string
	State  string
}

// DriverDerivation is what reading the drivers out of a build produced: which
// toolchain's extractor ran, what it covered, the drivers it found, and the
// reason it could not derive them at all. It is a record and not a list,
// because "no state is driven" and "nothing was visible" call for opposite
// responses — a build no extractor covers is could not derive, and a caller
// reading that as an empty list would reject every state in force for a build
// it never read.
type DriverDerivation struct {
	// Toolchain is the extractor that ran, and is empty where none did.
	Toolchain string
	// Coverage is what that extractor read, in the words the build's readers
	// are shown.
	Coverage string
	// Drivers is what it found, empty where it could not derive.
	Drivers []Driver
	// CouldNotDerive is why nothing was derived, and is empty where the
	// extraction ran.
	CouldNotDerive string
}

// DeriveDrivers reads the drivers out of a checkout of the build. Go is the one
// toolchain with an extractor, recognised by a go.mod at the root of the
// checkout; every other build is could not derive, a record naming that no
// extractor covers it.
func DeriveDrivers(dir string) (DriverDerivation, error) {
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		return DriverDerivation{
			CouldNotDerive: "no extractor covers this build: the factory ships one for Go, recognised by a go.mod at the root of the checkout",
		}, nil
	}
	found, err := Drivers(dir)
	if err != nil {
		return DriverDerivation{}, err
	}
	return DriverDerivation{
		Toolchain: Toolchain,
		Coverage:  "the screen and state each driver names in the .go files under the checkout",
		Drivers:   found,
	}, nil
}

// Drivers is every distinct screen and state named anywhere in a .go file under
// dir — dir being a checkout of the build. A driver is code picked out by what
// it names, so naming the screen and the state is the whole of how the build
// says a state is driven. Each pair appears once, in the order the walk first
// finds it, which is lexical by path.
//
// What the shape-match costs: a naming in a comment or a string counts the same
// as one in a driver's own name, because the check reads text and not what the
// code does. The gate this feeds rests on the driver running, not on this list
// being more than where the namings are.
func Drivers(dir string) ([]Driver, error) {
	var found []Driver
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			return nil
		}
		text, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range driverMarker.FindAllSubmatch(text, -1) {
			// A hexadecimal character after the thirty-second is a longer run of
			// hexadecimal, and its first 32 characters are not an id.
			if len(match[2]) > 0 {
				continue
			}
			one := Driver{Screen: string(match[1]), State: string(match[3])}
			if !slices.Contains(found, one) {
				found = append(found, one)
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("screenstatemachine: reading the drivers under %s: %w", dir, err)
	}
	return found, nil
}

// StateNotDrivenError is a state in force for the build that nothing drives:
// the run on the candidate environment has no way to put the screen in it, so
// the predicates decided over that state are decided over nothing.
type StateNotDrivenError struct {
	Screen, State string
}

func (e *StateNotDrivenError) Error() string {
	return fmt.Sprintf("screenstatemachine: %s declares the state %s and no driver in the build names it",
		e.Screen, e.State)
}

// DriverNotDeclaredError is a driver naming a state no machine in force
// declares — a state of a superseded machine, or of a screen this build has
// none of.
type DriverNotDeclaredError struct {
	Screen, State string
}

func (e *DriverNotDeclaredError) Error() string {
	return fmt.Sprintf("screenstatemachine: a driver in the build names %s of screen %s, which no machine in force declares",
		e.State, e.Screen)
}

// DriversCouldNotDeriveError is a build whose drivers no extractor could read.
// An absent extraction reads as could not derive and never as no drivers, so
// the gate resolves a human here rather than passing a build nothing was read
// from.
type DriversCouldNotDeriveError struct {
	Reason string
}

func (e *DriversCouldNotDeriveError) Error() string {
	return "screenstatemachine: the drivers could not be derived: " + e.Reason
}

// CheckDrivers rejects in both the directions the Implementation gate rejects
// in over the drivers, one error per pair joined: a state in force for the
// build that nothing drives is a [*StateNotDrivenError], and a driver naming a
// state no machine in force declares is a [*DriverNotDeclaredError]. A
// derivation that could not be made is a [*DriversCouldNotDeriveError] and
// nothing else, there being no list to check either way.
func CheckDrivers(derived DriverDerivation, inForce []Machine) error {
	if derived.CouldNotDerive != "" {
		return &DriversCouldNotDeriveError{Reason: derived.CouldNotDerive}
	}
	driven := make(map[Driver]bool, len(derived.Drivers))
	for _, d := range derived.Drivers {
		driven[d] = true
	}
	declared := make(map[Driver]bool)
	for _, m := range inForce {
		for _, state := range m.States {
			declared[Driver{Screen: m.Screen, State: state}] = true
		}
	}

	var defects []error
	for _, m := range inForce {
		for _, state := range m.States {
			if !driven[Driver{Screen: m.Screen, State: state}] {
				defects = append(defects, &StateNotDrivenError{Screen: m.Screen, State: state})
			}
		}
	}
	for _, d := range derived.Drivers {
		if !declared[d] {
			defects = append(defects, &DriverNotDeclaredError{Screen: d.Screen, State: d.State})
		}
	}
	return errors.Join(defects...)
}
