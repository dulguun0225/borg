package mergequeue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/release"
)

// The number the mint seats a release at is one above the higher of two
// readings: the highest number among the service's release records, and the
// highest number among the releases of that service the health monitor's store
// names. That store is outside the recovery unit and holds what production
// emitted while the records were behind, so it is what says how high the numbers
// a restore lost went. One above the records alone would reuse them.
//
// The design takes the second reading at the first mint after a restore, which
// the install event says there was. Nothing writes an install event yet, so the
// queue takes the higher of the two readings at every mint instead: where no
// restore happened the two readings agree or the store's is the lower, and the
// number is the one the records alone would have given. What it costs is one
// read of a store outside the records per mint.

// SkippedNumbersKind is what the payload of the row a mint writes when it passes
// over numbers says it is, so a reader can tell it from every other payload
// sharing the shape it is written under.
const SkippedNumbersKind = "merge_queue_skipped_numbers"

// skippedFormatVersion is the format version that row is appended with. It names
// decisionlog.ShapeInstallEvent through decisionlog.Formats.
//
// The log holds ten shapes and the design names no eleventh for a skipped-number
// row: it puts the gap beside the install event — the row that says there was a
// restore — rather than as a puzzle on Ops. So the queue writes it under that
// shape with itself as actor and the payload's kind saying what it is, which is
// the arrangement [RejectionKind] already has inside its own shape, and
// decisionlog.ShapeInstallEvent says the same of the two rows it carries.
const skippedFormatVersion = "install_event/1"

// SkippedNumbersPayload is what that row says: the two readings the mint was
// taken from, the number it seated at, and every number between.
type SkippedNumbersPayload struct {
	Kind      string `json:"kind"`
	ServiceID string `json:"service_id"`
	ReleaseID string `json:"release_id"`
	Number    int64  `json:"number"`
	// InRecords is the highest number among the service's release records before
	// this mint, and Seen is the highest the health monitor's store named.
	InRecords int64   `json:"highest_in_records"`
	Seen      int64   `json:"highest_seen"`
	Skipped   []int64 `json:"skipped"`
}

// recordSkipped writes the numbers the mint passed over, and writes nothing
// where it passed over none — which is every mint on a service whose records the
// health monitor's store does not run ahead of.
func (q *Queue) recordSkipped(ctx context.Context, serviceID string, inRecords, seen int64,
	minted release.Release) ([]int64, error) {
	if minted.Number <= inRecords+1 {
		return nil, nil
	}
	skipped := make([]int64, 0, minted.Number-inRecords-1)
	for n := inRecords + 1; n < minted.Number; n++ {
		skipped = append(skipped, n)
	}
	payload, err := json.Marshal(SkippedNumbersPayload{
		Kind:      SkippedNumbersKind,
		ServiceID: serviceID,
		ReleaseID: minted.ID,
		Number:    minted.Number,
		InRecords: inRecords,
		Seen:      seen,
		Skipped:   skipped,
	})
	if err != nil {
		return nil, fmt.Errorf("mergequeue: marshalling the numbers skipped on %s: %w", serviceID, err)
	}
	if _, err := q.log.AppendInstallEvent(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: skippedFormatVersion,
	}); err != nil {
		return nil, err
	}
	return skipped, nil
}

// mint writes the release record and, in the same transaction, the contract
// versions that release publishes. A contract changes only inside its service's
// items and every write to it happens at a release, so the fast-forward is the
// event for both — and one merge must not be able to leave a number with no
// version, or a version under a number nothing minted.
//
// itemID is empty on a mint over a commit a human accepted, which names a build
// and no item.
//
// The record is keyed on the commit, so minting a commit again writes nothing and
// answers with the release already written: the fast-forward and this write are
// one operation restartable from either side.
func (q *Queue) mint(ctx context.Context, serviceID, itemID string, verified Verified) (Outcome, error) {
	seen, err := q.numbers.HighestSeen(ctx, serviceID)
	if err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: reading the highest number seen of %s: %w", serviceID, err)
	}
	highest, found, err := release.Highest(ctx, q.pool, serviceID)
	if err != nil {
		return Outcome{}, err
	}
	var inRecords int64
	if found {
		inRecords = highest.Number
	}

	var published []contract.Published
	minted, err := q.releases.MintWith(ctx, Actor, release.Minting{
		ServiceID: serviceID,
		BuildID:   verified.BuildID,
		Commit:    verified.Commit,
		ItemID:    itemID,
		Floor:     seen,
	}, func(ctx context.Context, tx pgx.Tx, r release.Release) error {
		var err error
		published, err = contract.PublishAll(ctx, tx, Actor,
			serviceID, r.ID, r.Number, itemID, verified.Forms)
		return err
	})
	if err != nil {
		return Outcome{}, err
	}

	skipped, err := q.recordSkipped(ctx, serviceID, inRecords, seen, minted)
	if err != nil {
		return Outcome{}, err
	}
	return Outcome{
		ItemID:         itemID,
		Merged:         true,
		Release:        minted,
		BuildID:        verified.BuildID,
		Commit:         verified.Commit,
		Published:      published,
		SkippedNumbers: skipped,
	}, nil
}
