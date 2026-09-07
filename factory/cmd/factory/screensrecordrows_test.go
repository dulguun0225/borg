// The rows outside every item, decided at Factory over HTTP: the shipped role
// prompt an upgrade changed, and the four withdrawals and shortenings, each
// routed away from the actor on the record it decides.
package main

import (
	"context"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/dispatch"
	"github.com/dulguun0225/borg/factory/factorysettings"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/halt"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/safeguard"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestTheUpgradesShippedPromptIsDecidedAtFactory is the first of the five more
// things: a shipped role prompt whose words an upgrade changed enters the chain
// at the first start, the version below it stands in force until the row every
// version of what an agent is told fires is decided, and the version in force
// moves when it is. The row's third action follows: an edit in place authors a
// version with the human as its author and fires the row again.
func TestTheUpgradesShippedPromptIsDecidedAtFactory(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	role := dispatch.RoleSpecAuthor

	// The words the install ran on, entered under a bundle of its own. It is
	// the install's entry, which is the one that stands in force with nothing
	// decided; composing the path below is the first start on this build, and
	// what it enters for this role is an upgrade's.
	installed, err := artifact.NewStore(d.pool, d.token).EnterShipped(ctx, installActor,
		artifact.KindRolePrompt, string(role), "", "the words the install ran on",
		artifact.EnteredByInstall, "an earlier bundle")
	if err != nil {
		t.Fatalf("entering the install's own prompt: %v", err)
	}

	s := newScreens(t, ctx, d, out)
	head, found, err := artifact.Newest(ctx, d.pool, artifact.KindRolePrompt, string(role), "")
	if err != nil || !found {
		t.Fatalf("Newest = found %v, %v", found, err)
	}
	if head.ID == installed.ID || head.EnteredBy != artifact.EnteredByUpgradeFirstStart {
		t.Fatalf("the head of the chain is %+v, want the version this build's first start entered", head)
	}

	// Factory reads the version in force and the version awaiting its gate.
	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	prompt := rolePromptOf(t, factory, string(role))
	if prompt.VersionInForce != installed.ID {
		t.Errorf("the version in force is %s, want the words the install ran on, %s",
			prompt.VersionInForce, installed.ID)
	}
	if prompt.AwaitingGateVersion != head.ID {
		t.Errorf("the version awaiting the gate is %s, want %s", prompt.AwaitingGateVersion, head.ID)
	}
	if factory.RolePromptGateRow == nil || factory.RolePromptGateRow.VersionID != head.ID {
		t.Fatalf("Factory holds no row awaiting a decision over %s: %+v", head.ID, factory.RolePromptGateRow)
	}

	// Decided at Factory, the version in force moves.
	s.mustCall(t, "decideRecordRow", screens.DecideRecordRowArgs{
		RowKind: gate.RolePromptOrSkill.String(), RecordID: head.ID,
		Verdict: string(gate.VerdictApprove), OpenedInWorkAt: theOpenedInWorkAt,
	})
	s.get(t, "/api/factory", &factory)
	prompt = rolePromptOf(t, factory, string(role))
	if prompt.VersionInForce != head.ID {
		t.Errorf("the version in force is %s after the row was approved, want %s", prompt.VersionInForce, head.ID)
	}
	if prompt.AwaitingGateVersion != "" {
		t.Errorf("a version still awaits the gate after the row closed: %s", prompt.AwaitingGateVersion)
	}

	// The row's third action: not a verdict but a version the human authors,
	// which fires the row again over what they wrote.
	const wrote = "what the spec author is told, rewritten by a human at the row"
	s.mustCall(t, "editRecordRow", screens.EditRecordRowArgs{
		Row: gate.RolePromptOrSkill.String(), RecordID: head.ID, Version: wrote,
	})
	s.get(t, "/api/factory", &factory)
	prompt = rolePromptOf(t, factory, string(role))
	if prompt.AwaitingGateVersion == "" || prompt.AwaitingGateVersion == head.ID {
		t.Fatalf("the edit left %q awaiting the gate, want the version the human authored",
			prompt.AwaitingGateVersion)
	}
	if prompt.VersionInForce != head.ID {
		t.Errorf("the version in force moved to %s on an edit, and a version not decided is not in force",
			prompt.VersionInForce)
	}
	authored, err := artifact.Get(ctx, d.pool, prompt.AwaitingGateVersion)
	if err != nil {
		t.Fatalf("reading the version the human authored: %v", err)
	}
	if authored.Content != wrote {
		t.Errorf("the version the row holds reads %q, want what the human wrote", authored.Content)
	}
	if authored.Author != s.p.human.Key {
		t.Errorf("the version names %q as its author, want the human at the row, %q",
			authored.Author, s.p.human.Key)
	}

	// The row fired again over it, which is what makes an edit in place a
	// decision still to be taken rather than a version put in force.
	if factory.RolePromptGateRow == nil || factory.RolePromptGateRow.VersionID != authored.ID {
		t.Errorf("Factory holds no row over the version the human authored: %+v", factory.RolePromptGateRow)
	}
	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify: %v", err)
	}
}

