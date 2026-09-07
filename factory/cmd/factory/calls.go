package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/principal"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// calls is [screens.Calls] over the composition: every write the four screens
// make, each reaching the writer of the record it changes, through the package
// that owns that record. One call is new — a page a human fires on their own
// judgment, which package notifier already defines and no component fires —
// and one field is: when the actor opened the row in Work, which no other
// caller can answer for.
//
// Every call is made as the calling principal's own key, with the basis
// claimed: seam 5 is where a principal is checked and this milestone attaches
// none, so the actor field on every record these calls write says the key was
// claimed and verified by nothing.
type calls struct {
	p *path
	v *views
	// server is what [screens.Server.Changed] is called on after a successful
	// write. It is set once, after [screens.New] has composed the server over
	// this value, and is nil where nothing serves — a test driving a call
	// directly notifies no subscriber and needs none.
	server *screens.Server
}

var _ screens.Calls = (*calls)(nil)

// ErrReadOnlyRow is the refusal every acting call at Work, Ops, Factory and
// People makes before any screen-specific check: a People row holding no duty,
// no obligation and lending no credential was added only so a human can read
// the four screens, and it reads all four and acts nowhere.
var ErrReadOnlyRow = errors.New("factory: this People row holds no duty, holds no obligation and lends no credential, so it reads the four screens and acts nowhere")

// acting is the actor an acting call is made as, and the one refusal every one
// of them makes first.
//
// The owner is the one key this reading exempts, and has to be: the owner is
// the person the design gives no record, every unheld row widens to them, and
// an install whose declaration is empty is what a fresh one is — so a reading
// that refused a key holding nothing would refuse the owner writing the first
// fleet entry, which is the first act the design puts at Factory. Every other
// key holding nothing is the read-only row and is refused.
//
// What that costs is that the owner this process was started as acts whether
// or not the declaration names them, so the row this refuses is a row somebody
// else's key was added for and never the owner's own.
func (c *calls) acting(ctx context.Context, who principal.Principal) (record.Actor, error) {
	if who.Actor.Key == "" {
		return record.Actor{}, fmt.Errorf("%w: the call names no principal", ErrReadOnlyRow)
	}
	if who.Actor.Key == c.p.human.Key {
		return record.Actor{Kind: record.KindHuman, Key: who.Actor.Key, Basis: record.BasisClaimed}, nil
	}
	acts, err := actsAnywhere(ctx, c.v, who.Actor.Key)
	if err != nil {
		return record.Actor{}, err
	}
	if !acts {
		return record.Actor{}, fmt.Errorf("%w: %s", ErrReadOnlyRow, who.Actor.Key)
	}
	return record.Actor{Kind: record.KindHuman, Key: who.Actor.Key, Basis: record.BasisClaimed}, nil
}

// changed tells every subscriber on one address that a record it renders
// changed. It is called after a successful write and never before: a
// subscriber told of a change that did not happen re-reads the address and
// finds it as it was, which is worse than not being told.
func (c *calls) changed(kind, id string) {
	if c.server == nil {
		return
	}
	c.server.Changed(kind, id)
}

// zoneNamed is the IANA zone a calendar value a human authors carries. Every
// reader of the value computes in that zone and in no other, so a zone the
// runtime cannot load is refused at the write rather than resolved to UTC by
// whoever reads it next.
func zoneNamed(zone string) error {
	if zone == "" {
		return errors.New("factory: a calendar value carries the IANA time zone it was authored in, and this one names none")
	}
	if _, err := time.LoadLocation(zone); err != nil {
		return fmt.Errorf("factory: %q is no IANA time zone: %w", zone, err)
	}
	return nil
}

// openRow is the open event a verdict is written against, re-read at the
// submit. A row a close event or an abandonment has reached since it was drawn
// is refused here, before a human has written into a row already decided or
// already ended — which is the refusal the log holds for a second close, met at
// the screen.
func (c *calls) openRow(ctx context.Context, who principal.Principal, openEventID string) (gate.Opened, error) {
	rows, err := decisionlog.NewReader(c.p.d.pool, c.p.d.token).Read(ctx, who)
	if err != nil {
		return gate.Opened{}, err
	}
	var opening decisionlog.Row
	found := false
	for _, row := range rows {
		if row.Shape != decisionlog.ShapeDecision {
			continue
		}
		if row.Part == decisionlog.PartOpen && row.ID == openEventID {
			opening, found = row, true
			continue
		}
		if row.Closes != openEventID {
			continue
		}
		switch row.Part {
		case decisionlog.PartClose:
			return gate.Opened{}, fmt.Errorf("row %s is already decided: close event %s carries %s",
				openEventID, row.ID, row.Verdict)
		case decisionlog.PartAbandonment:
			return gate.Opened{}, fmt.Errorf("row %s is abandoned and no verdict is coming: %s",
				openEventID, row.Reason)
		}
	}
	if !found {
		return gate.Opened{}, fmt.Errorf("%w: %s", screens.ErrNotFound, openEventID)
	}
	return gate.OpenedFrom(opening)
}

// afterADecision is the addresses one verdict moved: the decision itself, the
// item its timeline is on, and — through the rule the four screens share — the
// board and the home view.
func (c *calls) afterADecision(opened gate.Opened) {
	c.changed("decision", opened.Row.ID)
	if opened.Subject.ItemID != "" {
		c.changed("item", opened.Subject.ItemID)
		return
	}
	c.changed("factory", listAddressID)
}
