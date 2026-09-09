// material.go is the classes of material a fleet entry may be handed and the
// ones it may not, withheld before the run and recorded on the manifest as
// excluded. It is split from run.go by subject at the 500-line bound.
package dispatch

import (
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/fleetentry"
	"github.com/dulguun0225/borg/factory/inputmanifest"
)

// withhold is the material the entry may be handed and the material it may not,
// which context assembly reads the classes for at every dispatch: a class the
// entry does not name is withheld before any selection rule selects anything,
// and the manifest records each withheld source as excluded with the entry as
// the reason. An entry naming no class is handed nothing but the role prompt.
//
// A class outside [fleetentry.MaterialClasses] is [ErrMaterialClassUnknown] and
// not silently withheld: the classes an entry names and the classes a stage
// hands over are one vocabulary, so a class no entry could ever name is a
// caller's mistake and not an owner's narrowing.
//
// What it withholds is what the manifest and the run record name. It is not
// what the role sends the provider: the payload each of the five methods passes
// is the caller's own, assembled from the same sources, and this strips nothing
// out of it — doc.go says so, that being what context assembly would own.
func withhold(entry Entry, material []inputmanifest.Material) ([]inputmanifest.Material,
	[]inputmanifest.Exclusion, error) {
	var handed []inputmanifest.Material
	var withheld []inputmanifest.Exclusion
	for _, one := range material {
		if !slices.Contains(fleetentry.MaterialClasses, one.Class) {
			return nil, nil, fmt.Errorf("%w: %q on %s", ErrMaterialClassUnknown, one.Class, one.Reference)
		}
		if slices.Contains(entry.MaterialClasses, one.Class) {
			handed = append(handed, one)
			continue
		}
		withheld = append(withheld, inputmanifest.Exclusion{
			What:   one.Reference,
			Reason: "withheld: the fleet entry " + entry.ID + " does not name class " + one.Class,
		})
	}
	return handed, withheld, nil
}

// sourcesOf is the sources handed over, as the agent run record names them:
// the reference of each material the manifest was written from, in the order
// the stage handed them over. It is called with what the entry's classes
// admitted and never with what the stage offered, so a class the entry does not
// name is on the manifest as excluded and on no run record as a source: the
// manifest names what was withheld and the run record names what was sent, and
// both name a source by reference.
func sourcesOf(material []inputmanifest.Material) []string {
	sources := make([]string, 0, len(material))
	for _, one := range material {
		sources = append(sources, one.Reference)
	}
	return sources
}
