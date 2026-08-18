package release

import (
	"crypto/sha256"
	"encoding/binary"

	"github.com/dulguun0225/borg/factory/record"
)

// Table is the one table this package owns.
const Table = "release"

// IDPrefix is what [record.NewID] is called with for a release.
const IDPrefix = "rel"

// lockName is what [AdvisoryLockKey] hashes, the service id appended. It
// names this package so that no other part of the factory derives the same
// key from a name of its own — the same arrangement decisionlog's one lock
// uses.
const lockName = "borg/factory/release/"

// AdvisoryLockKey is the PostgreSQL advisory lock [Writer.Mint] takes for the
// whole of its transaction, one key per service: the first eight bytes of
// SHA-256 of [lockName] plus the service id, big-endian, with the top bit
// cleared so the value is positive. TestAdvisoryLockKeyIsDerivedFromTheName
// recomputes it. Per service rather than one key, because two services'
// numbers have nothing to serialise against each other for.
func AdvisoryLockKey(serviceID string) int64 {
	sum := sha256.Sum256([]byte(lockName + serviceID))
	return int64(binary.BigEndian.Uint64(sum[:8]) & 0x7fffffffffffffff)
}

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// The unique constraint on (service_id, number) is the minting rule in the
// store: an insert that skipped [Writer.Mint]'s lock is refused rather than
// seated at a taken number.
//
// service_id, build_id, and item_id are id fields and not foreign keys: each
// is another package's record, and a cross-package link is a field the link
// walk reads. The store checks each for being present and not for pointing at
// anything; record's doc.go states that rule and its cost once.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	number bigint not null,
	build_id text not null,
	item_id text not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint build_id_present check (build_id <> ''),
	constraint item_id_present check (item_id <> ''),
	constraint number_positive check (number >= 1),
	constraint one_number_per_service unique (service_id, number)
)`,
}
