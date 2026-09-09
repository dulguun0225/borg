// A safeguard placed at Factory on a subject named by kind, and withdrawn at
// the row that decides its withdrawal.
package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/gatepolicy"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestASafeguardIsPlacedOnASubjectByNameAndWithdrawnById: the direction is never
// chosen, the subject is named by kind, and withdrawing is what stops a
// mechanism reading it.
func TestASafeguardIsPlacedOnASubjectByNameAndWithdrawnById(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)
	if s.mustCall(t, "declareArea", screens.DeclareAreaArgs{Name: "billing"}) == "" {
		t.Fatal("declareArea answered with no id")
	}

	for _, args := range []screens.PlaceSafeguardArgs{
		// A row-scoped safeguard is drawn on the service the row fires for and
		// keyed by the row, which is why this one names both.
		{Parameter: "risk_threshold", SubjectKind: "gate_row",
			SubjectName: "deploy_to_production", ServiceName: theService},
		{Parameter: "window_limit", SubjectKind: "service", SubjectName: theService, Bound: "2"},
		{Parameter: "item_size_target", SubjectKind: "area", SubjectName: "billing", Bound: "300"},
		// A kind this factory has no decider for is refused where the list is
		// widened, so the bound names kinds it can decide.
		{Parameter: "allowed_predicate_kinds", SubjectKind: "factory_settings", Bound: "range,sent_range"},
	} {
		if s.mustCall(t, "placeSafeguard", args) == "" {
			t.Fatalf("placeSafeguard %+v answered with no id", args)
		}
	}

	safeguards, err := safeguard.All(ctx, d.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(safeguards) != 4 {
		t.Fatalf("%d safeguards are placed, want four", len(safeguards))
	}
	for _, one := range safeguards {
		if one.Withdrawn {
			t.Errorf("safeguard %s is withdrawn the moment it was placed", one.ID)
		}
		if one.Subject.Kind == safeguard.SubjectService && !strings.HasPrefix(one.Subject.ID, "svc_") {
			t.Errorf("the safeguard on a service names %q, want the record's id", one.Subject.ID)
		}
		if one.Subject.Kind == safeguard.SubjectArea && !strings.HasPrefix(one.Subject.ID, "ar_") {
			t.Errorf("the safeguard on an area names %q, want the record's id", one.Subject.ID)
		}
	}

	// A safeguard on the factory-wide settings record names the record's id, because
	// that is what the mechanism reading safeguards on it reads them by — a safeguard
	// naming the word would apply to nothing.
	fp, err := factorysettings.Get(ctx, d.pool)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	onTheRecord := 0
	for _, one := range safeguards {
		if one.Subject.Kind == safeguard.SubjectPredicateKindsList {
			onTheRecord++
			if one.Subject.ID != fp.ID {
				t.Errorf("the safeguard on the factory-wide settings record names %q, want %s", one.Subject.ID, fp.ID)
			}
		}
	}
	if onTheRecord != 1 {
		t.Errorf("%d safeguards name the factory-wide settings record, want the one", onTheRecord)
	}

	// The safeguard on the allowed predicate kinds reaches the parameter it was
	// drawn on: what an owner reads afterwards is the union, which is the whole
	// of what a safeguard on a list does. The union is over kinds this factory
	// can decide against one observed exchange — the floor the list's own rule
	// sets, which no safeguard goes below — so a bound naming two of them adds
	// nothing the floor did not already hold.
	allowed, err := policy.NewReader(d.pool, d.token, score.Version{}).All(ctx, policy.Subjects{
		GateRow: "merge_to_master", Stage: item.StageImplementation,
	})
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for _, e := range allowed {
		if e.Parameter != gatepolicy.AllowedPredicateKinds {
			continue
		}
		// The factory's own kinds are the floor an owner extends, and a
		// safeguard adds to them and replaces none.
		if len(e.List) != len(gatepolicy.PredicateKinds) {
			t.Errorf("the allowed reads %v, want the factory's own %d",
				e.List, len(gatepolicy.PredicateKinds))
		}
		for _, name := range e.List {
			if _, err := gatepolicy.DecidablePredicate(name); err != nil {
				t.Errorf("the list in force holds %q, which nothing can decide", name)
			}
		}
	}

	// A safeguard leaves force at the row that decides its withdrawal, so it is
	// two calls: the withdrawal written, and the row decided by a human other
	// than the one who wrote it — the row is routed away from them, so the
	// reviewer is given a duty first, an acting call being refused on a People
	// row that holds nothing.
	s.mustCall(t, "withdrawSafeguard", screens.WithdrawSafeguardArgs{SafeguardID: safeguards[0].ID})
	// The withdrawal's id is read out of its own table: package safeguard has no
	// read that lists withdrawals, there being no caller for one but this.
	var withdrawalID string
	if err := d.pool.QueryRow(ctx, `select id from `+safeguard.WithdrawalTable+
		` where safeguard_id = $1`, safeguards[0].ID).Scan(&withdrawalID); err != nil {
		t.Fatalf("reading the withdrawal that was written: %v", err)
	}
	reviewer := owner(t, ctx, d.pool, d.token, "reviewer")
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: reviewer.Key, Duty: 1})
	s.principal = reviewer.Key
	s.mustCall(t, "decideRecordRow", screens.DecideRecordRowArgs{
		RowKind: gate.SafeguardWithdrawal.String(), RecordID: withdrawalID,
		Verdict: string(gate.VerdictApprove),
	})
	s.principal = s.p.human.Key

	safeguards, err = safeguard.All(ctx, d.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	withdrawn := 0
	for _, one := range safeguards {
		if one.Withdrawn {
			withdrawn++
		}
	}
	if withdrawn != 1 {
		t.Errorf("%d safeguards are withdrawn, want the one", withdrawn)
	}
}

