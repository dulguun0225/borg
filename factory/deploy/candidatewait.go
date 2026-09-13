package deploy

import (
	"context"
	"encoding/json"

	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

const candidateRunWaitKind = "candidate_run_unavailable"

// WaitRow is the part of a wait row the deployer needs to find a candidate's
// open wait.
type WaitRow struct {
	ID      string
	Shape   string
	Payload string
}

// WaitLog is the log seam used by the deployer for candidate run waits.
type WaitLog interface {
	Rows(ctx context.Context) ([]WaitRow, error)
	Open(ctx context.Context, actor record.Actor, payload string) (string, error)
	Close(ctx context.Context, actor record.Actor, row string, payload string) error
}

type candidateRunWait struct {
	Kind      string `json:"kind"`
	ItemID    string `json:"item_id"`
	Condition string `json:"condition"`
	Attempt   int    `json:"attempt"`
}

// CandidateRunWait finds the open unavailable wait for an item.
func CandidateRunWait(ctx context.Context, log WaitLog, itemID string) (string, error) {
	rows, err := log.Rows(ctx)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.Shape != "wait" {
			continue
		}
		var payload candidateRunWait
		if json.Unmarshal([]byte(row.Payload), &payload) == nil &&
			payload.Kind == candidateRunWaitKind && payload.ItemID == itemID {
			return row.ID, nil
		}
	}
	return "", nil
}

// OpenCandidateRunWait opens the second unavailable candidate-run wait, or
// returns the existing row when the condition is still present.
func OpenCandidateRunWait(ctx context.Context, log WaitLog, actor record.Actor, itemID, condition string) (string, error) {
	row, err := CandidateRunWait(ctx, log, itemID)
	if err != nil {
		return "", err
	}
	if row != "" {
		return row, nil
	}
	payload, err := json.Marshal(candidateRunWait{Kind: candidateRunWaitKind, ItemID: itemID, Condition: condition, Attempt: 2})
	if err != nil {
		return "", err
	}
	return log.Open(ctx, actor, string(payload))
}

// CloseCandidateRunWait ends an unavailable wait when the candidate run is
// performed.
func CloseCandidateRunWait(ctx context.Context, log WaitLog, actor record.Actor, row string) error {
	if row == "" {
		return nil
	}
	return log.Close(ctx, actor, row, "candidate run performed")
}

// CandidateTeardown is the target and environment seam for tearing down a
// candidate. The deployer stops the target before it writes the environment
// record.
type CandidateTeardown struct {
	Target        targetStopper
	Environments  environmentTeardowns
	EnvironmentID string
	Address       string
	ServiceName   string
	Principal     principal.Principal
	Credential    secretref.Ref
	Actor         record.Actor
	Reason        environment.Reason
}

type targetStopper interface {
	Stop(context.Context, principal.Principal, string, secretref.Ref) (targetseam.Placement, error)
}

type environmentTeardowns interface {
	TearDown(context.Context, record.Actor, string, environment.Reason, environment.Rate) error
}

// TearDownCandidate stops the candidate software and then tears down its
// environment record.
func TearDownCandidate(ctx context.Context, t CandidateTeardown) error {
	if t.EnvironmentID == "" {
		return nil
	}
	if _, err := t.Target.Stop(ctx, t.Principal, t.ServiceName, t.Credential); err != nil {
		return err
	}
	return t.Environments.TearDown(ctx, t.Actor, t.EnvironmentID, t.Reason, environment.Rate{})
}
