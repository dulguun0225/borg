// Tests of the switch on the subcommand name: a subcommand outside the set is
// refused with the list of them, and each of the eight refuses an invocation
// that names too little to act on.
package main

import (
	"strings"
	"testing"
)

// TestASubcommandOutsideTheSetIsRefused: the switch names what there is, so a
// typo is answered with the list rather than with nothing happening.
func TestASubcommandOutsideTheSetIsRefused(t *testing.T) {
	if err := chosen(nil); err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("no subcommand = %v, want the list of them", err)
	}
	err := chosen([]string{"approve"})
	if err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("a subcommand the screens replaced = %v, want the list of them", err)
	}
}

// TestEachSubcommandRefusesAnEmptyInvocation: each of the eight names what it
// acts on and what it is for, and refuses before it opens the store where
// either is missing — a walk with no deploy and a truncation with no boundary
// are the two the design itself refuses.
func TestEachSubcommandRefusesAnEmptyInvocation(t *testing.T) {
	// A schema of this test's own, because three of the eight — learn,
	// contracts and policy — open the store before they refuse anything, and a
	// test naming no schema would open whichever database the environment
	// points at.
	_, _ = newOwner(t)

	for _, c := range []struct {
		name string
		want string
		run  func([]string) error
	}{
		{"serve", "-secrets", serveCommand},
		{"run", "required", runCommand},
		{"walk", "one argument", walkCommand},
		{"watch", "one argument", watchCommand},
		{"truncate", "-boundary", truncateCommand},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.run(nil)
			if err == nil {
				t.Fatalf("%s with no argument was accepted", c.name)
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("%s refuses with %q, want it to name %q", c.name, err, c.want)
			}
		})
	}

	// Every one of the eight is reachable by name: a subcommand the switch does
	// not know is refused with the list, so an error that is not that one is a
	// subcommand the switch answers.
	for _, name := range []string{"serve", "run", "walk", "watch", "learn", "contracts", "policy", "truncate"} {
		if err := chosen([]string{name}); err != nil && strings.Contains(err.Error(), subcommands) {
			t.Errorf("%s is not one of the subcommands the switch answers", name)
		}
	}
}
