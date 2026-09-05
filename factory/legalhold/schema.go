package legalhold

import "github.com/dulguun0225/borg/factory/record"

// Table is the legal hold record's table.
const Table = "legal_hold"

// WithdrawalTable holds a legal hold's withdrawal: a second record naming the
// hold it ends, written pending and marked approved by a second write — a
// gate row of its own, taking [_A safeguard's withdrawal_]'s shape, when one
// exists. The hold stands until an approved withdrawal names it.
const WithdrawalTable = "legal_hold_withdrawal"

// IDPrefix is what [record.NewID] is called with for a legal hold.
const IDPrefix = "lgh"

// WithdrawalIDPrefix is what [record.NewID] is called with for a withdrawal.
const WithdrawalIDPrefix = "lghw"

// FormatVersion is written into every legal hold record's format_version
// column.
const FormatVersion = "legal_hold/1"

// FormatVersionWithdrawal is written into every withdrawal record's
// format_version column.
const FormatVersionWithdrawal = "legal_hold_withdrawal/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated.
//
// subject_id is empty for [SubjectFactory], the whole install having nothing
// to name, and required for the other two kinds; the CHECK enforces that
// pairing so a subject cannot be stored half-formed.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	subject_kind text not null,
	subject_id text not null,
	reason text not null,
	` + record.Constraints + `,
	constraint subject_kind_known check (subject_kind in ('service', 'project', 'factory')),
	constraint subject_id_matches_kind check (
		(subject_kind = 'factory' and subject_id = '')
		or (subject_kind <> 'factory' and subject_id <> '')),
	constraint reason_present check (reason <> '')
)`,

	`create index if not exists legal_hold_by_subject on ` + Table + ` (subject_kind, subject_id)`,

	`create table if not exists ` + WithdrawalTable + ` (
	` + record.Columns + `,
	legal_hold_id text not null,
	approved boolean not null default false,
	approved_at text,
	` + record.Constraints + `,
	constraint legal_hold_id_present check (legal_hold_id <> ''),
	constraint approved_at_matches_approval check (
		(approved and approved_at is not null) or (not approved and approved_at is null)),
	constraint approved_at_is_time_layout check (approved_at is null or approved_at ~ '` + record.TimePattern + `')
)`,

	`create index if not exists legal_hold_withdrawal_by_hold on ` + WithdrawalTable + ` (legal_hold_id)`,
}
