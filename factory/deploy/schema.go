package deploy

import "github.com/dulguun0225/borg/factory/record"

// Table is the deploy record, [TargetTable] is its completion per target, and
// [MitigationTable] is the mitigation the deployer writes when Ops asks for
// one. Three tables and one component: the deployer writes all three and
// nothing else writes any of them.
const (
	Table           = "deploy"
	TargetTable     = "deploy_target"
	MitigationTable = "mitigation"
)

// IDPrefix is what [record.NewID] is called with for a deploy, and
// [MitigationIDPrefix] for a mitigation. A row of [TargetTable] has no
// identifier of its own: it is a field of the deploy record spread over rows,
// keyed by the deploy and the address.
const (
	IDPrefix           = "dep"
	MitigationIDPrefix = "mit"
)

// FormatVersion is written into every deploy record's format_version column,
// and [FormatVersionMitigation] into every mitigation's.
const (
	FormatVersion           = "deploy/3"
	FormatVersionMitigation = "mitigation/1"
)

// lockName is what [AdvisoryLockKey] hashes, the service and the environment
// appended. It names this package so that no other part of the factory derives
// the same key from a name of its own — the arrangement release's mint lock
// has.
const lockName = "borg/factory/deploy/"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than restated,
// so the actor field and its constraints are the same ones every record table
// carries.
//
// # The deploy record
//
// It is keyed by service and environment and not by target: one record names
// one release for the whole environment, and completion is a field per target,
// which [TargetTable] holds a row of. Service and environment are the grain and
// not the identity — a rollout, a rollback, a revert's deploy, and a deploy the
// search calls for each write one for the same pair — so the identity is that
// pair and number, a sequence the deployer assigns per pair as it begins each
// deploy. one_number_per_pair is that rule in the store.
//
// service_id, environment_id, release_id, build_id, and the id lists are id
// fields and not foreign keys: each is another package's record, and a
// cross-package link is a field the link walk reads. The store checks each for
// being present and not for pointing at anything; record's doc.go states that
// rule and its cost once.
//
// What a record names as deployed is the build it put there and, where one
// exists, the release. Three records name no release: a deploy into a
// candidate's own environment, which fires one gate before the number exists; a
// deploy the search calls for, whose build is a commit on no branch; and a
// removal, which names neither release nor build and is what clears the current
// release. names_a_build_for_its_release is what keeps a release from being
// named without one.
//
// The strategy is a production deploy's and no other's: a strategy decides
// whether a control runs, and a control exists only where organic traffic does.
// Both fields are empty on every other deploy, which
// performed_names_its_picked holds one way round; the other way round is not a
// rule, because the performed field is empty until the deployer has performed
// something. It is written when the shift returns, or when the row without a
// control has replaced the instances of the first target, and never at the
// start: a deployer that stopped between the record's write and the shift would
// otherwise leave a record naming a control that never ran. The performed one is
// what the window, Ops, and the score read, and it differs from the picked one
// where a target declared as serving a share refused the shift.
//
// status is started, complete, or failed. Failed is where the deployer stopped
// the deploy before any target was complete, and the step that stopped it is on
// the record beside it: failed_names_its_step is that pair, and
// [Writer.MarkFailed] is what refuses to mark a record failed once a target has
// completed.
//
// The rollback's four columns are empty on every other record.
// undoing_together is what keeps them so: the failed release and the source
// arrive together and neither arrives without the other, so a record that names
// one of them is a rollback and a record that names neither is an ordinary
// deploy. The skipped releases are one id per line and empty where the rollback
// skipped nothing, which is every rollback on a service holding one window
// open — the arrangement item's waits_on column and environment's targets
// column both have. There is no revert intent here: the edge runs the other
// way, from the revert item's intent through its evidence to the release it
// reverts, and a stored column pointing back would be a second edge able to
// disagree.
//
// # What the record carries beside the deploy
//
// delivered_release_ids is a revert's deploy listing the releases it delivers:
// the ones the hold was holding, whose code is in its build. Without it Ops
// reads those releases as merged and never deployed while their code is live.
//
// schema_changes are the changes this deploy's build carries, one per line,
// applied to the service's store before the build takes traffic, and
// schema_changes_completed is whether they completed. A revert's deploy is the
// one deploy that can carry more than one: it delivers releases that never
// deployed on their own, and it applies each of their changes that no deploy
// applied, in release order. Which changes a store carries is read from the
// store's own schema history and never from here; what this says is what this
// deploy did — including a deploy that applied none because the history already
// held every one of them, which is completed and not a change that failed.
//
// backfill_contract, backfill_element and backfill_from_element are what a
// backfill item's release copies between, all three together or none of them,
// and backfill_copied is whether every row the old form holds is present in the
// new. The record marks the backfill complete by being marked complete, and the
// deployer marks it complete only once backfill_copied says the copy finished —
// so a record that is complete and names the element is the fact enforcement
// reads before it admits the item that moves reads to that element and the drop
// after it. A copy that runs for hours leaves the deploy standing incomplete
// until something writes that column, which is what Ops shows while it runs.
//
// snapshot_name and snapshot_digest are the copy taken and verified before a
// change that destroys stored data, and snapshot_deleted_at is written when the
// deployer deletes it at the end of the service's snapshot retention or earlier
// at an owner's call — so the record says where what the change destroyed could
// be read and, after that, that it no longer can. snapshot_deleted_names_one is
// what keeps a deletion from standing without a copy.
//
// configuration_digest is over the resolved value set of the service's
// configuration at this deploy, computed through the resolver seam 3 names. The
// configuration version so named is restored with the release at a rollback.
//
// way_in_token_digest is the digest of the token the deployer minted for the
// way in at this deploy, never the token. [ByWayInTokenDigest] is what reads
// it: the report store digests the token a submission presents and takes the
// service and the environment from the record that finds.
//
// # Completion per target
//
// [TargetTable] is a field of the deploy record and not a record of its own: it
// composes neither record.Columns nor record.Constraints, holds no actor and no
// format version, and ../../end-goal/records.md lists no row for it. A record
// per target stays refused — four records could each name a different release
// and each be right. There is a row beside each of the environment's targets,
// keyed by the deploy and the address, carrying the position that is the
// environment's order and holding one of not reached, complete, or rolled back.
//
// runs_here is whether the service runs on that target. The rows are the
// environment's and the rollout is the service's set, so this is what tells a
// target the deployer will reach from one it will not: a target the service
// does not run on holds a row that stays not reached, which is what the drift
// detector reads there, and the record is complete when every row that runs
// here is.
//
// release_instances, control_instances and kept_instances are the three sets of
// instances a production deploy runs on that target, each written when the
// deploy starts, which is when the window opens over it: the release's own, the
// control's, and the instances the build being replaced had, or the fraction of
// them an owner authored, kept until the last window that could return to it
// closes — a rollback returns production to them, and a share is not a
// capacity. Without the last those kept instances are an assertion and the
// drift detector has nothing to read what runs against.
//
// control_release_id is the release the control on this target runs — the
// newest release below this one whose window closed without failing it, which
// is the release a rollback of this deploy would return to — control_build_id
// is the build that release is, which is what runs, and control_instances is
// how many instances of it run here. A control is defined by which release it
// runs, so a target with none running names none.
//
// The three are written when the rollout reaches that target and the shift
// returns, and not at the start: there is one control per production target the
// release has reached, started on that target when the rollout reaches it, so a
// record naming a control on a target the rollout never reached would say a
// comparison ran where none did.
//

