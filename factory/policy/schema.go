package policy

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/dulguun0225/borg/factory/record"
)

// Table is the one table this package owns.
const Table = "policy_version"

// IDPrefix is what [record.NewID] is called with for a policy version.
const IDPrefix = "pv"

// FormatVersion is what every row of [Table] carries in its format_version
// column.
const FormatVersion = "policy_version/1"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// There is no update statement anywhere in this package: the table is
// append-only, so a decision naming a version names a write that happened and
// not one that was later edited. supersedes is the version this one replaced and
// is empty on the first, which makes the sequence readable without a column that
// orders it.
//
// policy_version_one_row_per_predecessor is that claim enforced — the table's name
// is in it because a unique constraint creates an index and an index name is unique
// across the schema, where a CHECK constraint's is unique only across its table, so
// score's identical promise cannot be spelled identically. It is unique over supersedes,
// which says two things at once: no two versions name the same predecessor, so the
// chain cannot fork, and at most one names none, so it has one beginning. Without
// it two writers reading the newest version at the same moment would each supersede
// it and the sequence a reader walks would branch, which is exactly what this
// record is read for — an auditor following what a decision was decided under.
// It went unenforced from M2 until 2026-08-20, when the same gap was found in
// score's own version table.
//
// The constraint is not the whole of it: what it turns a silent fork into is an
// error on the second writer, and what stops there being a second writer is
// [AdvisoryLockKey], which this package takes for the read of the newest version and
// the append that supersedes it. score's own writer has held that lock since M2 and
// this one did not.
//
// The columns name the write. parameter is empty on a creation, which authors no
// parameter; qualifier is the gate row or the stage the value was authored for,
// and is empty for a parameter whose scope needs no second name; safeguard_id
// names the safeguard an addition or a withdrawal was about, and is empty
// otherwise. doc.go says why the value itself is not among them.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	action text not null,
	parameter text not null,
	subject_kind text not null,
	subject_id text not null,
	qualifier text not null,
	safeguard_id text not null,
	supersedes text not null,
	` + record.Constraints + `,
	constraint action_known check (action in ('created', 'authored', 'safeguard_added', 'withdrawn')),
	constraint subject_kind_present check (subject_kind <> ''),
	constraint subject_id_present check (subject_id <> ''),
	constraint parameter_matches_action check (
		(action = 'created' and parameter = '')
		or (action <> 'created' and parameter <> '')
	),
	constraint safeguard_matches_action check (
		(action in ('safeguard_added', 'withdrawn') and safeguard_id <> '')
		or (action not in ('safeguard_added', 'withdrawn') and safeguard_id = '')
	),
	constraint policy_version_one_row_per_predecessor unique (supersedes)
)`,
}

// lockName is what [AdvisoryLockKey] hashes. It names this package so that no
// other part of the factory derives the same key from a name of its own — the
// arrangement score's one lock and release's per-service ones both use.
const lockName = "borg/factory/policy"

// AdvisoryLockKey is the PostgreSQL advisory lock every write in this package takes
// for the whole of its transaction: the first eight bytes of SHA-256 of [lockName],
// big-endian, with the top bit cleared so the value is positive.
// TestAdvisoryLockKeyIsDerivedFromTheName recomputes it.
//
// One key and not one per anything: what it serialises is reading the version in
// force and appending the one that supersedes it, and there is one sequence. The
// unique constraint on supersedes is what refuses a fork; this is what stops two
// writers producing one, so a second owner authoring at the same moment waits
// rather than failing.
func AdvisoryLockKey() int64 {
	sum := sha256.Sum256([]byte(lockName))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}
