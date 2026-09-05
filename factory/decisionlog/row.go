package decisionlog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strconv"

	"github.com/dulguun0225/borg/factory/record"
)

// Shape is what a row is. There are ten, and the log holds no other.
type Shape string

const (
	// ShapeDecision is something the factory or a human decided. It is the
	// only shape that names a policy version and a score version, on its
	// opening, and the only shape with more than two possible parts.
	ShapeDecision Shape = "decision"
	// ShapePageEvent is a page that was delivered, which is a delivery and
	// not a decision.
	ShapePageEvent Shape = "page_event"
	// ShapeWait is something the factory could not compute when a gate fired
	// — an unreachable credential among them — which is a wait and not a
	// decision. It is the only other shape with more than one part.
	ShapeWait Shape = "wait"
	// ShapeReworkRequest is the row written when an author sends its item
	// back with no gate fired.
	ShapeReworkRequest Shape = "rework_request"
	// ShapeQueueRejection is the merge queue's rejection of a candidate that
	// failed its own re-verification.
	ShapeQueueRejection Shape = "queue_rejection"
	// ShapeTruncation is appended when the log enforces its retention
	// setting, naming the actor who authored the value, the value, the
	// boundary it cut to, and the policy version and score version in force
	// at the cut.
	ShapeTruncation Shape = "truncation"
	// ShapePolicyVersion is appended at each owner write and at each write to
	// the People declaration other than the key-to-name mapping.
	ShapePolicyVersion Shape = "policy_version"
	// ShapeScoreVersion is appended by the score as its values move.
	ShapeScoreVersion Shape = "score_version"
	// ShapeInstallEvent is written at every upgrade and at every start after
	// the factory's records are restored from a backup.
	ShapeInstallEvent Shape = "install_event"
	// ShapeReadEvent is appended when the log itself is read, or when stored
	// report text a redaction could reach is read.
	ShapeReadEvent Shape = "read_event"
)

// Shapes is every shape a row may have. The CHECK constraint in [DDL] lists
// the same ten, and TestDDLListsEveryShape fails if the two stop agreeing.
var Shapes = []Shape{
	ShapeDecision, ShapePageEvent, ShapeWait, ShapeReworkRequest, ShapeQueueRejection,
	ShapeTruncation, ShapePolicyVersion, ShapeScoreVersion, ShapeInstallEvent, ShapeReadEvent,
}

// Part is which row of a decision or a wait a row is. The other eight shapes
// carry the empty part, which the part_matches_shape constraint in [DDL]
// enforces.
type Part string

const (
	// PartOpen is a decision's or a wait's first row: for a decision, the row
	// appended when the gate fires, naming both versions and everything the
	// verdict will be given over; for a wait, the row appended when the
	// condition is met. The stored value is hashed into the chain, so it
	// keeps the word the first record was written with; only the Go name
	// follows the design's "open event".
	PartOpen Part = "opening"
	// PartClose is a decision's or a wait's row that ends it in the ordinary
	// way: for a decision, the row appended when the verdict is given, naming
	// the opening it closes and neither version; for a wait, the row
	// appended when the condition is found gone.
	PartClose Part = "closing"
	// PartAbandonment is a decision that will never receive a verdict,
	// naming the opening it ends and, in [Entry.Reason], why no verdict is
	// coming. Only a decision may hold this part.
	PartAbandonment Part = "abandonment"
	// PartAcknowledgement names a decision's opening and the human who said
	// at Work that they have the row. Only a decision may hold this part.
	PartAcknowledgement Part = "acknowledgement"
)

// Formats maps every format version this package accepts to the [Shape] it
// serialises. [Row.FormatVersion] declares a row's shape this way, and an
// append naming a format version not in this table is refused: "the writer
// refuses a row declaring no shape." format_version_matches_shape in [DDL]
// lists the same pairs, and TestFormatVersionsMatchDDL is what keeps the two
// agreeing. A shape may gain a second format version later, where a
// serialisation or a field list needs to change; today each has exactly one.
var Formats = map[string]Shape{
	"decision/1":        ShapeDecision,
	"page_event/1":      ShapePageEvent,
	"wait/1":            ShapeWait,
	"rework_request/1":  ShapeReworkRequest,
	"queue_rejection/1": ShapeQueueRejection,
	"truncation/1":      ShapeTruncation,
	"policy_version/1":  ShapePolicyVersion,
	"score_version/1":   ShapeScoreVersion,
	"install_event/1":   ShapeInstallEvent,
	"read_event/1":      ShapeReadEvent,
}

