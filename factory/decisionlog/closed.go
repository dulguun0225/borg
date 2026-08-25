package decisionlog

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Closed is one decision that has both its rows: the open event a gate
// appended when it fired, and the close event that gave the verdict. A decision
// is read by joining two rows, which is what two appends cost; a pending gate is
// an open event with no close event yet and is not one of these.
type Closed struct {
	OpenEvent  Row
	CloseEvent Row
}

// ClosedDecisions is every decision both of whose rows are in the log, in the
// order the openings were appended. It takes the pool and not a [Writer],
// because reading the log is not a reason to hold the thing that appends to it.
//
// It reads the whole log to pair the rows, so a caller asking per firing reads
// the log per firing. That is what an outcome count over one author's decisions
// costs while the log is small, and it is the honest place for the cost: a query
// narrowed by what the payload names would put the payload's shape — which is
// the gate's and carries the vector — inside the log.
func ClosedDecisions(ctx context.Context, pool *pgxpool.Pool) ([]Closed, error) {
	rows, err := Read(ctx, pool)
	if err != nil {
		return nil, err
	}
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
	return closed, nil
}
