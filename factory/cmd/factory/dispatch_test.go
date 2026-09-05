// Tests of dispatch: a subcommand outside the set is refused with the
// list of them.
package main

import (
	"strings"
	"testing"
)

// TestASubcommandOutsideTheSetIsRefused: dispatch names what there is, so a typo
// is answered with the list rather than with nothing happening.
func TestASubcommandOutsideTheSetIsRefused(t *testing.T) {
	if err := dispatch(nil); err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("dispatch with no subcommand = %v, want the list of them", err)
	}
	err := dispatch([]string{"authour"})
	if err == nil || !strings.Contains(err.Error(), subcommands) {
		t.Errorf("dispatch of a misspelt subcommand = %v, want the list of them", err)
	}
	if err := walkCommand(nil); err == nil {
		t.Error("walk with no deploy id was accepted")
	}
	if err := runCommand(nil); err == nil || !strings.Contains(err.Error(), "required") {
		t.Errorf("run with no flags = %v, want a required flag named", err)
	}
}
