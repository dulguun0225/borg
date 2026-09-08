package erasurelist

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// writeRows writes rows to path directly, bypassing [Append], so a test can
// control the instant a row carries.
func writeRows(t *testing.T, path string, rows []Row) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatalf("marshal row: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write row: %v", err)
		}
	}
}

func TestAppendThenReadByKind(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")

	if err := Append(path, "key-1", KindReport, "spans 0-12"); err != nil {
		t.Fatalf("Append report: %v", err)
	}
	if err := Append(path, "key-2", KindMapping, "the mapping for p_abc"); err != nil {
		t.Fatalf("Append mapping: %v", err)
	}
	if err := Append(path, "key-3", KindReport, "spans 30-40"); err != nil {
		t.Fatalf("Append report 2: %v", err)
	}

	reports, err := ReadKind(path, KindReport)
	if err != nil {
		t.Fatalf("ReadKind report: %v", err)
	}
	if len(reports) != 2 {
		t.Fatalf("got %d report rows, want 2", len(reports))
	}
	if reports[0].Key != "key-1" || reports[1].Key != "key-3" {
		t.Fatalf("got keys %q, %q; want key-1, key-3 in append order", reports[0].Key, reports[1].Key)
	}
	for _, r := range reports {
		if r.FormatVersion != FormatVersion {
			t.Errorf("row %s: FormatVersion = %q, want %q", r.Key, r.FormatVersion, FormatVersion)
		}
	}

	mappings, err := ReadKind(path, KindMapping)
	if err != nil {
		t.Fatalf("ReadKind mapping: %v", err)
	}
	if len(mappings) != 1 || mappings[0].Key != "key-2" {
		t.Fatalf("got %v, want one row with key key-2", mappings)
	}
}

func TestAppendSameKeyTwiceLeavesOneRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")

	if err := Append(path, "same-key", KindReport, "spans 0-12"); err != nil {
		t.Fatalf("first Append: %v", err)
	}
	if err := Append(path, "same-key", KindReport, "a different description"); err != nil {
		t.Fatalf("second Append: %v", err)
	}

	rows, err := ReadKind(path, KindReport)
	if err != nil {
		t.Fatalf("ReadKind: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Removed != "spans 0-12" {
		t.Errorf("Removed = %q, want the first write's value unchanged", rows[0].Removed)
	}
}

func TestUnknownFormatVersionIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")

	writeRows(t, path, []Row{{
		FormatVersion: "erasure/2",
		Key:           "key-1",
		At:            record.Now(),
		Kind:          KindReport,
		Removed:       "spans 0-12",
	}})

	if _, err := ReadKind(path, KindReport); !errors.Is(err, ErrFormatVersion) {
		t.Fatalf("ReadKind: got %v, want ErrFormatVersion", err)
	}
	if err := Append(path, "key-2", KindReport, "spans 30-40"); !errors.Is(err, ErrFormatVersion) {
		t.Fatalf("Append: got %v, want ErrFormatVersion", err)
	}
}

func TestRetireRemovesRowsOlderThanRetention(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")
	now := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	retention := 30 * 24 * time.Hour

	old := Row{
		FormatVersion: FormatVersion,
		Key:           "old",
		At:            record.FormatTime(now.Add(-40 * 24 * time.Hour)),
		Kind:          KindReport,
		Removed:       "spans 0-12",
	}
	recent := Row{
		FormatVersion: FormatVersion,
		Key:           "recent",
		At:            record.FormatTime(now.Add(-10 * 24 * time.Hour)),
		Kind:          KindReport,
		Removed:       "spans 30-40",
	}
	writeRows(t, path, []Row{old, recent})

	if err := Retire(path, retention, now); err != nil {
		t.Fatalf("Retire: %v", err)
	}

	rows, err := ReadKind(path, KindReport)
	if err != nil {
		t.Fatalf("ReadKind after Retire: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "recent" {
		t.Fatalf("got %v, want only the recent row", rows)
	}
}

func TestRetireOnAbsentFileLeavesItAbsent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")

	if err := Retire(path, 24*time.Hour, time.Now()); err != nil {
		t.Fatalf("Retire: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Retire on an absent file created %s", path)
	}
}

func TestEmptyOrAbsentFileReadsAsNoRows(t *testing.T) {
	dir := t.TempDir()
	absent := filepath.Join(dir, "absent.log")

	rows, err := ReadKind(absent, KindReport)
	if err != nil {
		t.Fatalf("ReadKind on an absent file: %v", err)
	}
	if rows != nil {
		t.Fatalf("got %v, want no rows for an absent file", rows)
	}

	empty := filepath.Join(dir, "empty.log")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatalf("create empty file: %v", err)
	}
	rows, err = ReadKind(empty, KindReport)
	if err != nil {
		t.Fatalf("ReadKind on an empty file: %v", err)
	}
	if rows != nil {
		t.Fatalf("got %v, want no rows for an empty file", rows)
	}
}

func TestAKindNoneOfTheFourIsRefused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")
	err := Append(path, "key-1", "diary", "a page")
	if !errors.Is(err, ErrKind) {
		t.Fatalf("a kind no store replays was taken: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("a refused row created the file: %v", statErr)
	}
}

func TestZeroRetentionKeepsEveryRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "erasure.log")
	old := time.Now().Add(-400 * 24 * time.Hour)
	writeRows(t, path, []Row{{
		FormatVersion: FormatVersion, Key: "key-old", At: old.UTC().Format(record.TimeLayout),
		Kind: KindReport, Removed: "spans 0-3",
	}})
	if err := Retire(path, 0, time.Now()); err != nil {
		t.Fatalf("Retire with nothing authored: %v", err)
	}
	rows, err := ReadKind(path, KindReport)
	if err != nil {
		t.Fatalf("ReadKind: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("an owner who authored no retention lost %d row(s)", 1-len(rows))
	}
}
