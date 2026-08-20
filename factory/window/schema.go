package window

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "watch_window"

// IDPrefix is what [record.NewID] is called with for a watch window.
const IDPrefix = "win"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// deploy_id is unique, which is the "one per production deploy" rule in the
// store: a second window over one deploy is refused rather than left for a reader
// to notice two boundaries over one release. release_id is unique for the same
// reason one release is watched once — "a release its service has not watched
// before" — so a redeploy of a watched release is refused here as well as
// declined by the caller.
//
// exit is empty while the window is open and holds one of the four values when it
// is closed, and closed_at moves with it: exit_and_closed_together enforces that
// in both directions, so a window with an exit and no time and a window with a
// time and no exit are both refused. That is the one place a window's two states
// could disagree.
//
// held_out is copied onto the row for the reason clean_available is not enough on
// its own: a window on a held-out release runs to the cap because the score is
// measuring what it auto-passed, and one on a first release runs to the cap
// because it has nothing to compare against. Both have clean_available false and
// they are not the same window.
//
// The size, the confidence, the cap, and the boundary's formula are copied onto
// the row at the open rather than read back from the service record later, and
// doc.go says why. policy_version and score_version are the same thing for the
// same reason: a window closed under one policy is not readable against today's.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	deploy_id text not null unique,
	release_id text not null unique,
	service_id text not null,
	clean_available boolean not null,
	held_out boolean not null,
	size double precision not null,
	confidence double precision not null,
	cap_seconds double precision not null,
	formula text not null,
	policy_version text not null,
	score_version text not null,
	exit text not null,
	closed_at text not null,
	` + record.Constraints + `,
	constraint deploy_id_present check (deploy_id <> ''),
	constraint release_id_present check (release_id <> ''),
	constraint service_id_present check (service_id <> ''),
	constraint size_is_a_share check (size > 0 and size <= 1),
	constraint confidence_is_a_share check (confidence > 0 and confidence < 1),
	constraint cap_positive check (cap_seconds > 0),
	constraint formula_present check (formula <> ''),
	constraint policy_version_present check (policy_version <> ''),
	constraint score_version_present check (score_version <> ''),
	constraint exit_known check (exit in ('', 'harm', 'clean', 'cap', 'swept')),
	constraint exit_and_closed_together check ((exit <> '') = (closed_at <> '')),
	constraint closed_at_is_time_layout check (closed_at = '' or closed_at ~ '` + record.TimePattern + `')
)`,
}
