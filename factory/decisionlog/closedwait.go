package decisionlog

// ClosedWait is one wait that has both of its rows: the opening written when
// the component met the condition, and the closing written when the
// condition was found gone. The pair is what makes how long it held a
// subtraction, [Closed] doing the same for a decision.
type ClosedWait struct {
	OpenEvent  Row
	CloseEvent Row
}

// pairClosedWaits joins every wait opening in rows with the closing that ends
// it, where one exists, in the order the openings were appended.
func pairClosedWaits(rows []Row) []ClosedWait {
	closings := make(map[string]Row, len(rows))
	for _, row := range rows {
		if row.Shape == ShapeWait && row.Part == PartClose {
			closings[row.Closes] = row
		}
	}
	var closed []ClosedWait
	for _, row := range rows {
		if row.Shape != ShapeWait || row.Part != PartOpen {
			continue
		}
		if closing, found := closings[row.ID]; found {
			closed = append(closed, ClosedWait{OpenEvent: row, CloseEvent: closing})
		}
	}
	return closed
}
