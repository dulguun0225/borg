package localtarget

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// The service's store on this platform is a directory: dir/<service>.data,
// whatever the process writes there. Two files sit beside it — the schema
// history the deployer keeps in the store, and the script a change is applied
// by — and a snapshot is a copy of the directory verified by digest.

// DataDir is the service's store on this target: the directory its process
// writes into. A service with no such directory has no store here, which is
// what a snapshot of nothing and an empty schema history both read as.
func DataDir(dir, service string) string { return filepath.Join(dir, service+".data") }

// HistoryFile is the schema history the deployer keeps in the service's store:
// one line per change applied, five fields separated by spaces — the change's
// identity, its checksum, `widened` or `removed`, the release that shipped it,
// and `applied` or `found_applied`. Which changes a store carries is read from
// here and never from a deploy record.
//
// A deploy that names no release writes [noRelease] in the fourth field, a line
// being read by its fields and an empty one leaving four where there are five.
func HistoryFile(dir, service string) string { return filepath.Join(dir, service+".schema") }

// noRelease is the fourth field of a history line written by a deploy that names
// no release — a candidate's own environment, and a build the search called for.
const noRelease = "-"

// The fifth field: what the deployer did with the change. A found-applied row is
// the adoption's word that the store arrived carrying the change.
const (
	fieldApplied      = "applied"
	fieldFoundApplied = "found_applied"
)

// SchemaScript is the script the service ships for one change, run by
// [Local.ApplySchemaChange] with the store's directory as its one argument. A
// change the service ships no script for cannot be applied here.
func SchemaScript(dir, service, change string) string {
	return filepath.Join(dir, service+"."+change+".sql")
}

// SnapshotDir is where a snapshot of the service's store is kept: a copy of
// [DataDir] under the name the deploy record then carries. It sits beside the
// store on this platform, which is what a snapshot kept on the service's own
// store hosting is here.
func SnapshotDir(dir, service, name string) string {
	return filepath.Join(dir, service+".snapshot."+name)
}

var (
	// ErrNoSchemaScript is returned by [Local.ApplySchemaChange] for a change
	// the service ships no script for. The change is not recorded as applied: a
	// history holding a change nothing performed is what the store rule rests on
	// and would be wrong.
	ErrNoSchemaScript = errors.New("localtarget: the service ships no script for that change")
	// ErrSnapshotUnverified is returned by [Local.Snapshot] where the copy does
	// not digest to what was copied. A snapshot the target cannot take and
	// verify is a deploy not performed.
	ErrSnapshotUnverified = errors.New("localtarget: the snapshot does not digest to what was copied")
	// ErrNameNotLocal is returned for a change or a snapshot name that is not a
	// local path element, the same boundary a build and a service name are held
	// to, and about the same joins.
	ErrNameNotLocal = errors.New("localtarget: the name is not a local path")
)

// ApplySchemaChange runs the script the service ships for the change, with the
// store's directory as its one argument, and appends the change to the schema
// history where the script succeeds. A change already in the history is applied
// again by nobody: the deployer applies the changes its build declares that the
// history lacks, and this refuses nothing on its own — it is the deployer that
// reads the history first.
//
// A script that fails leaves the history alone, so a change that failed to apply
// is a change the next read of the history still lacks.
//
// A change marked found applied is written into the history and run on nothing:
// an adopted service's store arrives at the schema its head declares, so the
// deploy of the adoption item's release writes one row per change the build
// declares and applies none. The script is still read, the checksum being over
// its text either way, so a change the service ships no script for is
// [ErrNoSchemaScript] there too.
func (l *Local) ApplySchemaChange(ctx context.Context, p principal.Principal, c targetseam.SchemaChange) error {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return err
	}
	if err := c.Validate(); err != nil {
		return err
	}
	if !filepath.IsLocal(c.Service) {
		return fmt.Errorf("%w: %q", ErrServiceNotLocal, c.Service)
	}
	if !filepath.IsLocal(c.Change) {
		return fmt.Errorf("%w: %q", ErrNameNotLocal, c.Change)
	}

	script := SchemaScript(l.dir, c.Service, c.Change)
	if _, err := os.Stat(script); errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("%w: %s of service %q", ErrNoSchemaScript, c.Change, c.Service)
	} else if err != nil {
		return fmt.Errorf("localtarget: reading the script for %s of service %q: %w", c.Change, c.Service, err)
	}

	if !c.FoundApplied {
		store := DataDir(l.dir, c.Service)
		if err := os.MkdirAll(store, 0o755); err != nil {
			return fmt.Errorf("localtarget: making the store of service %q: %w", c.Service, err)
		}
		if output, err := exec.CommandContext(ctx, script, store).CombinedOutput(); err != nil {
			return fmt.Errorf("localtarget: applying %s to the store of service %q: %w: %s",
				c.Change, c.Service, err, strings.TrimSpace(string(output)))
		}
	}

	text, err := os.ReadFile(script)
	if err != nil {
		return fmt.Errorf("localtarget: reading the script for %s of service %q: %w", c.Change, c.Service, err)
	}
	sum := sha256.Sum256(text)
	widened := "widened"
	if c.Destroys {
		widened = "removed"
	}
	release := c.Release
	if release == "" {
		release = noRelease
	}
	did := fieldApplied
	if c.FoundApplied {
		did = fieldFoundApplied
	}
	line := strings.Join([]string{c.Change, hex.EncodeToString(sum[:]), widened, release, did}, " ") + "\n"

	file, err := os.OpenFile(HistoryFile(l.dir, c.Service), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("localtarget: opening the schema history of service %q: %w", c.Service, err)
	}
	defer file.Close()
	if _, err := file.WriteString(line); err != nil {
		return fmt.Errorf("localtarget: writing the schema history of service %q: %w", c.Service, err)
	}
	return nil
}

