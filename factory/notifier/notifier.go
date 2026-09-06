package notifier

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/people"
)

// PageEventKind is what a page event's payload says it is, so a reader can tell
// one from every other shape in the log without knowing this package's fields.
const PageEventKind = "page_event"

// PageEventFormatVersion is the format version every page event this package
// appends carries, which is what tells [decisionlog.Writer] it is a page event
// and not one of the log's other nine shapes.
const PageEventFormatVersion = "page_event/1"

// Payload is what a page event says. It names the wait, which per-person key
// was reached, which of the four events it is, and whose wait it was —
// everything the design asks a page event to name, and the routing beside
// it so a reader can see who was reached and why them.
type Payload struct {
	Kind     string `json:"kind"`
	Row      string `json:"row"`
	WaitKind string `json:"wait_kind"`
	Waiting  string `json:"waiting"`
	Event    string `json:"event"`
	// Reached is the per-person key the event names, never a name — the
	// mapping to one is People's alone, kept outside the chain.
	Reached string `json:"reached"`
	Holding string `json:"holding"`
	// At is set only on the page event the notifier appends to catch up the
	// drift detector's own delivery at the factory's next start: the
	// detector's own time for it, because the row's own append time is the
	// catch-up's and would otherwise say the process was down for less time
	// than it was.
	At string `json:"at,omitempty"`
}

// Notifier is the one notifier. It is composed with the owner's identifier,
// because a page widens to the owner and the design gives the owner no
// record — the identifier is not a People key, since there is no row to
// hold one.
type Notifier struct {
	pool      *pgxpool.Pool
	token     lease.Token
	log       *decisionlog.Writer
	reader    *decisionlog.Reader
	deliverer Deliverer
	owner     string
}

// New returns the notifier over pool, appending page events through log,
// reading the log back and the delivery record's writes fenced with token,
// delivering through deliverer, and widening to owner.
func New(pool *pgxpool.Pool, log *decisionlog.Writer, token lease.Token, deliverer Deliverer, owner string) (*Notifier, error) {
	if owner == "" {
		return nil, errors.New("notifier: the owner is who a page widens to, and this one names none")
	}
	if deliverer == nil {
		return nil, errors.New("notifier: a notifier with no channel to deliver on delivers nothing")
	}
	return &Notifier{
		pool: pool, token: token, log: log, reader: decisionlog.NewReader(pool, token),
		deliverer: deliverer, owner: owner,
	}, nil
}

// Notify delivers one wait: mail and chat to whoever the routing reaches, and —
// where the wait qualifies — a page to every one of them, with one reached event
// appended per human. The first delivery reaches every human holding the duty at
// once: there is no rotation naming which one it reaches first, because a rotation
// would be a declaration enforced by nothing and what a stale one does is
// what happens here without it.
//
// A wait naming a service whose paging hours it arrives outside skips the
// page channel — hours.go says why that alone does not deliver it later —
// and still reaches mail and chat at once. A [KindHarmMarkedReport] wait is
// refused with no delivery at all where the factory-wide settings' off
// switch is set, and past that service's cap its own page channel is skipped
// and one page per interval goes out instead, naming the service and how many
// marked intents arrived past the cap.
//
// A channel that refuses the send does not stop the channels after it. The
// page is the narrow channel that carries what mail and chat cannot, and a
// chat integration that stopped would otherwise suppress every page in the
// factory; each refusal is recorded on its own delivery record, every
// remaining channel is still attempted, and the refusals are returned
// together once the page has been tried.
//
// It returns the page events it appended, which is empty for a wait that does not
// qualify — that wait went out on mail and chat, and neither writes a page event,
// though both still write a delivery record.
func (n *Notifier) Notify(ctx context.Context, w Wait) ([]decisionlog.Row, error) {
	pages, err := prepare(w)
	if err != nil {
		return nil, err
	}
	overTheCap, past := false, 0
	if pages && w.Kind == KindHarmMarkedReport {
		off, err := harmMarkPagesOff(ctx, n.pool)
		if err != nil {
			return nil, err
		}
		if off {
			return nil, nil
		}
		if overTheCap, past, err = n.overHarmMarkCap(ctx, w, time.Now()); err != nil {
			return nil, err
		}
	}
	reach, err := n.routeTo(ctx, w)
	if err != nil {
		return nil, err
	}
	deferred := false
	if pages {
		deferred, err = n.deferredToHours(ctx, w, time.Now())
		if err != nil {
			return nil, err
		}
	}

	var appended []decisionlog.Row
	var refused []error
	for _, channel := range Channels {
		if channel == ChannelPage && (!pages || deferred || overTheCap) {
			continue
		}
		for _, human := range reach {
			event := Event("")
			if channel == ChannelPage {
				event = EventReached
			}
			row, err := n.deliver(ctx, Delivery{Channel: channel, To: human, Wait: w, Event: event})
			if err != nil {
				refused = append(refused, err)
				continue
			}
			if channel == ChannelPage {
				appended = append(appended, row)
			}
		}
	}
	if overTheCap {
		if err := n.pageOverTheCap(ctx, w, past); err != nil {
			refused = append(refused, err)
		}
	}
	return appended, errors.Join(refused...)
}

