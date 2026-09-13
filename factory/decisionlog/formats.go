package decisionlog

import "fmt"

// Formats maps every format version this package accepts to the [Shape] it
// serialises. [Row.FormatVersion] declares a row's shape this way, and an
// append naming a format version not in this table is refused: "the writer
// refuses a row declaring no shape." format_version_matches_shape in [DDL]
// lists the same pairs, and TestFormatVersionsMatchDDL is what keeps the two
// agreeing. The extended versions carry the same payload bytes as their legacy
// counterparts and hash the additional structured columns. An append naming
// a legacy version is promoted to its extended counterpart when it carries
// any of those columns. Existing rows keep their stored format and hash.
var Formats = map[string]Shape{
	"decision/1":        ShapeDecision,
	"decision/2":        ShapeDecision,
	"page_event/1":      ShapePageEvent,
	"page_event/2":      ShapePageEvent,
	"page_event/3":      ShapePageEvent,
	"page_event/4":      ShapePageEvent,
	"wait/1":            ShapeWait,
	"wait/2":            ShapeWait,
	"rework_request/1":  ShapeReworkRequest,
	"rework_request/2":  ShapeReworkRequest,
	"queue_rejection/1": ShapeQueueRejection,
	"queue_rejection/2": ShapeQueueRejection,
	"truncation/1":      ShapeTruncation,
	"truncation/2":      ShapeTruncation,
	"policy_version/1":  ShapePolicyVersion,
	"policy_version/2":  ShapePolicyVersion,
	"score_version/1":   ShapeScoreVersion,
	"score_version/2":   ShapeScoreVersion,
	"install_event/1":   ShapeInstallEvent,
	"install_event/2":   ShapeInstallEvent,
	"read_event/1":      ShapeReadEvent,
	"read_event/2":      ShapeReadEvent,
}

// extendedFormat preserves the payload dialect while adding structured columns.
func extendedFormat(version string) string {
	switch version {
	case "decision/1":
		return "decision/2"
	case "page_event/1":
		return "page_event/3"
	case "page_event/2":
		return "page_event/4"
	case "wait/1":
		return "wait/2"
	case "rework_request/1":
		return "rework_request/2"
	case "queue_rejection/1":
		return "queue_rejection/2"
	case "truncation/1":
		return "truncation/2"
	case "policy_version/1":
		return "policy_version/2"
	case "score_version/1":
		return "score_version/2"
	case "install_event/1":
		return "install_event/2"
	case "read_event/1":
		return "read_event/2"
	default:
		return version
	}
}

func extendedEncoding(version string) bool {
	switch version {
	case "decision/2", "page_event/3", "page_event/4", "wait/2", "rework_request/2",
		"queue_rejection/2", "truncation/2", "policy_version/2", "score_version/2", "install_event/2", "read_event/2":
		return true
	default:
		return false
	}
}

func (r Row) hasExtendedFields() bool {
	return r.ReturnsTo != "" || r.Reading != "" || r.MovedRelease != "" ||
		r.CallerKind != "" || r.CallerKey != "" || r.CallerKeyBasis != "" ||
		r.CallerDispatchID != "" || r.CallerScope != ""
}

// ValidateFormat checks that the stored version declares this shape and
// covers every populated structured column. Hash verification calls it
// before ChainHash, since legacy encodings cannot cover added columns.
func (r Row) ValidateFormat() error {
	shape, known := Formats[r.FormatVersion]
	if !known || shape != r.Shape || (!extendedEncoding(r.FormatVersion) && r.hasExtendedFields()) {
		return fmt.Errorf("decisionlog: row %d (%s) has fields incompatible with format %q", r.Seq, r.ID, r.FormatVersion)
	}
	return nil
}
