package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// Every component's pass, each on its own interval, run by the one process
// [serveCommand] is. ../../../end-goal/one-process.md says a component here is
// a pass or a loop of its own and that one process is not one failure, so each
// pass's error is reported and the loop goes on: a pass that fails on one
// service's work does not stop the rest.

// The names a pass is known by: the flag that sets its interval is
// -every-<name>, and an error is reported against the same name. They are the
// component's own words and not this file's invention — the path's advance, the
// watch, the pending holds re-evaluated, the pages the paging hours held back,
// the drift sweep, the deprecation list, the acceptance rounds, the score's own
// pass over the outcomes, and the grouping of the reports that arrived.
const (
	passAdvance      = "advance"
	passWatch        = "watch"
	passReevaluate   = "reevaluate"
	passDeferred     = "deferred-pages"
	passDrift        = "drift-sweep"
	passDeprecations = "deprecations"
	passAcceptance   = "acceptance"
	passScore        = "score"
	passGrouper      = "grouper"
)

// The intervals each pass runs on where an owner sets none. They are this
// interface's own choice and not the design's: the design says each component is
// a pass of its own and leaves the numbers to whoever runs the process. The two
// that move an item — the path's advance, which carries the merge queue and the
// production deploys inside it — run oftenest; the readings that only report,
// the grouping of what arrived among them, run on the minute; and the two that
// walk every record of a class run on five.
var defaultIntervals = map[string]time.Duration{
	passAdvance:      5 * time.Second,
	passWatch:        30 * time.Second,
	passReevaluate:   10 * time.Second,
	passDeferred:     60 * time.Second,
	passDrift:        60 * time.Second,
	passDeprecations: 300 * time.Second,
	passAcceptance:   60 * time.Second,
	passScore:        300 * time.Second,
	passGrouper:      60 * time.Second,
}

// intervals is what each pass runs on, by name.
type intervals map[string]*time.Duration

// intervalFlags declares one -every-<name> flag per pass, so an owner running
// the process can set any of them without this file holding a second list.
func intervalFlags(flags *flag.FlagSet) intervals {
	set := intervals{}
	for _, name := range passOrder {
		set[name] = flags.Duration("every-"+name, defaultIntervals[name],
			"how often the "+name+" pass runs")
	}
	return set
}

// passOrder is the passes in the order they are declared and reported, which is
// the order the design lists the components in: the path first, then what
// follows a deploy, then what walks a class of record.
var passOrder = []string{
	passAdvance, passWatch, passReevaluate, passDeferred,
	passDrift, passDeprecations, passAcceptance, passScore, passGrouper,
}

// pass is one component's pass: the name its interval flag and its errors are
// reported under, how often it runs, and the call itself. The call reports
// whether the tick moved anything, which is what [passes.announce] answers on.
type pass struct {
	name  string
	every time.Duration
	do    func(context.Context) (bool, error)
}

// passes is every pass with a ticker of its own, run by one goroutine. Nothing
// here is locked: the tickers only say which pass is due, and the pass itself
// runs on the goroutine [passes.Run] holds, so no two passes touch the path at
// once. The composition they run over is reached from the HTTP handlers too,
// and what that needs is locked there — the three fields shared.go's accessors
// guard, and never a lock around a pass, which may spend minutes in one model
// call while a view waits.
type passes struct {
	p   *path
	out io.Writer
	// changed is what tells every subscriber on an address that a record it
	// renders changed. A pass writes records a screen is rendering, and a
	// screen already open learns that one moved from the subscription and never
	// from a reload — so every tick announces the addresses its pass could have
	// moved. It is nil where nothing serves, which is every subcommand.
	changed func(kind, id string)
	list    []pass
	// scoreSeen is the score version the score pass last found in force. That
	// pass appends a version only where the table it computes has moved, so a
	// tick answering a version this process has already seen wrote nothing.
	scoreSeen string
}

// newPasses composes the passes over one composition, at the intervals given.
func newPasses(p *path, every intervals, changed func(kind, id string)) *passes {
	ps := &passes{p: p, out: p.d.out, changed: changed, scoreSeen: p.scoreVersion}
	do := map[string]func(context.Context) (bool, error){
		passAdvance: func(ctx context.Context) (bool, error) {
			// The path's own pass, and with it the merge queue and the
			// production deploys: [path.layer] runs the queue once per service
			// and deploys what it merged, so the queue is not a pass of its
			// own — what orders merges is inside the pass that reaches them.
			a, err := p.advance(ctx)
			return a.moved, err
		},
		passScore: func(ctx context.Context) (bool, error) {
			version, err := p.ensureScore(ctx)
			moved := version != "" && version != ps.scoreSeen
			if moved {
				ps.scoreSeen = version
			}
			return moved, err
		},
		passWatch:        p.watchServices,
		passReevaluate:   p.reevaluatePending,
		passDeferred:     p.pagesHeldToTheHours,
		passDrift:        p.driftDetectorPages,
		passDeprecations: p.raiseRemovals,
		passAcceptance:   p.acceptancePass,
		passGrouper:      p.groupReports,
	}
	for _, name := range passOrder {
		interval := defaultIntervals[name]
		if set, given := every[name]; given && set != nil {
			interval = *set
		}
		ps.list = append(ps.list, pass{name: name, every: interval, do: do[name]})
	}
	return ps
}