// TestTheFourRecordRowsAreRoutedAwayFromWhoWroteTheRecord is the four rows
// outside every item, each decided at Factory and each routed away from the
// actor the record it decides names: a safeguard's withdrawal, a halt's
// withdrawal, a legal hold's ending, and a shortening of decision-log
// retention.
//
// One human writes all four and the owner decides them, which is the shape the
// design gives every one of them. Each close event carries when the actor
// opened the row in Work, the one field only a screen fills.
//
// None of the four names a duty, so each widens to the owner and the owner is
// the only decider this package can name — what happens where a row does name a
// duty with a second holder is TestASecondHolderIsWhatMakesAWithdrawalDecidable.
func TestTheFourRecordRowsAreRoutedAwayFromWhoWroteTheRecord(t *testing.T) {
	ctx, d, out := newPath(t, approvals)
	s := newScreens(t, ctx, d, out)

	// The human who writes the four records. A duty is declared on the row
	// first, an acting call being refused on a People row that holds nothing.
	keeper := owner(t, ctx, d.pool, d.token, "keeper")
	s.mustCall(t, "declareDuty", screens.DeclareDutyArgs{HumanKey: keeper.Key, Duty: 1})

	safeguardID := s.mustCall(t, "placeSafeguard", screens.PlaceSafeguardArgs{
		Parameter: "window_limit", SubjectKind: "service", SubjectName: theService, Bound: "2",
	})
	haltID := s.mustCall(t, "setHalt", screens.SetHaltArgs{Reason: "the store is being restored"})
	holdID := s.mustCall(t, "setLegalHold", screens.SetLegalHoldArgs{
		SubjectKind: "service", SubjectName: theService, Reason: "counsel asked for it",
	})

	// Every one of the four records is written as the keeper, so every row is
	// routed away from them.
	s.principal = keeper.Key
	s.mustCall(t, "withdrawSafeguard", screens.WithdrawSafeguardArgs{SafeguardID: safeguardID})
	s.mustCall(t, "withdrawHalt", screens.WithdrawHaltArgs{HaltID: haltID})
	s.mustCall(t, "withdrawLegalHold", screens.WithdrawLegalHoldArgs{LegalHoldID: holdID})
	s.mustCall(t, "authorParameter", screens.AuthorParameterArgs{
		Parameter: "decision_log_retention", Value: "1",
	})
	s.principal = s.p.human.Key

	rows := []struct {
		kind     string
		recordID string
	}{
		{gate.SafeguardWithdrawal.String(), withdrawalOf(t, ctx, d, safeguard.WithdrawalTable, "safeguard_id", safeguardID)},
		{gate.HaltWithdrawal.String(), withdrawalOf(t, ctx, d, halt.WithdrawalTable, "halt_id", haltID)},
		{gate.LegalHoldWithdrawal.String(), withdrawalOf(t, ctx, d, legalhold.WithdrawalTable, "legal_hold_id", holdID)},
		{gate.DecisionLogRetentionShortening.String(), pendingShortening(t, ctx, d.pool).ID},
	}

	// Factory lists all four pending a disposition, which is where the design
	// puts them.
	var factory screens.Factory
	s.get(t, "/api/factory", &factory)
	pending := map[string]bool{}
	for _, one := range factory.RecordDecidingRows {
		pending[one.Kind] = true
	}
	for _, row := range rows {
		if !pending[row.kind] {
			t.Errorf("Factory does not list %s pending a disposition: %+v", row.kind, factory.RecordDecidingRows)
		}
	}

	for _, row := range rows {
		s.mustCall(t, "decideRecordRow", screens.DecideRecordRowArgs{
			RowKind: row.kind, RecordID: row.recordID,
			Verdict: string(gate.VerdictApprove), OpenedInWorkAt: theOpenedInWorkAt,
		})
	}

	// Each row barred the keeper and each close event carries when the row was
	// opened in Work.
	opened, closed := recordRowEvents(t, ctx, d)
	for _, row := range rows {
		opening, is := opened[row.kind]
		if !is {
			t.Errorf("the log holds no opening at %s", row.kind)
			continue
		}
		if opening.payload.WaitsOn.NotHuman != keeper.Key {
			t.Errorf("%s bars %q, want the human who wrote the record, %q",
				row.kind, opening.payload.WaitsOn.NotHuman, keeper.Key)
		}
		if !opening.payload.HumanDecides {
			t.Errorf("%s auto-passed, and a human is at every one of these rows", row.kind)
		}
		closing, is := closed[opening.id]
		if !is {
			t.Errorf("the opening at %s was not closed", row.kind)
			continue
		}
		if closing.OpenedInWorkAt != theOpenedInWorkAt {
			t.Errorf("the close at %s says the row was opened in Work at %q, want %q",
				row.kind, closing.OpenedInWorkAt, theOpenedInWorkAt)
		}
		if closing.Actor.Key != s.p.human.Key {
			t.Errorf("the close at %s was written by %q, want the owner who decided it", row.kind, closing.Actor.Key)
		}
	}

	// Each record left force, which is what the close event of its own row
	// does and what writing the withdrawal alone does not.
	placed, err := safeguard.All(ctx, d.pool)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	for _, one := range placed {
		if one.ID == safeguardID && !one.Withdrawn {
			t.Error("the safeguard stands after the row that decides its withdrawal closed")
		}
	}
	standing, err := halt.Standing(ctx, d.pool)
	if err != nil {
		t.Fatalf("halt.Standing: %v", err)
	}
	for _, one := range standing {
		if one.ID == haltID {
			t.Error("the halt stands after the row that decides its withdrawal closed")
		}
	}
	reaching, err := legalhold.Reaching(ctx, d.pool, legalhold.Subject{
		Kind: legalhold.SubjectService, ID: theServiceRecord(t, ctx, s.p).ID,
	})
	if err != nil {
		t.Fatalf("Reaching: %v", err)
	}
	if reaching {
		t.Error("the legal hold reaches its subject after the row that decides its ending closed")
	}
	settings, err := factorysettings.Get(ctx, d.pool)
	if err != nil {
		t.Fatalf("factorysettings.Get: %v", err)
	}
	if !settings.DecisionLogRetentionSeconds.Present || settings.DecisionLogRetentionSeconds.Number != 1 {
		t.Errorf("the retention in force is %+v, want the one second the row decided",
			settings.DecisionLogRetentionSeconds)
	}

	if err := verifyLog(t, ctx, d); err != nil {
		t.Errorf("the chain does not verify after the four rows: %v", err)
	}
}

