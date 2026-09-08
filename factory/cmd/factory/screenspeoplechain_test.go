// The People declaration as a chain, over HTTP: a duty declared on two keys,
// the second holder withdrawn so one row can be closed with a self-approval
// count and restored after, and the mapping erased with the key standing on
// every record it was written to and the erasure-list row landing before it.
package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/screens"
)

// theRoutedDuty is the duty the safeguard in this test routes its rows to. Any
// of the twelve would do; what the test turns on is how many humans hold it.
const theRoutedDuty = 6

// TestASecondHolderIsWhatMakesAWithdrawalDecidable is the People declaration
// read as a chain rather than as today's state: a safeguard's withdrawal is
// routed away from the human who wrote it, so the row is decidable only while
// somebody else holds its duty. Withdrawing the second holder leaves the writer
// as the only decider, and the close then carries the self-approval field —
// which is what an install that cannot separate the two records instead of
// refusing.
//
// The holder is restored afterwards, and the declaration read today shows both
// holding again: that the row closed at a moment when only one did is in the
// chain of policy versions and nowhere else.
func TestASecondHolderIsWhatMakesAWithdrawalDecidable(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	// The report store, because erasing a mapping appends a row of the erasure
	// list and that store is the list's one writer.
	erased := newReports(t, ctx, &d)
	s := newScreens(t, ctx, d, out)

	alice := owner(t, ctx, d.pool, d.token, "alice")
	bob := owner(t, ctx, d.pool, d.token, "bob")
	before := len(versionsOf(t, ctx, d))
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: alice.Key, Duty: theRoutedDuty})
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: bob.Key, Duty: theRoutedDuty})

	// A safeguard whose rows route to that duty, so its withdrawal's row waits
	// on its holders rather than widening to the owner. Only a safeguard that
	// adds a human at a gate routes anything, which is the risk threshold and
	// no other parameter.
	safeguardID := s.mustCall(t, "placeSafeguard", screens.PlaceSafeguardArgs{
		Parameter: "risk_threshold", SubjectKind: "gate_row",
		SubjectName: "deploy_to_production", ServiceName: theService,
		RouteDuty: theRoutedDuty,
	})
	if safeguardID == "" {
		t.Fatal("placeSafeguard answered with no id")
	}

	s.principal = alice.Key
	s.mustCall(t, "withdrawSafeguard", screens.WithdrawSafeguardArgs{SafeguardID: safeguardID})
	var withdrawalID string
	if err := d.pool.QueryRow(ctx, `select id from `+safeguard.WithdrawalTable+
		` where safeguard_id = $1`, safeguardID).Scan(&withdrawalID); err != nil {
		t.Fatalf("reading the withdrawal that was written: %v", err)
	}

	// Alice wrote it and Bob holds the duty, so the row does not route to her.
	status, body := s.call(t, "decideRecordRow", screens.DecideRecordRowArgs{
		RowKind: gate.SafeguardWithdrawal.String(), RecordID: withdrawalID,
		Verdict: string(gate.VerdictApprove),
	})
	if status == http.StatusNoContent || status == http.StatusOK {
		t.Fatalf("the human who wrote the withdrawal decided its own row while another holder existed: %s", body)
	}
	if !strings.Contains(body, "could decide it") {
		t.Errorf("the refusal reads %s, want it naming who could decide it instead", body)
	}

	// Bob's holding is withdrawn. The row is kept and not deleted: who held a
	// duty at any row is a read of the chain.
	s.principal = s.p.human.Key
	s.mustCall(t, "withdrawDuty", screens.WithdrawDutyArgs{HumanKey: bob.Key, Duty: theRoutedDuty})

	// Alice is now the only decider, so the row fires to her, closes, and the
	// close says what it is.
	s.principal = alice.Key
	s.mustCall(t, "decideRecordRow", screens.DecideRecordRowArgs{
		RowKind: gate.SafeguardWithdrawal.String(), RecordID: withdrawalID,
		Verdict: string(gate.VerdictApprove), OpenedInWorkAt: theOpenedInWorkAt,
	})
	s.principal = s.p.human.Key

	closing := closingByActor(t, ctx, d, alice.Key)
	if !closingPayload(t, closing).SelfApproval {
		t.Errorf("the close by the writer of the record carries no self-approval field: %s", closing.Payload)
	}

	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	counted := int64(0)
	for _, one := range factory.SelfApprovalCounts {
		if one.HumanKey == alice.Key {
			counted = one.Count
		}
	}
	if counted != 1 {
		t.Errorf("Factory counts %d self-approval(s) for the writer, want the one: %+v",
			counted, factory.SelfApprovalCounts)
	}

	// The holder is restored. The declaration read today shows both holding,
	// which is what it showed before the withdrawal — so nothing in it says the
	// row closed at a moment when only one did.
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: bob.Key, Duty: theRoutedDuty})
	holders, err := people.Holders(ctx, d.pool, people.OfDuty(theRoutedDuty))
	if err != nil {
		t.Fatalf("reading the holders of duty %d: %v", theRoutedDuty, err)
	}
	if len(holders) != 2 {
		t.Errorf("%d human(s) hold duty %d today, want both after the second was restored: %v",
			len(holders), theRoutedDuty, holders)
	}

	// Three of the writes above were on the declaration — Bob declared,
	// withdrawn and declared again — and each appended a policy version, which
	// is the chain the moment the row closed is read from.
	if after := len(versionsOf(t, ctx, d)); after-before < 3 {
		t.Errorf("%d policy version(s) were appended by the declaration's writes, want at least three",
			after-before)
	}

	// The mapping erased: the key stands on every record it was written to, so
	// the chain, its links and its counts are undisturbed and what those
	// records name is gone.
	s.mustCall(t, "deleteMapping", screens.DeleteMappingArgs{HumanKey: alice.Key})
	if _, err := people.NameOf(ctx, d.pool, alice.Key); err == nil {
		t.Error("the mapping still resolves the erased key to a name")
	}
	// The erasure-list row landed before the deletion, keyed by the mapping's
	// key and naming the mapping, so a restore that brought the name back is
	// replayed against it.
	rows, err := erasurelist.ReadKind(erased.list, erasurelist.KindMapping)
	if err != nil {
		t.Fatalf("reading the erasure list: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != alice.Key {
		t.Fatalf("the erasure list holds %+v, want one row keyed by the erased mapping", rows)
	}
	if strings.Contains(rows[0].Removed, "alice") {
		t.Errorf("the erasure-list row says %q, and it carries the name it removed", rows[0].Removed)
	}
	var declaration screens.People
	s.get(t, "/api/people", &declaration)
	found := false
	for _, row := range declaration.Rows {
		if row.Key != alice.Key {
			continue
		}
		found = true
		if row.Name == "alice" {
			t.Errorf("People still resolves %s to %q after the mapping was erased", row.Key, row.Name)
		}
		if len(row.Duties) == 0 {
			t.Errorf("the erased key holds no duty on the People view, and only the mapping was erased: %+v", row)
		}
	}
	if !found {
		t.Errorf("People holds no row for the erased key, and the holding it left behind still routes: %+v",
			declaration.Rows)
	}

	// The key is on the withdrawal record and on the close event that decided
	// it, both unchanged.
	written, err := safeguard.GetWithdrawal(ctx, d.pool, withdrawalID)
	if err != nil {
		t.Fatalf("reading the withdrawal back: %v", err)
	}
	if written.Actor.Key != alice.Key {
		t.Errorf("the withdrawal names %q after the erasure, want the key that wrote it", written.Actor.Key)
	}
	if closingByActor(t, ctx, d, alice.Key).ID == "" {
		t.Error("the close event no longer names the key that wrote it")
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the erasure: %v", err)
	}
}

// closingByActor is the newest close event one per-person key wrote.
func closingByActor(t *testing.T, ctx context.Context, d deps, key string) decisionlog.Row {
	t.Helper()
	found := decisionlog.Row{}
	for _, row := range readLog(t, ctx, d) {
		if row.Shape == decisionlog.ShapeDecision && row.Part == decisionlog.PartClose &&
			row.Actor.Key == key {
			found = row
		}
	}
	if found.ID == "" {
		t.Fatalf("the log holds no close event written by %s", key)
	}
	return found
}