// Run runs every pass on its own interval until ctx is done. One goroutine per
// ticker forwards the name of the pass that is due and does no work of its own;
// the pass runs on this goroutine, which is what keeps two of them from
// overlapping.
//
// A pass's error is reported and the loop goes on: one process is not one
// failure, and a component that fails on one service's work leaves the rest
// running.
func (ps *passes) Run(ctx context.Context) {
	due := make(chan string)
	for _, one := range ps.list {
		ticker := time.NewTicker(one.every)
		defer ticker.Stop()
		go func(name string, c <-chan time.Time) {
			for {
				select {
				case <-ctx.Done():
					return
				case <-c:
					select {
					case due <- name:
					case <-ctx.Done():
						return
					}
				}
			}
		}(one.name, ticker.C)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case name := <-due:
			moved, err := ps.Tick(ctx, name)
			if err != nil {
				fmt.Fprintf(ps.out, "The %s pass failed and the rest go on: %v\n", name, err)
			}
			if moved {
				ps.announce(name)
			}
		}
	}
}

// Tick runs one pass once, named. It is what [passes.Run] calls and what a test
// drives a pass with, so nothing about a pass is reachable only through a
// ticker.
func (ps *passes) Tick(ctx context.Context, name string) (bool, error) {
	for _, one := range ps.list {
		if one.name == name {
			return one.do(ctx)
		}
	}
	return false, fmt.Errorf("factory: %q is no pass of this process", name)
}

// announce tells every subscriber that the addresses this pass could have
// moved changed. The home view is announced after every tick, whatever the
// pass: the badge, the last check rows and the readiness reading are read from
// records every one of them writes. Beside it, each pass announces the lists it
// moves — the board for the two that move an item, and Ops for everything
// downstream of a deploy.
//
// It is announced after a tick that moved something and not after every tick.
// Every pass reports whether it wrote, and a subscriber told of a change that
// did not happen re-reads the address for nothing: a home view is read from
// records every pass writes, so an idle install with one screen open would
// re-read it every five seconds and append a read event to the chained log at
// each one, which is unbounded growth out of an idle factory. The drift sweep
// is the one pass with no cheaper signal and says so where it is written: it
// records the notifier's own last check every time it runs, and the home view
// renders that.
func (ps *passes) announce(name string) {
	if ps.changed == nil {
		return
	}
	ps.changed("home", listAddressID)
	switch name {
	case passAdvance, passReevaluate:
		ps.changed("work", listAddressID)
		ps.changed("ops", listAddressID)
	case passWatch, passDrift, passDeprecations:
		ps.changed("ops", listAddressID)
	case passAcceptance:
		ps.changed("work", listAddressID)
	case passScore, passDeferred:
		ps.changed("factory", listAddressID)
	case passGrouper:
		// An intent raised from reports is a timeline in Work, and the reports
		// still in no group are a number on Factory.
		ps.changed("work", listAddressID)
		ps.changed("factory", listAddressID)
	}
}

// listAddressID is the id every list address is addressed under: there is one
// of each, so an address needs no id of its own and a client sends "-" in its
// place.
const listAddressID = "-"

// watchServices is the watch over every service this install knows: one
// evaluation of everything downstream of a deploy per service, which is what
// closes an analysis window. It takes one reading per service and leaves what is
// still open to the next tick, the interval being what says how often a window
// is read.
func (p *path) watchServices(ctx context.Context) (bool, error) {
	moved := false
	for _, name := range p.d.serviceNames() {
		svc, found, err := service.ByName(ctx, p.d.pool, name)
		if err != nil {
			return moved, err
		}
		if !found {
			continue
		}
		watched, err := p.watchPass(ctx, svc)
		moved = moved || watched
		if err != nil {
			return moved, err
		}
	}
	return moved, nil
}

// reevaluatePending is the gate's own pass over every pending row a hold stands
// on, which is the fixed interval [gate.Gate.ReevaluatePending] was written for
// and which nothing called until this process existed: a hold the factory set
// holds the open row rather than firing the gate again, so what closes the row
// when the hold lifts is this.
func (p *path) reevaluatePending(ctx context.Context) (bool, error) {
	found, err := p.gate.ReevaluatePending(ctx)
	if err != nil {
		return false, err
	}
	for _, one := range found {
		switch {
		case one.Closed.ID != "":
			fmt.Fprintf(p.d.out, "A held row closed on its own: every hold lifted and the number is under the threshold; close event %s\n",
				one.Closed.ID)
		case one.WaitsOnAHuman:
			fmt.Fprintln(p.d.out, "A held row's holds have lifted and it waits on the human it names")
		default:
			fmt.Fprintf(p.d.out, "A held row goes on waiting: %v still stands\n", one.Holds)
		}
	}
	return len(found) > 0, nil
}

// acceptancePass is the acceptance round asked of every intent the records say
// is ready for one. The run subcommand asks it of the intents that run took in;
// a process that outlives a run has no such list, so the intents are read off
// the items — an intent ready for the round has items, all of them live.
func (p *path) acceptancePass(ctx context.Context) (bool, error) {
	ids, err := p.intentIDs(ctx)
	if err != nil {
		return false, err
	}
	sets := make([]*decompositionSet, 0, len(ids))
	for _, id := range ids {
		sets = append(sets, &decompositionSet{intentID: id})
	}
	return p.acceptanceRounds(ctx, sets)
}

// ensureScore is the score's own pass over the outcomes: it computes the
// supplied table from every outcome in the store and appends a version where it
// has moved, which is what `factory learn` does once. It answers with the
// version in force after it, so the caller can tell a tick that appended one
// from a tick that found the table where it left it.
//
// What it does not do is move the version this composition holds. The policy
// reader and the gate are composed with the version in force at the start, so a
// version this pass appends is read by the next start of the process and not by
// this one. What that costs is that a firing after the score has learned is
// decided under the version before it, until the process restarts.
func (p *path) ensureScore(ctx context.Context) (string, error) {
	version, err := score.NewWriter(p.d.pool, p.d.token, marksOf(p.d.pool)).Ensure(ctx, scoreActor)
	return version.ID, err
}