// rolePromptOf is Factory's row for one role.
func rolePromptOf(t *testing.T, view screens.Factory, role string) screens.RolePrompt {
	t.Helper()
	for _, one := range view.RolePrompts {
		if one.Role == role {
			return one
		}
	}
	t.Fatalf("Factory holds no role prompt row for %s: %+v", role, view.RolePrompts)
	return screens.RolePrompt{}
}

// withdrawalOf is the id of the withdrawal written against one record, read out
// of the withdrawal's own table: no package here lists withdrawals, there being
// no caller for one but a test.
func withdrawalOf(t *testing.T, ctx context.Context, d deps, table, column, recordID string) string {
	t.Helper()
	var id string
	if err := d.pool.QueryRow(ctx,
		`select id from `+table+` where `+column+` = $1`, recordID).Scan(&id); err != nil {
		t.Fatalf("reading the withdrawal written against %s: %v", recordID, err)
	}
	return id
}

// openingRow is one open event at a row outside every item: its id and what its
// payload says.
type openingRow struct {
	id      string
	payload gate.OpeningPayload
}

// recordRowEvents is the openings at the four rows outside every item, keyed by
// the row, and every close event keyed by the opening it closes.
func recordRowEvents(t *testing.T, ctx context.Context, d deps) (map[string]openingRow, map[string]decisionlog.Row) {
	t.Helper()
	opened := map[string]openingRow{}
	closed := map[string]decisionlog.Row{}
	for _, row := range readLog(t, ctx, d) {
		if row.Shape != decisionlog.ShapeDecision {
			continue
		}
		switch row.Part {
		case decisionlog.PartOpen:
			payload := openingPayload(t, row)
			if strings.HasSuffix(payload.Gate, "withdrawal") ||
				payload.Gate == gate.DecisionLogRetentionShortening.String() {
				opened[payload.Gate] = openingRow{id: row.ID, payload: payload}
			}
		case decisionlog.PartClose:
			closed[row.Closes] = row
		}
	}
	if len(opened) == 0 {
		t.Fatal("the log holds no opening at a row outside every item")
	}
	return opened, closed
}
