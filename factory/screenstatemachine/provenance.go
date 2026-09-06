package screenstatemachine

import (
	"context"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A superseding machine that removes protection. The machine is closed, so a
// transition a machine does not declare is forbidden, and a superseding machine
// declaring one the machine it supersedes did not is what admits behaviour the
// confirmed one forbade. Where the superseded machine was human-confirmed, that
// is a resolved factor at the Spec row, routed the way a criterion's withdrawal
// is.

// SupersessionRemovingProtection is one such revision: the superseding machine,
// the human-confirmed machine it supersedes, and the transitions it declares
// that the superseded one did not.
type SupersessionRemovingProtection struct {
	Machine    Machine
	Superseded Machine
	// Added is every transition the superseding machine declares from a state
	// and on an event the superseded machine declared no transition for. A
	// transition redirected rather than added is not here: the superseded
	// machine declared behaviour there, so the closed reading forbade nothing
	// that is now admitted.
	Added []Transition
}

// SupersessionsRemovingProtection is every machine the spec version introduces
// that supersedes a human-confirmed machine and declares a transition that one
// did not. The score reads it: this is a resolved factor at the Spec row,
// routed to the actor of the superseded machine's introducing decision.
//
// humanConfirmed is the spec versions a human decided, assembled by the caller
// — human-confirmed is a query over the introducing spec version's decision,
// which is the decision log's fact and not this table's, so the caller reads it
// and passes what it read. doc.go names the caller, and it is not built.
func SupersessionsRemovingProtection(ctx context.Context, pool *pgxpool.Pool,
	specArtifactID string, humanConfirmed []string) ([]SupersessionRemovingProtection, error) {
	if specArtifactID == "" || len(humanConfirmed) == 0 {
		return nil, nil
	}
	revisions, err := read(ctx, pool, selectMachine+` where spec_artifact_id = $1 and supersedes <> '' order by at`,
		specArtifactID)
	if err != nil {
		return nil, err
	}
	if len(revisions) == 0 {
		return nil, nil
	}

	ids := make([]string, 0, len(revisions))
	for _, m := range revisions {
		ids = append(ids, m.Supersedes)
	}
	superseded, err := read(ctx, pool, selectMachine+` where id = any($1)`, ids)
	if err != nil {
		return nil, err
	}
	by := make(map[string]Machine, len(superseded))
	for _, m := range superseded {
		by[m.ID] = m
	}

	var removing []SupersessionRemovingProtection
	for _, revision := range revisions {
		old, held := by[revision.Supersedes]
		if !held || !slices.Contains(humanConfirmed, old.SpecArtifactID) {
			continue
		}
		added := addedTransitions(old, revision)
		if len(added) == 0 {
			continue
		}
		removing = append(removing, SupersessionRemovingProtection{
			Machine: revision, Superseded: old, Added: added,
		})
	}
	return removing, nil
}

// addedTransitions is every transition the revision declares from a state and
// on an event the superseded machine declared none for.
func addedTransitions(old, revision Machine) []Transition {
	declared := make(map[[2]string]bool, len(old.Transitions))
	for _, t := range old.Transitions {
		declared[[2]string{t.From, t.Event}] = true
	}
	var added []Transition
	for _, t := range revision.Transitions {
		if !declared[[2]string{t.From, t.Event}] {
			added = append(added, t)
		}
	}
	return added
}

// read runs one of this package's selects and scans the rows into machines.
func read(ctx context.Context, pool *pgxpool.Pool, sql string, args ...any) ([]Machine, error) {
	rows, err := pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("screenstatemachine: reading the machines: %w", err)
	}
	defer rows.Close()

	var all []Machine
	for rows.Next() {
		m, err := scan(rows)
		if err != nil {
			return nil, fmt.Errorf("screenstatemachine: reading a row: %w", err)
		}
		all = append(all, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("screenstatemachine: reading the machines: %w", err)
	}
	return all, nil
}
