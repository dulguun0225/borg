package deploy

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
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

// MarkSnapshotDeleted writes the deletion beside the snapshot's name, so the
// record says the copy is gone. It is the record's half of [DeleteSnapshot],
// which is what deletes the copy itself through the seam, and it is written
// after that call: a record saying a copy is gone while the copy is still
// readable is the one order an erasure obligation cannot take.
func (w *Writer) MarkSnapshotDeleted(ctx context.Context, id string) error {
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

// Deleting is one snapshot the deployer deletes: the record that names it, the
// service whose store the copy is of, and the target the copy is reached
// through. The caller supplies the target and the service's name for the reason
// every other call at this seam takes them — a deploy record names a service by
// id, and the store is reached with the environment's credential.
type Deleting struct {
	Principal principal.Principal
	// DeployID is the record naming the copy, which is where the deletion is
	// written once the copy is gone.
	DeployID    string
	ServiceName string
	// Target is where the copy is reached: the store is one per service per
	// environment, so this is the target the snapshot was taken through, every
	// target of the environment reaching the same store.
	Target     targetseam.Target
	Credential secretref.Ref
}

// DeleteSnapshot deletes the copy the record names and writes the deletion on
// that record. The deployer performs it at the end of the service's snapshot
// retention on its own pass, which is [DeleteExpiredSnapshots], and earlier when
// an owner calls for it from Ops on the deploy record that names it — the call
// the deployer already takes for a mitigation.
//
// The copy goes first and the record second, so a stop between them leaves a
// copy already gone on a record that still names it, which the next pass deletes
// again for nothing. The other order would leave a record saying a copy is gone
// while it is still readable, which is what an owner's erasure obligation
// reaches this copy for.
//
// A record naming no copy, or one whose deletion is already written, is
// [ErrNoSnapshot] and nothing is reached.
func DeleteSnapshot(ctx context.Context, w *Writer, d Deleting) error {
	record, err := Get(ctx, w.Pool(), d.DeployID)
	if err != nil {
		return err
	}
	if record.Snapshot.Name == "" || record.Snapshot.DeletedAt != "" {
		return fmt.Errorf("%w: %s names %q, deleted %q",
			ErrNoSnapshot, d.DeployID, record.Snapshot.Name, record.Snapshot.DeletedAt)
	}
	err = d.Target.DeleteSnapshot(ctx, d.Principal, targetseam.SnapshotRequest{
		Service: d.ServiceName, Name: record.Snapshot.Name, Credential: d.Credential,
	})
	if err != nil {
		return fmt.Errorf("deploy: deleting the snapshot %s of %s: %w",
			record.Snapshot.Name, d.DeployID, err)
	}
	return w.MarkSnapshotDeleted(ctx, d.DeployID)
}

// Pass is the deployer's own pass over the snapshots one service's deploys
// took: how long the service record says a copy is kept, and what the copies
// are reached through.
type Pass struct {
	Principal   principal.Principal
	ServiceID   string
	ServiceName string
	// Retention is how long a copy is kept, which the service record authors. A
	// pass with none deletes nothing: a retention nobody authored is not a
	// retention of no time at all.
	Retention  time.Duration
	Target     targetseam.Target
	Credential secretref.Ref
}

// DeleteExpiredSnapshots is that pass: every record of the service naming a copy
// taken longer ago than the retention, and not deleted yet, has its copy deleted
// and the deletion written. It answers with the records it deleted a copy for.
//
// The span is measured from the record's own timestamp, which is when the deploy
// started and when the copy was taken — the copy is taken before the change, in
// the same step, and no field dates it apart from the deploy.
func DeleteExpiredSnapshots(ctx context.Context, w *Writer, p Pass) ([]string, error) {
	if p.Retention <= 0 || p.ServiceID == "" || p.Target == nil {
		return nil, nil
	}
	expired, err := ExpiredSnapshots(ctx, w.Pool(), p.ServiceID, p.Retention)
	if err != nil {
		return nil, err
	}
	var deleted []string
	for _, one := range expired {
		err := DeleteSnapshot(ctx, w, Deleting{
			Principal: p.Principal, DeployID: one.ID, ServiceName: p.ServiceName,
			Target: p.Target, Credential: p.Credential,
		})
		if err != nil {
			return deleted, err
		}
		deleted = append(deleted, one.ID)
	}
	return deleted, nil
}

// ExpiredSnapshots is every deploy record of one service naming a copy that has
// outlived the retention and has not been deleted, oldest first. It takes the
// pool and not a [Writer], because reading which copies are owed a deletion is
// not a reason to be handed the thing that deletes them.
func ExpiredSnapshots(ctx context.Context, pool *pgxpool.Pool, serviceID string,
	retention time.Duration) ([]Deploy, error) {
	if serviceID == "" || retention <= 0 {
		return nil, nil
	}
	taken := record.FormatTime(time.Now().Add(-retention))
	return query(ctx, pool, "the snapshots of "+serviceID+" past their retention", selectDeploy+`
		where service_id = $1 and snapshot_name <> '' and snapshot_deleted_at = '' and at <= $2
		order by at, id`, serviceID, taken)
}
