package environment

import (
	"fmt"
	"strings"
)

// The targets are stored as one column holding one address per line. Plural is
// what the design requires and a column is what it says they are — a field
// rather than records of their own — and a newline is the separator because an
// address may hold a comma and may not hold a line ending.
//
// What that costs: reading the targets of every environment is a scan and a
// split rather than a query, which is the right trade while a deploy reads the
// targets of the one environment it is deploying into.

func joinTargets(targets []string) string { return strings.Join(targets, "\n") }

func splitTargets(stored string) []string {
	if stored == "" {
		return nil
	}
	return strings.Split(stored, "\n")
}

// What a candidate's environment was composed from is stored the same way: one
// dependency per line, the service id and the release id separated by a space.
// Neither id holds a space or a line ending — both are [record.NewID]'s
// alphabet — so the two separators need no escaping, and a stored line that does
// not split into two is an error rather than a silently short entry.

func joinComposed(composedFrom []Composed) string {
	lines := make([]string, 0, len(composedFrom))
	for _, d := range composedFrom {
		lines = append(lines, d.ServiceID+" "+d.ReleaseID)
	}
	return strings.Join(lines, "\n")
}

func splitComposed(stored string) ([]Composed, error) {
	if stored == "" {
		return nil, nil
	}
	var composedFrom []Composed
	for _, line := range strings.Split(stored, "\n") {
		serviceID, releaseID, ok := strings.Cut(line, " ")
		if !ok || serviceID == "" || releaseID == "" {
			return nil, fmt.Errorf("environment: %q is not a service id and a release id", line)
		}
		composedFrom = append(composedFrom, Composed{ServiceID: serviceID, ReleaseID: releaseID})
	}
	return composedFrom, nil
}
