package screenstatemachine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/record"
)

var (
	// ErrServiceIDEmpty is returned by [Insert] for a machine naming no
	// service. record's doc.go states what a link is checked for.
	ErrServiceIDEmpty = errors.New("screenstatemachine: the service id is empty")
	// ErrSpecArtifactIDEmpty is returned by [Insert] for a machine naming no
	// spec version.
	ErrSpecArtifactIDEmpty = errors.New("screenstatemachine: the spec artifact id is empty")
	// ErrItemIDEmpty is returned by [Insert] for a machine naming no item.
	ErrItemIDEmpty = errors.New("screenstatemachine: the item id is empty")
	// ErrSupersedesNotFound is returned by [Insert] for a draft naming a
	// machine to supersede that does not exist.
	ErrSupersedesNotFound = errors.New("screenstatemachine: the machine it supersedes does not exist")
)

// Of is what a machine belongs to: the service it is a screen of, the spec
// version that introduced or revised it, and the item that spec version
// belongs to.
type Of struct {
	ServiceID      string
	SpecArtifactID string
	ItemID         string
}

// Transition is one edge of the machine: the state it leaves from, the event
// it answers, and where it goes. To is a state of this machine, and is empty
// where Screen names another screen instead — a transition leaves the screen
// or it stays, never both.
type Transition struct {
	From   string
	Event  string
	To     string
	Screen string
}

// Machine is one screen state machine as it is stored.
type Machine struct {
	ID             string
	Actor          record.Actor
	At             string
	ServiceID      string
	SpecArtifactID string
	ItemID         string
	// Screen is the screen's identity: this machine's own id where it
	// introduces the screen, and the id of the machine at the head of the
	// chain of supersessions otherwise.
	Screen      string
	Supersedes  string
	Initial     string
	States      []string
	Events      []string
	Transitions []Transition
	Terminal    []string
}

const insertMachine = `insert into ` + Table + `
	(id, format_version, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
	screen, supersedes, initial, states, events, transitions, terminal)
	values ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`

// Insert writes one machine inside tx. Its one caller is the artifact store,
// which calls it inside the transaction that submits the spec version
// introducing or revising the machine — that is why it takes a transaction
// rather than a pool, so the spec version and its machine commit together or
// not at all.
//
// It refuses a draft [Validate] rejects. Where draft.Supersedes is empty, the
// new row's screen is its own id; otherwise the screen is read off the
// superseded row, [ErrSupersedesNotFound] where there is none.
func Insert(ctx context.Context, tx pgx.Tx, actor record.Actor, of Of, draft Draft) (Machine, error) {
	if err := actor.Validate(); err != nil {
		return Machine{}, err
	}
	if of.ServiceID == "" {
		return Machine{}, ErrServiceIDEmpty
	}
	if of.SpecArtifactID == "" {
		return Machine{}, ErrSpecArtifactIDEmpty
	}
	if of.ItemID == "" {
		return Machine{}, ErrItemIDEmpty
	}
	if err := Validate(draft); err != nil {
		return Machine{}, err
	}

	m := Machine{
		ID:             record.NewID(IDPrefix),
		Actor:          actor,
		At:             record.Now(),
		ServiceID:      of.ServiceID,
		SpecArtifactID: of.SpecArtifactID,
		ItemID:         of.ItemID,
		Supersedes:     draft.Supersedes,
		Initial:        draft.Initial,
		States:         draft.States,
		Events:         draft.Events,
		Transitions:    draft.Transitions,
		Terminal:       draft.Terminal,
	}
	if draft.Supersedes == "" {
		m.Screen = m.ID
	} else {
		err := tx.QueryRow(ctx, `select screen from `+Table+` where id = $1`, draft.Supersedes).Scan(&m.Screen)
		if errors.Is(err, pgx.ErrNoRows) {
			return Machine{}, fmt.Errorf("%w: %s", ErrSupersedesNotFound, draft.Supersedes)
		} else if err != nil {
			return Machine{}, fmt.Errorf("screenstatemachine: reading the machine %s supersedes: %w", draft.Supersedes, err)
		}
	}

	transitions, err := json.Marshal(m.Transitions)
	if err != nil {
		return Machine{}, fmt.Errorf("screenstatemachine: encoding the transitions: %w", err)
	}
	if _, err := tx.Exec(ctx, insertMachine,
		m.ID, FormatVersion, string(m.Actor.Kind), m.Actor.Key, string(m.Actor.Basis), m.At,
		m.ServiceID, m.SpecArtifactID, m.ItemID, m.Screen, m.Supersedes, m.Initial,
		m.States, m.Events, string(transitions), m.Terminal,
	); err != nil {
		return Machine{}, fmt.Errorf("screenstatemachine: writing %s: %w", m.ID, err)
	}
	return m, nil
}

const selectMachine = `select id, actor_kind, actor_key, actor_key_basis, at, service_id, spec_artifact_id, item_id,
	screen, supersedes, initial, states, events, transitions, terminal
	from ` + Table

// InForce is every machine in force for one build of the service: introduced
// by an item in the build, less any machine another one in that same set
// supersedes — the chain of supersessions is the screen, so only the newest
// revision within the build stands. itemIDs is the build's set of items,
// assembled by the caller the way [criterion.InForce] takes it.
//
// No items is no machines and no error, for the reason [criterion.InForce]
// gives.
func InForce(ctx context.Context, pool *pgxpool.Pool, serviceID string, itemIDs []string) ([]Machine, error) {
	if len(itemIDs) == 0 {
		return nil, nil
	}
	rows, err := pool.Query(ctx, selectMachine+` where service_id = $1 and item_id = any($2) order by at`,
		serviceID, itemIDs)
	if err != nil {
		return nil, fmt.Errorf("screenstatemachine: reading the machines of %s: %w", serviceID, err)
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
		return nil, fmt.Errorf("screenstatemachine: reading the machines of %s: %w", serviceID, err)
	}

	superseded := make(map[string]bool, len(all))
	for _, m := range all {
		if m.Supersedes != "" {
			superseded[m.Supersedes] = true
		}
	}
	inForce := make([]Machine, 0, len(all))
	for _, m := range all {
		if !superseded[m.ID] {
			inForce = append(inForce, m)
		}
	}
	return inForce, nil
}

// scanner is what [pgx.Row] and [pgx.Rows] share, so one scan reads either.
type scanner interface {
	Scan(dest ...any) error
}

func scan(row scanner) (Machine, error) {
	var m Machine
	var kind, basis, transitions string
	if err := row.Scan(&m.ID, &kind, &m.Actor.Key, &basis, &m.At, &m.ServiceID, &m.SpecArtifactID, &m.ItemID,
		&m.Screen, &m.Supersedes, &m.Initial, &m.States, &m.Events, &transitions, &m.Terminal); err != nil {
		return Machine{}, err
	}
	m.Actor.Kind = record.Kind(kind)
	m.Actor.Basis = record.Basis(basis)
	if err := json.Unmarshal([]byte(transitions), &m.Transitions); err != nil {
		return Machine{}, fmt.Errorf("screenstatemachine: decoding the transitions of %s: %w", m.ID, err)
	}
	return m, nil
}
