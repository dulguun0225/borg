package intent_test

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/record"
)

// TestAReopenedInterviewSupersedesWhatItDoesNotRestate: a statement the new
// reading restates unchanged keeps its record and its id; every other
// requirement of the earlier reading is superseded in the same call, pointing
// at the requirements that replace it, and the pointer is empty where the
// requester retracted the statement.
func TestAReopenedInterviewSupersedesWhatItDoesNotRestate(t *testing.T) {
	ctx, pool, in := newIntake(t)

	kept := "When a charge fails, the system shall retry it once."
	replaced := "If the retry fails, then the system shall show the shopper the failure."
	retracted := "While the shopper is checking out, the system shall hold the cart."
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: kept},
		intent.NewRequirement{Statement: replaced},
		intent.NewRequirement{Statement: retracted},
	)

	first, err := intent.Requirements(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(first) != 3 {
		t.Fatalf("the first reading is %d requirements, want 3", len(first))
	}
	byStatement := map[string]intent.Requirement{}
	for _, r := range first {
		byStatement[r.Statement] = r
	}

	// The interview reopens and a second confirming round writes a new
	// reading against the first.
	if err := in.SendBack(ctx, intake, intentID, intent.SentBackByReworkRequest); err != nil {
		t.Fatalf("SendBack: %v", err)
	}
	if _, err := in.OpenRound(ctx, intake, intentID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, intentID, "Is this the new reading?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	splitOne := "If the retry fails, then the system shall show the shopper the failure and the reason."
	splitTwo := "If the retry fails, then the system shall record the failure for the shopper's next visit."
	second, err := in.Confirm(ctx, intake, intent.Confirmation{
		IntentID: intentID, QuestionID: asked.ID, Answer: "Yes.",
		IntendedEffect: "A shopper whose card fails once still completes the order.",
		Tier:           intent.Tier{Value: 2, PolicyVersion: "pv_2"},
		Requirements: []intent.NewRequirement{
			{Statement: kept},
			{Statement: splitOne, Supersedes: []string{byStatement[replaced].ID}},
			{Statement: splitTwo, Supersedes: []string{byStatement[replaced].ID}},
		},
	})
	if err != nil {
		t.Fatalf("Confirm the second reading: %v", err)
	}
	if len(second) != 3 {
		t.Fatalf("the second reading is %d requirements, want 3", len(second))
	}
	if second[0].ID != byStatement[kept].ID {
		t.Errorf("the restated statement is %s, want its own record %s kept", second[0].ID, byStatement[kept].ID)
	}

	every, err := intent.EveryRequirement(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("EveryRequirement: %v", err)
	}
	if len(every) != 5 {
		t.Fatalf("EveryRequirement = %d records, want the three of the first reading and the two new", len(every))
	}
	for _, r := range every {
		switch r.ID {
		case byStatement[replaced].ID:
			if r.InForce() {
				t.Errorf("the replaced statement %s is still in force", r.ID)
			}
			want := []string{second[1].ID, second[2].ID}
			slices.Sort(want)
			got := slices.Clone(r.SupersededBy)
			slices.Sort(got)
			if !slices.Equal(got, want) {
				t.Errorf("the replaced statement points at %v, want the two that replaced it, %v", got, want)
			}
		case byStatement[retracted].ID:
			if r.InForce() {
				t.Errorf("the retracted statement %s is still in force", r.ID)
			}
			if len(r.SupersededBy) != 0 {
				t.Errorf("the retracted statement points at %v, want an empty pointer", r.SupersededBy)
			}
			if r.SupersededAt == "" {
				t.Errorf("the retracted statement is superseded with no time on it: %+v", r)
			}
		default:
			if !r.InForce() {
				t.Errorf("requirement %s is superseded and should not be: %+v", r.ID, r)
			}
		}
	}
}

// TestConfirmRefusesASupersessionOfSomethingNotInForce: the pointer is written
// against the reading in force, so naming anything else is refused rather than
// stored as a link to nothing.
func TestConfirmRefusesASupersessionOfSomethingNotInForce(t *testing.T) {
	ctx, _, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")
	if _, err := in.OpenRound(ctx, intake, taken.ID); err != nil {
		t.Fatalf("OpenRound: %v", err)
	}
	asked, err := in.Ask(ctx, intake, taken.ID, "Is this right?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	_, err = in.Confirm(ctx, intake, intent.Confirmation{
		IntentID: taken.ID, QuestionID: asked.ID, Answer: "Yes.",
		IntendedEffect: "who it is for", Tier: intent.Tier{Value: 1, PolicyVersion: "pv_1"},
		Requirements: []intent.NewRequirement{{
			Statement:  "When a charge fails, the system shall retry it once.",
			Supersedes: []string{"rq_nothing"},
		}},
	})
	if !errors.Is(err, intent.ErrSupersedesUnknown) {
		t.Errorf("Confirm naming a requirement not in force = %v, want ErrSupersedesUnknown", err)
	}
}

