package lastcheck

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "last_check"

// IDPrefix is what [record.NewID] is called with for a last check.
const IDPrefix = "lc"

// FormatVersion is written into format_version on every insert.
const FormatVersion = "last_check/2"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated.
//
// component_known lists the same components [Components] does, and
// TestDDLListsEveryComponent fails if the two stop agreeing. The drift
// detector's is not among them: it is the eighth last check and it lives in the
// detector's own store, which no factory component may write.
//
// subject_matches_component is the shape of the seam between the components that
// keep one record per thing and the ones that keep a single record for
// themselves: the health monitor's names a service, the deployer's a target
// address or the production environment record's id, and the notifier's, the three passes' and
// dispatch's name nothing. Writing it as a CHECK is what stops a component that
// keeps one per thing from collapsing to a single row nobody can tell apart.
//
// interval_positive is the whole of what makes a stopped component visible: a
// record older than the interval it names has missed a pass, and a record naming
// no interval is one no reader can hold against a clock.
//
// newest_record is the newest time the store the component reads holds a record
// for its subject, empty where the component reads no such store or the store
// holds none. It is a column and not a line in the payload because the pass
// that writes it reads it: the health monitor writes the newest time the
// emission holds for a service, and a read whose newest record is older than
// the interval this record carries is no volume rather than a low one.
//
// The unique constraint is what [Writer.Record]'s insert conflicts on, so a
// component's pass over one subject is one row overwritten and never a history.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	component text not null,
	subject text not null,
	checked_at text not null,
	interval_seconds bigint not null,
	further_pass_owed boolean not null,
	newest_record text not null,
	payload text not null,
	` + record.Constraints + `,
	constraint actor_is_a_component check (actor_kind = 'component'),
	constraint component_known check (component in ('health_monitor', 'deployer', 'notifier', 'constraints_pass', 'advisory_pass', 'deprecation_pass', 'dispatch')),
	constraint subject_matches_component check (
		(component in ('notifier', 'constraints_pass', 'advisory_pass', 'deprecation_pass', 'dispatch')) = (subject = '')
	),
	constraint checked_at_is_time_layout check (checked_at ~ '` + record.TimePattern + `'),
	constraint newest_record_is_time_layout check (newest_record = '' or newest_record ~ '` + record.TimePattern + `'),
	constraint interval_positive check (interval_seconds > 0),
	constraint one_row_per_component_and_subject unique (component, subject)
)`,
}
