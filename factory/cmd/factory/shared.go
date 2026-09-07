package main

import (
	"github.com/dulguun0225/borg/factory/service"
)

// The in-memory state a pass and an HTTP handler both reach, behind one lock.
// ../../../end-goal/one-process.md puts the passes and the screens in one
// process: [passes.Run] runs a pass on its own goroutine while the server
// answers a view or a call on another, and three fields of [path] are read and
// written from both — the service records read once per run, the candidate per
// item, and the log as the pass read it.
//
// The lock is on these accessors and never around a pass. A pass runs a model
// call and takes minutes over it, and a view answered between two passes is
// what the process exists for, so a lock held for the length of a pass would
// stop every screen.
//
// The candidate values byItem holds are not guarded and do not need to be: they
// are the pass's own, written all through a pass without a lock. A caller
// outside the pass takes [path.rehydrate], which reads one back out of the
// records and puts it in no map — so no *candidate is ever held by two
// goroutines.

// heldService is the service record already read for this id, and whether one
// was.
func (p *path) heldService(serviceID string) (service.Service, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	svc, found := p.serviceByID[serviceID]
	return svc, found
}

// keepService holds a service record for the steps that read it again. Two
// callers reading the same id write the same value, so the later write is not
// a conflict.
func (p *path) keepService(svc service.Service) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.serviceByID[svc.ID] = svc
}

// heldCandidate is the candidate this run holds for one item, or nil.
func (p *path) heldCandidate(itemID string) *candidate {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.byItem[itemID]
}

// holdCandidate holds c as the candidate of its item. Its callers are the two
// that create one — decomposition, and the run taking an intent in — where the
// item has just been written and nothing else can hold a candidate for it.
func (p *path) holdCandidate(c *candidate) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.byItem[c.itemID] = c
}

// refreshCandidate holds fresh for one item and answers the candidate now
// held: fresh itself where nothing was held, and the value already held,
// rewritten in place from fresh, where one was. Rewriting rather than
// replacing is what keeps the run reporting the same value the records just
// filled; doing both under one lock is what keeps a reader from finding half
// of one value and half of the other.
func (p *path) refreshCandidate(itemID string, fresh *candidate) *candidate {
	p.mu.Lock()
	defer p.mu.Unlock()
	held := p.byItem[itemID]
	if held == nil {
		p.byItem[itemID] = fresh
		return fresh
	}
	*held = *fresh
	return held
}

// heldLog is the log as the pass now running read it, or nil where no pass is
// running. The value is built whole before it is held and never written
// afterwards, so a caller may read it after the lock is given up.
func (p *path) heldLog() *read {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.logRead
}

// keepLog holds the log one pass read, and clears it with nil at the end of
// that pass.
func (p *path) keepLog(held *read) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.logRead = held
}
