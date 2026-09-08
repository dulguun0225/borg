package erasurelist

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/dulguun0225/borg/factory/record"
)

// FormatVersion is the payload format version every row of the erasure list
// carries. See doc.go.
const FormatVersion = "erasure/1"

// ErrFormatVersion is returned where a row names a payload format version
// other than [FormatVersion]: the row is refused rather than read as though
// it held this package's own shape.
var ErrFormatVersion = errors.New("erasurelist: unknown format version")

// The four kinds of record an erasure reaches, which are the four stores
// that replay the rows naming their own records after a restore: the report
// store, intake's statement, the artifact store's version, and the People
// mapping. A row names one of these and nothing else, so a store's replay
// reads by a name every writer spells the same way.
const (
	KindReport          = "report"
	KindStatement       = "statement"
	KindArtifactVersion = "artifact_version"
	KindMapping         = "mapping"
)

// ErrKind is returned where a row would name a kind that is none of the
// four: a row under a name no store replays would protect nothing.
var ErrKind = errors.New("erasurelist: unknown kind")

// knownKind reports whether kind is one of the four.
func knownKind(kind string) bool {
	switch kind {
	case KindReport, KindStatement, KindArtifactVersion, KindMapping:
		return true
	}
	return false
}

// Row is one line of the erasure list. See doc.go for what each field is.
type Row struct {
	FormatVersion string `json:"format_version"`
	Key           string `json:"key"`
	At            string `json:"at"`
	Kind          string `json:"kind"`
	Removed       string `json:"removed"`
}

// Append writes one row naming key, kind and removed, kind being one of the
// four this package names, unless the file at path already carries a row
// under that key, in which case it writes nothing: the step that would have appended it, taken again, leaves the
// file as it was. The instant is this package's own read of the clock — the
// way every writer in the graph supplies its own rather than taking one from
// its caller — formatted with [record.Now]. path is created if it does not
// exist.
func Append(path, key, kind, removed string) error {
	if !knownKind(kind) {
		return fmt.Errorf("%w: %q", ErrKind, kind)
	}
	rows, err := readAll(path)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.Key == key {
			return nil
		}
	}

	row := Row{
		FormatVersion: FormatVersion,
		Key:           key,
		At:            record.Now(),
		Kind:          kind,
		Removed:       removed,
	}
	line, err := json.Marshal(row)
	if err != nil {
		return fmt.Errorf("erasurelist: marshal row: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("erasurelist: open %s: %w", path, err)
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		f.Close()
		return fmt.Errorf("erasurelist: append to %s: %w", path, err)
	}
	return f.Close()
}

// ReadKind returns every row naming kind, in the order they were appended.
// This is how a store replays the rows naming its own records against
// whatever a restore brought back before it serves again. A file that does
// not exist reads as no rows.
func ReadKind(path, kind string) ([]Row, error) {
	rows, err := readAll(path)
	if err != nil {
		return nil, err
	}
	var matched []Row
	for _, r := range rows {
		if r.Kind == kind {
			matched = append(matched, r)
		}
	}
	return matched, nil
}

// readAll reads every row of the file at path, in the order they were
// appended. A file that does not exist reads as no rows. A row naming a
// format version other than [FormatVersion] is refused with
// [ErrFormatVersion] before any of its other fields are trusted, rather than
// read as though it held the shape this package knows.
func readAll(path string) ([]Row, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("erasurelist: open %s: %w", path, err)
	}
	defer f.Close()

	var rows []Row
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var row Row
		if err := json.Unmarshal(line, &row); err != nil {
			return nil, fmt.Errorf("erasurelist: reading %s: %w", path, err)
		}
		if row.FormatVersion != FormatVersion {
			return nil, fmt.Errorf("%w: %q in %s", ErrFormatVersion, row.FormatVersion, path)
		}
		rows = append(rows, row)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("erasurelist: reading %s: %w", path, err)
	}
	return rows, nil
}
