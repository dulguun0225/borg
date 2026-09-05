// Every row's actions and what it waits on.
package gate_test

import (
	"slices"
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
)

// TestEveryRowHasActionsAndOffersRefer: nothing may be fired that has no
// actions, and refer is on every row because it is about the human at it and
// not about the event — who a row waits on beyond that is read from the People
// declaration and the holds standing at the firing, and is not a fact of the
// row alone.
func TestEveryRowHasActionsAndOffersRefer(t *testing.T) {
	for _, row := range gate.Rows {
		actions, err := gate.Actions(row)
		if err != nil {
			t.Errorf("Actions(%s) = %v", row, err)
			continue
		}
		if len(actions) < 2 {
			t.Errorf("%s offers %v, and every row approves and does one other thing", row, actions)
		}
		if actions[0] != gate.VerdictApprove {
			t.Errorf("%s does not offer approve first: %v", row, actions)
		}
		if !slices.Contains(actions, gate.VerdictRefer) {
			t.Errorf("%s does not offer refer: %v", row, actions)
		}
	}
}
