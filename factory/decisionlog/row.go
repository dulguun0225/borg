package decisionlog

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"hash"
	"strconv"

	"github.com/dulguun0225/borg/factory/record"
)

// Shape is what a row is. There are three, and the log holds no other.
type Shape string

const (
	// ShapeDecision is something the factory or a human decided. It is the
	// only shape that names a policy version and a score version.
	ShapeDecision Shape = "decision"
	// ShapePageEvent is a page that was delivered, which is a delivery and
	// not a decision.
	ShapePageEvent Shape = "page_event"
	// ShapeWait is something the factory could not compute when a gate fired
	// — an unreachable credential among them — which is a wait and not a
	// decision.
	ShapeWait Shape = "wait"
)

// Shapes is every shape a row may have. The CHECK constraint in [DDL] lists
// the same three, and TestDDLListsEveryShape fails if the two stop agreeing.
var Shapes = []Shape{ShapeDecision, ShapePageEvent, ShapeWait}

// Entry is what a caller hands an append method: who decided, what about, and
// under which versions. The three version rules are the three methods, not
// three types, so a caller that puts a version on a page event is told so
// rather than being unable to say it.
//
// The writer fills in the rest of the row — the identifier, the timestamp, the
// sequence value, and both hashes — so a caller cannot set them.
type Entry struct {
	// Actor is who decided. It is required and validated by
	// [record.Actor.Validate].
	Actor record.Actor
	// Payload is what the row says, as the exact bytes the chain hashes. This
	// package neither parses it nor constrains its format.
	Payload string
	// PolicyVersion is the gate policy the decision was decided under. It is
	// required by [Writer.AppendDecision] and refused by the other two.
	PolicyVersion string
	// ScoreVersion is the risk score the decision was decided under. The same
	// rule applies to it.
	ScoreVersion string
}

// Row is one row of the log as it is stored.
type Row struct {
	Seq           int64
	ID            string
	Actor         record.Actor
	At            string
	Shape         Shape
	Payload       string
	PolicyVersion string
	ScoreVersion  string
	PrevHash      string
	Hash          string
}

// hashFormat is the first field of every serialisation. A change to the field
// order or the framing below changes this string, so a row hashed under one
// format never verifies as a row hashed under another.
const hashFormat = "borg/factory/decisionlog/v1"

// ChainHash is the hash the row's chain requires, computed from its stored
// fields and its predecessor's hash. It reads Row.Hash for nothing, so
// comparing the two is what says whether the stored hash is the right one.
//
// The field order is the one doc.go states. Each field is written as its
// length in bytes, big-endian in eight bytes, then its bytes.
func (r Row) ChainHash() string {
	h := sha256.New()
	for _, field := range []string{
		hashFormat,
		r.PrevHash,
		strconv.FormatInt(r.Seq, 10),
		r.ID,
		string(r.Actor.Kind),
		r.Actor.Name,
		r.At,
		string(r.Shape),
		r.Payload,
		r.PolicyVersion,
		r.ScoreVersion,
	} {
		writeField(h, field)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// writeField writes one length-prefixed field. hash.Hash never returns an
// error from Write, which is why none is handled.
func writeField(h hash.Hash, field string) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	h.Write(length[:])
	h.Write([]byte(field))
}