// Widen is the one widening: to the owner, once. A wait no page reached anybody
// about is [ErrNothingReached], a second widening is [ErrAlreadyWidened], and a
// wait a human has already acknowledged is [ErrAcknowledged].
//
// Nothing here decides that a page is unanswered. What the design gives the
// notifier is that it widens exactly once and to the owner; how long it waits first
// is its caller's, and at this milestone the caller is what drives the pass that
// notices.
func (n *Notifier) Widen(ctx context.Context, w Wait) (decisionlog.Row, error) {
	if _, err := prepare(w); err != nil {
		return decisionlog.Row{}, err
	}
	events, err := n.EventsFor(ctx, w.Row)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if err := reached(events, w.Row); err != nil {
		return decisionlog.Row{}, err
	}
	for _, e := range events {
		switch Event(e.Event) {
		case EventWidened:
			return decisionlog.Row{}, fmt.Errorf("%w: %s widened to %s", ErrAlreadyWidened, w.Row, e.Reached)
		case EventAcknowledged:
			return decisionlog.Row{}, fmt.Errorf("%w: %s", ErrAcknowledged, w.Row)
		}
	}
	return n.deliver(ctx, Delivery{Channel: ChannelPage, To: n.owner, Wait: w, Event: EventWidened})
}

// Acknowledge is a human saying they have the row: it stops only the
// widening. It fails with [ErrNothingReached] over a wait no page ever
// reached anybody about, and with [ErrAlreadyAnswered] over one already
// answered. by is the acknowledging human's per-person key.
//
// The same act on a row that is a decision appends an acknowledgement to
// the decision as well — [decisionlog.Writer.AppendDecisionAcknowledgement],
// which package gate calls — one act writing both; this package writes only
// its own half, the page-event side, because neither package imports the
// other.
func (n *Notifier) Acknowledge(ctx context.Context, w Wait, by string) (decisionlog.Row, error) {
	if _, err := prepare(w); err != nil {
		return decisionlog.Row{}, err
	}
	events, err := n.EventsFor(ctx, w.Row)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if err := reached(events, w.Row); err != nil {
		return decisionlog.Row{}, err
	}
	for _, e := range events {
		if Event(e.Event) == EventAnswered {
			return decisionlog.Row{}, fmt.Errorf("%w: %s", ErrAlreadyAnswered, w.Row)
		}
	}
	return n.deliver(ctx, Delivery{Channel: ChannelPage, To: by, Wait: w, Event: EventAcknowledged})
}

// Answered is written when the wait stops waiting, naming who ended it. Its
// caller is the component that ends the wait, at the same write it ends it with —
// except for a mismatch cleared inside the drift detector's own store, which calls
// nothing, so there the caller is whoever read that store and found it cleared.
func (n *Notifier) Answered(ctx context.Context, w Wait, by string) (decisionlog.Row, error) {
	if _, err := prepare(w); err != nil {
		return decisionlog.Row{}, err
	}
	if by == "" {
		by = n.owner
	}
	events, err := n.EventsFor(ctx, w.Row)
	if err != nil {
		return decisionlog.Row{}, err
	}
	if err := reached(events, w.Row); err != nil {
		return decisionlog.Row{}, err
	}
	return n.deliver(ctx, Delivery{Channel: ChannelPage, To: by, Wait: w, Event: EventAnswered})
}

