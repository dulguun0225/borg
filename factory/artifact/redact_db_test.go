// The database tests of the erasure: [artifact.Store.Redact], the one
// exception to insert-and-never-update, [artifact.Store.RedactionPass], this
// store's own pass over the redactions naming versions it wrote, and
// [artifact.Store.Replay], what a restore is served through. They share
// db_test.go's newStore and the actors it declares.
package artifact_test

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/erasurelist"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/redaction"
)

var redactor = record.Actor{Kind: record.KindHuman, Key: "person:owner", Basis: record.BasisClaimed}

// redacting writes one redaction naming a version's content, the way the
// erasure at Factory does. The erasure-list row it appends goes to a list of
// its own — never the one [listIn] returns — because this store writes none
// of its own and the check against [listIn]'s empty one is what says so.
func redacting(t *testing.T, ctx context.Context, pool *pgxpool.Pool, versionID string,
	spans ...redaction.Span) redaction.Redaction {
	t.Helper()
	r, err := redaction.NewWriter(pool, tokenOf(t, ctx, pool)).Insert(ctx, redactor, redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindArtifactVersion, ID: versionID},
		Reason: "the words quote a report that named a person",
		Spans:  spans,
	}, nil, erasureAppender(t))
	if err != nil {
		t.Fatalf("writing the redaction of %s: %v", versionID, err)
	}
	return r
}

// erasureAppender is a working [redaction.ErasureAppender] over a fresh file,
// the same call reportstore.Store.AppendErasure makes over its own: a plain
// call to erasurelist.Append. [redaction.Insert] refuses a call supplying
// none, and this package composes no store to hand it one of its own.
func erasureAppender(t *testing.T) redaction.ErasureAppender {
	t.Helper()
	list := filepath.Join(t.TempDir(), "erasure-list")
	return func(kind, key, removed string) error {
		return erasurelist.Append(list, key, kind, removed)
	}
}

// listIn is the erasure list this test's composition holds: a file beside the
// test's own directory, the way it is a file beside the targets on a host.
func listIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "erasure-list")
}

// TestRedactDestroysASpanAndKeepsBothDigests is the one exception to
// insert-and-never-update, the digest of the words as written kept beside the
// digest of what remains, and the erasure-list row that lands before it.
func TestRedactDestroysASpanAndKeepsBothDigests(t *testing.T) {
	ctx, pool, s := newStore(t)

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "the secret is 12345 and nothing else", theManifest)
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}

	r := redacting(t, ctx, pool, impl.ID, redaction.Span{Start: 14, End: 19})
	if err := s.Redact(ctx, r); err != nil {
		t.Fatalf("Redact: %v", err)
	}
	// The store writes no row of the erasure list: the report store is that
	// list's one writer, and the row for an artifact version was appended
	// through it by the action before this was called.
	rows, err := erasurelist.ReadKind(listIn(t), erasurelist.KindArtifactVersion)
	if err != nil || len(rows) != 0 {
		t.Errorf("the artifact store left %+v on an erasure list, %v; want it to write none", rows, err)
	}

	read, err := artifact.Get(ctx, pool, impl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if read.Content != "the secret is xxxxx and nothing else" {
		t.Errorf("the redacted content is %q", read.Content)
	}
	// A decision recorded the digest of the words it decided, and it still
	// names them: content_digest is what it was, and the digest of what the
	// erasure left stands beside it.
	if read.ContentDigest != impl.ContentDigest {
		t.Errorf("the content digest is %q, was %q; a decision that recorded the first names nothing now",
			read.ContentDigest, impl.ContentDigest)
	}
	if read.RedactedContentDigest == "" || read.RedactedContentDigest == read.ContentDigest {
		t.Errorf("the digest of what the redaction left is %q, beside %q; want the digest of the redacted words",
			read.RedactedContentDigest, read.ContentDigest)
	}
	if impl.RedactedContentDigest != "" {
		t.Errorf("the version was written with %q in the redacted digest, want it empty until an erasure",
			impl.RedactedContentDigest)
	}
}

func TestRedactRefusesASpanOutsideTheContent(t *testing.T) {
	ctx, pool, s := newStore(t)

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "short", theManifest)
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	r := redacting(t, ctx, pool, impl.ID, redaction.Span{Start: 0, End: 50})
	if err := s.Redact(ctx, r); !errors.Is(err, artifact.ErrSpanOutOfRange) {
		t.Errorf("Redact with an out-of-range span = %v, want ErrSpanOutOfRange", err)
	}
	read, err := artifact.Get(ctx, pool, impl.ID)
	if err != nil || read.Content != "short" {
		t.Errorf("the content is %q, %v; want a refused redaction to have destroyed nothing",
			read.Content, err)
	}

	statement := r
	statement.Target.Kind = redaction.KindStatement
	if err := s.Redact(ctx, statement); !errors.Is(err, artifact.ErrNotAnArtifactVersion) {
		t.Errorf("the artifact store redacting a statement = %v, want ErrNotAnArtifactVersion", err)
	}
}

