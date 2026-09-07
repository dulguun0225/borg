// The process the lease was always for: every component's pass reachable on its
// own, the one route it serves until the screens replace it, and the
// demonstration the resumable pass exists for — a row a pass leaves waiting in
// Work, closed by a human through the gate component, and the item moving on the
// next pass.
package main

import (
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dulguun0225/borg/factory/artifact"
	"github.com/dulguun0225/borg/factory/clientdist"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/screens"
)

// TestEveryPassRunsWithNothingToDo ticks each of the process's passes once
// against an install that has taken nothing in. Each is a component's own pass
// and a pass with nothing to do is not an error — which is what a process that
// starts before any intent arrives is.
func TestEveryPassRunsWithNothingToDo(t *testing.T) {
	ctx, d, out := newPath(t, "")
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	ps := newPasses(p, nil, nil)
	for _, name := range passOrder {
		moved, err := ps.Tick(ctx, name)
		if err != nil {
			t.Errorf("the %s pass with nothing to do: %v\noutput so far:\n%s", name, err, out)
		}
		// A pass with nothing to do announces nothing: every tick that
		// announced would have an idle install re-reading the home view for
		// ever, and every read of it appends a read event to the chained log.
		if moved {
			t.Errorf("the %s pass with nothing to do reports that it moved something:\n%s", name, out)
		}
	}
	if _, err := ps.Tick(ctx, "no-such-pass"); err == nil {
		t.Error("a name no pass has was run rather than refused")
	}
}

// TestAPassResumesAnItemAHumanDecided is the resumable pass demonstrated. One
// pass fires the Spec row, finds a human decides it, and leaves it pending in
// Work; nothing in the process closes it. A human closes it through
// [gate.Gate.Decide], which is the call a screen makes, and the next pass reads
// the item back out of the records and continues from that verdict — the
// implementation plan authored by a pass that never held the spec in memory.
func TestAPassResumesAnItemAHumanDecided(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	p, err := compose(ctx, d)
	if err != nil {
		t.Fatalf("composing the path: %v\n%s", err, out)
	}
	var s shipped
	if err := p.takeIn(ctx, &s, of(theStatement)); err != nil {
		t.Fatalf("taking the intent in: %v\n%s", err, out)
	}
	itemID := only(t, s).itemID

	ps := newPasses(p, nil, nil)
	if _, err := ps.Tick(ctx, passAdvance); err != nil {
		t.Fatalf("the first advance pass: %v\n%s", err, out)
	}
	pending, err := p.gate.Pending(ctx)
	if err != nil {
		t.Fatalf("reading the pending rows: %v", err)
	}
	if len(pending) != 1 || pending[0].Gate.Kind != gate.KindSpec {
		t.Fatalf("the pass left %d row(s) pending, want the Spec row alone: %v\n%s", len(pending), pending, out)
	}
	if !strings.Contains(out.String(), "Waiting in Work at "+gate.Spec.String()+" on "+itemID) {
		t.Errorf("the pass does not report the item waiting in Work where it asked for a verdict before:\n%s", out)
	}
	if _, found, err := artifact.NewestOfKind(ctx, d.pool, itemID, artifact.KindImplementationPlan); err != nil {
		t.Fatalf("reading the plan version: %v", err)
	} else if found {
		t.Fatal("the pass authored an implementation plan over a spec nobody had decided")
	}

	// A second pass with the row still pending performs nothing: the item is
	// waiting on a human and the process is not stuck, it is shown as stuck.
	before := out.Len()
	if moved, err := ps.Tick(ctx, passAdvance); err != nil {
		t.Fatalf("the second advance pass: %v\n%s", err, out)
	} else if moved {
		t.Error("a pass over an item waiting on a human announced a change")
	}
	if !strings.Contains(out.String()[before:], "Waiting in Work") {
		t.Errorf("a pass over an item waiting on a human does not say so:\n%s", out.String()[before:])
	}
	if p.moved {
		t.Error("a pass over an item waiting on a human moved something")
	}

	// The human at Work, through the gate component's own call — which is what
	// the screens will do and what this test does directly.
	acted, err := p.d.decide(ctx, p)
	if err != nil {
		t.Fatalf("deciding the Spec row at Work: %v\n%s", err, out)
	}
	if acted != 1 {
		t.Fatalf("the human acted on %d row(s), want the one pending", acted)
	}

	if moved, err := ps.Tick(ctx, passAdvance); err != nil {
		t.Fatalf("the pass after the verdict: %v\n%s", err, out)
	} else if !moved {
		t.Error("the pass after a human's verdict announced nothing")
	}
	if !p.moved {
		t.Error("the pass after a human's verdict moved nothing")
	}
	if _, found, err := artifact.NewestOfKind(ctx, d.pool, itemID, artifact.KindImplementationPlan); err != nil {
		t.Fatalf("reading the plan version: %v", err)
	} else if !found {
		t.Fatalf("the pass after the verdict did not continue the item to its implementation plan:\n%s", out)
	}
}

