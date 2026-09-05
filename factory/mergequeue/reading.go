package mergequeue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/dulguun0225/borg/factory/build"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
)

// A candidate that fails its own re-verification — against the master that
// actually resulted, not against a speculation ahead of it — is rejected by the
// queue: the item goes back up to Implementation, the queue naming no other stage
// and there being none between, and counts an attempt there. That it failed on
// its merits is established rather than assumed, because a re-verification is
// never a repeat of the run that passed: the build is new and the environment
// recomposed. So a criterion that passed on the candidate environment and fails
// here is run once more, over the re-verification's own build and composition —
// once, and never until green — and the rejection takes one of three readings.

// Reading is which of the three readings of a failure the queue took. The words
// are the design's own.
type Reading string

const (
	// ReadingAnEncodingCouldNotHoldItsAnswer is the confirming run disagreeing
	// with the re-verification: the disagreement over one build that makes a
	// criterion undecided. The way back is authoring the criterion's encoding
	// again, which is the check Implementation authors beside the code.
	ReadingAnEncodingCouldNotHoldItsAnswer Reading = "an encoding that could not hold its answer"
	// ReadingAgainstTheMasterItMerges is the failure repeating with the two
	// compositions matching: the candidate failed against the master it will
	// actually merge into.
	ReadingAgainstTheMasterItMerges Reading = "a candidate that no longer passes against what it will actually ship beside"
	// ReadingADependencysReleaseMoved is the failure repeating with the two
	// compositions differing: what changed is a release the author's work never
	// saw. A design system record replaced between the two builds takes this
	// reading too, whatever else failed beside the move.
	ReadingADependencysReleaseMoved Reading = "a dependency's release moved between the two runs"
)

// Readings is every reading a rejection may name.
var Readings = []Reading{
	ReadingAnEncodingCouldNotHoldItsAnswer, ReadingAgainstTheMasterItMerges,
	ReadingADependencysReleaseMoved,
}

// What one entry of [Moved] says moved. A dependency at another release, a seed
// replaced, and a value set replaced are one reading: a composition that
// differs.
const (
	MovedRelease  = "a dependency's release"
	MovedSeed     = "the version of the store's seed"
	MovedValueSet = "the version of the non-production value set"
)

// Moved is one thing the composition the run that passed was performed against
// and the composition the re-verification ran against disagree about. From and To
// are empty where the thing was absent from that composition altogether.
type Moved struct {
	What string `json:"what"`
	// ServiceID is the dependency whose release moved, and is empty on a seed or
	// a value set replaced.
	ServiceID string `json:"service_id,omitempty"`
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
}

// Rejection is what the queue read a failure as, and what it wrote into the log.
//
// The attempt counts on every reading, so an item a moving dependency keeps
// rejecting still walks toward an escalation: work the environment will not let
// finish is work a human should see, and the limit bounds spend, which the rework
// is. On the third reading the attempt and the prior part ways — counted like a
// reject, taught like a hold — because the two record different things: the limit
// what was spent, the prior who authored what failed, and there what failed is
// not what was authored.
type Rejection struct {
	Reading Reading
	// Criteria is what failed, and is empty where the candidate failed for a
	// reason no criterion decided: a merge conflict, a breaking contract diff, a
	// consumer contract the candidate does not satisfy, a design system record
	// replaced, or a resolved set that moved.
	Criteria []string
	// Moved is what the two compositions disagree about, on the third reading and
	// empty on the other two.
	Moved []Moved
	// ReplacedDesignSystemRecord is the record the candidate's build was made
	// against where a design system move rejected it, and is empty otherwise.
	ReplacedDesignSystemRecord string
	// LearnsAs is what the score learns from this rejection: as from a reject, or
	// as from a hold.
	LearnsAs gate.Verdict
	// PriorMoves is whether the per-author prior moves. It does not on the third
	// reading: a prior kept per model version and factory-wide would carry one
	// moved dependency's noise onto every item that model authors.
	PriorMoves bool
	// ReturnsTo is the stage the item goes back to, which is Implementation and
	// nothing else: there is no stage of the queue's own and none between.
	ReturnsTo gate.ReturnsTo
	// CountsAnAttempt is whether the attempt is counted at that stage.
	CountsAnAttempt bool
	// Why is what failed, in words a human reads on the row.
	Why string
	// Row is the log row this rejection was written as.
	Row string
}