// prepare validates the wait and answers whether it fires a page. Both calls are
// one method so that every entry point refuses the same waits.
func prepare(w Wait) (bool, error) {
	if err := w.validate(); err != nil {
		return false, err
	}
	return w.pages()
}

// routeTo is who the wait reaches: every per-person key holding its duty or
// its obligation, and the owner where it belongs to neither or where nobody
// holds it. A duty with no holder is a routing answer and not a missing
// one — the page reaches the owner, who is the person that would have
// written the row.
func (n *Notifier) routeTo(ctx context.Context, w Wait) ([]string, error) {
	if w.Holding == (people.Holding{}) {
		return []string{n.owner}, nil
	}
	holders, err := people.Holders(ctx, n.pool, w.Holding)
	if err != nil {
		return nil, err
	}
	if len(holders) == 0 {
		return []string{n.owner}, nil
	}
	return holders, nil
}

// deliver hands one delivery to the channel, records the delivery — on
// every channel, including a refused send — and, on the page channel where
// the transport accepted it, appends the event. The delivery record is
// written after the attempt because it names whether the transport
// accepted the send; the page event is written after the record for the
// same reason [Writer.deliver] always kept: the record says a page was
// delivered, so writing it before the delivery failed would say something
// that did not happen.
func (n *Notifier) deliver(ctx context.Context, d Delivery) (decisionlog.Row, error) {
	sendErr := n.deliverer.Deliver(ctx, d)
	if err := n.recordDelivery(ctx, d, sendErr == nil); err != nil {
		return decisionlog.Row{}, err
	}
	if sendErr != nil {
		return decisionlog.Row{}, fmt.Errorf("notifier: delivering %s about %s to %s: %w",
			d.Channel, d.Wait.Row, d.To, sendErr)
	}
	if d.Channel != ChannelPage {
		return decisionlog.Row{}, nil
	}

	holding := ""
	if d.Wait.Holding != (people.Holding{}) {
		holding = d.Wait.Holding.String()
	}
	payload, err := json.Marshal(Payload{
		Kind:     PageEventKind,
		Row:      d.Wait.Row,
		WaitKind: string(d.Wait.Kind),
		Waiting:  d.Wait.Waiting,
		Event:    string(d.Event),
		Reached:  d.To,
		Holding:  holding,
	})
	if err != nil {
		return decisionlog.Row{}, fmt.Errorf("notifier: marshalling the page event about %s: %w", d.Wait.Row, err)
	}
	return n.log.AppendPageEvent(ctx, decisionlog.Entry{
		Actor: Actor, Payload: string(payload), FormatVersion: PageEventFormatVersion,
	})
}

// EventsFor is the page events on one wait, in the order they were appended,
// which is what a page is: the name for the sequence of events on one row rather
// than a record of its own.
//
// A payload it cannot read is skipped rather than returned as an error, the way
// every other reader of this log treats one — a payload is unconstrained bytes by
// decisionlog's contract, so a page event in a shape this package does not know is
// one some other component wrote. It reads the whole log, which is what a page
// having no record of its own costs, and what would remove it is an index on a log
// this package does not own.
//
// It reads through the notifier's own [decisionlog.Reader], which appends a read
// event naming [Actor] as the principal before it answers.
func (n *Notifier) EventsFor(ctx context.Context, row string) ([]Payload, error) {
	rows, err := n.reader.Read(ctx, Actor)
	if err != nil {
		return nil, err
	}
	var events []Payload
	for _, r := range rows {
		if r.Shape != decisionlog.ShapePageEvent {
			continue
		}
		var payload Payload
		if err := json.Unmarshal([]byte(r.Payload), &payload); err != nil {
			continue
		}
		if payload.Kind == PageEventKind && payload.Row == row {
			events = append(events, payload)
		}
	}
	return events, nil
}

// reached refuses a widening, an acknowledgement or an answer over a wait no
// page ever started.
func reached(events []Payload, row string) error {
	for _, e := range events {
		if Event(e.Event) == EventReached {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrNothingReached, row)
}
