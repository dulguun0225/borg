// The erasure demonstrated: one report's words removed as one action, in the
// three records that quoted them, with every link standing; the erasure-list
// row landing before the redaction; a legal hold refusing the whole erasure and
// the refusal recorded; and a replay taking the words out again after a restore
// put them back.
package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/localtarget"
	"github.com/dulguun0225/borg/factory/policy"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/redaction"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/screens"
	"github.com/dulguun0225/borg/factory/service"
)

// theWordsOf is the phrase the owner erases: the report's own middle,
// everything between its first space and its last. The statement summarizing
// the report and the version authored against it both quote it, which is what
// makes one action reach three records, and taking it off the report rather
// than writing it out again keeps this test independent of the words a fixture
// happens to carry.
func theWordsOf(t *testing.T, text string) string {
	t.Helper()
	start := strings.Index(text, " ") + 1
	end := strings.LastIndex(text, " ")
	if start <= 0 || end <= start {
		t.Fatalf("the report %q has no middle to erase", text)
	}
	return text[start:end]
}

// TestOneErasureReachesEveryRecordThatQuotedTheWords: an owner names one report
// and the bytes of it that go, and the factory walks the links from there.
func TestOneErasureReachesEveryRecordThatQuotedTheWords(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	reports := newReports(t, ctx, &d)
	if _, err := run(ctx, d, of(theStatement)); err != nil {
		t.Fatalf("the path stopped: %v\noutput so far:\n%s", err, out)
	}
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path that erases: %v\n%s", err, out)
	}
	ps := newPasses(p, nil, nil)
	acting := owner(t, ctx, d.pool, d.token, d.human)
	view := &views{p: p}
	made := &calls{p: p, v: view}

	socket := localtarget.WayInSocket(d.dir, theService)
	waitForTheWayIn(t, socket)
	reportThrough(t, overTheSocket(socket), firstReport, "bug", false)
	if moved, err := ps.Tick(ctx, passGrouper); err != nil {
		t.Fatalf("the grouper pass: %v\n%s", err, out)
	} else if !moved {
		t.Fatalf("the grouper grouped nothing:\n%s", out)
	}

	reportID := reports.ids(t, ctx)[0]
	intentID := intentOfReport(t, ctx, d, reportID)
	if intentID == "" {
		t.Fatal("the report was not grouped, so there is no statement to erase from")
	}
	text := reports.stored(t, ctx)[0].text
	words := theWordsOf(t, text)
	statement := readIntent(t, ctx, d, intentID).Statement
	if !strings.Contains(statement, words) {
		t.Fatalf("the statement does not quote the report's words: %q", statement)
	}
	itemID, versionID := quotedInAVersion(t, ctx, d, p, intentID, statement)

	start := strings.Index(text, words)
	if err := made.PerformErasure(ctx, asPrincipal(acting), screens.PerformErasureArgs{
		ReportID: reportID,
		Spans:    []screens.ErasureSpan{{Start: start, End: start + len(words)}},
		Reason:   "the words name a person, and the person asked",
	}); err != nil {
		t.Fatalf("PerformErasure: %v\n%s", err, out)
	}

	assertTheRowsLandedFirst(t, ctx, d, reports.list)
	assertTheWordsAreGone(t, ctx, d, reports, erased{
		reportID: reportID, intentID: intentID, itemID: itemID, versionID: versionID,
		words: words, textWas: text,
	})
	assertTheListNamesTheRemovalAndNotTheWords(t, reports.list, words,
		map[string]string{
			erasurelist.KindReport:          reportID,
			erasurelist.KindStatement:       intentID,
			erasurelist.KindArtifactVersion: versionID,
		})
	// The owner's own read of the report, made by the erasure before it
	// destroyed anything, is what makes who had read the words answerable.
	assertTheWordsLeftAReadEvent(t, ctx, d, reportID)

	assertALegalHoldRefusesTheErasure(t, ctx, d, p, made, acting, reports, reportID)
	assertAReplayDestroysThemAgain(t, ctx, d, p, reports, reportID, text, words)
}

// quotedInAVersion writes the item and the spec version the erasure walks to:
// a version authored against the intent the reports were grouped into, quoting
// what the statement quotes. It is the third record the words are inside, and
// the artifact store is its own writer of them.
func quotedInAVersion(t *testing.T, ctx context.Context, d deps, p *path,
	intentID, statement string) (string, string) {
	t.Helper()
	svc := onlyService(t, ctx, d)
	it, err := item.NewDecomposition(d.pool, d.token).Create(ctx, decompositionActor, item.New{
		IntentID: intentID, ServiceID: svc, AreaChain: []string{p.areaID}, Branch: "candidate/erasure",
		RequirementsAnswered: oneRequirement,
	}, p.projectID, p.projectID, nil)
	if err != nil {
		t.Fatalf("writing an item of the intent grouped from reports: %v", err)
	}
	version, _, _, err := p.store.SubmitSpec(ctx, p.specAuthorActor(),
		artifact.By{Authorship: artifact.AuthorshipAgent, Author: d.modelName},
		it.ID, svc, "the spec authored against: "+statement, []criterion.Draft{}, nil, nil, "im_1")
	if err != nil {
		t.Fatalf("submitting the spec version quoting the report: %v", err)
	}
	return it.ID, version.ID
}

