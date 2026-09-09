package policy

import "github.com/dulguun0225/borg/factory/gatepolicy"

// Source is where the value in force came from before any safeguard clamped it.
type Source string

const (
	// FromAuthored is a value an owner authored on the record its scope names.
	FromAuthored Source = "authored"
	// FromSupplied is what the score supplies where an owner authored nothing.
	FromSupplied Source = "supplied"
	// FromNothing is neither an authored value, nor one the design fixes, nor
	// one the score supplies.
	FromNothing Source = "neither"
	// FromFactory is the value the design fixes where an owner authored none —
	// [gatepolicy.Definition.Unauthored] — rather than one an outcome teaches:
	// all of a release's instances kept, every hour a paging hour, no proof test
	// at all, the log and the report store kept for the life of the install,
	// arrival unbounded, and a snapshot standing until an owner deletes it. The
	// list of allowed predicate kinds is the same source read as a list, and it
	// is a floor rather than a value replaced: gate policy has an owner extend
	// the list and a safeguard only add to it, which presupposes something to
	// extend.
	FromFactory Source = "the factory's own"
)

func sourceOf(authored gatepolicy.Authored) Source {
	if authored.Present {
		return FromAuthored
	}
	return FromSupplied
}
