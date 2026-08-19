package deploy

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "deploy"

// IDPrefix is what [record.NewID] is called with for a deploy.
const IDPrefix = "dep"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// service_id, environment_id, release_id, and build_id are id fields and not
// foreign keys: each is another package's record, and a cross-package link is a
// field the link walk reads. The environment is named by the record's id and not
// by its name, the environment being a record from this milestone on. The store
// checks each for being present and not for pointing at anything; record's
// doc.go states that rule and its cost once.
//
// build_id is on every deploy — the build is what runs, and a target reports what
// it runs rather than what that was called. release_id is empty exactly on a
// deploy into a candidate's own environment, that deploy happening one gate before
// the number exists.
//
// The last four columns are a rollback's and empty on every other record. Each
// defaults to the empty string, which is what an ordinary deploy holds: the writer
// names all four on every insert, and the default is for a row inserted around the
// writer — which is what the store's own refusals are tested through.
// undoing_together is what keeps them so: the condemned release and the source
// arrive together and neither arrives without the other, so a record that names one
// of them is a rollback and a record that names neither is an ordinary deploy. The
// swept releases are one id per line and empty where the rollback swept nothing,
// which is every rollback on a service holding one window open — the arrangement
// item's waits_on column and environment's targets column both have.
//
// There are no columns for the control. A control is named on the production
// deploy record in the design, and on a substrate that moves a process rather than
// traffic none is ever started — so the two fields would be columns nothing writes,
// which is how the release record's contract versions are deferred as well.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	environment_id text not null,
	release_id text not null,
	build_id text not null,
	strategy text not null,
	status text not null,
	condemned_release_id text not null default '',
	swept_release_ids text not null default '',
	source text not null default '',
	revert_intent_id text not null default '',
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint environment_id_present check (environment_id <> ''),
	constraint build_id_present check (build_id <> ''),
	constraint strategy_known check (strategy in ('straight', 'with_control')),
	constraint status_known check (status in ('started', 'complete', 'rolled_back')),
	constraint undoing_together check ((condemned_release_id <> '') = (source <> '')),
	constraint undoing_is_a_rollbacks check (
		condemned_release_id <> '' or (swept_release_ids = '' and revert_intent_id = '')
	)
)`,
}
