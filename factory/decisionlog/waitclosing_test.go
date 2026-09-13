package decisionlog_test

import (
	"errors"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
)

func TestWaitCloseRequiresAComponentExceptAnOwnersCeilingClear(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	const payload = `{"kind":"credential_at_ceiling","credential_name":"model.test","period_start":"2026-09-01"}`
	opened, err := log.AppendWaitOpen(ctx, decisionlog.Entry{Actor: gate, FormatVersion: "wait/1", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	for name, entry := range map[string]decisionlog.Entry{
		"ordinary human close": {Actor: owner, Payload: "gone", Principal: principal.OfComponent("work")},
		"no Work caller":       {Actor: owner, Payload: payload},
		"agent clear":          {Actor: principal.OfAgent("model", "dispatch", "scope").Actor, Payload: payload, Principal: principal.OfComponent("work")},
		"wrong credential":     {Actor: owner, Payload: `{"kind":"credential_at_ceiling","credential_name":"model.other","period_start":"2026-09-01"}`, Principal: principal.OfComponent("work")},
		"wrong period":         {Actor: owner, Payload: `{"kind":"credential_at_ceiling","credential_name":"model.test","period_start":"2026-10-01"}`, Principal: principal.OfComponent("work")},
	} {
		entry.FormatVersion, entry.Closes = "wait/1", opened.ID
		if _, err := log.AppendWaitClose(ctx, entry); !errors.Is(err, decisionlog.ErrWaitCloseNotComponent) {
			t.Errorf("%s: %v", name, err)
		}
		bad := aRow()
		bad.FormatVersion, bad.Part, bad.Closes = "wait/2", decisionlog.PartClose, opened.ID
		bad.Actor, bad.Payload = entry.Actor, entry.Payload
		bad.CallerKind, bad.CallerKey, bad.CallerKeyBasis = entry.Principal.Actor.Kind, entry.Principal.Actor.Key, entry.Principal.Actor.Basis
		if got := refusedBy(t, insertAround(ctx, pool, bad)); got != "wait_close_actor_component" {
			t.Errorf("%s SQL refusal: %s", name, got)
		}
	}
	closed, err := log.AppendWaitClose(ctx, decisionlog.Entry{Actor: owner, Principal: principal.OfComponent("work"), FormatVersion: "wait/1", Payload: payload, Closes: opened.ID})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Actor != owner || closed.CallerKey != "work" {
		t.Fatalf("lost ceiling authorisation: %+v", closed)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatal(err)
	}
}

func TestAHumanAtWorkClosesTheCommitWaitByRepeatingItsOpening(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	const payload = `{"kind":"master holds a commit the queue did not make","service_id":"svc_a","commit":"abc123"}`
	opened, err := log.AppendWaitOpen(ctx, decisionlog.Entry{Actor: gate, FormatVersion: "wait/1", Payload: payload})
	if err != nil {
		t.Fatal(err)
	}
	for name, entry := range map[string]decisionlog.Entry{
		"no Work caller":  {Actor: owner, Payload: payload},
		"another commit":  {Actor: owner, Payload: `{"kind":"master holds a commit the queue did not make","service_id":"svc_a","commit":"def456"}`, Principal: principal.OfComponent("work")},
		"another service": {Actor: owner, Payload: `{"kind":"master holds a commit the queue did not make","service_id":"svc_b","commit":"abc123"}`, Principal: principal.OfComponent("work")},
	} {
		entry.FormatVersion, entry.Closes = "wait/1", opened.ID
		if _, err := log.AppendWaitClose(ctx, entry); !errors.Is(err, decisionlog.ErrWaitCloseNotComponent) {
			t.Errorf("%s: %v", name, err)
		}
		bad := aRow()
		bad.FormatVersion, bad.Part, bad.Closes = "wait/2", decisionlog.PartClose, opened.ID
		bad.Actor, bad.Payload = entry.Actor, entry.Payload
		bad.CallerKind, bad.CallerKey, bad.CallerKeyBasis = entry.Principal.Actor.Kind, entry.Principal.Actor.Key, entry.Principal.Actor.Basis
		if got := refusedBy(t, insertAround(ctx, pool, bad)); got != "wait_close_actor_component" {
			t.Errorf("%s SQL refusal: %s", name, got)
		}
	}
	closed, err := log.AppendWaitClose(ctx, decisionlog.Entry{Actor: owner, Principal: principal.OfComponent("work"), FormatVersion: "wait/1", Payload: payload, Closes: opened.ID})
	if err != nil {
		t.Fatal(err)
	}
	if closed.Actor != owner || closed.CallerKey != "work" {
		t.Fatalf("lost the acceptance's actor or caller: %+v", closed)
	}
	if err := decisionlog.NewReader(pool, token).Verify(ctx, ownerReading); err != nil {
		t.Fatal(err)
	}
}
