package policy

import "github.com/dulguun0225/borg/factory/gatepolicy"

// Source is where the value in force came from before any safeguard clamped it.
type Source string

const (
	// FromAuthored is a value an owner authored on the record its scope names.
	FromAuthored Source = "authored"
	// FromSupplied is what the score supplies where an owner authored nothing.
	FromSupplied Source = "supplied"
	// FromNothing is neither an authored value nor a supplied one, which is a
	// numeric parameter the score supplies nothing for. Nothing reaches it today.
	FromNothing Source = "neither"
	// FromFactory is the factory's own value, which is what an owner extends
	// rather than replaces. The list of allowed predicate kinds is the one
	// parameter with this source: gate policy has an owner extend the list and a
	// safeguard only add to it, which presupposes something to extend, and the
	// score supplies none — no outcome teaches a kind of assertion. So the
	// unauthored value is the kinds this factory can decide.
	FromFactory Source = "the factory's own"
)

func sourceOf(authored gatepolicy.Authored) Source {
	if authored.Present {
		return FromAuthored
	}
	return FromSupplied
}
