package reportstore_test

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/reportstore"
)

// TestARedactionAppendsTheErasureRowFirstAndThenDestroysTheWords: the row
// lands before the bytes are destroyed, because it is what says the words
// must not come back with a restore; it names the removal and never the
// words; and the report, its link and its counts all stand with the words
// gone.
func TestARedactionAppendsTheErasureRowFirstAndThenDestroysTheWords(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	sub := submission("tok")
	sub.Text = "the button ruins everything"
	written, err := store.Submit(ctx, sub, time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit = %+v, %v", written, err)
	}
	if err := store.Link(ctx, written.Report.ID, "int_a"); err != nil {
		t.Fatalf("Link: %v", err)
	}

	redaction := reportstore.Redaction{
		ID: "red_a", ErasureKey: "ers_a", ReportID: written.Report.ID,
		Spans: []reportstore.Span{{Start: 11, End: 16}},
	}
	if err := store.Redact(ctx, redaction); err != nil {
		t.Fatalf("Redact: %v", err)
	}

	read, err := store.Get(ctx, principal.OfComponent("factory"), written.Report.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Text != "the button xxxxx everything" {
		t.Errorf("the report reads %q, want the named span destroyed and the rest standing", read.Text)
	}
	if read.IntentID != "int_a" {
		t.Errorf("the report's link is %q, want the link standing with the words gone", read.IntentID)
	}

	rows, err := erasurelist.ReadKind(s.listPath, erasurelist.KindReport)
	if err != nil {
		t.Fatalf("ReadKind: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "ers_a" {
		t.Fatalf("the erasure list holds %+v, want one row keyed by the erasure", rows)
	}
	if strings.Contains(rows[0].Removed, "ruins") {
		t.Errorf("the erasure-list row says %q, and it carries the words it removed", rows[0].Removed)
	}
	if !strings.Contains(rows[0].Removed, written.Report.ID) || !strings.Contains(rows[0].Removed, "11-16") {
		t.Errorf("the erasure-list row says %q, want the report and the span it removed", rows[0].Removed)
	}

	// The same redaction applied again appends nothing and changes nothing.
	if err := store.Redact(ctx, redaction); err != nil {
		t.Fatalf("Redact again: %v", err)
	}
	if rows, err = erasurelist.ReadKind(s.listPath, erasurelist.KindReport); err != nil || len(rows) != 1 {
		t.Errorf("the erasure list holds %+v, %v; want the second redaction to append nothing", rows, err)
	}
}

// TestTheRedactionPassDestroysWhatEveryRedactionNames: the store reads the
// redactions naming its own records, on a pass of its own, and destroys the
// named spans inside them.
func TestTheRedactionPassDestroysWhatEveryRedactionNames(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	first := submission("tok")
	first.Text = "0123456789"
	one, err := store.Submit(ctx, first, time.Now())
	if err != nil || !one.Accepted {
		t.Fatalf("Submit: %+v, %v", one, err)
	}
	second := submission("tok")
	second.Text = "abcdefghij"
	second.SourceKey = "src_b"
	two, err := store.Submit(ctx, second, time.Now())
	if err != nil || !two.Accepted {
		t.Fatalf("Submit: %+v, %v", two, err)
	}

	s.redactions = []reportstore.Redaction{
		{ID: "red_a", ErasureKey: "ers_a", ReportID: one.Report.ID,
			Spans: []reportstore.Span{{Start: 0, End: 3}}},
		{ID: "red_b", ErasureKey: "ers_b", ReportID: two.Report.ID,
			Spans: []reportstore.Span{{Start: 7, End: 10}}},
		{ID: "red_c", ErasureKey: "ers_c", ReportID: "rep_gone",
			Spans: []reportstore.Span{{Start: 0, End: 1}}},
	}
	destroyed, err := store.RedactionPass(ctx)
	if err != nil {
		t.Fatalf("RedactionPass: %v", err)
	}
	if destroyed != 2 {
		t.Errorf("RedactionPass destroyed %d, want the two naming a report this store holds", destroyed)
	}

	who := principal.OfComponent("factory")
	read, err := store.Get(ctx, who, one.Report.ID)
	if err != nil || read.Text != "xxx3456789" {
		t.Errorf("the first report reads %q, %v", read.Text, err)
	}
	if read, err = store.Get(ctx, who, two.Report.ID); err != nil || read.Text != "abcdefgxxx" {
		t.Errorf("the second report reads %q, %v", read.Text, err)
	}
}

// TestAReplayDestroysWhatARestoreBroughtBack: the erasure list is never
// rolled back by a restore, so the store replays the rows naming its own
// records against whatever came back before it serves again.
func TestAReplayDestroysWhatARestoreBroughtBack(t *testing.T) {
	ctx, pool, store, s := newStore(t)
	s.place("tok", deploy())

	sub := submission("tok")
	sub.Text = "0123456789"
	written, err := store.Submit(ctx, sub, time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit: %+v, %v", written, err)
	}
	if err := store.Redact(ctx, reportstore.Redaction{
		ID: "red_a", ErasureKey: "ers_a", ReportID: written.Report.ID,
		Spans: []reportstore.Span{{Start: 2, End: 5}},
	}); err != nil {
		t.Fatalf("Redact: %v", err)
	}

	// A restore puts the words back: the backup was taken before the erasure.
	if _, err := pool.Exec(ctx, `update `+reportstore.ReportTable+` set text = $1 where id = $2`,
		"0123456789", written.Report.ID); err != nil {
		t.Fatalf("restoring the words: %v", err)
	}

	replayed, err := store.Replay(ctx)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if replayed != 1 {
		t.Errorf("Replay destroyed %d rows' worth, want the one row naming a report", replayed)
	}
	read, err := store.Get(ctx, principal.OfComponent("factory"), written.Report.ID)
	if err != nil || read.Text != "01xxx56789" {
		t.Errorf("after the replay the report reads %q, %v; want the words destroyed again", read.Text, err)
	}
}

// TestAnErasureListWithNoRowsReplaysNothing: a store that has erased nothing
// replays nothing, and a list that was never written is not a file that has
// to exist.
func TestAnErasureListWithNoRowsReplaysNothing(t *testing.T) {
	ctx, _, store, s := newStore(t)
	if _, err := os.Stat(s.listPath); !os.IsNotExist(err) {
		t.Fatalf("the erasure list exists before anything erased: %v", err)
	}
	replayed, err := store.Replay(ctx)
	if err != nil || replayed != 0 {
		t.Errorf("Replay = %d, %v; want nothing replayed", replayed, err)
	}
}

// TestARedactionNamingNoErasureKeyIsRefused: the key is what the row is
// appended under, so a redaction without one is a row that could be appended
// twice — and the words would be destroyed with nothing on the list to stop a
// restore putting them back.
func TestARedactionNamingNoErasureKeyIsRefused(t *testing.T) {
	ctx, _, store, s := newStore(t)
	s.place("tok", deploy())

	sub := submission("tok")
	sub.Text = "the button ruins everything"
	written, err := store.Submit(ctx, sub, time.Now())
	if err != nil || !written.Accepted {
		t.Fatalf("Submit = %+v, %v", written, err)
	}
	err = store.Redact(ctx, reportstore.Redaction{
		ID: "red_a", ReportID: written.Report.ID, Spans: []reportstore.Span{{Start: 0, End: 3}},
	})
	if !errors.Is(err, reportstore.ErrErasureKeyEmpty) {
		t.Errorf("a redaction naming no erasure key = %v, want ErrErasureKeyEmpty", err)
	}
	read, err := store.Get(ctx, principal.OfComponent("factory"), written.Report.ID)
	if err != nil || read.Text != sub.Text {
		t.Errorf("the report reads %q, %v; want the words standing", read.Text, err)
	}
}
