// The People view on a fresh install: the owner's row, which comes from no
// record and is the one place a human at a screen can read the key their calls
// have to carry.
package main

import (
	"testing"

	"github.com/dulguun0225/borg/factory/screens"
)

// TestAFreshInstallsPeopleViewHoldsTheOwnerRow: the design gives the owner no
// record, so an install declares nothing about them — and the key every acting
// call is exempted on, which ./calls.go reads off the deps, would then be
// readable nowhere the four screens reach. The view holds it as its own first
// row instead: the owner's key, the name -human gave it, acting anywhere, and
// marked as the owner's so the screen can say which row it is.
//
// The one thing the row carries on a fresh install is the model credential the
// install lent as the owner's, which is what a fleet entry names and what every
// agent run record says whose account it spent.
func TestAFreshInstallsPeopleViewHoldsTheOwnerRow(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	var view screens.People
	s.get(t, "/api/people", &view)

	if len(view.Rows) != 1 {
		t.Fatalf("a fresh install's People view holds %d rows, want the owner's alone: %+v",
			len(view.Rows), view.Rows)
	}
	row := view.Rows[0]
	if !row.Owner {
		t.Errorf("the one row is not marked as the owner's: %+v", row)
	}
	if !row.ActsAnywhere {
		t.Errorf("the owner's row reads as acting nowhere, and every unheld row widens to them: %+v", row)
	}
	if row.Key != s.p.human.Key {
		t.Errorf("the owner's row carries key %q, want the key the deps were composed with, %q",
			row.Key, s.p.human.Key)
	}
	if row.Name != d.human {
		t.Errorf("the owner's row is named %q, want the name -human gave, %q", row.Name, d.human)
	}
	if len(row.Duties) != 0 || len(row.Obligations) != 0 {
		t.Errorf("the owner's row holds a duty or an obligation on a fresh install: %+v", row)
	}
	if len(row.Credentials) != 1 || row.Credentials[0].Name != d.modelCredentialName {
		t.Errorf("the owner's credentials are %+v, want the one the install lent, %q",
			row.Credentials, d.modelCredentialName)
	}
}

// TestTheOwnersRowStaysFirstOnceSomebodyElseHoldsADuty: the row is first because
// a human who has declared nothing is looking for it, and a declaration written
// after it does not move it.
func TestTheOwnersRowStaysFirstOnceSomebodyElseHoldsADuty(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	alice := owner(t, ctx, d.pool, d.token, "alice")
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: alice.Key, Duty: 1})

	var view screens.People
	s.get(t, "/api/people", &view)

	if len(view.Rows) != 2 {
		t.Fatalf("the People view holds %d rows, want the owner's and alice's: %+v",
			len(view.Rows), view.Rows)
	}
	if view.Rows[0].Key != s.p.human.Key || !view.Rows[0].Owner {
		t.Errorf("the first row is %+v, want the owner's", view.Rows[0])
	}
	if view.Rows[1].Key != alice.Key || view.Rows[1].Owner {
		t.Errorf("the second row is %+v, want alice's and not marked as the owner's", view.Rows[1])
	}
}