// reached_at is written before the deployer calls that target and complete_at
// after it, both carrying the fencing token, which is what bounds what a
// deployer whose lease lapsed mid-call can leave behind. replacement is what
// the seam reported, which is the drain and nothing else: neither rollout row
// drops a request, and a platform unable to hold one open across the
// replacement refuses the call instead, which the deployer records as a
// target refused rather than as a replacement here — the column holds the
// one value the seam may ever report and is never a second word for a cut.
//
// release_torn_down_at, control_torn_down_at and kept_torn_down_at are the three
// fleets' spans carried into a duration, each ending where that fleet did: the
// release's own when the release replacing it completed here, the control's and
// the kept fleet's when the last window that could return to that release
// closed. Each span starts when the deploy started and the window opened over
// it, which is the record's own timestamp. release_instance_hours,
// control_instance_hours and kept_instance_hours are the hours each fleet ran,
// and instance_hours is the three added up, which is what instance-hours per
// release sums. amount is each span converted at the rate in force when that
// span was written, added together and never repriced by a rate corrected later,
// and rate is the rate in force at the last of those writes. The amount and the
// rate are null together where the service record carried no instance-hour
// rate, which is not an amount of zero.
//
// # The mitigation
//
// [MitigationTable] is a record and composes the columns every record table
// does. It names the actor under seam 1 — a human at Ops, whose instruction the
// deployer performs it on — the operation, the target, and the deploy record it
// modifies, which the drift detector reads as intended state. There are two
// operations and not three: shifting traffic off a target, and changing the
// instance count of a release the factory deployed. Ending every instance of a
// service on a target is a removal, which retirement calls for.
//
// A mitigation stands until a human ends it at Ops, so ended_at is written
// beside the human who ended it: ended_actor_kind, ended_actor_key and
// ended_actor_key_basis are that human's, the three fields every actor is named
// by, and the kind is human because nothing else may end one. The two arrive
// together, which ended_names_who_ended_it holds — a mitigation that stopped
// standing with nobody named would say a human ended it and not which.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	service_id text not null,
	environment_id text not null,
	number bigint not null,
	release_id text not null,
	build_id text not null,
	delivered_release_ids text not null default '',
	strategy_picked text not null default '',
	strategy_performed text not null default '',
	status text not null,
	failed_step text not null default '',
	schema_changes text not null default '',
	schema_changes_completed boolean not null default false,
	snapshot_name text not null default '',
	snapshot_digest text not null default '',
	snapshot_deleted_at text not null default '',
	configuration_digest text not null default '',
	way_in_token_digest text not null default '',
	backfill_contract text not null default '',
	backfill_element text not null default '',
	backfill_from_element text not null default '',
	backfill_copied boolean not null default false,
	failed_release_id text not null default '',
	skipped_release_ids text not null default '',
	source text not null default '',
	` + record.Constraints + `,
	constraint service_id_present check (service_id <> ''),
	constraint environment_id_present check (environment_id <> ''),
	constraint number_positive check (number >= 1),
	constraint one_number_per_pair unique (service_id, environment_id, number),
	constraint names_a_build_for_its_release check (release_id = '' or build_id <> ''),
	constraint strategies_known check (
		strategy_picked in ('', 'without_control', 'with_control')
		and strategy_performed in ('', 'without_control', 'with_control')
	),
	constraint performed_names_its_picked check (strategy_performed = '' or strategy_picked <> ''),
	constraint status_known check (status in ('started', 'complete', 'failed')),
	constraint failed_names_its_step check ((status = 'failed') = (failed_step <> '')),
	constraint schema_changes_completed_names_one check (schema_changes <> '' or not schema_changes_completed),
	constraint backfill_copied_names_one check (backfill_element <> '' or not backfill_copied),
	constraint backfill_names_all_three check (
		(backfill_contract = '' and backfill_element = '' and backfill_from_element = '')
		or (backfill_contract <> '' and backfill_element <> '' and backfill_from_element <> '')
	),
	constraint snapshot_names_its_digest check ((snapshot_name = '') = (snapshot_digest = '')),
	constraint snapshot_deleted_names_one check (snapshot_deleted_at = '' or snapshot_name <> ''),
	constraint snapshot_deleted_at_is_time_layout check (
		snapshot_deleted_at = '' or snapshot_deleted_at ~ '` + record.TimePattern + `'),
	constraint undoing_together check ((failed_release_id <> '') = (source <> '')),
	constraint undoing_is_a_rollbacks check (failed_release_id <> '' or skipped_release_ids = '')
)`,

	`create table if not exists ` + TargetTable + ` (
	deploy_id text not null,
	position int not null,
	address text not null,
	runs_here boolean not null default true,
	completion text not null,
	release_instances int not null default 0,
	control_instances int not null default 0,
	control_release_id text not null default '',
	control_build_id text not null default '',
	kept_instances int not null default 0,
	replacement text not null default '',
	reached_at text not null default '',
	complete_at text not null default '',
	release_torn_down_at text not null default '',
	control_torn_down_at text not null default '',
	kept_torn_down_at text not null default '',
	release_instance_hours double precision not null default 0,
	control_instance_hours double precision not null default 0,
	kept_instance_hours double precision not null default 0,
	instance_hours double precision not null default 0,
	amount double precision,
	rate double precision,
	constraint one_row_per_target primary key (deploy_id, address),
	constraint address_present check (address <> ''),
	constraint position_not_negative check (position >= 0),
	constraint completion_known check (completion in ('not_reached', 'complete', 'rolled_back')),
	constraint instances_not_negative check (
		release_instances >= 0 and control_instances >= 0 and kept_instances >= 0),
	constraint torn_down_at_is_time_layout check (
		(release_torn_down_at = '' or release_torn_down_at ~ '` + record.TimePattern + `')
		and (control_torn_down_at = '' or control_torn_down_at ~ '` + record.TimePattern + `')
		and (kept_torn_down_at = '' or kept_torn_down_at ~ '` + record.TimePattern + `')),
	constraint replacement_known check (replacement in ('', 'drained')),
	constraint complete_names_its_replacement check (completion <> 'complete' or replacement <> ''),
	constraint amount_names_its_rate check ((amount is null) = (rate is null)),
	constraint instance_hours_not_negative check (
		release_instance_hours >= 0 and control_instance_hours >= 0
		and kept_instance_hours >= 0 and instance_hours >= 0)
)`,

	`create table if not exists ` + MitigationTable + ` (
	` + record.Columns + `,
	operation text not null,
	address text not null,
	deploy_id text not null,
	began_at text not null,
	ended_at text not null default '',
	ended_actor_kind text not null default '',
	ended_actor_key text not null default '',
	ended_actor_key_basis text not null default '',
	` + record.Constraints + `,
	constraint operation_known check (operation in ('shift_traffic', 'set_instance_count')),
	constraint address_present check (address <> ''),
	constraint deploy_id_present check (deploy_id <> ''),
	constraint began_at_is_time_layout check (began_at ~ '` + record.TimePattern + `'),
	constraint ended_at_is_time_layout check (ended_at = '' or ended_at ~ '` + record.TimePattern + `'),
	constraint ended_by_a_human check (ended_actor_kind in ('', 'human')),
	constraint ended_names_who_ended_it check (
		(ended_at = '') = (ended_actor_key = '')
		and (ended_actor_key = '') = (ended_actor_kind = '')
		and (ended_actor_key = '') = (ended_actor_key_basis = '')),
	constraint ended_actor_key_basis_known check (ended_actor_key_basis in ('', 'claimed', 'verified'))
)`,
}
