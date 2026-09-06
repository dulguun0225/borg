package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

// The factory's own store keeps a schema history: one row per change, naming
// the version that shipped it, the change's identity, a checksum of its text,
// and whether the change widened the store or removed something from it. It is
// the store's account of itself and not a record of the graph, the way the
// lease row is, so it composes neither record.Columns nor record.Constraints.

// HistoryTable is the table the schema history is kept in.
const HistoryTable = "schema_history"

// Version is the version of the factory this source ships as, and the order the
// history is read in. It rises by one for a version that changes this store,
// which is what makes "a row from a version further ahead" and "the version
// after it" readings of a number rather than of a release name.
const Version = 1

// Effect is what a change did to the store. There are two, and the forward
// promise turns on which: a version starts against a store the version after it
// widened, and refuses to start against one that had something removed it does
// not declare.
type Effect string

const (
	// EffectWidening adds a form beside what is already there and removes
	// nothing, which is the shape a change to this store takes in the ordinary
	// case: the version that ships it writes both forms, and the removal ships
	// no earlier than the version after.
	EffectWidening Effect = "widening"
	// EffectRemoval drops a form the version before it read. It is the second
	// half of the ordinary sequence, and a one-way step where no widening can
	// carry the change.
	EffectRemoval Effect = "removal"
)

// Effects is every effect a change may have. The CHECK in [HistoryDDL] lists
// the same two.
var Effects = []Effect{EffectWidening, EffectRemoval}

var (
	// ErrChangeIncomplete is returned by [Start] for a declared change missing
	// its identity, its text, or an effect that is one of [Effects].
	ErrChangeIncomplete = errors.New("postgres: a declared change names an identity, its text, and its effect")
	// ErrRemovalWithoutASnapshot is returned by [Start] for a declared removal
	// naming no snapshot. Before a removal is applied a snapshot of this store
	// is taken and verified, and one that cannot be taken is an upgrade not
	// performed.
	ErrRemovalWithoutASnapshot = errors.New("postgres: a removal names the snapshot taken and verified before it")
	// ErrRemovalNotDeclared is returned by [Start] where the history holds a
	// removal this version does not declare: the store has had something taken
	// out of it that this version still reads.
	ErrRemovalNotDeclared = errors.New("postgres: the history holds a removal this version does not declare")
	// ErrVersionAhead is returned by [Start] where the history holds a change
	// from a version further ahead than the one after this, or one from the
	// version after that did not widen the store. A version reads what the
	// version before it wrote and the version before reads what it wrote; a
	// skipped version is not a supported upgrade.
	ErrVersionAhead = errors.New("postgres: the history holds a change from a version this one cannot start against")
	// ErrHistoryDisagrees is returned by [Start] where the history holds a
	// change this version declares under a different checksum, which is a
	// change the history cannot honour.
	ErrHistoryDisagrees = errors.New("postgres: the history holds this change under a different checksum")
)

// Change is one change to the factory's own store, as a version declares it and
// as the history records it.
type Change struct {
	// Version is the version that shipped the change.
	Version int
	// ID is the change's identity, which is what the history is keyed on: a
	// change is recorded once however often the version starts.
	ID string
	// Text is the change's own text, which the checksum is taken over. It is
	// not applied from here — [Apply] is what applies the schema — so a change
	// declares the text it stands for and the history records what that text
	// was.
	Text string
	// Effect is whether the change widened the store or removed something from
	// it.
	Effect Effect
	// Checksum is the checksum of the change's text as the history holds it. It
	// is set on a change read back and empty on a declared one, whose checksum
	// is [checksumOf] over its text.
	Checksum string
	// Snapshot names the snapshot of this store taken and verified before a
	// removal is applied, and is empty on a widening. The step that takes one
	// is not built, so a version declaring a removal has nothing to name yet
	// and [Start] refuses it.
	Snapshot string
	// AppliedAt is when the history recorded the change, in [record.TimeLayout],
	// and is empty on a declared change that has not been recorded.
	AppliedAt string
}

