package erasurelist

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/dulguun0225/borg/factory/record"
)

// Retire removes every row of the file at path whose erasure is older than
// retention, measured against now, and rewrites the file with what is left;
// a row not yet that old is kept. retention is backup_retention_seconds,
// which has no reader here — the caller reads it and hands it in as a plain
// [time.Duration]. Where an owner authored none the caller passes zero, and
// zero retires nothing: rows are kept for the life of the install, which is
// what the field is there to end. A file that does not exist stays that
// way.
func Retire(path string, retention time.Duration, now time.Time) error {
	if retention <= 0 {
		return nil
	}
	rows, err := readAll(path)
	if err != nil {
		return err
	}
	_, statErr := os.Stat(path)
	existed := statErr == nil

	var kept []Row
	for _, r := range rows {
		at, err := record.ParseTime(r.At)
		if err != nil {
			return fmt.Errorf("erasurelist: parsing instant in %s: %w", path, err)
		}
		if now.Sub(at) <= retention {
			kept = append(kept, r)
		}
	}
	if !existed && len(kept) == 0 {
		return nil
	}
	return writeAll(path, kept)
}

// writeAll replaces the file at path with rows, one JSON line each: the rows
// are written to a temporary file beside path and renamed over it, so a
// reader never sees a partially rewritten file.
func writeAll(path string, rows []Row) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("erasurelist: creating temp file for %s: %w", path, err)
	}
	tmpName := tmp.Name()

	for _, r := range rows {
		line, err := json.Marshal(r)
		if err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return fmt.Errorf("erasurelist: marshal row: %w", err)
		}
		if _, err := tmp.Write(append(line, '\n')); err != nil {
			tmp.Close()
			os.Remove(tmpName)
			return fmt.Errorf("erasurelist: writing %s: %w", tmpName, err)
		}
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("erasurelist: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("erasurelist: renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}
