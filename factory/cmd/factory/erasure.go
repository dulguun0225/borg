// The one erasure an owner performs at Factory: one action over one report's
// words, the links from it walked, and the three steps of the event in the
// order the design sets — the erasure-list rows, the redaction records, and
// each target's own destruction.
package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/intent"
	"github.com/dulguun0225/borg/factory/item"
	"github.com/dulguun0225/borg/factory/legalhold"
	"github.com/dulguun0225/borg/factory/people"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/redaction"
	"github.com/dulguun0225/borg/factory/reportstore"
	"github.com/dulguun0225/borg/factory/screens"
)

// PerformErasure is the erasure as one action. An owner names one report and
// the bytes of it that go; the factory walks the links that exist — the report
// to the intent it was grouped into, that intent's statement, and the artifact
// versions authored against it — and finds the same words in each, because
// what an owner has is the words and not a byte range in a record they never
// read.
//
// The event has three steps and this is the order of them, which is the order
// the design sets for an event that writes more than one record: every
// erasure-list row first, keyed so the same erasure performed again appends
// none; then the redaction records, each with the policy version beside it;
// then each target's own writer destroying its own bytes. [redaction.Insert]
// makes the first two one call: it appends the row itself, through the
// appendErasure it is handed — the report store's own AppendErasure, the
// list's one writer — before it writes the record, so a stop between them
// leaves the event visibly owing rather than visibly done, and the row is
// what a restore replays.
//
// A legal hold reaching any one of the targets refuses the whole action before
// any row lands, and the refusal is recorded as a policy version: an erasure
// half performed under a hold would need a restore to undo, and what a hold
// over a decision preserves is what the decision was made on.
func (c *calls) PerformErasure(ctx context.Context, who principal.Principal,
	args screens.PerformErasureArgs) error {
	actor, err := c.acting(ctx, who)
	if err != nil {
		return err
	}
	store := c.p.d.reports
	if store == nil {
		return fmt.Errorf("%w: this factory holds no report store, so there is nothing to erase from",
			screens.ErrRefused)
	}
	if args.ReportID == "" {
		return fmt.Errorf("%w: an erasure names the report whose words go", screens.ErrRefused)
	}
	if args.Reason == "" {
		return fmt.Errorf("%w: an erasure names its reason, which is what the record carries in place of the words",
			screens.ErrRefused)
	}
	if len(args.Spans) == 0 {
		return fmt.Errorf("%w: an erasure names the spans it removes", screens.ErrRefused)
	}

	// The read is the owner's own and appends a read event, the way every read
	// of a report's words does: who had already read them before the redaction
	// is what makes the erasure answerable.
	report, err := store.Get(ctx, asPrincipal(actor), args.ReportID)
	if errors.Is(err, reportstore.ErrNotFound) {
		return fmt.Errorf("%w: %s", screens.ErrNotFound, args.ReportID)
	} else if err != nil {
		return err
	}
	spans := make([]redaction.Span, 0, len(args.Spans))
	for _, span := range args.Spans {
		spans = append(spans, redaction.Span{Start: span.Start, End: span.End})
	}
	words, err := wordsAt(report.Text, spans)
	if err != nil {
		return fmt.Errorf("%w: %v", screens.ErrRefused, err)
	}

	targets := []redaction.Writing{{
		Target: redaction.Target{Kind: redaction.KindReport, ID: report.ID},
		Reason: args.Reason, Spans: spans,
	}}
	linked, subjects, err := c.linkedToTheReport(ctx, report, words, args.Reason)
	if err != nil {
		return err
	}
	targets = append(targets, linked...)

	reaches := c.holdReachingAnyOf(subjects)
	held, err := redaction.Reaching(ctx, c.p.d.pool, reaches)
	if err != nil {
		return err
	}
	if held {
		if _, err := c.p.factory.RecordRedactionRefusal(ctx, actor, targets[0],
			"a legal hold stands over a record this erasure reaches"); err != nil {
			return err
		}
		return fmt.Errorf("%w: %s", redaction.ErrLegalHoldReaches, report.ID)
	}

	// Each target's row lands before its record: [redaction.Insert] appends
	// it through the appendErasure it is handed here — the report store's own
	// AppendErasure — keyed by the same [redaction.Key] the record carries,
	// so the two cannot be keyed differently and the erasure performed again
	// appends neither.
	written := make([]redaction.Redaction, 0, len(targets))
	for _, target := range targets {
		performed, _, err := c.p.factory.WriteRedaction(ctx, actor, target, reaches, store.AppendErasure)
		if err != nil {
			return err
		}
		written = append(written, performed)
	}
	if err := c.destroyWhatWasNamed(ctx, store, written); err != nil {
		return err
	}
	c.changed("factory", listAddressID)
	c.changed("work", listAddressID)
	return nil
}