// TestTheRedactionPassDestroysWhatEveryRedactionNamesOfItsOwn: each target's
// writer destroys the bytes inside its own records, reading the redactions
// naming records it writes and no others.
func TestTheRedactionPassDestroysWhatEveryRedactionNamesOfItsOwn(t *testing.T) {
	ctx, pool, s := newStore(t)

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "the secret is 12345 and nothing else", theManifest)
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	redacting(t, ctx, pool, impl.ID, redaction.Span{Start: 14, End: 19})
	other, err := redaction.NewWriter(pool, tokenOf(t, ctx, pool)).Insert(ctx, redactor, redaction.Writing{
		Target: redaction.Target{Kind: redaction.KindStatement, ID: "in_1"},
		Reason: "intake's own to destroy", Spans: []redaction.Span{{Start: 0, End: 1}},
	}, nil, erasureAppender(t))
	if err != nil {
		t.Fatalf("writing a redaction of another kind: %v", err)
	}

	destroyed, err := s.RedactionPass(ctx)
	if err != nil {
		t.Fatalf("RedactionPass: %v", err)
	}
	if destroyed != 1 {
		t.Errorf("the pass destroyed %d versions, want the 1 redaction naming one", destroyed)
	}
	read, err := artifact.Get(ctx, pool, impl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(read.Content, "12345") {
		t.Errorf("the content is %q, with the bytes a redaction named still in it", read.Content)
	}
	if read.Version != impl.Version || read.Supersedes != impl.Supersedes {
		t.Errorf("the chain moved: version %d supersedes %q, was %d and %q",
			read.Version, read.Supersedes, impl.Version, impl.Supersedes)
	}
	if other.Target.Kind != redaction.KindStatement {
		t.Errorf("the redaction of another kind read back as %s", other.Target)
	}

	// A redaction naming a version this store does not hold is passed over,
	// and a pass run twice destroys the same range twice, which the second
	// time changes nothing.
	redacting(t, ctx, pool, "art_nothing", redaction.Span{Start: 0, End: 1})
	again, err := s.RedactionPass(ctx)
	if err != nil {
		t.Fatalf("RedactionPass a second time: %v", err)
	}
	if again != 1 {
		t.Errorf("the second pass destroyed in %d versions, want the 1 it holds", again)
	}
}

// TestReplayDestroysAgainWhatTheListSaysWasRemoved: the erasure list is never
// rolled back, so a restore that brought the words back is served through this
// before the store serves anything.
func TestReplayDestroysAgainWhatTheListSaysWasRemoved(t *testing.T) {
	ctx, pool, s := newStore(t)

	impl, err := s.SubmitImplementation(ctx, implementer, byAgent, "it_a", "the secret is 12345 and nothing else", theManifest)
	if err != nil {
		t.Fatalf("SubmitImplementation: %v", err)
	}
	// The rows the erasure action appended through the report store, written
	// here directly because this package composes no store: what a replay
	// reads is the file, whoever wrote it.
	list := filepath.Join(t.TempDir(), "erasure-list")
	if err := erasurelist.Append(list, "key", erasurelist.KindArtifactVersion, impl.ID+" 14-19"); err != nil {
		t.Fatalf("appending the erasure-list row: %v", err)
	}
	if err := erasurelist.Append(list, "another", erasurelist.KindStatement, "in_1 0-1"); err != nil {
		t.Fatalf("appending a row of another kind: %v", err)
	}

	destroyed, err := s.Replay(ctx, list)
	if err != nil {
		t.Fatalf("Replay: %v", err)
	}
	if destroyed != 1 {
		t.Errorf("the replay destroyed %d versions, want the 1 row naming one", destroyed)
	}
	read, err := artifact.Get(ctx, pool, impl.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if strings.Contains(read.Content, "12345") {
		t.Errorf("the replay left the content as %q", read.Content)
	}
	if _, err := s.Replay(ctx, filepath.Join(t.TempDir(), "none")); err != nil {
		t.Errorf("a replay over a list that does not exist: %v", err)
	}
}

// tokenOf is the lease's number as newStore's own acquisition left it, for a
// writer this test composes beside the store.
func tokenOf(t *testing.T, ctx context.Context, pool *pgxpool.Pool) lease.Token {
	t.Helper()
	var number int64
	if err := pool.QueryRow(ctx, `select number from `+lease.Table+` where id = 1`).Scan(&number); err != nil {
		t.Fatalf("reading the lease's number: %v", err)
	}
	return lease.Token(number)
}
