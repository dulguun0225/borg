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
// The four text columns are what the version names — the published formula, the
// factor set, and the values the score supplies — stored as the text a reader
// reads rather than as structure a reader would have to reassemble. A version
// differs from its predecessor exactly where one of the four does, which is what
// Writer.Ensure compares, and the unique index is that comparison enforced by the
// store: two versions saying the same thing would be a sequence nothing could
// tell apart.
//
// The index is over a digest of the four and not over the four themselves,
// because the published formula alone is longer than the largest key a btree
// index takes. The digest is computed by the index and stored in no column: a
// column holding it would be one fact in two places, and nothing here rests on
// the digest being hard to collide — what the chain's integrity rests on is the
// decision log's own hash. The separator is a character no text here contains,
// so two versions cannot collide by one field ending where the next begins.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	formula_version text not null,
	formula text not null,
	factor_set text not null,
	supplied text not null,
	supersedes text not null,
	` + record.Constraints + `,
	constraint formula_version_present check (formula_version <> ''),
	constraint formula_present check (formula <> ''),
	constraint factor_set_present check (factor_set <> ''),
	constraint supplied_present check (supplied <> '')
)`,
}
