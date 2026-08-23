package gatepolicy

import "slices"

// Clamp applies one safeguard's bound to the value in force. A ceiling caps it,
// a floor raises it, and either leaves a value already narrower than the bound
// alone — which is the whole of "a safeguard is a bound and not a precedence". A
// safeguard that adds a human clamps nothing and returns the value unchanged;
// what it adds is read from the direction by the gate and not from a number.
func Clamp(direction Direction, bound, value float64) float64 {
	switch direction {
	case DirectionCeiling:
		return min(bound, value)
	case DirectionFloor:
		return max(bound, value)
	default:
		return value
	}
}

// ClampList applies a safeguard's bound to a list-valued parameter, which is
// the union of the two: a floor under a list may only extend it, because a kind
// of assertion added is coverage added and one removed would invalidate
// consumer contracts already ratified at a gate. The result is sorted, so two
// safeguards applied in either order give one answer.
func ClampList(bound, value []string) []string {
	union := slices.Clone(value)
	for _, name := range bound {
		if !slices.Contains(union, name) {
			union = append(union, name)
		}
	}
	slices.Sort(union)
	return union
}
