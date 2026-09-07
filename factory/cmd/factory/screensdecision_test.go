// A verdict typed at a screen: the pending row read at its own two addresses,
// closed with the one field only a screen fills, and the subscription telling
// a client on the item that the record moved.
package main

import (
	"bufio"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/decisionlog"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/record"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestAVerdictAtAScreenCarriesWhenTheRowWasOpened is the second episode's own
// mechanism: a pass leaves the Spec row pending in Work, the row is read at
// GET /api/item/{id} and at GET /api/decision/{id}, and POST /api/call/decide
// closes it carrying when the actor opened it — the one field no caller has
// ever filled and which decisionlog has carried on every close event since the
// log's shapes were written.
//
// A client subscribed to the item's own address is told the record changed
// before it reads it again, which is what push not poll holds inside the
// product.
func TestAVerdictAtAScreenCarriesWhenTheRowWasOpened(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	s := newScreens(t, ctx, d, out)

	var taken shipped
	if err := s.p.takeIn(ctx, &taken, of(theStatement)); err != nil {
		t.Fatalf("taking the intent in: %v\n%s", err, out)
	}
	itemID := only(t, taken).itemID
	if _, err := s.p.advance(ctx); err != nil {
		t.Fatalf("the first pass: %v\n%s", err, out)
	}

	pending, err := s.p.gate.Pending(ctx)
	if err != nil {
		t.Fatalf("reading the pending rows: %v", err)
	}
	if len(pending) != 1 || pending[0].Gate.Kind != gate.KindSpec {
		t.Fatalf("the pass left %d row(s) pending, want the Spec row alone: %v\n%s", len(pending), pending, out)
	}
	openEventID := pending[0].Row.ID

	// The row on the item's own timeline, and at its own address.
	var view screens.Item
	s.get(t, "/api/item/"+itemID, &view)
	found := false
	for _, one := range view.Decisions {
		if one.OpenEventID != openEventID {
			continue
		}
		found = true
		if one.Verdict != "" || one.Score != nil {
			t.Errorf("the pending row carries verdict %q and a number, and both arrive with the close", one.Verdict)
		}
		if len(one.Vector) == 0 {
			t.Error("the pending row carries no vector, and the vector is what a human decides on")
		}
	}
	if !found {
		t.Fatalf("the item's timeline does not show the row pending on it: %+v", view.Decisions)
	}

	var decision screens.Decision
	s.get(t, "/api/decision/"+openEventID, &decision)
	if decision.ItemID != itemID || decision.Closed != nil || decision.Abandoned != nil {
		t.Errorf("the decision reads %+v, want the item's own row still pending", decision)
	}
	if decision.OpenedInWorkAt != "" {
		t.Error("the pending row already carries when it was opened in Work, and that arrives with the close")
	}

	// A client on the item's address, connected before the call.
	events := s.subscribe(t, "item", itemID)

	openedInWork := record.FormatTime(time.Now().Add(-90 * time.Second))
	s.mustCall(t, "decide", screens.DecideArgs{
		OpenEventID: openEventID, Verdict: string(gate.VerdictApprove),
		OpenedInWorkAt: openedInWork,
	})

	// The close event in the log carries it.
	closing := decisionlog.Row{}
	for _, row := range readLog(t, ctx, d) {
		if row.Part == decisionlog.PartClose && row.Closes == openEventID {
			closing = row
		}
	}
	if closing.ID == "" {
		t.Fatalf("the log holds no close event for %s", openEventID)
	}
	if closing.OpenedInWorkAt != openedInWork {
		t.Errorf("the close event carries opened_in_work_at %q, want the %q the screen sent",
			closing.OpenedInWorkAt, openedInWork)
	}
	if closing.Verdict != string(gate.VerdictApprove) {
		t.Errorf("the close event carries verdict %q, want approve", closing.Verdict)
	}

	// The decision's own address reads it back, and so does the timeline.
	s.get(t, "/api/decision/"+openEventID, &decision)
	if decision.Closed == nil || decision.Closed.Verdict != string(gate.VerdictApprove) {
		t.Fatalf("the decision reads %+v after the verdict, want it closed as approve", decision.Closed)
	}
	if decision.OpenedInWorkAt != openedInWork {
		t.Errorf("the decision reads opened_in_work_at %q, want %q", decision.OpenedInWorkAt, openedInWork)
	}

	// The subscription said so, before a human read it again.
	select {
	case event := <-events:
		if event != "changed" {
			t.Errorf("the stream sent %q, want a changed event", event)
		}
	case <-time.After(5 * time.Second):
		t.Error("the client subscribed to the item was not told the record changed")
	}

	// A second verdict over one opening is refused at the screen, before a
	// human has written into a row already decided.
	status, body := s.call(t, "decide", screens.DecideArgs{
		OpenEventID: openEventID, Verdict: string(gate.VerdictApprove),
	})
	if status == http.StatusNoContent {
		t.Error("a row already decided took a second verdict")
	} else if !strings.Contains(body, "already decided") {
		t.Errorf("the refusal reads %s, want it naming the row as already decided", body)
	}
}

// subscribe opens one server-sent-events connection on an address and answers
// with the names of the events it reads, until the test ends.
func (s *screenServer) subscribe(t *testing.T, kind, id string) <-chan string {
	t.Helper()
	request, err := http.NewRequest("GET", s.url+"/api/stream/"+kind+"/"+id, nil)
	if err != nil {
		t.Fatalf("building the subscription on %s/%s: %v", kind, id, err)
	}
	s.headers(request)
	answer, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("subscribing to %s/%s: %v", kind, id, err)
	}
	if answer.StatusCode != http.StatusOK {
		t.Fatalf("subscribing to %s/%s answered %d, want 200", kind, id, answer.StatusCode)
	}
	t.Cleanup(func() { answer.Body.Close() })
	events := make(chan string, 4)
	go func() {
		lines := bufio.NewScanner(answer.Body)
		for lines.Scan() {
			if name, is := strings.CutPrefix(lines.Text(), "event: "); is {
				select {
				case events <- name:
				default:
				}
			}
		}
	}()
	return events
}