// TestASafeguardRefusesWhatItCannotBind: a subject naming a project nobody
// declared, a subject with no kind, a bound of the wrong shape, and a gate row
// that is not one of the rows built.
func TestASafeguardRefusesWhatItCannotBind(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	for _, c := range []struct {
		name string
		call string
		args any
	}{
		{"nothing at all", "placeSafeguard", screens.PlaceSafeguardArgs{}},
		{"a project nobody declared", "placeSafeguard", screens.PlaceSafeguardArgs{
			Parameter: "window_limit", SubjectKind: "project", SubjectName: "payments", Bound: "2"}},
		{"a subject with no kind", "placeSafeguard", screens.PlaceSafeguardArgs{
			Parameter: "window_limit", SubjectName: theService, Bound: "2"}},
		{"a gate row nobody built", "placeSafeguard", screens.PlaceSafeguardArgs{
			Parameter: "risk_threshold", SubjectKind: "gate_row",
			SubjectName: "deploy_to_staging", ServiceName: theService}},
		{"a word where a bound belongs", "placeSafeguard", screens.PlaceSafeguardArgs{
			Parameter: "window_limit", SubjectKind: "factory_settings", Bound: "two"}},
		{"a parameter that does not exist", "placeSafeguard", screens.PlaceSafeguardArgs{
			Parameter: "gut_feel", SubjectKind: "factory_settings", Bound: "2"}},
		{"a safeguard withdrawn that does not exist", "withdrawSafeguard",
			screens.WithdrawSafeguardArgs{SafeguardID: "sfg_nothing"}},
	} {
		if status, body := s.call(t, c.call, c.args); status == http.StatusNoContent ||
			status == http.StatusOK {
			t.Errorf("a safeguard with %s was accepted: %s", c.name, body)
		}
	}

	placed, err := safeguard.All(ctx, d.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(placed) != 0 {
		t.Errorf("%d safeguards were placed by refused calls", len(placed))
	}
}
