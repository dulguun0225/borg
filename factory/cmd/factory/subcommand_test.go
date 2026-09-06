// Tests of the switch on the subcommand name: a subcommand outside the set is
// refused with the list of them, and each of the six a human uses on something
// already running refuses an invocation that names too little to act on.
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
	err := chosen([]string{"authour"})
	if err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("a misspelt subcommand = %v, want the list of them", err)
	}
	if err := walkCommand(nil); err == nil {
		t.Error("walk with no deploy id was accepted")
	}
	if err := runCommand(nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("run with no flags = %v, want a required flag named", err)
	}
}

// TestTheSubcommandsOnSomethingRunningRefuseAnEmptyInvocation: each of the six
// names what it acts on and what it is for, and refuses before it opens the
// store where either is missing — a rollback with no reason and a truncation
// with no boundary are the two the design itself refuses.
func TestTheSubcommandsOnSomethingRunningRefuseAnEmptyInvocation(t *testing.T) {
	refusals := map[string]func([]string) error{
		"rollback":      rollbackCommand,
		"drop":          dropCommand,
		"accept-commit": acceptCommitCommand,
		"mark-rollback": markRollbackCommand,
		"mitigate":      mitigateCommand,
		"truncate":      truncateCommand,
	}
	for name, command := range refusals {
		t.Run(name, func(t *testing.T) {
			if err := command(nil); err == nil {
				t.Errorf("%s with no argument was accepted", name)
			}
		})
	}
	if err := rollbackCommand([]string{"demo"}); err == nil || !strings.Contains(err.Error(), "-reason") {
		t.Errorf("rollback with no reason = %v, want the reason named", err)
	}
	if err := markRollbackCommand([]string{"dep_1"}); err == nil || !strings.Contains(err.Error(), "-reason") {
		t.Errorf("mark-rollback with no reason = %v, want the reason named", err)
	}
	if err := truncateCommand(nil); err == nil || !strings.Contains(err.Error(), "-boundary") {
		t.Errorf("truncate with no boundary = %v, want the boundary named", err)
	}
	if err := acceptCommitCommand([]string{"demo"}); err == nil || !strings.Contains(err.Error(), "two arguments") {
		t.Errorf("accept-commit with one argument = %v, want both named", err)
	}
	// Every one of the six is reachable by name: a subcommand the switch does
	// not know is refused with the list, so an error that is not that one is a
	// subcommand the switch answers.
	for name := range refusals {
		if err := chosen([]string{name}); err != nil && strings.Contains(err.Error(), subcommands) {
			t.Errorf("%s is not one of the subcommands the switch answers", name)
		}
	}
}
