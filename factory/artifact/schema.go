package artifact

import "github.com/dulguun0225/borg/factory/record"

// Table is the one table this package owns.
const Table = "artifact"

// IDPrefix is what [record.NewID] is called with for a row of this table.
const IDPrefix = "art"

// FormatVersion is what this package writes into format_version on every
// insert into [Table].
const FormatVersion = "artifact/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated, so the actor field and its constraints are the same ones every
// record table carries.
//
// The unique constraint on (item_id, kind, role, subject, version) is what
// keeps the version chain a chain: two submissions that read the same prior
// version would write the same next one, and the store refuses the second
// rather than holding a lock. item_id, role and subject are id fields and not
// foreign keys, checked for being present where required and never for
// pointing at anything; record's doc.go states that rule and its cost once.
//
// One of item_id, role and subject names the chain, and which one depends on
// the kind: a spec, an implementation and a consumer contract belong to an
// item; a role prompt belongs to a role; a skill belongs to a subject — an
// area, a service, or a project; a selection rule belongs to the factory as a
// whole and names none of the three. chain_key_matches_kind is that rule as a
// CHECK, so a row naming the wrong discriminator for its kind is refused
// around the writer as well as by it.
//
// author is required on every version an authorship names, and that is what
// makes a per-author prior computable: the prior is kept per model version and
// per human and is computed from that author's own work, so a version naming
// the role that wrote it and not the author would leave the score with
// nothing to group outcomes by. It is not the actor: the actor is the
// component that wrote the record, and two agents in different roles on one
// model are one author under two actors.
//
// The one version an authorship does not name is the entry nobody wrote — the
// shipped version the factory enters at install or at an upgrade's first
// start, on a call that authors nothing. Both authorship and author are empty
// together on such a row, author_pair_together being the CHECK that admits
// exactly that pair and refuses any other partial one, and
// shipped_bundle_identity is present on it and on no other row, naming the
// release of the product that entered it.
//
// entered_by names which of the two events entered such a row, and is empty on
// every authored one. The two are not the same entry: the install's entries
// enter in force ungated, a factory with nothing decided in it having to run,
// and an upgrade's first start enters a version awaiting the gate every
// version fires. Version 1 of a chain is written by either event, and
// shipped_bundle_identity is present on both, so without this column the row
// does not say which one wrote it and [InForce] cannot tell the ungated case
// from the pending one.
//
// content_digest is the sha256 of content in hexadecimal, computed at the
// write and never supplied by the caller — [Store.Redact] is the one path
// that recomputes it after the write, over the redacted content.
//
// input_manifest_id names the input manifest the version was authored from,
// supplied by the caller that dispatched the run and empty where that caller
// wrote none; it is empty on every shipped version too, an entry authoring
// nothing having read no manifest.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	item_id text not null,
	role text not null,
	subject text not null,
	kind text not null,
	version int not null,
	supersedes text not null,
	authorship text not null,
	author text not null,
	content text not null,
	content_digest text not null,
	shipped_bundle_identity text not null,
	entered_by text not null,
	input_manifest_id text not null,
	` + record.Constraints + `,
	constraint kind_known check (kind in
		('spec', 'implementation_plan', 'tasks', 'implementation', 'consumer_contract',
		 'role_prompt', 'skill', 'selection_rule')),
	constraint version_starts_at_one check (version >= 1),
	constraint authorship_known check (authorship in ('', 'agent', 'human', 'gate')),
	constraint author_pair_together check ((authorship = '') = (author = '')),
	constraint content_digest_present check (content_digest <> ''),
	constraint chain_key_matches_kind check (
		(kind in ('spec', 'implementation_plan', 'tasks', 'implementation', 'consumer_contract')
			and item_id <> '' and role = '' and subject = '')
		or (kind = 'role_prompt' and item_id = '' and role <> '' and subject = '')
		or (kind = 'skill' and item_id = '' and role = '' and subject <> '')
		or (kind = 'selection_rule' and item_id = '' and role = '' and subject = '')
	),
	constraint shipped_bundle_identity_matches_authorship check
		((authorship = '') = (shipped_bundle_identity <> '')),
	constraint entered_by_known check (entered_by in ('', 'install', 'upgrade_first_start')),
	constraint entered_by_matches_authorship check ((authorship = '') = (entered_by <> '')),
	constraint input_manifest_only_when_authored check (authorship <> '' or input_manifest_id = ''),
	constraint one_row_per_version unique (item_id, kind, role, subject, version)
)`,
}