// TestHealthzAnswersWithTheFactoryVersion is the one route this process serves
// beside the four screens: it carries neither the factory version nor a
// principal, because it is what a reader outside the process reads before it
// has either.
func TestHealthzAnswersWithTheFactoryVersion(t *testing.T) {
	handler := served(screens.New(nil, nil, factoryVersion, clientdist.Browser()))
	recorded := httptest.NewRecorder()
	handler.ServeHTTP(recorded, httptest.NewRequest("GET", "/healthz", nil))
	if recorded.Code != 200 {
		t.Errorf("GET /healthz answered %d, want 200", recorded.Code)
	}
	if strings.TrimSpace(recorded.Body.String()) != factoryVersion {
		t.Errorf("GET /healthz answered %q, want the factory version %q", recorded.Body.String(), factoryVersion)
	}
	other := httptest.NewRecorder()
	handler.ServeHTTP(other, httptest.NewRequest("POST", "/healthz", nil))
	if other.Code == 200 {
		t.Error("POST /healthz was served, and the route is a read")
	}
}

// TestAPassAndTheScreensRunAtOnce is what the one process makes possible and
// no subcommand did: [passes.Run] advances the path on its own goroutine while
// package screens answers a view on another, both over the one composition. It
// is run under -race, where a map read beside a map write is a failure and not
// a flake — the three fields shared.go guards are reached from both sides.
func TestAPassAndTheScreensRunAtOnce(t *testing.T) {
	ctx, d, out := newPath(t, theAnswer+"\n"+approvals)
	screen := newScreens(t, ctx, d, out)
	var s shipped
	if err := screen.p.takeIn(ctx, &s, of(theStatement)); err != nil {
		t.Fatalf("taking the intent in: %v\n%s", err, out)
	}
	ps := newPasses(screen.p, nil, nil)

	passed := make(chan error, 1)
	go func() {
		for range 3 {
			if _, err := ps.Tick(ctx, passAdvance); err != nil {
				passed <- err
				return
			}
		}
		passed <- nil
	}()
	for range 20 {
		screen.get(t, "/api/home", nil)
		screen.get(t, "/api/work", nil)
	}
	if err := <-passed; err != nil {
		t.Fatalf("the advance pass beside the screens: %v\n%s", err, out)
	}
}

// TestAnIntervalOfZeroIsRefusedAtTheFlag: a ticker is made per pass at the
// start of the run and [time.NewTicker] panics on an interval of zero or less,
// so the flag that would set one is refused before the lease is taken and
// before anything reads the store.
func TestAnIntervalOfZeroIsRefusedAtTheFlag(t *testing.T) {
	for _, given := range []string{"0", "-5s"} {
		err := serveCommand([]string{
			"-secrets", "unread", "-model", "unread", "-targets", "unread",
			"-service", "one=/unread", "-every-" + passAdvance + "=" + given,
		})
		if err == nil {
			t.Fatalf("-every-%s=%s was accepted", passAdvance, given)
		}
		if !strings.Contains(err.Error(), "-every-"+passAdvance) {
			t.Errorf("the refusal of -every-%s=%s does not name the flag: %v", passAdvance, given, err)
		}
	}
}

// TestALeaseTakenStopsTheProcess is [renewals.after], which is what the
// renewal goroutine decides on: exactly one instance runs, so a renewal
// refused at the fence stops this one, and a renewal that failed for any other
// reason is retried for as long as the lease still stands.
func TestALeaseTakenStopsTheProcess(t *testing.T) {
	began := time.Now()
	unreachable := errors.New("the store is not reachable")

	var fenced renewals
	if err := fenced.after(fmt.Errorf("%w: current number 3, token 2", lease.ErrFenced), began); err == nil {
		t.Error("a renewal refused at the fence left the process running")
	} else if !errors.Is(err, lease.ErrFenced) {
		t.Errorf("the failure the process stops on does not carry the refusal: %v", err)
	}

	var failing renewals
	if err := failing.after(unreachable, began); err != nil {
		t.Errorf("the first failed renewal stopped the process, and the lease stands for %v yet: %v", leaseTTL, err)
	}
	if err := failing.after(unreachable, began.Add(leaseTTL-time.Second)); err != nil {
		t.Errorf("a renewal still failing inside the ttl stopped the process: %v", err)
	}
	if err := failing.after(nil, began.Add(leaseTTL-time.Second)); err != nil {
		t.Errorf("a renewal that landed stopped the process: %v", err)
	}
	// The renewal that landed cleared the failure, so the count starts again.
	if err := failing.after(unreachable, began.Add(leaseTTL)); err != nil {
		t.Errorf("the first failure after a renewal that landed stopped the process: %v", err)
	}
	if err := failing.after(unreachable, began.Add(2*leaseTTL)); err == nil {
		t.Error("renewals failing for longer than the ttl left the process running")
	}
}

// TestARefusedRenewalIsReadAsTheLeaseBeingTaken drives the same decision from
// package lease's own refusal rather than from an error this test wrote. The
// fixture already holds the lease every writer of this schema carries; it is
// released and taken by a second instance, which is the whole of what being
// fenced is — the token this process holds is no longer the lease's number.
func TestARefusedRenewalIsReadAsTheLeaseBeingTaken(t *testing.T) {
	ctx, d, _ := newPath(t, "")
	if err := lease.Release(ctx, d.pool, d.token); err != nil {
		t.Fatalf("releasing the lease the fixture holds: %v", err)
	}
	if _, err := lease.Acquire(ctx, d.pool, "another-instance", leaseTTL); err != nil {
		t.Fatalf("the second instance acquiring the released lease: %v", err)
	}
	var failing renewals
	if err := failing.after(lease.Renew(ctx, d.pool, d.token, leaseTTL), time.Now()); err == nil {
		t.Error("a renewal of a lease another instance holds left the process running")
	} else if !errors.Is(err, lease.ErrFenced) {
		t.Errorf("the failure the process stops on does not carry the refusal: %v", err)
	}
}