// linkedToTheReport is every target beside the report itself, and every
// legal-hold subject the targets hang from: the statement of the intent the
// report was grouped into, each artifact version authored against that intent,
// the project the intent lies in, and the service of each of its items. The
// walk is made once for both, the query being the same one either way.
//
// The spans are searched for rather than given, and the target's own text is
// read first, so a span always falls inside what it names. A record that does
// not quote the words is no target: the erasure reaches what quoted them and
// nothing else.
func (c *calls) linkedToTheReport(ctx context.Context, report reportstore.Report,
	words []string, reason string) ([]redaction.Writing, []legalhold.Subject, error) {
	// A hold's subject is a service, a project or the whole install and never a
	// report, a statement or a version, so the report's own service stands for
	// the report here.
	subjects := []legalhold.Subject{{Kind: legalhold.SubjectService, ID: report.ServiceID}}
	if report.IntentID == "" {
		return nil, subjects, nil
	}
	in, err := intent.Get(ctx, c.p.d.pool, report.IntentID)
	if err != nil {
		return nil, nil, err
	}
	if in.ProjectID != "" {
		subjects = append(subjects, legalhold.Subject{Kind: legalhold.SubjectProject, ID: in.ProjectID})
	}
	var targets []redaction.Writing
	if spans := spansOfWords(in.Statement, words); len(spans) > 0 {
		targets = append(targets, redaction.Writing{
			Target: redaction.Target{Kind: redaction.KindStatement, ID: in.ID},
			Reason: reason, Spans: spans,
		})
	}
	items, err := item.ForIntent(ctx, c.p.d.pool, in.ID)
	if err != nil {
		return nil, nil, err
	}
	for _, it := range items {
		subjects = append(subjects, legalhold.Subject{Kind: legalhold.SubjectService, ID: it.ServiceID})
		versions, err := artifact.ForItem(ctx, c.p.d.pool, it.ID)
		if err != nil {
			return nil, nil, err
		}
		for _, version := range versions {
			spans := spansOfWords(version.Content, words)
			if len(spans) == 0 {
				continue
			}
			targets = append(targets, redaction.Writing{
				Target: redaction.Target{Kind: redaction.KindArtifactVersion, ID: version.ID},
				Reason: reason, Spans: spans,
			})
		}
	}
	return targets, subjects, nil
}

// destroyWhatWasNamed is the third step: each target's own writer destroys the
// bytes inside its own records, so no component writes another's record. The
// report store is handed its redaction across the seam it reaches the graph
// through, being a second database; intake and the artifact store take the
// record itself.
//
// The erasure is effective at the redaction whatever this does: every reader
// reads its target through the redactions naming it from the moment one
// exists, so a store whose destruction lags serves nothing meanwhile and
// finishes on its own pass.
func (c *calls) destroyWhatWasNamed(ctx context.Context, store *reportstore.Store,
	written []redaction.Redaction) error {
	for _, one := range written {
		var err error
		switch one.Target.Kind {
		case redaction.KindReport:
			spans := make([]reportstore.Span, 0, len(one.Spans))
			for _, span := range one.Spans {
				spans = append(spans, reportstore.Span{Start: span.Start, End: span.End})
			}
			err = store.Redact(ctx, reportstore.Redaction{
				ID: one.ID, ErasureKey: one.ErasureKey, ReportID: one.Target.ID, Spans: spans,
			})
		case redaction.KindStatement:
			err = c.p.intake.Redact(ctx, one)
		case redaction.KindArtifactVersion:
			err = c.p.store.Redact(ctx, one)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// holdReachingAnyOf is the caller's half of the legal hold's refusal: whether
// a hold stands over any subject the erasure's targets hang from. Package
// redaction reads the hold over the whole install itself and takes this for
// the rest, the arrangement people.DeleteMapping already has.
func (c *calls) holdReachingAnyOf(subjects []legalhold.Subject) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		for _, subject := range subjects {
			if subject.ID == "" {
				continue
			}
			held, err := legalhold.Reaching(ctx, c.p.d.pool, subject)
			if err != nil {
				return false, err
			}
			if held {
				return true, nil
			}
		}
		return false, nil
	}
}

// wordsAt is what the spans name inside the report's text, read against its
// own length first so a span outside the words fails before anything is
// written. They are what the same erasure is searched for by in every other
// record: what an owner points at is words, and a byte range is one record's
// spelling of them.
func wordsAt(text string, spans []redaction.Span) ([]string, error) {
	words := make([]string, 0, len(spans))
	for _, span := range spans {
		if span.Start < 0 || span.End > len(text) || span.Start > span.End {
			return nil, fmt.Errorf("the span [%d,%d) is outside a report of length %d",
				span.Start, span.End, len(text))
		}
		if span.Start == span.End {
			return nil, fmt.Errorf("the span [%d,%d) removes nothing", span.Start, span.End)
		}
		words = append(words, text[span.Start:span.End])
	}
	return words, nil
}

// spansOfWords is where each of the words stands inside text, every occurrence
// of each, as half-open byte ranges. It is how the erasure finds the same
// words in a record nobody named, and the target's own text is what it
// searches, so what comes back always falls inside it.
func spansOfWords(text string, words []string) []redaction.Span {
	var spans []redaction.Span
	for _, word := range words {
		if word == "" {
			continue
		}
		from := 0
		for {
			at := strings.Index(text[from:], word)
			if at < 0 {
				break
			}
			start := from + at
			spans = append(spans, redaction.Span{Start: start, End: start + len(word)})
			from = start + len(word)
		}
	}
	return spans
}

// replayTheErasureList is what a restore is served through: before this
// process serves anything, each store destroys again what the list says was
// removed from records it writes. The list lives outside the recovery unit and
// is never rolled back, so a backup taken before an erasure carries the words
// and this is what takes them out again.
//
// Four stores replay, which is one per kind of row the list carries. Package
// people holds no reader of the list — the list has one writer, the report
// store, and package people imports neither — so this composition reads the
// rows naming a mapping and hands it the keys.
func replayTheErasureList(ctx context.Context, p *path, store *reportstore.Store,
	erasureList string) error {
	if _, err := store.Replay(ctx); err != nil {
		return err
	}
	if _, err := p.intake.Replay(ctx, erasureList); err != nil {
		return err
	}
	if _, err := p.store.Replay(ctx, erasureList); err != nil {
		return err
	}
	rows, err := erasurelist.ReadKind(erasureList, erasurelist.KindMapping)
	if err != nil {
		return err
	}
	erased := make([]string, 0, len(rows))
	for _, row := range rows {
		erased = append(erased, row.Key)
	}
	if _, err := people.Replay(ctx, p.d.pool, p.d.token, erased); err != nil {
		return err
	}
	return nil
}
