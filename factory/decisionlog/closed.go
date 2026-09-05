package decisionlog

// Closed is one decision that has both the rows an ordinary verdict leaves:
// the opening a gate appended when it fired, and the closing that gave the
// verdict. A decision ended by an abandonment instead is not one of these —
// [Reader.Pending] is where an abandoned opening stops appearing, since it
// is no longer waiting on anyone, but it is never paired here. Verdict and
// Reason are the closing's own columns.
type Closed struct {
	OpenEvent  Row
	CloseEvent Row
}

// pairClosedDecisions joins every decision opening in rows with the closing
// that ends it, where one exists, in the order the openings were appended.
func pairClosedDecisions(rows []Row) []Closed {
	closings := make(map[string]Row, len(rows))
	for _, row := range rows {
		if row.Shape == ShapeDecision && row.Part == PartClose {
			closings[row.Closes] = row
		}
	}
	var closed []Closed
	for _, row := range rows {
		if row.Shape != ShapeDecision || row.Part != PartOpen {
			continue
		}
		if closing, found := closings[row.ID]; found {
			closed = append(closed, Closed{OpenEvent: row, CloseEvent: closing})
		}
	}
	return closed
}
