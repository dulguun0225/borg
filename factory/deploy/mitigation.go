package deploy

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// Operation is what a mitigation does. There are three: two the design names as
// the class — shifting traffic off a target, and changing the instance count of
// a release the factory deployed — and ending every instance of a service on a
// target beside them.
type Operation string

const (
	// OperationShiftTraffic is traffic shifted off a target, or onto one.
	OperationShiftTraffic Operation = "shift_traffic"
	// OperationSetInstanceCount is the instance count of a release the factory
	// deployed, changed.
	OperationSetInstanceCount Operation = "set_instance_count"
	// OperationEndEveryInstance is every instance of the service on that target,
	// ended.
	OperationEndEveryInstance Operation = "end_every_instance"
)

// Operations is every operation a mitigation may name. The CHECK constraint in
// [DDL] lists the same three.
var Operations = []Operation{OperationShiftTraffic, OperationSetInstanceCount, OperationEndEveryInstance}

var (
	// ErrOperationUnknown is returned for an operation outside [Operations].
	ErrOperationUnknown = errors.New("deploy: the mitigation names no operation of the class")
	// ErrMitigationIncomplete is returned for a mitigation naming no target or
	// no deploy record. The drift detector reads a mitigation as intended state,
	// so one naming neither would say nothing about what is intended where.
	ErrMitigationIncomplete = errors.New("deploy: the mitigation names no target or no deploy record")
	// ErrMitigationNotFound is returned where the named mitigation does not
	// exist or has already ended.
	ErrMitigationNotFound = errors.New("deploy: no mitigation is open under that id")
	// ErrNotAHuman is returned for a mitigation whose actor is not a human. The
	// deployer performs one on a human's instruction from Ops, so the actor
	// under seam 1 is that human.
	ErrNotAHuman = errors.New("deploy: a mitigation is performed on a human's instruction")
)

// Mitigation is one operation the deployer performed on a human's instruction
// from Ops: the actor under seam 1, the operation, the target, and the deploy
// record it modifies, which the drift detector reads as intended state.
type Mitigation struct {
	ID        string
	Actor     record.Actor
	At        string
	Operation Operation
	// Address is the target the operation was performed on.
	Address string
	// DeployID is the deploy record the operation modifies — what runs there was
	// put there by that deploy, and the mitigation is what says the state now
	// differs from it on purpose.
	DeployID string
	BeganAt  string
	// EndedAt is when the mitigation stopped standing, and is empty while it
	// stands. The drift detector reads an open one as intended state and a
	// closed one as nothing.
	EndedAt string
}

// Standing reports whether the mitigation is still in force.
func (m Mitigation) Standing() bool { return m.EndedAt == "" }

// Mitigating is what a caller asks the deployer to perform: the operation, the
// target it is performed on, and the deploy record it modifies. The caller is
// the command-line interface until Ops is a screen.
type Mitigating struct {
	Actor     record.Actor
	Principal principal.Principal
	Operation Operation
	Address   string
	Target    targetseam.Target
	DeployID  string
	// ServiceName is what the target acts on.
	ServiceName string
	// Build is the build the operation names, required by both operations that
	// name one.
	Build string
	// Share is what a shift asks for, and Count what an instance count asks for.
	Share      float64
	Count      int
	Credential secretref.Ref
}

// Mitigate performs one mitigation and writes its record: the record is written
// before the call, so a call that stops halfway leaves a mitigation standing
// rather than an operation nothing recorded, and the record stays open until
// [Writer.EndMitigation] closes it.
//
// The operation is performed through the same seam every deploy is, so a
// platform that cannot perform it refuses and the refusal is returned with the
// record already written — what was asked for is on the record whether or not
// the platform could do it.
func Mitigate(ctx context.Context, w *Writer, m Mitigating) (Mitigation, error) {
	written, err := w.BeginMitigation(ctx, m.Actor, Mitigation{
		Operation: m.Operation, Address: m.Address, DeployID: m.DeployID,
	})
	if err != nil {
		return Mitigation{}, err
	}

	switch m.Operation {
	case OperationShiftTraffic:
		err = m.Target.ShiftTraffic(ctx, m.Principal, targetseam.Shift{
			Service: m.ServiceName, Build: m.Build, Share: m.Share, Credential: m.Credential,
		})
	case OperationSetInstanceCount:
		err = m.Target.SetInstanceCount(ctx, m.Principal, targetseam.InstanceCount{
			Service: m.ServiceName, Build: m.Build, Count: m.Count, Credential: m.Credential,
		})
	case OperationEndEveryInstance:
		// What the seam reports about how those instances ended is not written
		// here: a mitigation is not a deploy, and the replacement field it would
		// go on is the deploy record's own row per target.
		_, err = m.Target.Stop(ctx, m.Principal, m.ServiceName, m.Credential)
	default:
		err = fmt.Errorf("%w: %q", ErrOperationUnknown, m.Operation)
	}
	if err != nil {
		return written, fmt.Errorf("deploy: performing the mitigation %s on %s: %w",
			m.Operation, m.Address, err)
	}
	return written, nil
}

