package decisionlog_test

import (
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/principal"
)

// Upgrades must preserve immutable rows while applying new checks to future
// appends. A legacy rework request has no structured reason or return target.
func TestUpgradePreservesLegacyRowsAndEnforcesNewRules(t *testing.T) {
	ctx, pool, log, token := newLog(t)
	for _, column := range []string{"returns_to", "reading", "moved_release", "caller_kind", "caller_key", "caller_key_basis", "caller_dispatch_id", "caller_scope"} {
		if _, err := pool.Exec(ctx, `alter table decision_log drop column `+column+` cascade`); err != nil {
			t.Fatal(err)
		}
	}
	for _, statement := range []string{
		`alter table decision_log drop constraint reason_scope`,
		`alter table decision_log drop constraint reason_required`,
		`alter table decision_log add constraint reason_scope check ((shape = 'decision' and part in ('closing', 'abandonment')) or reason = '')`,
		`alter table decision_log add constraint reason_required check (not (shape = 'decision' and part = 'closing' and verdict in ('reject', 'hold') and reason = '') and not (shape = 'decision' and part = 'abandonment' and reason = ''))`,
	} {
		if _, err := pool.Exec(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	legacy := aRow()
	legacy.Seq, legacy.PrevHash = 1, ""
	legacy.Shape, legacy.Part, legacy.FormatVersion = decisionlog.ShapeReworkRequest, "", "rework_request/1"
	legacy.Hash = legacy.ChainHash()
	_, err := pool.Exec(ctx, `insert into decision_log
 (seq, id, format_version, actor_kind, actor_key, actor_key_basis, at, shape, payload,
 policy_version, score_version, part, closes, verdict, reason, opened_in_work_at, self_approval, prev_hash, hash)
 values (nextval('decision_log_seq'), $1, $2, $3, $4, $5, $6, $7, $8, '', '', '', '', '', '', '', false, '', $9)`,
		legacy.ID, legacy.FormatVersion, legacy.Actor.Kind, legacy.Actor.Key, legacy.Actor.Basis,
		legacy.At, legacy.Shape, legacy.Payload, legacy.Hash)
	if err != nil {
		t.Fatal(err)
	}
	for pass := range 2 {
		for _, statement := range decisionlog.DDL {
			if _, err := pool.Exec(ctx, statement); err != nil {
				t.Fatalf("upgrade pass %d: %v", pass, err)
			}
		}
	}
	reader := decisionlog.NewReader(pool, token)
	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("legacy chain after upgrade: %v", err)
	}
	var hash, version string
	if err := pool.QueryRow(ctx, `select hash, format_version from decision_log where id = $1`, legacy.ID).Scan(&hash, &version); err != nil {
		t.Fatal(err)
	}
	if hash != legacy.Hash || version != legacy.FormatVersion {
		t.Fatal("upgrade rewrote a legacy row")
	}
	newRow, err := log.AppendReworkRequest(ctx, decisionlog.Entry{
		Actor: gate, FormatVersion: "rework_request/1", Reason: "contradictory spec", ReturnsTo: "spec",
	})
	if err != nil {
		t.Fatalf("new rework under upgraded reason_scope: %v", err)
	}
	if newRow.FormatVersion != "rework_request/2" {
		t.Fatalf("stored format = %q", newRow.FormatVersion)
	}
	bad := aRow()
	bad.FormatVersion, bad.Shape, bad.Part = "rework_request/2", decisionlog.ShapeReworkRequest, ""
	bad.Reason = "contradictory spec"
	if got := refusedBy(t, insertAround(ctx, pool, bad)); got != "returns_to_required" {
		t.Fatalf("missing target refused by %s", got)
	}
	bad = aRow()
	bad.FormatVersion, bad.Shape, bad.Part = "queue_rejection/2", decisionlog.ShapeQueueRejection, ""
	if got := refusedBy(t, insertAround(ctx, pool, bad)); got != "reading_required" {
		t.Fatalf("missing reading refused by %s", got)
	}
	if err := reader.Verify(ctx, ownerReading); err != nil {
		t.Fatalf("extended chain after upgrade: %v", err)
	}
}

func TestVerifyDetectsStructuredFieldTampering(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "extended row"
		if legacy {
			name = "legacy row"
		}
		t.Run(name, func(t *testing.T) {
			ctx, pool, log, token := newLog(t)
			entry := decisionlog.Entry{Actor: notifierActor, FormatVersion: "page_event/2", Payload: "unchanged"}
			if !legacy {
				entry.Principal = principal.OfAgent("model", "dispatch", "scope")
			}
			row, err := log.AppendPageEvent(ctx, entry)
			if err != nil {
				t.Fatal(err)
			}
			reader := decisionlog.NewReader(pool, token)
			if err := reader.Verify(ctx, ownerReading); err != nil {
				t.Fatal(err)
			}
			if !legacy && row.FormatVersion != "page_event/4" {
				t.Fatalf("stored format = %q", row.FormatVersion)
			}
			if legacy {
				// A superuser can bypass the SQL rule. Verify must still reject added
				// columns that a legacy format's hash never covered.
				if _, err := pool.Exec(ctx, `alter table decision_log drop constraint structured_fields_match_format`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := pool.Exec(ctx, `update decision_log set caller_kind = 'agent', caller_key = 'model', caller_key_basis = 'claimed', caller_dispatch_id = 'dispatch', caller_scope = 'tampered' where id = $1`, row.ID); err != nil {
				t.Fatal(err)
			}
			broken := brokenBy(t, reader.Verify(ctx, ownerReading))
			want := decisionlog.BreakFields
			if legacy {
				want = decisionlog.BreakFormat
			}
			if broken.Row.ID != row.ID || broken.Break != want {
				t.Fatalf("tamper reported as %+v", broken)
			}
		})
	}
}

func TestLegacyFormatCannotCarryStructuredColumns(t *testing.T) {
	ctx, pool, _, _ := newLog(t)
	row := aRow()
	row.CallerKind, row.CallerKey, row.CallerKeyBasis = "component", "work", "claimed"
	if got := refusedBy(t, insertAround(ctx, pool, row)); got != "structured_fields_match_format" {
		t.Fatalf("legacy extended fields refused by %q", got)
	}
	// Removing the columns from an extended row does not remove their framing.
	row.FormatVersion = "wait/2"
	with := row.ChainHash()
	row.CallerKind, row.CallerKey, row.CallerKeyBasis = "", "", ""
	if row.ChainHash() == with || strings.TrimSpace(row.ChainHash()) == "" {
		t.Fatal("clearing the caller did not change the extended hash")
	}
}
