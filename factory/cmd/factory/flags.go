package main

import (
	"errors"
	"fmt"
	"strings"
)

// The two repeated flags "run" takes, each a [flag.Value]: the services this
// install knows, and the intents one run is given. They are here rather than
// beside runCommand because a flag type is a parse and not a step of the path.

// serviceFlag is -service given more than once, one name and repository per
// service this install knows. It is a repeated flag rather than one name and one
// path, because an interface has consumers and the consumers are other services in
// the same factory: a run that could name only one service could never demonstrate
// a contract at all.
type serviceFlag []serviceRepo

func (s *serviceFlag) String() string {
	named := make([]string, 0, len(*s))
	for _, one := range *s {
		named = append(named, one.name+"="+one.repo)
	}
	return strings.Join(named, ", ")
}

func (s *serviceFlag) Set(value string) error {
	name, repo, found := strings.Cut(value, "=")
	name, repo = strings.TrimSpace(name), strings.TrimSpace(repo)
	if !found || name == "" || repo == "" {
		return errors.New("a service is written name=path, the path being its git repository")
	}
	for _, already := range *s {
		if already.name == name {
			return fmt.Errorf("service %q is named twice, and a service is one repository", name)
		}
	}
	*s = append(*s, serviceRepo{name: name, repo: repo})
	return nil
}

// statements is -intent given more than once, one intent per decomposition. It is a
// repeated flag rather than a count, because what a run needs per decomposition is the
// statement itself: two candidates at once is the whole of what an environment per
// candidate buys, and one flag per intent is how the command-line interface says it.
//
// An intent that changes more than one service names them before the statement,
// comma separated and then a colon — which is this interface being told what
// decomposition yields, a stage that decides the decomposition being a later
// milestone's. The
// prefix is read only where every name in it is a service this install knows, so a
// statement whose own text happens to hold a colon is still one statement.
type statements []asked

func (s *statements) String() string {
	said := make([]string, 0, len(*s))
	for _, one := range *s {
		said = append(said, strings.Join(one.services, ",")+": "+one.statement)
	}
	return strings.Join(said, "; ")
}

// setFor adds one -intent value, resolving its service prefix against the
// services this install knows. A value with no readable prefix is one statement on
// the first service named, which is what every single-service run is.
func (s *statements) setFor(value string, known []serviceRepo) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("an intent's statement is not empty")
	}
	if len(known) == 0 {
		return errors.New("an intent names the services it changes, and this install knows none")
	}
	statement, services := value, []string{known[0].name}
	if prefix, rest, found := strings.Cut(value, ":"); found {
		named := strings.Split(prefix, ",")
		all := true
		for n, name := range named {
			named[n] = strings.TrimSpace(name)
			if !namesService(known, named[n]) {
				all = false
				break
			}
		}
		if all && len(named) > 0 && strings.TrimSpace(rest) != "" {
			statement, services = strings.TrimSpace(rest), named
		}
	}
	*s = append(*s, asked{statement: statement, services: services})
	return nil
}

func namesService(known []serviceRepo, name string) bool {
	for _, one := range known {
		if one.name == name {
			return true
		}
	}
	return false
}
