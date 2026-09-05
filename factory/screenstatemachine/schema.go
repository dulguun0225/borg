package screenstatemachine

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "screen_state_machine"

// IDPrefix is what [record.NewID] is called with for a row of this table. The
// screen's identity is the id of the machine that introduced it, so a screen
// is named by an id of this shape and there is no screen table: a screen with
// no state machine is nothing the factory can check.
const IDPrefix = "ssm"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "screen_state_machine/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// service_id, spec_artifact_id, item_id, screen and supersedes are id fields
// and not foreign keys, each checked for being present where it is required
// and not for pointing at anything; record's doc.go states that rule and its
// cost once. supersedes is empty on a machine that introduces a screen, and
// screen is that machine's own id — the chain of supersessions is the screen.
//
// The states, the events and the terminal states are arrays, and the
// transitions are the JSON [Machine.Transitions] marshals to: a transition has
// four parts and a column per part would be a table of its own for a field
// that is read whole or not at all. What the encoding costs is that no query
// reaches inside a transition; [InForce] reads the machines and [Validate] and
// [CheckTransitionTargets] read the transitions in Go.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	spec_artifact_id text not null,
	item_id text not null,
	screen text not null,
	supersedes text not null,
	initial text not null,
	states text[] not null,
	events text[] not null,
	transitions text not null,
	terminal text[] not null,
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint spec_artifact_id_present check (spec_artifact_id <> ''),
	constraint item_id_present check (item_id <> ''),
	constraint screen_present check (screen <> ''),
	constraint initial_present check (initial <> ''),
	constraint transitions_present check (transitions <> ''),
	constraint one_machine_supersedes_one unique (supersedes, service_id)
		deferrable initially immediate
)`,

	// The unique constraint above would refuse a second machine that
	// supersedes nothing, every such machine carrying the same empty string,
	// so it is written as a partial index instead: one superseding machine per
	// superseded one, and any number of machines introducing screens.
	`alter table ` + Table + ` drop constraint if exists one_machine_supersedes_one`,
	`create unique index if not exists one_machine_supersedes_one
	on ` + Table + ` (supersedes) where supersedes <> ''`,
}
