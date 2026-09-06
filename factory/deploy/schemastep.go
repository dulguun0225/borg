package deploy

import (
	"context"
	"fmt"
	"slices"

	"github.com/dulguun0225/borg/factory/targetseam"
)

// The step before any traffic moves: the service's store brought to what the
// build declares, through the first target's credential, and the snapshot taken
// before a change that destroys stored data. Which changes the store carries is
// read from the store's own schema history and never from a deploy record; the
// record reports what this step did.

// applyToTheStore is every step before traffic: the snapshot before a change
// that destroys stored data, and the changes the store's history lacks, applied
// in order through the environment's credential. The store is one per service
// per environment, so the changes are applied once — through the first target,
// every target of the environment holding the same credential and reaching the
// same store.
//
// A deploy that owes nothing, the history already holding every change the build
// declares, is a deploy whose changes completed: the store carries them. Marking
// it there is what tells that record from one whose change failed to apply, which
// is the record with a change named and nothing completed.
//
// The deploy of an adoption item's release applies nothing at all:
// [writeFoundApplied] is that path.
func applyToTheStore(ctx context.Context, w *Writer, p Performance, d Deploy) error {
	if len(p.SchemaChanges) == 0 || len(p.Reaches) == 0 {
		return nil
	}
	through := p.Reaches[0]
	if p.Adoption {
		return writeFoundApplied(ctx, w, p, d, through)
	}

	running, err := through.Target.ReadRunning(ctx, p.Principal, p.ServiceName, p.Credential)
	if err != nil {
		return fail(ctx, w, p, d, StepSchemaChange, fmt.Errorf("%w: reading the schema history: %w",
			ErrSchemaChangeRefused, err))
	}
	carried := make([]string, 0, len(running.SchemaHistory))
	for _, applied := range running.SchemaHistory {
		carried = append(carried, applied.Change)
	}

	var owed []targetseam.SchemaChange
	destructive := false
	for _, change := range p.SchemaChanges {
		if slices.Contains(carried, change.Change) {
			continue
		}
		owed = append(owed, change)
		destructive = destructive || change.Destroys
	}
	if len(owed) == 0 {
		return w.MarkSchemaChangesComplete(ctx, d.ID)
	}

	if destructive {
		if p.SnapshotName == "" {
			return fail(ctx, w, p, d, StepSnapshot, fmt.Errorf("%w: it names no copy", ErrSnapshotRefused))
		}
		taken, err := through.Target.Snapshot(ctx, p.Principal, targetseam.SnapshotRequest{
			Service: p.ServiceName, Name: p.SnapshotName, Credential: p.Credential,
		})
		if err != nil {
			return fail(ctx, w, p, d, StepSnapshot, fmt.Errorf("%w: %w", ErrSnapshotRefused, err))
		}
		if err := w.NameSnapshot(ctx, d.ID, taken.Name, taken.Digest); err != nil {
			return err
		}
	}

	for _, change := range owed {
		change.Release = p.What.ReleaseID
		if err := through.Target.ApplySchemaChange(ctx, p.Principal, change); err != nil {
			return fail(ctx, w, p, d, StepSchemaChange, fmt.Errorf("%w: %s: %w",
				ErrSchemaChangeRefused, change.Change, err))
		}
	}
	return w.MarkSchemaChangesComplete(ctx, d.ID)
}

// writeFoundApplied is the store step of an adoption item's release. An adopted service
// arrives with its store already at the schema its head declares and a history
// holding nothing, so applying the head's changes would run the whole of them
// against a live store. This writes one row per change the build declares,
// naming this release and marked as found applied rather than applied by the
// deployer, and applies none; the mark is a field of every row, so a later reader
// tells a change the factory applied from one it took on the adoption's word. The
// next release's deploy then applies exactly what its build declares that the
// history does not hold, as any deploy does.
//
// What it costs is that the factory has checked nothing about those changes and
// the rows claim what the adoption's shape claims. A store not at the head's
// schema holds rows for changes it does not hold, and what finds that is the
// release that fails against the missing form.
func writeFoundApplied(ctx context.Context, w *Writer, p Performance, d Deploy, through Reach) error {
	if p.What.ReleaseID == "" {
		return fail(ctx, w, p, d, StepSchemaChange,
			fmt.Errorf("%w: an adoption's changes are found applied at a numbered release", ErrSchemaChangeRefused))
	}
	for _, change := range p.SchemaChanges {
		change.Release = p.What.ReleaseID
		change.FoundApplied = true
		if err := through.Target.ApplySchemaChange(ctx, p.Principal, change); err != nil {
			return fail(ctx, w, p, d, StepSchemaChange, fmt.Errorf("%w: writing %s into the history as found applied: %w",
				ErrSchemaChangeRefused, change.Change, err))
		}
	}
	return w.MarkSchemaChangesComplete(ctx, d.ID)
}
