package main

import (
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/environment"
	"github.com/dulguun0225/borg/factory/gate"
	"github.com/dulguun0225/borg/factory/score"
	"github.com/dulguun0225/borg/factory/service"
)

// asked is one intent a run is given: the statement, and the services it changes
// in the order decomposition declares them, each item waiting on the one before it. One
// service is the ordinary case and several is what a contract migration is.
type asked struct {
	statement string
	services  []string
	// resumeIntentID names an intent already waiting to work rather than
	// taking a new one in, for the one caller that knows which: a named human
	// at Ops asking for the revert a rollback already raised. Package intent's
	// rewrite offers no statement-keyed lookup [path.take] could use in its
	// place — its own doc comment says so — so this is how this interface
	// gives it the id directly instead of matching the words. Empty for every
	// ordinary statement, which [path.take] takes in fresh.
	resumeIntentID string
}

// shipped is what the run did, which is the install's own records, one [decompositionSet]
// per intent, and one [candidate] per item. The run subcommand discards it; the
// end-to-end test asserts over it.
type shipped struct {
	// serviceID is the first service the run named, which is every single-service
	// run's only one; serviceIDs is all of them in that order.
	serviceID  string
	serviceIDs []string
	areaID     string
	// environmentID is production's, which is the record every gate row reads its
	// threshold from.
	environmentID  string
	decompositions []*decompositionSet
	candidates     []*candidate
}

// decompositionSet is one intent's set as decomposition produced it and the Decomposition row
// decided it. A decomposition that yielded one item has no firing here: the row fires where
// there is a set to ratify and nowhere else.
type decompositionSet struct {
	intentID string
	itemIDs  []string
	// fired is the Decomposition row's firing, and is empty where decomposition yielded
	// one item.
	fired fired
	// decided is whether the row fired at all, and approved whether it approved.
	decided  bool
	approved bool
	// reDecompositions is the intent's re-decomposition count after a rejection, which is what the
	// attempt limit is compared against.
	reDecompositions int
}

// candidate is one item followed as far as the run took it, each field filled as
// the record it names was written and empty from wherever that candidate stopped.
// A candidate is an item plus its build, which is why the two builds are here
// separately: the one the implementation stage made, and the one the queue's
// re-verification made from the candidate branch with master merged into it.
type candidate struct {
	intentID string
	itemID   string
	// svc is the service record this item changes, and repo is that service's
	// repository — the record's own field, so the run reads where the work is
	// rather than being told twice.
	svc            service.Service
	branch         string
	implArtifactID string
	// consumerContractArtifactID is the consumer contract version derived from the
	// same build, and is empty where the build declares nothing about another
	// service.
	consumerContractArtifactID string
	criterionIDs               []string
	buildID                    string
	commit                     string
	// measurement is the build's diff, taken where the repository is and handed to
	// every firing over that build. It is re-taken after the re-verification,
	// because that produced a different build against a master that had moved.
	measurement score.Measurement
	// waitsOn is the items decomposition declared this one waiting on, which is how a
	// consumer's item is ordered behind its producer's.
	waitsOn []string
	// requirementIDs is which of the intent's requirements this item answers,
	// written by decomposition; the crude interface derives one per intent, so
	// this is empty or one long today.
	requirementIDs []string

	// The candidate's own environment and what happened on it.
	environmentID     string
	environmentDir    string
	composedFrom      []environment.Composed
	candidateDeployID string
	criteria          []gate.CriterionResult
	tornDown          bool

	// The three firings, each as it was decided. The Decomposition row is not
	// among them: it decides a set and is on the [decompositionSet].
	candidateGate fired
	mergeGate     fired
	deployGate    fired

	// checked is what enforcement found about this candidate's contracts at its
	// merge row, and published is what the fast-forward wrote for each contract its
	// build declares.
	checked   *contractcheck.Checked
	published []contract.Published

	// factoryHold is the factory's own hold where one stopped the candidate before
	// its gate could fire, and holdWaitRow is the log row where that hold is
	// written — which is the substrate's and not the dependency's.
	factoryHold string
	holdWaitRow string

	// rejected is true where the human rejected at the merge row or at the
	// candidate deploy row. That stops this candidate and is not an error: a reject
	// is the gate working.
	rejected bool
	// autoRejected is true where a mechanical check rejected at the merge row
	// before a verdict was asked for, and autoRejectedBy names which.
	autoRejected   bool
	autoRejectedBy string
	// superseded is true where the Decomposition row rejected the set this item
	// was part of.
	superseded bool
	// held is true where the human held at a deploy row.
	held bool

	// What the queue did.
	queued            bool
	merged            bool
	reverifiedBuildID string
	reverifiedCommit  string
	releaseID         string
	releaseNumber     int64
	queueRejected     bool
	queueWhy          string
	queueWaitRow      string

	deployID string
	// windowID is the analysis window opened over the production deploy, and is empty
	// where none was — a rollback opens none, and neither does a redeploy of a release
	// already watched.
	windowID string
}