// history is what the store's schema history holds, in the order the changes
// were applied, and nothing where the service has none.
func (l *Local) history(service string) ([]targetseam.SchemaChangeApplied, error) {
	content, err := os.ReadFile(HistoryFile(l.dir, service))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("localtarget: reading the schema history of service %q: %w", service, err)
	}
	var applied []targetseam.SchemaChangeApplied
	for _, line := range strings.Split(strings.TrimRight(string(content), "\n"), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 5 {
			return nil, fmt.Errorf("localtarget: the schema history of service %q reads %q, not a change, a checksum, what it did, its release and whether it was found applied",
				service, line)
		}
		row := targetseam.SchemaChangeApplied{
			Change: fields[0], Checksum: fields[1], Widened: fields[2] == "widened",
			Release: fields[3], FoundApplied: fields[4] == fieldFoundApplied,
		}
		if row.Release == noRelease {
			row.Release = ""
		}
		applied = append(applied, row)
	}
	return applied, nil
}

// Snapshot copies the service's store and verifies the copy by digest, which is
// what a deploy takes before it applies a change that destroys stored data. A
// service with no store here snapshots an empty directory: what the copy
// promises is that what the change is about to destroy can still be read, and a
// store holding nothing destroys nothing.
//
// The copy is verified by digesting both directories after it: a copy that
// digests to something else is [ErrSnapshotUnverified] and the copy is removed,
// so a name the deploy record carries never points at a copy nothing verified.
func (l *Local) Snapshot(_ context.Context, p principal.Principal, s targetseam.SnapshotRequest) (targetseam.Snapshot, error) {
	if err := targetseam.CheckPrincipal(p); err != nil {
		return targetseam.Snapshot{}, err
	}
	if err := s.Validate(); err != nil {
		return targetseam.Snapshot{}, err
	}
	if !filepath.IsLocal(s.Service) {
		return targetseam.Snapshot{}, fmt.Errorf("%w: %q", ErrServiceNotLocal, s.Service)
	}
	if !filepath.IsLocal(s.Name) {
		return targetseam.Snapshot{}, fmt.Errorf("%w: %q", ErrNameNotLocal, s.Name)
	}

	store := DataDir(l.dir, s.Service)
	copied := SnapshotDir(l.dir, s.Service, s.Name)
	if err := os.RemoveAll(copied); err != nil {
		return targetseam.Snapshot{}, fmt.Errorf("localtarget: clearing the snapshot %q of service %q: %w", s.Name, s.Service, err)
	}
	if err := copyTree(store, copied); err != nil {
		return targetseam.Snapshot{}, err
	}

	want, err := digestTree(store)
	if err != nil {
		return targetseam.Snapshot{}, err
	}
	got, err := digestTree(copied)
	if err != nil {
		return targetseam.Snapshot{}, err
	}
	if got != want {
		_ = os.RemoveAll(copied)
		return targetseam.Snapshot{}, fmt.Errorf("%w: %q of service %q", ErrSnapshotUnverified, s.Name, s.Service)
	}
	return targetseam.Snapshot{Name: s.Name, Digest: got}, nil
}

// copyTree copies from to to, making to where from does not exist — a store
// that holds nothing is a snapshot of nothing and not an error.
func copyTree(from, to string) error {
	if _, err := os.Stat(from); errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(to, 0o755); err != nil {
			return fmt.Errorf("localtarget: making the snapshot at %s: %w", to, err)
		}
		return nil
	}
	err := filepath.WalkDir(from, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(from, path)
		if err != nil {
			return err
		}
		target := filepath.Join(to, relative)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, content, 0o644)
	})
	if err != nil {
		return fmt.Errorf("localtarget: copying %s to %s: %w", from, to, err)
	}
	return nil
}

// digestTree is the sha256 over a directory's files, each one's path relative to
// the directory and then its bytes, in path order, so that two directories
// holding the same files digest the same and one holding anything else does not.
func digestTree(dir string) (string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) && path == dir {
				return fs.SkipAll
			}
			return err
		}
		if !entry.IsDir() {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("localtarget: reading %s: %w", dir, err)
	}
	sort.Strings(paths)

	sum := sha256.New()
	for _, path := range paths {
		relative, err := filepath.Rel(dir, path)
		if err != nil {
			return "", fmt.Errorf("localtarget: reading %s: %w", path, err)
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("localtarget: reading %s: %w", path, err)
		}
		sum.Write([]byte(relative))
		sum.Write([]byte{0})
		sum.Write(content)
		sum.Write([]byte{0})
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}