// BeginMitigation writes the mitigation, open. The actor is the human at Ops
// whose instruction the deployer performs it on, which is what the drift
// detector reads beside the operation.
func (w *Writer) BeginMitigation(ctx context.Context, actor record.Actor, m Mitigation) (Mitigation, error) {
	if err := actor.Validate(); err != nil {
		return Mitigation{}, err
	}
	if actor.Kind != record.KindHuman {
		return Mitigation{}, fmt.Errorf("%w: %s", ErrNotAHuman, actor.Kind)
	}
	if !slices.Contains(Operations, m.Operation) {
		return Mitigation{}, fmt.Errorf("%w: %q", ErrOperationUnknown, m.Operation)
	}
	if m.Address == "" || m.DeployID == "" {
		return Mitigation{}, fmt.Errorf("%w: target %q, deploy %q", ErrMitigationIncomplete, m.Address, m.DeployID)
	}

	written := m
	written.ID = record.NewID(MitigationIDPrefix)
	written.Actor = actor
	written.At = record.Now()
	written.BeganAt = written.At
	written.EndedAt = ""

	err := w.inTransaction(ctx, "beginning the mitigation "+written.ID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `insert into `+MitigationTable+`
			(id, format_version, actor_kind, actor_key, actor_key_basis, at, operation, address, deploy_id, began_at)
			values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			written.ID, FormatVersionMitigation, string(actor.Kind), actor.Key, string(actor.Basis),
			written.At, string(written.Operation), written.Address, written.DeployID, written.BeganAt)
		return err
	})
	if err != nil {
		return Mitigation{}, err
	}
	return written, nil
}

// EndMitigation writes when the mitigation stopped standing, so the drift
// detector stops reading it as intended state.
func (w *Writer) EndMitigation(ctx context.Context, id string) error {
	return w.inTransaction(ctx, "ending the mitigation "+id, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `update `+MitigationTable+` set ended_at = $1
			where id = $2 and ended_at = ''`, record.Now(), id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return fmt.Errorf("%w: %s", ErrMitigationNotFound, id)
		}
		return nil
	})
}

const selectMitigation = `select id, actor_kind, actor_key, actor_key_basis, at, operation, address,
	deploy_id, began_at, ended_at
	from ` + MitigationTable

// Mitigations is every mitigation against one deploy record, oldest first. It
// takes the pool and not a [Writer], because reading them is not a reason to be
// handed the thing that performs them: the drift detector reads them as
// intended state and writes nothing.
func Mitigations(ctx context.Context, pool *pgxpool.Pool, deployID string) ([]Mitigation, error) {
	return queryMitigations(ctx, pool, "the mitigations of "+deployID,
		selectMitigation+` where deploy_id = $1 order by began_at, id`, deployID)
}

// StandingMitigations is every mitigation still in force, oldest first, whatever
// the deploy. It is what the drift detector reads before it calls a target's
// state a mismatch.
func StandingMitigations(ctx context.Context, pool *pgxpool.Pool) ([]Mitigation, error) {
	return queryMitigations(ctx, pool, "the standing mitigations",
		selectMitigation+` where ended_at = '' order by began_at, id`)
}

func queryMitigations(ctx context.Context, pool *pgxpool.Pool, reading, statement string, args ...any) ([]Mitigation, error) {
	rows, err := pool.Query(ctx, statement, args...)
	if err != nil {
		return nil, fmt.Errorf("deploy: reading %s: %w", reading, err)
	}
	defer rows.Close()

	var read []Mitigation
	for rows.Next() {
		var m Mitigation
		var kind, basis, operation string
		err := rows.Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At, &operation, &m.Address,
			&m.DeployID, &m.BeganAt, &m.EndedAt)
		if err != nil {
			return nil, fmt.Errorf("deploy: reading one of %s: %w", reading, err)
		}
		m.Actor.Kind = record.Kind(kind)
		m.Actor.Basis = record.Basis(basis)
		m.Operation = Operation(operation)
		read = append(read, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("deploy: reading %s: %w", reading, err)
	}
	return read, nil
}