// RejectionKind is what a rejection row's payload says it is, so a reader can
// tell the queue's rejection rows from every other kind of payload sharing the
// log's queue_rejection shape.
const RejectionKind = "merge_queue_rejection"

// rejectionFormatVersion is the format version every rejection is appended with.
// It names decisionlog.ShapeQueueRejection through decisionlog.Formats.
const rejectionFormatVersion = "queue_rejection/1"

// RejectionPayload is what the queue writes into the log when a candidate fails
// its own re-verification. It is its own shape and neither a wait nor a decision:
// no gate fired — the Merge to master gate's own having closed as an approval —
// so there is no firing to open a row at and no factor vector computed for it.
//
// It says which reading it was and what moved, which is what makes the one
// rejection Factory's gate rejection rate cannot see legible on the item's
// timeline in Work. It says that an attempt was counted, which is the one thing
// about this row a reader cannot see from the row: the count is on the item's
// per-stage row, at the stage the item is sent back to.
type RejectionPayload struct {
	Kind      string `json:"kind"`
	ItemID    string `json:"item_id,omitempty"`
	ServiceID string `json:"service_id"`
	BuildID   string `json:"build_id,omitempty"`
	Commit    string `json:"commit,omitempty"`
	Why       string `json:"why"`
	// Reading is empty on the rejection of a commit a human accepted, which is a
	// commit that failed its re-verification with no item to send back and no
	// criterion history to read it against.
	Reading                    Reading  `json:"reading,omitempty"`
	Criteria                   []string `json:"criteria,omitempty"`
	Moved                      []Moved  `json:"moved,omitempty"`
	ReplacedDesignSystemRecord string   `json:"replaced_design_system_record,omitempty"`
	LearnsAs                   string   `json:"learns_as,omitempty"`
	PriorMoves                 bool     `json:"prior_moves"`
	ReturnsTo                  string   `json:"returns_to,omitempty"`
	CountsAnAttempt            bool     `json:"counts_an_attempt"`
}

// reject is the rejection of a candidate whose re-verification failed on its own
// merits, and the confirming run that says which reading it is.
func (q *Queue) reject(ctx context.Context, c *candidate) (Outcome, error) {
	r := Rejection{
		Why:             c.verified.Why,
		Criteria:        c.verified.FailedCriteria,
		ReturnsTo:       gate.ReturnsToImplementation,
		CountsAnAttempt: true,
	}
	if len(c.verified.FailedCriteria) == 0 {
		// Nothing a criterion decided failed, so there is nothing to run once
		// more: the candidate failed against the master it will actually merge
		// into, and the score learns as from a reject.
		r.Reading, r.LearnsAs, r.PriorMoves = ReadingAgainstTheMasterItMerges, gate.VerdictReject, true
		return q.writeRejection(ctx, c, r)
	}

	confirmation, err := q.repo.Confirm(ctx, c.it, c.verified)
	if err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: the confirming run over %s: %w", c.it.ID, err)
	}
	if len(confirmation.Repeated) == 0 {
		// A repeat that disagrees is the disagreement over one build that makes a
		// criterion undecided. The score learns from it the way it learns from a
		// reject, an encoding that cannot hold its answer being a defect of the
		// item's own build.
		r.Reading, r.LearnsAs, r.PriorMoves = ReadingAnEncodingCouldNotHoldItsAnswer, gate.VerdictReject, true
		r.Criteria = confirmation.Disagreed
		return q.writeRejection(ctx, c, r)
	}

	// A failure that repeats is real, and what the score learns turns on the
	// criterion and the two compositions.
	r.Criteria = confirmation.Repeated
	if confirmation.Why != "" {
		r.Why = confirmation.Why
	}
	if moved := movedBetween(c.verified.ApprovedComposition, c.verified.Composition); len(moved) > 0 {
		r.Reading, r.LearnsAs, r.PriorMoves = ReadingADependencysReleaseMoved, gate.VerdictHold, false
		r.Moved = moved
		return q.writeRejection(ctx, c, r)
	}
	r.Reading, r.LearnsAs, r.PriorMoves = ReadingAgainstTheMasterItMerges, gate.VerdictReject, true
	return q.writeRejection(ctx, c, r)
}

