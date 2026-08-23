package score

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/dulguun0225/borg/factory/record"
)

// Table is the one table this package owns.
const Table = "score_version"

// IDPrefix is what [record.NewID] is called with for a score version.
const IDPrefix = "scv"

// lockName is what [AdvisoryLockKey] hashes. It names this package so that no
// other part of the factory derives the same key from a name of its own — the
// arrangement decisionlog's one lock and release's per-service ones both use.
const lockName = "borg/factory/score"

// AdvisoryLockKey is the PostgreSQL advisory lock [Writer.Ensure] takes for the
// whole of its transaction: the first eight bytes of SHA-256 of [lockName],
// big-endian, with the top bit cleared so the value is positive.
// TestAdvisoryLockKeyIsDerivedFromTheName recomputes it.
//
// One key and not one per anything: what it serialises is reading the newest
// version and appending the one that supersedes it, and there is one sequence.
func AdvisoryLockKey() int64 {
	sum := sha256.Sum256([]byte(lockName))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// There is no update statement anywhere in this package: the table is
// append-only, so a decision naming a version names what that version said and
// not what the score says now. supersedes is the version this one replaced and
// is empty on the first, which makes the sequence readable without a column
// that orders it.
//
// Five columns are what the version names: the published formula, the factor set,
// the rules by which a supplied value moves, and the values the score supplies. A
// version differs from its predecessor exactly where one of the five does, which
// is what Writer.Ensure compares.
//
// Four of the five are the text a reader reads. supplied is the exception and is
// structure — the JSON encoding of the supplied table — because package policy
// reads a number out of it at every gate firing and no reader of prose can. What
// that costs is a column a human reading the row sees as JSON, which is why
// SuppliedValues.Text exists and why the crude interface prints that and not this.
//
// Nothing in the store refuses two versions that say the same thing, and that is
// deliberate rather than missing. What would be wrong is a version saying what the
// one below it says, and only the lock Writer.Ensure holds can refuse that — two
// versions saying the same thing where they are not adjacent are legitimate, and
// with a learned value they are ordinary: the window limit rises to 2 on a service and falls
// back
// to 1 at the next rollback that sweeps, which is a table equal to one two
// versions down. A unique index over the five would refuse exactly that, so a
// value that moved could never move back. This comment claimed such an index from
// M2 until 2026-08-20, when building the learning found that no statement here
// ever created one — the claim was wrong and the absence was right. Refusing it in
// the store could only be a trigger anyway: it is a comparison between two rows
// made at the insert, and a trigger is logic the source does not show.
//
// What the store does refuse is a sequence that is not one.
// score_version_one_row_per_predecessor is unique over supersedes, which says two things at once: no two versions name
// the same predecessor, so the chain cannot fork, and at most one names none, so it
// has one beginning. That is what "the sequence is readable without a column that
// orders it" actually rests on, and it went unenforced from M2 until 2026-08-20 —
// found by looking for the index this comment used to claim. It is the arrangement
// decisionlog's one-closing-per-opening index already has, one table along.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	formula_version text not null,
	formula text not null,
	factor_set text not null,
	rules text not null,
	supplied text not null,
	supersedes text not null,
	` + record.Constraints + `,
	constraint formula_version_present check (formula_version <> ''),
	constraint formula_present check (formula <> ''),
	constraint factor_set_present check (factor_set <> ''),
	constraint rules_present check (rules <> ''),
	constraint supplied_present check (supplied <> ''),
	constraint score_version_one_row_per_predecessor unique (supersedes)
)`,
}
