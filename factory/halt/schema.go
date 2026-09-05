package halt

import "github.com/dulguun0225/borg/factory/record"

// Table is the halt record's table. Its subject is the factory, so there is
// no subject column: a row here reaches every service at once.
const Table = "halt"

// WithdrawalTable holds a halt's withdrawal: a second record naming the halt
// it ends, written pending and marked approved by a second write — the gate
// row [_A halt's withdrawal_] decides, when it exists. The halt stands until
// an approved withdrawal names it.
const WithdrawalTable = "halt_withdrawal"

// IDPrefix is what [record.NewID] is called with for a halt.
const IDPrefix = "hlt"

// WithdrawalIDPrefix is what [record.NewID] is called with for a withdrawal.
const WithdrawalIDPrefix = "hltw"

// FormatVersion is written into every halt record's format_version column.
const FormatVersion = "halt/1"

// FormatVersionWithdrawal is written into every withdrawal record's
// format_version column.
const FormatVersionWithdrawal = "halt_withdrawal/1"

// DDL is this package's schema, in the order the statements are applied.
// [record.Columns] and [record.Constraints] are composed rather than
// restated.
var DDL = []string{
	`create table if not exists ` + Table + ` (
	` + record.Columns + `,
	reason text not null,
	` + record.Constraints + `,
	constraint reason_present check (reason <> '')
)`,

	`create table if not exists ` + WithdrawalTable + ` (
	` + record.Columns + `,
	halt_id text not null,
	approved boolean not null default false,
	approved_at text,
	` + record.Constraints + `,
	constraint halt_id_present check (halt_id <> ''),
	constraint approved_at_matches_approval check (
		(approved and approved_at is not null) or (not approved and approved_at is null)),
	constraint approved_at_is_time_layout check (approved_at is null or approved_at ~ '` + record.TimePattern + `')
)`,

	`create index if not exists halt_withdrawal_by_halt on ` + WithdrawalTable + ` (halt_id)`,
}