// assertTheRowsLandedFirst: the erasure-list row is the first step of the event
// and the redaction record the last, so a stop between them leaves the event
// visibly owing rather than visibly done. Each row's instant is against the
// record written under the same key.
func assertTheRowsLandedFirst(t *testing.T, ctx context.Context, d deps, list string) {
	t.Helper()
	for _, kind := range []struct {
		row    string
		target redaction.TargetKind
	}{
		{erasurelist.KindReport, redaction.KindReport},
		{erasurelist.KindStatement, redaction.KindStatement},
		{erasurelist.KindArtifactVersion, redaction.KindArtifactVersion},
	} {
		rows, err := erasurelist.ReadKind(list, kind.row)
		if err != nil {
			t.Fatalf("reading the erasure list for %s: %v", kind.row, err)
		}
		if len(rows) != 1 {
			t.Fatalf("the erasure list holds %d row(s) naming a %s, want one", len(rows), kind.row)
		}
		written, err := redaction.OverKind(ctx, d.pool, kind.target)
		if err != nil {
			t.Fatalf("reading the redactions naming a %s: %v", kind.target, err)
		}
		if len(written) != 1 {
			t.Fatalf("%d redaction(s) name a %s, want one", len(written), kind.target)
		}
		if written[0].ErasureKey != rows[0].Key {
			t.Errorf("the %s row is keyed %s and its redaction %s, and one erasure keys both alike",
				kind.row, rows[0].Key, written[0].ErasureKey)
		}
		if rows[0].At > written[0].At {
			t.Errorf("the %s row was appended at %s and its redaction written at %s, and the row lands first",
				kind.row, rows[0].At, written[0].At)
		}
	}
}

// assertTheWordsAreGone: the bytes are destroyed in each of the three records
// by that record's own writer, and every link stands — the report is still
// linked to the intent it was grouped into, the group still counts it, and the
// version is still the item's.
func assertTheWordsAreGone(t *testing.T, ctx context.Context, d deps, reports *reports, was erased) {
	t.Helper()
	stored := reports.stored(t, ctx)
	if len(stored) != 1 {
		t.Fatalf("the store holds %d report(s), want the one submitted: a redaction removes no row", len(stored))
	}
	if strings.Contains(stored[0].text, was.words) {
		t.Errorf("the report still carries the words: %q", stored[0].text)
	}
	if len(stored[0].text) != len(was.textWas) {
		t.Errorf("the report's length moved from %d to %d, and a redaction destroys bytes in place",
			len(was.textWas), len(stored[0].text))
	}

	in := readIntent(t, ctx, d, was.intentID)
	if strings.Contains(in.Statement, was.words) {
		t.Errorf("the statement still carries the words: %q", in.Statement)
	}
	version, err := artifact.Get(ctx, d.pool, was.versionID)
	if err != nil {
		t.Fatalf("reading the version the words were quoted in: %v", err)
	}
	if strings.Contains(version.Content, was.words) {
		t.Errorf("the artifact version still carries the words: %q", version.Content)
	}

	// No link breaks: what is erased is text inside a record and never the
	// record, so the row, its grouping link, its count and the version's item
	// all stand.
	if linked := intentOfReport(t, ctx, d, was.reportID); linked != was.intentID {
		t.Errorf("the report is linked to %q after the erasure, want %q", linked, was.intentID)
	}
	group, err := d.reports.Grouped(ctx, was.intentID)
	if err != nil {
		t.Fatalf("reading the group of %s: %v", was.intentID, err)
	}
	if group.Reports != 1 {
		t.Errorf("the group counts %d report(s) after the erasure, want the one it was raised from",
			group.Reports)
	}
	if version.ItemID != was.itemID {
		t.Errorf("the version names item %q after the erasure, want %q", version.ItemID, was.itemID)
	}
}

// erased is what one erasure was over, which the assertions over its three
// records read: the records the links reach, the words that went, and the
// report's text as it stood before.
type erased struct {
	reportID, intentID, itemID, versionID string
	words, textWas                        string
}