// Entry is what a caller hands an append method: who decided, what about,
// under which format version and versions, and — where the shape takes one —
// which row it closes, its verdict, and its reason. Which of these a given
// method requires and which it refuses is stated on the method, not on the
// type, so a caller that sets a field the method refuses is told so rather
// than being unable to say it.
//
// The writer fills in the rest of the row — the identifier, the timestamp,
// the sequence value, and both hashes — so a caller cannot set them.
type Entry struct {
	// Actor is who decided, or who read. It is required and validated by
	// [record.Actor.Validate].
	Actor record.Actor
	// Payload is what the row says, as the exact bytes the chain hashes.
	// This package neither parses it nor constrains its format.
	Payload string
	// FormatVersion is required on every entry and declares the row's shape
	// through [Formats]. A value not in that table, or one that declares a
	// shape a given method does not write, is refused.
	FormatVersion string
	// PolicyVersion is the gate policy a decision was decided under, or the
	// pair in force at a truncation's cut. Required by
	// [Writer.AppendDecisionOpen] and [Writer.Truncate]; refused by every
	// other method.
	PolicyVersion string
	// ScoreVersion is the risk score a decision was decided under, or the
	// pair in force at a truncation's cut. The same rule as PolicyVersion
	// applies to it.
	ScoreVersion string
	// Closes is the id of the row a decision's closing, abandonment, or
	// acknowledgement, or a wait's closing, names. Refused everywhere else.
	Closes string
	// Verdict is a decision closing's verdict, one of approve, reject, hold,
	// or refer. Required by [Writer.AppendDecisionClose]; refused by every
	// other method.
	Verdict string
	// Reason is a decision closing's reason, required where Verdict is
	// reject or hold, or a decision abandonment's own reason — "why no
	// verdict is coming" — required there always, doc.go stating why the two
	// share one column. Refused by every other method.
	Reason string
	// OpenedInWorkAt is when the actor opened the gate's row in Work, in
	// [record.TimeLayout], or the empty string. Set on a decision closing
	// alone, by the caller reporting it as Work; refused elsewhere.
	OpenedInWorkAt string
	// SelfApproval is a decision closing's self-approval field. Set on a
	// decision closing alone; refused elsewhere.
	SelfApproval bool
}

// Row is one row of the log as it is stored.
type Row struct {
	Seq            int64
	ID             string
	FormatVersion  string
	Actor          record.Actor
	At             string
	Shape          Shape
	Payload        string
	PolicyVersion  string
	ScoreVersion   string
	Part           Part
	Closes         string
	Verdict        string
	Reason         string
	OpenedInWorkAt string
	SelfApproval   bool
	PrevHash       string
	Hash           string
}

// ChainHash is the hash the row's chain requires, computed from its stored
// fields and its predecessor's hash. It reads Row.Hash for nothing, so
// comparing the two is what says whether the stored hash is the right one.
//
// The field order is the one doc.go states, and it hashes the row's own
// stored FormatVersion rather than a package-wide constant: a later format
// version changing the serialisation changes what it hashes, not what an
// earlier row already wrote. Every format version this package accepts today
// shares the one serialisation below; a format version needing a different
// one would branch here on r.FormatVersion.
//
// Each field is written as its length in bytes, big-endian in eight bytes,
// then its bytes.
func (r Row) ChainHash() string {
	h := sha256.New()
	for _, field := range []string{
		r.FormatVersion,
		r.PrevHash,
		strconv.FormatInt(r.Seq, 10),
		r.ID,
		string(r.Actor.Kind),
		r.Actor.Key,
		string(r.Actor.Basis),
		r.At,
		string(r.Shape),
		r.Payload,
		r.PolicyVersion,
		r.ScoreVersion,
		string(r.Part),
		r.Closes,
		r.Verdict,
		r.Reason,
		r.OpenedInWorkAt,
		selfApprovalField(r.SelfApproval),
	} {
		writeField(h, field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func selfApprovalField(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// writeField writes one length-prefixed field. hash.Hash never returns an
// error from Write, which is why none is handled.
func writeField(h hash.Hash, field string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	h.Write(length[:])
	h.Write([]byte(field))
}
