package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/lease"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/secretref"
	"github.com/dulguun0225/borg/factory/targetseam"
)

// serviceRepo is one service this install knows: its name, and the git repository
// that is it. A service is one repository with one long-lived branch and no
// repository holds two, so this pair is the whole of what the interface has to be
// told before a decomposition can write the record.
//
// It is a list on [deps] rather than one name and one path because contracts need
// a second service: an interface has consumers, and the consumers are other
// services in the same factory.
type serviceRepo struct {
	name string
	repo string
}

// deps is everything the path composes, explicit so the end-to-end test
// drives the same code the run subcommand does — a fake model, scripted
// input, temp directories — with nothing swapped anywhere but here.
type deps struct {
	pool *pgxpool.Pool
	// token is the fencing token this process holds the lease with, per
	// ../../../end-goal/one-process.md: every writer the composition constructs, and
	// every reader that appends a read event, carries it.
	token     lease.Token
	model     agent.Model
	modelName string // the provider's model id, which is the author a per-author prior is kept on
	// modelCredentialName is the credential name the model was reached
	// through — model.openrouter or model.anthropic — carried so that every
	// agentrun record this run writes names the credential it was served on.
	modelCredentialName string
	// project is the name of the project this run installs and works in,
	// resolved to the record by [policy.Factory.Install].
	project string
	// targets is one target per environment. There is one per environment and not
	// one per install, because a candidate's environment is a place of its own.
	targets *targetSet
	// dir is production's target, and the directory each candidate environment's
	// own target is made under.
	dir        string
	credential secretref.Ref // the deploy credential, deploy.local
	in         io.Reader     // what the human answers on
	out        io.Writer
	human      string // the deciding human's name
	// services is every service this install knows, with the repository each is.
	// A statement names the services its intent changes, and a name not here is an
	// error rather than a service the run invents.
	services []serviceRepo
	area     string // the area's name, empty where the run names none
	// candidateCeiling is how many candidate environments this substrate has room
	// for at once. It is the factory's own infrastructure limit and not gate
	// policy: the design says of the condition it holds on that no parameter of an
	// owner's limits it. A candidate that meets it waits, and the wait is written
	// into the log because it is not a record and no gate fired.
	candidateCeiling int
	// driftdetector is the drift detector's own store, read and never written: the gate
	// asks it for a mismatch at the production deploy row, and the notifier reads
	// it for both ends of the page about one. It is nil where no drift detector is
	// installed, which is a factory whose every check reads a record the factory
	// wrote — the state the drift detector exists to remove.
	driftdetector *pgxpool.Pool
	// watchFor is how long a run watches its own windows before it leaves them open,
	// and watchEvery is how often it reads the quantity while it does. A window's
	// duration is measured and never set, so a run cannot know in advance how long to
	// wait: what it gives up on, `factory watch` continues.
	watchFor   time.Duration
	watchEvery time.Duration
	// draw is what the score's held-out sample is drawn from. It is nil in a run,
	// which is the runtime's own generator: the sample is one in ten of the firings
	// the score would have gated, and a run that composed a fixed draw would either
	// sample nothing or sample everything. A test composes one that answers a fixed
	// sequence, which is the only way a selection is assertable.
	draw score.Draw
}

// repoOf is the repository of the named service, and an error for a name this
// install was not told about. A run that could invent one would write a service
// record naming a directory nobody chose.
func (d deps) repoOf(name string) (string, error) {
	for _, s := range d.services {
		if s.name == name {
			return s.repo, nil
		}
	}
	return "", fmt.Errorf("factory: no service named %q is configured; this install knows %s",
		name, strings.Join(d.serviceNames(), ", "))
}

// serviceNames is every service this install knows, in the order it was told them.
// It is the order a run's queues are run in and the order its watches are, so two
// services' merges are ordered by configuration rather than by whichever map
// iteration came first.
func (d deps) serviceNames() []string {
	names := make([]string, 0, len(d.services))
	for _, s := range d.services {
		names = append(names, s.name)
	}
	return names
}

// targetSet is one [targetseam.Target] per environment, made on demand and kept.
// It was kept because what a local target had running was in its own memory, and
// that is no longer so — the target records what runs in its own directory, which is
// what let a second process read it. What the set is now is the set of directories
// this run has deployed into, which is what its caller stops in cleanup.
type targetSet struct {
	make func(dir string) targetseam.Target
	made map[string]targetseam.Target
}

// newTargetSet returns a set whose targets are made by make.
func newTargetSet(make func(dir string) targetseam.Target) *targetSet {
	return &targetSet{make: make, made: map[string]targetseam.Target{}}
}

// at is the target for one environment's directory.
func (s *targetSet) at(dir string) targetseam.Target {
	if made, ok := s.made[dir]; ok {
		return made
	}
	made := s.make(dir)
	s.made[dir] = made
	return made
}
