package policy

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "policy_version"

// IDPrefix is what [record.NewID] is called with for a policy version.
const IDPrefix = "pv"

// DDL is this package's schema. [record.Columns] and [record.Constraints] are
// composed rather than restated.
//
// There is no update statement anywhere in this package: the table is
// append-only, so a decision naming a version names a write that happened and
// not one that was later edited. supersedes is the version this one replaced and
// is empty on the first, which makes the sequence readable without a column that
// orders it.
//
// The columns name the write. parameter is empty on a creation, which authors no
// parameter; qualifier is the gate row or the stage the value was authored for,
// and is empty for a parameter whose scope needs no second name; pin_id names
// the pin a pinning or a withdrawal was about, and is empty otherwise. doc.go
// says why the value itself is not among them.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	action text not null,
	parameter text not null,
	subject_kind text not null,
	subject_id text not null,
	qualifier text not null,
	pin_id text not null,
	supersedes text not null,
	` + record.Constraints + `,
	constraint action_known check (action in ('created', 'authored', 'pinned', 'withdrawn')),
	constraint subject_kind_present check (subject_kind <> ''),
	constraint subject_id_present check (subject_id <> ''),
	constraint parameter_matches_action check (
		(action = 'created' and parameter = '')
		or (action <> 'created' and parameter <> '')
	),
	constraint pin_matches_action check (
		(action in ('pinned', 'withdrawn') and pin_id <> '')
		or (action not in ('pinned', 'withdrawn') and pin_id = '')
	)
)`,
}
