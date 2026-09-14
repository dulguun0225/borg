package environment

import (
	"fmt"
	"net/url"
	"strings"
)

// The targets are stored as one column holding one target per line. Plural is
// what the design requires and a column is what it says they are — a field
// rather than records of their own — and a newline is the separator because an
// address may hold a comma and may not hold a line ending. The order the lines
// are in is the order a rollout reaches the targets in, so the column is a list
// and not a set.
//
// A line is the share declaration, a space, and the address: "share /srv/one" or
// "noshare /srv/one". The declaration comes first so that the address is
// everything after the first space and may hold spaces of its own, which an
// address named the way a registry or a repository is may.
//
// What that costs: reading the targets of every environment is a scan and a
// split rather than a query, which is the right trade while a deploy reads the
// targets of the one environment it is deploying into.

const (
	servesAShare = "share"
	servesNone   = "noshare"
)

func joinTargets(targets []Target) string {
	lines := make([]string, 0, len(targets))
	for _, t := range targets {
		declaration := servesNone
		if t.ServesAShare {
			declaration = servesAShare
		}
		lines = append(lines, declaration+" "+t.Address)
	}
	return strings.Join(lines, "\n")
}

func splitTargets(stored string) ([]Target, error) {
	if stored == "" {
		return nil, nil
	}
	var targets []Target
	for _, line := range strings.Split(stored, "\n") {
		declaration, address, ok := strings.Cut(line, " ")
		if !ok || address == "" || (declaration != servesAShare && declaration != servesNone) {
			return nil, fmt.Errorf("environment: %q is not a share declaration and an address", line)
		}
		targets = append(targets, Target{Address: address, ServesAShare: declaration == servesAShare})
	}
	return targets, nil
}

// What a candidate's environment was composed from is stored one dependency per
// line: service id, release id, then each interface and encoded address pair.
//
// The seed version and the value-set version are columns of their own rather than
// lines here, being one each rather than a list.

func joinComposed(composedFrom []Composed) string {
	lines := make([]string, 0, len(composedFrom))
	for _, d := range composedFrom {
		fields := []string{d.ServiceID, d.ReleaseID}
		for _, address := range d.Addresses {
			fields = append(fields, url.QueryEscape(address.Interface), url.QueryEscape(address.Address))
		}
		lines = append(lines, strings.Join(fields, " "))
	}
	return strings.Join(lines, "\n")
}

func splitComposed(stored string) ([]Composed, error) {
	if stored == "" {
		return nil, nil
	}
	var composedFrom []Composed
	for _, line := range strings.Split(stored, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || fields[0] == "" || fields[1] == "" || (len(fields)-2)%2 != 0 {
			return nil, fmt.Errorf("environment: %q is not a service id and a release id", line)
		}
		addresses := make([]ComposedAddress, 0, (len(fields)-2)/2)
		for n := 2; n < len(fields); n += 2 {
			interfaceName, storedAddress := fields[n], fields[n+1]
			interfaceName, err := url.QueryUnescape(interfaceName)
			if err != nil || interfaceName == "" {
				return nil, fmt.Errorf("environment: %q has an invalid composed interface", line)
			}
			address, err := url.QueryUnescape(storedAddress)
			if err != nil || address == "" {
				return nil, fmt.Errorf("environment: %q has an invalid composed address", line)
			}
			addresses = append(addresses, ComposedAddress{Interface: interfaceName, Address: address})
		}
		composedFrom = append(composedFrom, Composed{ServiceID: fields[0], ReleaseID: fields[1], Addresses: addresses})
	}
	return composedFrom, nil
}
