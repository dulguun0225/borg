package deploy

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/record"
)

// MarkSchemaChangesComplete records that the changes this deploy's build carries
// completed, which is what puts a schema change on the trail an incident's links
// walk. It runs on the deploy that applied them, on the deploy that applied none
// because the store's history already held every one, and on an adoption's
// deploy, which wrote them into the history as found applied — in all three the
// store carries what the build declares, and only a change that failed to apply
// leaves a record naming changes that did not complete.
func (w *Writer) MarkSchemaChangesComplete(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "completing the schema changes of "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+Table+` set schema_changes_completed = true
			where id = $1 and schema_changes <> ''`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s carries no schema change", ErrNotFound, id)
		}
		return nil
	})
}

// NameSnapshot writes the copy taken and verified before a change that destroys
// stored data, so the record says where what the change destroyed can still be
// read.
func (w *Writer) NameSnapshot(ctx context.Context, id, name, digest string) error {
	if name == "" || digest == "" {
		return fmt.Errorf("%w: %s names %q with digest %q", ErrNoSnapshot, id, name, digest)
	}
	return w.inTransaction(ctx, "naming the snapshot of "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+Table+` set snapshot_name = $1, snapshot_digest = $2 where id = $3`,
			name, digest, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s", ErrNotFound, id)
		}
		return nil
	})
}

// DeleteSnapshot writes the deletion beside the snapshot's name, which the
// deployer does at the end of the service's snapshot retention on its own pass
// or earlier when an owner calls for it from Ops. What the deletion is performed
// through is the service's own store hosting: this seam has no operation that
// deletes one, so the record says the copy is gone and the copy is removed
// outside the factory. doc.go says so.
func (w *Writer) DeleteSnapshot(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "deleting the snapshot of "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+Table+` set snapshot_deleted_at = $1
			where id = $2 and snapshot_name <> '' and snapshot_deleted_at = ''`, record.Now(), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s", ErrNoSnapshot, id)
		}
		return nil
	})
}