// rejectDesignSystemMove is the rejection a design system move causes: the two
// builds name two design system constraint records and the records differ on a
// component or a token the candidate's build uses, so the candidate fails
// whatever its criteria answered. An item drafted before the move and merging
// after it would otherwise leave a departure outside the move's conformance
// items, which were enumerated once, when the move arrived.
//
// It takes the reading a moved dependency takes, whatever else failed beside the
// move — an owner's move being no defect of the author's — with the moved release
// named where one also moved.
func (q *Queue) rejectDesignSystemMove(ctx context.Context, c *candidate, replaced string) (Outcome, error) {
	why := "the design system constraint record was replaced between the approved build and this one"
	if c.verified.Why != "" {
		why = why + "; the re-verification also found: " + c.verified.Why
	}
	return q.writeRejection(ctx, c, Rejection{
		Reading:                    ReadingADependencysReleaseMoved,
		Moved:                      movedBetween(c.verified.ApprovedComposition, c.verified.Composition),
		ReplacedDesignSystemRecord: replaced,
		LearnsAs:                   gate.VerdictHold,
		PriorMoves:                 false,
		ReturnsTo:                  gate.ReturnsToImplementation,
		CountsAnAttempt:            true,
		Why:                        why,
	})
}

// rejectResolvedSet is the rejection a moved resolved set causes: the re-resolved
// set's digests are compared to the approved build's, and a difference rejects on
// the terms a candidate that fails its own merits already does. A version is not
// an identity for bytes, so what is compared is the digest.
func (q *Queue) rejectResolvedSet(ctx context.Context, c *candidate, differs string) (Outcome, error) {
	return q.writeRejection(ctx, c, Rejection{
		Reading:         ReadingAgainstTheMasterItMerges,
		LearnsAs:        gate.VerdictReject,
		PriorMoves:      true,
		ReturnsTo:       gate.ReturnsToImplementation,
		CountsAnAttempt: true,
		Why:             differs,
	})
}

// writeRejection appends the row and answers with the outcome that names it. The
// row is the only record of the rejection, and the item's own transition is the
// caller's write, so there is nothing here to leave half-written.
func (q *Queue) writeRejection(ctx context.Context, c *candidate, r Rejection) (Outcome, error) {
	payload, err := json.Marshal(RejectionPayload{
		Kind:                       RejectionKind,
		ItemID:                     c.it.ID,
		ServiceID:                  c.it.ServiceID,
		BuildID:                    c.verified.BuildID,
		Commit:                     c.verified.Commit,
		Why:                        r.Why,
		Reading:                    r.Reading,
		Criteria:                   r.Criteria,
		Moved:                      r.Moved,
		ReplacedDesignSystemRecord: r.ReplacedDesignSystemRecord,
		LearnsAs:                   string(r.LearnsAs),
		PriorMoves:                 r.PriorMoves,
		ReturnsTo:                  string(r.ReturnsTo),
		CountsAnAttempt:            r.CountsAnAttempt,
	})
	if err != nil {
		return Outcome{}, fmt.Errorf("mergequeue: marshalling the rejection of %s: %w", c.it.ID, err)
	}
	row, err := q.log.AppendQueueRejection(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: rejectionFormatVersion,
	})
	if err != nil {
		return Outcome{}, err
	}
	r.Row = row.ID
	return Outcome{
		ItemID:    c.it.ID,
		BuildID:   c.verified.BuildID,
		Commit:    c.verified.Commit,
		Why:       r.Why,
		Rejection: r,
	}, nil
}

