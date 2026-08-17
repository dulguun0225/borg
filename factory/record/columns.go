package record

// Columns is the column list every record table begins with: the identifier
// [NewID] returns, the two actor fields, and the time the writer wrote the row
// in [TimeLayout]. A table composes it as the first thing inside its CREATE
// TABLE and follows it with a comma and its own columns.
//
// The timestamp is text and not timestamptz because a record whose hash covers
// it has to read back byte for byte, and a column type that parses and
// reformats what it stores cannot promise that.
const Columns = `id text not null primary key,
	actor_kind text not null,
	actor_name text not null,
	at text not null`

// TimePattern is [TimeLayout] written as a regular expression, in the subset
// PostgreSQL and Go's regexp both read the same way. [Constraints] is what
// puts it in the store, and TestConstraintsMatchTheTimeLayout is what keeps
// the two spellings of the same format from drifting apart.
const TimePattern = `^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{9}Z$`

// Constraints is what the store refuses regardless of which writer inserted
// the row: an actor kind outside [Kinds], an empty actor name, and a
// timestamp that is not [TimeLayout]. A table composes it among its own table
// constraints.
//
// The timestamp is checked for its shape and not merely for being there,
// because one writer following the layout is not what makes the format hold.
// The next package to compose [Columns] gets its own writer, and a column
// documented as one format and enforced as "not empty" is a column that ends
// up holding two — which the decision log's chain would hash and verify
// without complaint, the bytes being whatever was stored. What it costs:
// changing [TimeLayout] is now a change to every record table's schema, and
// to the rows already in them.
//
// PostgreSQL requires a constraint name to be unique within its table and not
// within the schema, so every record table names these three the same and a
// violation reads the same wherever it comes from.
const Constraints = `constraint actor_kind_known check (actor_kind in ('human', 'component')),
	constraint actor_name_present check (actor_name <> ''),
	constraint at_is_time_layout check (at ~ '` + TimePattern + `')`
