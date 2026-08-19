package environment

import "strings"

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