// checksumOf is a change's checksum: the sha256 of its text in hexadecimal,
// which is what the history holds and what a later start compares against.
func checksumOf(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

// Changes is every change to this store the version this source ships as
// declares. Version 1 is the store's first form, so it declares one change: the
// schema [Apply] creates, as a widening over an empty store.
//
// A version that changes this store adds a line here with its own version
// number, and the removal half of the sequence ships no earlier than the
// version after the widening that carried it.
var Changes = []Change{
	{
		Version: 1,
		ID:      "the factory's first schema",
		Text:    "every table the packages Apply names declare, as version 1 ships them",
		Effect:  EffectWidening,
	},
}

// HistoryDDL is the schema history's own table, applied before the history is
// read. It carries no actor and no format version: ../../end-goal/records.md
// lists no row for it, the history being the store's account of itself.
var HistoryDDL = []string{
	`create table if not exists ` + HistoryTable + ` (
	id text primary key,
	version integer not null,
	checksum text not null,
	effect text not null check (effect in ('widening', 'removal')),
	snapshot text not null,
	applied_at text not null
)`,
}

// Start is this version's first start against the factory's own store: it
// applies the history's own table, reads the history, refuses to start where the
// history and this version disagree, applies the schema through [Apply], and
// records every change this version declares that the history does not already
// hold. It answers with the changes it recorded, which the install event names.
//
// It refuses in the four cases the forward promise states: a removal the history
// holds that this version does not declare, a change from a version further
// ahead than the one after this or one from the version after that did not
// widen the store, a change this version declares that the history holds under
// another checksum, and a removal declared with no snapshot named.
//
// Which caller is not built: the install's first-start step, which is what calls
// this and then writes the install event naming what it applied and, at a
// removal, the snapshot standing for the version before. The command-line
// interface calls [Apply] today and takes no reading of the history.
func Start(ctx context.Context, pool *pgxpool.Pool) ([]Change, error) {
	for n, statement := range HistoryDDL {
		if _, err := pool.Exec(ctx, statement); err != nil {
			return nil, fmt.Errorf("postgres: applying the schema history statement %d: %w", n+1, err)
		}
	}
	held, err := History(ctx, pool)
	if err != nil {
		return nil, err
	}
	if err := checkDeclared(Changes); err != nil {
		return nil, err
	}
	if err := checkHistory(held, Changes); err != nil {
		return nil, err
	}
	if err := Apply(ctx, pool); err != nil {
		return nil, err
	}
	return recordChanges(ctx, pool, held, Changes)
}

// History is every change the store's history holds, in the order it was
// applied. It is read at a start and by whatever reports what this store is at.
func History(ctx context.Context, pool *pgxpool.Pool) ([]Change, error) {
	rows, err := pool.Query(ctx, `select id, version, checksum, effect, snapshot, applied_at
		from `+HistoryTable+` order by applied_at, id`)
	if err != nil {
		return nil, fmt.Errorf("postgres: reading the schema history: %w", err)
	}
	defer rows.Close()

	var held []Change
	for rows.Next() {
		var c Change
		var effect string
		if err := rows.Scan(&c.ID, &c.Version, &c.Checksum, &effect, &c.Snapshot, &c.AppliedAt); err != nil {
			return nil, fmt.Errorf("postgres: reading a change from the schema history: %w", err)
		}
		c.Effect = Effect(effect)
		held = append(held, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("postgres: reading the schema history: %w", err)
	}
	return held, nil
}

// checkDeclared refuses a change this version declares incompletely, and the one
// declared removal nothing can honour yet: the step that takes and verifies a
// snapshot before a removal is not built, so a removal has no snapshot to name.
func checkDeclared(declared []Change) error {
	for _, c := range declared {
		if c.ID == "" || c.Text == "" || !slices.Contains(Effects, c.Effect) {
			return fmt.Errorf("%w: %+v", ErrChangeIncomplete, c)
		}
		if c.Effect == EffectRemoval && c.Snapshot == "" {
			return fmt.Errorf("%w: %s", ErrRemovalWithoutASnapshot, c.ID)
		}
	}
	return nil
}

// checkHistory is the reading a version's first start makes: it starts against a
// history that holds, beyond the changes it declares, only rows from the version
// after it that widened the store.
func checkHistory(held, declared []Change) error {
	for _, row := range held {
		mine := declaredAs(declared, row.ID)
		if mine.ID != "" {
			if row.Checksum != checksumOf(mine.Text) {
				return fmt.Errorf("%w: %s, history %s, this version %s",
					ErrHistoryDisagrees, row.ID, row.Checksum, checksumOf(mine.Text))
			}
			continue
		}
		if row.Effect == EffectRemoval {
			return fmt.Errorf("%w: %s, shipped by version %d", ErrRemovalNotDeclared, row.ID, row.Version)
		}
		if row.Version != Version+1 {
			return fmt.Errorf("%w: %s, shipped by version %d, this version %d",
				ErrVersionAhead, row.ID, row.Version, Version)
		}
	}
	return nil
}

// declaredAs is the change this version declares under that identity, and the
// zero change where it declares none.
func declaredAs(declared []Change, id string) Change {
	for _, c := range declared {
		if c.ID == id {
			return c
		}
	}
	return Change{}
}

// recordChanges writes every declared change the history does not already hold,
// and answers with those rows: a change already recorded is skipped, so a
// version starting twice records nothing the second time.
func recordChanges(ctx context.Context, pool *pgxpool.Pool, held, declared []Change) ([]Change, error) {
	var applied []Change
	for _, c := range declared {
		if declaredAs(held, c.ID).ID != "" {
			continue
		}
		c.Checksum, c.AppliedAt = checksumOf(c.Text), record.Now()
		if _, err := pool.Exec(ctx, `insert into `+HistoryTable+`
			(id, version, checksum, effect, snapshot, applied_at) values ($1, $2, $3, $4, $5, $6)
			on conflict (id) do nothing`,
			c.ID, c.Version, c.Checksum, string(c.Effect), c.Snapshot, c.AppliedAt); err != nil {
			return nil, fmt.Errorf("postgres: recording the change %s: %w", c.ID, err)
		}
		applied = append(applied, c)
	}
	return applied, nil
}
