// Every row's actions and what it waits on.
package gate_test

import (
	"testing"

	"github.com/dulguun0225/borg/factory/gate"
)

// TestEveryRowHasActionsAndAWait: nothing may be fired that has no actions, and
// a row that waits on nobody would leave a pending decision no reader can chase.
func TestEveryRowHasActionsAndAWait(t *testing.T) {
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
		if gate.WaitsOn(row) == "" {
			t.Errorf("%s names nothing it waits on", row)
		}
	}
}