// TestDecompositionDerivesOneRequirementPerItemShare: a requirement the split
// spreads over several items is assigned to none of them, and decomposition
// derives one requirement per item, each pointing at the one it derives from.
func TestDecompositionDerivesOneRequirementPerItemShare(t *testing.T) {
	ctx, pool, in := newIntake(t)
	whole := "When a charge fails, the system shall retry it once and tell the ledger."
	intentID := confirmed(t, ctx, in, "checkout should retry", intent.NewRequirement{Statement: whole})

	reading, err := intent.Requirements(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(reading) != 1 {
		t.Fatalf("the reading is %d requirements, want 1", len(reading))
	}

	shares := []intent.Derivation{
		{IntentID: intentID, DerivedFrom: reading[0].ID, ItemID: "it_checkout",
			Statement: "When a charge fails, the system shall retry it once."},
		{IntentID: intentID, DerivedFrom: reading[0].ID, ItemID: "it_ledger",
			Statement: "When a retry is made, the system shall record it in the ledger."},
	}
	var derived []intent.Requirement
	for _, share := range shares {
		written, err := in.DeriveForItem(ctx, intake, share)
		if err != nil {
			t.Fatalf("DeriveForItem: %v", err)
		}
		if written.Kind != intent.KindDerived || written.DerivedFrom != reading[0].ID || written.ItemID != share.ItemID {
			t.Errorf("the share is %+v, want a derived requirement of %s for %s", written, reading[0].ID, share.ItemID)
		}
		derived = append(derived, written)
	}

	forItem, err := intent.ForItem(ctx, pool, "it_ledger")
	if err != nil {
		t.Fatalf("ForItem: %v", err)
	}
	if len(forItem) != 1 || forItem[0].ID != derived[1].ID {
		t.Errorf("ForItem = %+v, want the share derived for that item", forItem)
	}

	inForce, err := intent.Requirements(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(inForce) != 3 {
		t.Errorf("the reading in force is %d requirements, want the whole and its two shares", len(inForce))
	}

	for _, refused := range []struct {
		name       string
		derivation intent.Derivation
		want       error
	}{
		{"no item", intent.Derivation{IntentID: intentID, DerivedFrom: reading[0].ID,
			Statement: "The system shall do it."}, intent.ErrItemIDEmpty},
		{"nothing to derive from", intent.Derivation{IntentID: intentID, ItemID: "it_x",
			Statement: "The system shall do it."}, intent.ErrDerivedFromNotInForce},
		{"a requirement that does not exist", intent.Derivation{IntentID: intentID, DerivedFrom: "rq_nothing",
			ItemID: "it_x", Statement: "The system shall do it."}, intent.ErrRequirementNotFound},
		{"a statement fitting no pattern", intent.Derivation{IntentID: intentID, DerivedFrom: reading[0].ID,
			ItemID: "it_x", Statement: "do the ledger part"}, intent.ErrEscapeReasonMissing},
	} {
		if _, err := in.DeriveForItem(ctx, intake, refused.derivation); !errors.Is(err, refused.want) {
			t.Errorf("DeriveForItem with %s = %v, want %v", refused.name, err, refused.want)
		}
	}
}

// TestDecompositionMarksARequirementUnanswerable: the mark carries a tagged
// reason and is write-once, and what shows a requirement stopped by nothing is
// the count of them on the intent.
func TestDecompositionMarksARequirementUnanswerable(t *testing.T) {
	ctx, pool, in := newIntake(t)
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "When a charge fails, the system shall retry it once."},
	)
	reading, err := intent.Requirements(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}

	marked, err := in.MarkUnanswerable(ctx, intake, reading[0].ID, "no service in this factory reaches the provider")
	if err != nil {
		t.Fatalf("MarkUnanswerable: %v", err)
	}
	if !marked.Unanswerable() {
		t.Errorf("the requirement is not marked: %+v", marked)
	}

	read, err := intent.Requirements(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(read) != 1 || !read[0].Unanswerable() {
		t.Errorf("Requirements = %+v, want the marked requirement still in force and marked", read)
	}

	if _, err := in.MarkUnanswerable(ctx, intake, reading[0].ID, "a second reason"); !errors.Is(err, intent.ErrAlreadyUnanswerable) {
		t.Errorf("MarkUnanswerable twice = %v, want ErrAlreadyUnanswerable", err)
	}
	if _, err := in.MarkUnanswerable(ctx, intake, reading[0].ID, ""); !errors.Is(err, intent.ErrReasonEmpty) {
		t.Errorf("MarkUnanswerable with no reason = %v, want ErrReasonEmpty", err)
	}
	if _, err := in.MarkUnanswerable(ctx, intake, "rq_nothing", "a reason"); !errors.Is(err, intent.ErrRequirementNotFound) {
		t.Errorf("MarkUnanswerable on a missing requirement = %v, want ErrRequirementNotFound", err)
	}
}

// TestAStatementFittingNoPatternIsAdmittedAndCounted: a form everything can
// escape is not a form, so the escape carries a tagged reason and is counted.
func TestAStatementFittingNoPatternIsAdmittedAndCounted(t *testing.T) {
	ctx, pool, in := newIntake(t)
	intentID := confirmed(t, ctx, in, "checkout should retry",
		intent.NewRequirement{Statement: "When a charge fails, the system shall retry it once."},
		intent.NewRequirement{
			Statement:    "The wording on the failure page is the marketing team's.",
			EscapeReason: "a constraint on the copy, not a behaviour with a response clause",
		},
	)

	reading, err := intent.Requirements(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Requirements: %v", err)
	}
	if len(reading) != 2 || reading[1].Pattern != "" || reading[1].EscapeReason == "" {
		t.Fatalf("the reading is %+v, want the second admitted with no pattern and a reason", reading)
	}

	escaped, total, err := intent.Escaped(ctx, pool, intentID)
	if err != nil {
		t.Fatalf("Escaped: %v", err)
	}
	if escaped != 1 || total != 2 {
		t.Errorf("Escaped = %d of %d, want 1 of 2", escaped, total)
	}
}

// TestTheStoreRefusesARequirementAroundTheWriter inserts by raw SQL, so what
// it exercises is the CHECK constraints and not the writer's own refusals.
func TestTheStoreRefusesARequirementAroundTheWriter(t *testing.T) {
	ctx, pool, in := newIntake(t)
	taken := requested(t, ctx, in, "checkout should retry")

	insert := `insert into requirement (id, format_version, actor_kind, actor_key, actor_key_basis, at,
		intent_id, statement, pattern, escape_reason, kind, derived_from, item_id,
		superseded_at, superseded_by, unanswerable_reason)
		values ($1, '` + intent.FormatVersionRequirement + `', 'component', 'intake', 'claimed', $2,
		$3, $4, $5, $6, $7, $8, $9, $10, $11, '')`
	for _, refused := range []struct {
		name                                 string
		intentID, statement, pattern, escape string
		kind, derivedFrom, itemID            string
		supersededAt, supersededBy           string
		constraint                           string
	}{
		{"no intent", "", "The system shall do it.", "always_true", "", "confirmed", "", "", "", "", "intent_id_present"},
		{"no statement", taken.ID, "", "always_true", "", "confirmed", "", "", "", "", "statement_present"},
		{"a pattern outside the six", taken.ID, "s", "eventually", "", "confirmed", "", "", "", "", "pattern_known"},
		{"no pattern and no reason", taken.ID, "s", "", "", "confirmed", "", "", "", "", "escape_reason_with_no_pattern"},
		{"a reason beside a pattern", taken.ID, "s", "event", "why", "confirmed", "", "", "", "", "escape_reason_with_no_pattern"},
		{"a kind outside the three", taken.ID, "s", "event", "", "guessed", "", "", "", "", "kind_known"},
		{"derived from nothing", taken.ID, "s", "event", "", "derived", "", "it_1", "", "", "derived_names_what_it_derives_from"},
		{"derived naming no item", taken.ID, "s", "event", "", "derived", "rq_1", "", "", "", "derived_names_an_item"},
		{"a confirmed requirement naming an item", taken.ID, "s", "event", "", "confirmed", "", "it_1", "", "", "derived_names_an_item"},
		{"a supersession pointer with no time", taken.ID, "s", "event", "", "confirmed", "", "", "", "[]", "superseded_together"},
		{"a supersession time in another layout", taken.ID, "s", "event", "", "confirmed", "", "", "yesterday", "[]",
			"superseded_at_is_time_layout"},
	} {
		_, err := pool.Exec(ctx, insert,
			record.NewID(intent.RequirementIDPrefix), record.Now(),
			refused.intentID, refused.statement, refused.pattern, refused.escape,
			refused.kind, refused.derivedFrom, refused.itemID, refused.supersededAt, refused.supersededBy)
		if err == nil || !strings.Contains(err.Error(), refused.constraint) {
			t.Errorf("inserting %s = %v, want a violation of %s", refused.name, err, refused.constraint)
		}
	}
	_ = in
}