// movedBetween is what two compositions disagree about: a dependency at another
// release, one absent from either, the store's seed replaced, and the value set
// replaced. All three are compared together, because a seed or a value set
// replaced between two runs is a composition that differs, read the way a moved
// release is.
func movedBetween(approved, reverified environment.Composition) []Moved {
	var moved []Moved
	after := make(map[string]string, len(reverified.From))
	for _, entry := range reverified.From {
		after[entry.ServiceID] = entry.ReleaseID
	}
	before := make(map[string]string, len(approved.From))
	for _, entry := range approved.From {
		before[entry.ServiceID] = entry.ReleaseID
	}
	for _, entry := range approved.From {
		if to, present := after[entry.ServiceID]; !present || to != entry.ReleaseID {
			moved = append(moved, Moved{
				What: MovedRelease, ServiceID: entry.ServiceID, From: entry.ReleaseID, To: to,
			})
		}
	}
	for _, entry := range reverified.From {
		if _, present := before[entry.ServiceID]; !present {
			moved = append(moved, Moved{What: MovedRelease, ServiceID: entry.ServiceID, To: entry.ReleaseID})
		}
	}
	if approved.SeedVersion != reverified.SeedVersion {
		moved = append(moved, Moved{What: MovedSeed, From: approved.SeedVersion, To: reverified.SeedVersion})
	}
	if approved.ValueSetVersion != reverified.ValueSetVersion {
		moved = append(moved, Moved{What: MovedValueSet,
			From: approved.ValueSetVersion, To: reverified.ValueSetVersion})
	}
	return moved
}

// designSystemMoved is the record the candidate's build was made against where a
// design system move rejects it, and is empty where nothing moved. The comparison
// is of two fields — the design system constraint record the two builds name — and
// a build naming no record compares as nothing. Whether the two records differ on
// a component or a token the candidate's build uses is the reading [DesignSystem]
// supplies.
func (q *Queue) designSystemMoved(ctx context.Context, c *candidate) (string, error) {
	if !c.approvedFound || c.verified.BuildID == "" || c.approved.ID == c.verified.BuildID {
		return "", nil
	}
	made, err := build.Get(ctx, q.pool, c.verified.BuildID)
	if err != nil {
		return "", err
	}
	before, after := c.approved.DesignSystemConstraintID, made.DesignSystemConstraintID
	if before == "" || after == "" || before == after {
		return "", nil
	}
	differs, err := q.designSystem.Differs(ctx, before, after, made.ID)
	if err != nil {
		return "", fmt.Errorf("mergequeue: comparing design system records %s and %s: %w", before, after, err)
	}
	if !differs {
		return "", nil
	}
	return before, nil
}

// resolvedSetDiffers is the difference between the re-resolved set's digests and
// the approved build's, in words a human reads on the rejection row, and is empty
// where the two sets resolved the same bytes. The comparison is keyed by the
// source and the package rather than by the package alone, because one name in
// two registries is two packages.
func (q *Queue) resolvedSetDiffers(ctx context.Context, c *candidate) (string, error) {
	if !c.approvedFound || c.verified.BuildID == "" || c.approved.ID == c.verified.BuildID {
		return "", nil
	}
	before, err := build.Resolved(ctx, q.pool, c.approved.ID)
	if err != nil {
		return "", err
	}
	after, err := build.Resolved(ctx, q.pool, c.verified.BuildID)
	if err != nil {
		return "", err
	}
	digests := make(map[string]string, len(after))
	for _, entry := range after {
		digests[entry.Source+" "+entry.Package] = entry.Digest
	}
	for _, entry := range before {
		key := entry.Source + " " + entry.Package
		digest, present := digests[key]
		if !present {
			return fmt.Sprintf("the approved build resolved %s from %s and this one resolved it from nowhere",
				entry.Package, entry.Source), nil
		}
		if digest != entry.Digest {
			return fmt.Sprintf("%s from %s resolved to digest %q and the approved build resolved digest %q",
				entry.Package, entry.Source, digest, entry.Digest), nil
		}
		delete(digests, key)
	}
	for _, entry := range after {
		if _, present := digests[entry.Source+" "+entry.Package]; present {
			return fmt.Sprintf("this build resolved %s from %s and the approved build resolved it from nowhere",
				entry.Package, entry.Source), nil
		}
	}
	return "", nil
}
