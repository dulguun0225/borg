package main

import (
	"context"
	"encoding/json"

	"github.com/dulguun0225/borg/factory/criterion"
	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
)

// rejectedSpecs supplies the decision-log fact that the criterion record
// cannot read itself. A rejected version contributes neither promises nor
// withdrawals when another version of the same item proceeds.
func (p *path) rejectedSpecs(ctx context.Context) ([]string, error) {
	closed, err := decisionlog.NewReader(p.d.pool, p.d.token).
		ClosedDecisions(ctx, gate.ComponentPrincipal(gate.Spec))
	if err != nil {
		return nil, err
	}
	var rejected []string
	for _, d := range closed {
		if d.CloseEvent.Verdict != string(gate.VerdictReject) {
			continue
		}
		var opening gate.OpeningPayload
		if err := json.Unmarshal([]byte(d.OpenEvent.Payload), &opening); err != nil {
			return nil, err
		}
		if opening.Gate == gate.Spec.String() && opening.ArtifactID != "" {
			rejected = append(rejected, opening.ArtifactID)
		}
	}
	return rejected, nil
}

func (p *path) criteriaInForce(ctx context.Context, serviceID string, itemIDs []string) ([]criterion.Criterion, error) {
	rejected, err := p.rejectedSpecs(ctx)
	if err != nil {
		return nil, err
	}
	return criterion.InForce(ctx, p.d.pool, serviceID, itemIDs, rejected...)
}
