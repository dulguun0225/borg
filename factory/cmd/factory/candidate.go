package main

import (
	"github.com/dulguun0225/borg/factory/agent"
	"github.com/dulguun0225/borg/factory/contract"
	"github.com/dulguun0225/borg/factory/contractcheck"
	"github.com/dulguun0225/borg/factory/criterion"
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
	svc    service.Service
	branch string
	// specArtifactID, planArtifactID and tasksArtifactID are the versions the
	// three stages above implementation authored, and spec, plan and tasks the
	// text of each — what the stage below is handed, and what a reject hands
	// back to the stage that re-authors.
	specArtifactID  string
	planArtifactID  string
	tasksArtifactID string
	spec            string
	plan            string
	tasks           string
	// screens is the machines the spec introduced — the screen's id, its
	// declared states and its transitions — which the implementation stage
	// authors what drives the screen into each state, and the screen's own
	// transition function, against.
	screens        []agent.ScreenInForce
	implArtifactID string
	// compileFailure is the compiler's own words, trimmed to its first lines,
	// where the build the implementer wrote does not compile — and empty where
	// it does. It is what the Implementation row rejects on mechanically before
	// a verdict is asked for, in place of a build record: commitAndBuild sets it
	// and leaves buildID empty rather than stopping the run, and the stage
	// resets it at the top of every attempt so a later, compiling build does not
	// carry a stale reading into the gate.
	compileFailure string
	// consumerContractArtifactID is the consumer contract version derived from the
	// same build, and is empty where the build declares nothing about another
	// service.
	consumerContractArtifactID string
	criterionIDs               []string
	buildID                    string
	commit                     string
	// basedOnMaster is whether the item's branch was cut from master, which is
	// what the build's diff is taken against: a candidate decomposed before the
	// first release has no master and is diffed against the empty tree.
	basedOnMaster bool
	// measurement is the build's diff, taken where the repository is and handed to
	// every firing over that build. It is re-taken after the re-verification,
	// because that produced a different build against a master that had moved.
	measurement score.Measurement
	// waitsOn is the items decomposition declared this one waiting on, which is how a
	// consumer's item is ordered behind its producer's.
	waitsOn []string
	// requirementIDs is which of the intent's requirements this item answers,
	// written by decomposition; the command-line interface derives one per intent, so
	// this is empty or one long today.
	requirementIDs []string
	// statement is the intent's statement and requirements the requirements
	// this item answers, both as the spec author is told them: a criterion
	// names the requirement it answers, so the ids reach the role that writes
	// them.
	statement    string
	requirements []agent.Requirement
	// promised is the criteria the service already has in force, which the
	// spec author authors against — a criterion it restates would be a second
	// promise under a second id.
	promised []criterion.Criterion
	// constraints is the constraints in force the drafting stage holds, and
	// hazard the item's area where it is graded irreversible. They are what a
	// criterion's constraint-derived and hazard-derived provenance is written
	// from, and the stage names neither unless the role was told it.
	constraints []agent.Constraint
	hazard      agent.Hazard

	// The candidate's own environment and what happened on it.
	environmentID  string
	environmentDir string
	composedFrom   []environment.Composed
	// approvedComposition is what the environment was composed from at the run
	// that passed at Merge to master, kept beside composedFrom because the
	// re-verification recomposes and overwrites that one. Comparing the two is
	// the whole of how the queue tells its second reading of a failure from its
	// third: what changed between them is a release the author's work never saw.
	approvedComposition []environment.Composed
	candidateDeployID   string
	criteria            []gate.CriterionResult
	// encodingDefect is what [path.checkEncodings] found wrong with the build's
	// encodings against the criteria in force — a criterion with no encoding
	// naming it, an encoding naming a criterion not in force or withdrawn, or
	// one declaring no place or two — joined onto one line, and empty where the
	// check found nothing. encodingCouldNotDerive is whether the encodings
	// could not be derived at all, which is a different outcome from a defect:
	// it puts a human at the Merge to master row rather than rejecting.
	encodingDefect         string
	encodingCouldNotDerive bool
	tornDown               bool
	// buildHistory is every build this item's own criteria have been decided
	// against on the candidate environment, oldest first, appended once per
	// [path.decideCriteria] call. It is what a criterion's own outcome history
	// is read over: the design narrows that history to builds composed from
	// one seed version and whose diffs reach the requirement the criterion
	// names, and neither narrowing is read here yet, so this is every build of
	// this candidate rather than that filtered set — the caller [criterion.Unreliable]
	// asks for, kept the smallest way this run can supply it.
	buildHistory []string

	// The seven firings, each as it was decided. The Decomposition row is not
	// among them: it decides a set and is on the [decompositionSet].
	specGate           fired
	planGate           fired
	tasksGate          fired
	implementationGate fired
	candidateGate      fired
	mergeGate          fired
	deployGate         fired

	// checked is what enforcement found about this candidate's contracts at its
	// merge row, and published is what the fast-forward wrote for each contract its
	// build declares.
	checked   *contractcheck.Checked
	published []contract.Published

	// factoryHold is the factory's own hold where one stopped the candidate before
	// its gate could fire, and holdWaitRow is the log row where that hold is
	// written — which is the platform's and not the dependency's.
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