// assertTheListNamesTheRemovalAndNotTheWords: a row names what was removed by
// the record and the bounds of each span, so a replay can destroy it again, and
// never a byte of what stood there — the list is itself a copy of what was
// meant to be gone if it carried the words.
func assertTheListNamesTheRemovalAndNotTheWords(t *testing.T, list, words string,
	records map[string]string) {
	t.Helper()
	named := map[string]string{}
	for _, kind := range []string{
		erasurelist.KindReport, erasurelist.KindStatement, erasurelist.KindArtifactVersion,
	} {
		of, err := erasurelist.ReadKind(list, kind)
		if err != nil {
			t.Fatalf("reading the erasure list for %s: %v", kind, err)
		}
		for _, row := range of {
			named[kind] = row.Removed
			if strings.Contains(row.Removed, words) {
				t.Errorf("the %s row carries the words it says were removed: %q", kind, row.Removed)
			}
			if row.FormatVersion != erasurelist.FormatVersion {
				t.Errorf("the %s row declares format version %q", kind, row.FormatVersion)
			}
		}
	}
	for what, id := range records {
		if !strings.HasPrefix(named[what], id+" ") {
			t.Errorf("the %s row says %q was removed, want the record %s and its spans", what, named[what], id)
		}
	}
}

// assertALegalHoldRefusesTheErasure: an erasure inside a standing hold's reach
// is refused whole, before any row lands, and the refusal is recorded as a
// policy version — what a hold over a decision preserves is what the decision
// was made on, and a half-performed erasure would need a restore to undo.
func assertALegalHoldRefusesTheErasure(t *testing.T, ctx context.Context, d deps, p *path,
	made *calls, acting record.Actor, reports *reports, reportID string) {
	t.Helper()
	before := rowsOnTheList(t, reports.list)
	if _, _, err := p.factory.SetLegalHold(ctx, acting, legalhold.Subject{
		Kind: legalhold.SubjectService, ID: onlyService(t, ctx, d),
	}, "counsel asked for everything about this service to stand"); err != nil {
		t.Fatalf("setting the legal hold: %v", err)
	}

	err := made.PerformErasure(ctx, asPrincipal(acting), screens.PerformErasureArgs{
		ReportID: reportID,
		// The first word of the report, which the erasure above left standing.
		Spans:  []screens.ErasureSpan{{Start: 0, End: strings.Index(firstReport, " ")}},
		Reason: "a second erasure, asked for while the hold stands",
	})
	if !errors.Is(err, redaction.ErrLegalHoldReaches) {
		t.Fatalf("the erasure under a standing hold = %v, want it refused", err)
	}
	if now := rowsOnTheList(t, reports.list); now != before {
		t.Errorf("the refused erasure appended %d erasure-list row(s), and it is refused before any lands",
			now-before)
	}

	version, err := p.policy.Newest(ctx, asPrincipal(acting))
	if err != nil {
		t.Fatalf("reading the policy version in force: %v", err)
	}
	if version.Action != policy.ActionRedactionRefused || version.Refusal == "" {
		t.Errorf("the newest policy version is %q with refusal %q, want the refusal on the record",
			version.Action, version.Refusal)
	}
	if version.Scope.ID != reportID {
		t.Errorf("the refusal names %s, want the report the erasure was over", version.Scope.ID)
	}
}

// assertAReplayDestroysThemAgain: the erasure list is outside the recovery unit
// and is never rolled back, so a backup taken before the erasure carries the
// words. The rewind here is the restore — the words put back by hand, in the
// store and nowhere else — and the replay is what takes them out again before
// anything is served.
func assertAReplayDestroysThemAgain(t *testing.T, ctx context.Context, d deps, p *path,
	reports *reports, reportID, textWas, words string) {
	t.Helper()
	if _, err := reports.reads.Exec(ctx, `update `+reportstore.ReportTable+
		` set text = $1 where id = $2`, textWas, reportID); err != nil {
		t.Fatalf("rewinding the report's words the way a restore would: %v", err)
	}
	if back := reports.stored(t, ctx)[0].text; !strings.Contains(back, words) {
		t.Fatalf("the rewind put back %q, want the words the erasure destroyed", back)
	}

	if err := replayTheErasureList(ctx, p, d.reports, reports.list); err != nil {
		t.Fatalf("replaying the erasure list: %v", err)
	}
	if after := reports.stored(t, ctx)[0].text; strings.Contains(after, words) {
		t.Errorf("the replay left the words a restore put back: %q", after)
	}
}

// rowsOnTheList is how many rows the erasure list holds over all four kinds,
// read before and after a refused erasure.
func rowsOnTheList(t *testing.T, list string) int {
	t.Helper()
	n := 0
	for _, kind := range []string{
		erasurelist.KindReport, erasurelist.KindStatement,
		erasurelist.KindArtifactVersion, erasurelist.KindMapping,
	} {
		rows, err := erasurelist.ReadKind(list, kind)
		if err != nil {
			t.Fatalf("reading the erasure list for %s: %v", kind, err)
		}
		n += len(rows)
	}
	return n
}

// onlyService is the id of the one service these tests run on, read off the
// report the way in wrote: the store takes it from the deploy record the token
// found, so it is the same id every record of this run names.
func onlyService(t *testing.T, ctx context.Context, d deps) string {
	t.Helper()
	svc, found, err := service.ByName(ctx, d.pool, theService)
	if err != nil || !found {
		t.Fatalf("reading the service %s: found %v, %v", theService, found, err)
	}
	return svc.ID
}
