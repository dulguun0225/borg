# Components and what they call

[_Tight integration_](what-the-factory-does/01-tight-integration.md) states the rule this
file completes: every record in the graph has one writer, the single component that writes
it. [_Records and their writers_](records.md) holds a row per record naming that writer.
This file holds a row per component, and it is the only place the components are listed and
the only place a call from one to another is.

Three things follow from that list being absent. The set of components is derivable only by
collecting writers a record at a time, which omits every component that writes nothing: [the
gate component](how-the-factory-works/01-one-pipeline.md), [the
score](how-the-factory-works/04-risk-score/README.md), the way in that
[_Reports_](how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) ships inside every
deployed service, and the resolver that turns a credential's name into its value, which
[seam 3](deferred.md) names. The call edges fare no better. Each is stated on its own and as
a cost, a dependency edge from every [stage](how-the-factory-works/01-one-pipeline.md) back to
[dispatch](how-the-factory-works/02-intent-into-items/05-dispatch.md) and another from the far end
of [the pipeline](how-the-factory-works/01-one-pipeline.md) back to its start among them. No file
collects them, so whether they close a loop of calls is answerable only by a reader holding
all of them at once. And nothing says what a component added later may call.

Two rules, the shape [`terms.txt`](terms.txt) already has. A component does not exist until
it has a row here, and a call edge does not exist until the row of the component making the
call names it. Every writer [_Records and their writers_](records.md) names has a row here.

The table holds no reasons. Each component links to the section that defines it, which is
where the row is argued and where an edit to it goes.

| Component | What it is | What it may call |
|---|---|---|
| [intake](how-the-factory-works/02-intent-into-items/01-intake/README.md) | the one entrance an [intent](how-the-factory-works/02-intent-into-items/01-intake/README.md) is written through, whichever of the three sources raised it | [the notifier](how-the-factory-works/08-operations/07-pages.md), at each round of [the interview](how-the-factory-works/02-intent-into-items/02-the-interview.md) and at an intent escalated |
| [the report store](how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) | the store an end user's [report](how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) is written into, sized for what a population of end users sends, and the only thing that may remove one | nothing |
| the way in, which [_Reports_](how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) defines | the factory's own software shipped inside every deployed service, and the one entrance from outside the factory | the report store, which refuses a report over either of the two rates an owner authors on the [factory-wide settings record](how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md) |
| [decomposition](how-the-factory-works/02-intent-into-items/03-decomposition/README.md) | what turns a refined intent into [items](how-the-factory-works/01-one-pipeline.md) and writes a [service](how-the-factory-works/02-intent-into-items/03-decomposition/README.md)'s identity | intake, to record that an intent has been decomposed again; the gate component, at the [Decomposition](how-the-factory-works/03-gates/07-what-particular-gates-decide/01-decomposition.md) gate |
| [dispatch](how-the-factory-works/02-intent-into-items/05-dispatch.md) | the match of an item's stage against a [role](how-the-factory-works/01-one-pipeline.md) and of its service and [area](how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md) against a [scope](how-the-factory-works/01-one-pipeline.md), and what runs an [agent](how-the-factory-works/01-one-pipeline.md) | the notifier, at an item escalated; the resolver, for the credential the [fleet entry](how-the-factory-works/10-fleet/README.md) names; context assembly, at each dispatch and at each [evaluation set](how-the-factory-works/10-fleet/02-a-model-under-a-name.md) run |
| [context assembly](how-the-factory-works/01-one-pipeline.md) | what selects, at each dispatch, what the entry's model reads at once from the material the [stage](how-the-factory-works/01-one-pipeline.md) hands the agent, under the [selection rule](how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md) version in force, and writes the [input manifest](how-the-factory-works/01-one-pipeline.md) | nothing |
| [the gate component](how-the-factory-works/01-one-pipeline.md) | what fires every [gate](how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md), and the caller on every [decision](how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md) | [the log](deferred.md), at every [firing](how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md); the score, for the number and the [factor vector](how-the-factory-works/04-risk-score/01-factors-at-least.md) it decides on; the artifact store, where a human takes [Edit in place](how-the-factory-works/03-gates/03-actions-at-each-gate.md); the notifier, where a decision waits on a human |
| [the score](how-the-factory-works/04-risk-score/README.md) | what computes the risk of a decision, picks the [rollout strategy](how-the-factory-works/03-gates/02-the-rollout-strategy.md), and supplies every value an owner authored none for | the log, as the values it supplies move |
| [the artifact store](how-the-factory-works/01-one-pipeline.md) | the one writer of every [artifact version](how-the-factory-works/01-one-pipeline.md), so the writer is never the position its author occupies | the gate component, at the write that submits a version under decision |
| [the log](deferred.md) | the one writer of the append-only chain, so the chain is one implementation and one head | nothing |
| [the merge queue](how-the-factory-works/05-environments/03-the-merge-queue.md) | what orders [candidates](how-the-factory-works/06-releases/03-what-a-build-is-called-and-when.md) into [master](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md), the one long-lived branch, and mints [the release number](how-the-factory-works/06-releases/04-the-release-number.md), reading master from the version control system that holds it rather than from a record | the gate component, before a merge; the build runner, at a [re-verification](how-the-factory-works/05-environments/03-the-merge-queue.md); the log, for its [rejection](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md) and for [the backlog cap](how-the-factory-works/08-operations/03-overlapping-windows.md)'s stop |
| [the build runner](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md) | what performs every [build](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md), reaching no [deploy target](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md) and crossing [seam 4](deferred.md) never | nothing |
| [the deployer](how-the-factory-works/08-operations/09-the-deployer.md) | what composes and tears down [candidate environments](how-the-factory-works/05-environments/02-an-environment-per-candidate.md), performs every deploy, applies [a build's schema change](how-the-factory-works/06-releases/05-the-deploy-record/README.md), executes the rollout, performs a [mitigation](deferred.md) on a human's instruction, and writes [the deploy record](how-the-factory-works/06-releases/05-the-deploy-record/README.md) and the mitigation record | the gate component, before a deploy; a [deploy target](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md), through [seam 4](deferred.md); the resolver, for the credential the [environment record](how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md) holds; the log, for [a wait no gate could compute](how-the-factory-works/03-gates/04-what-a-gate-may-change.md) |
| [the health monitor](how-the-factory-works/08-operations/01-the-health-monitor.md) | what compares a [release](how-the-factory-works/06-releases/02-the-release-record.md) against a [control](how-the-factory-works/08-operations/01-the-health-monitor.md) and against the service's own recent history, and opens and closes [the analysis window](how-the-factory-works/08-operations/02-the-analysis-window.md) | the deployer, to tear a control down, to perform a [rollback](how-the-factory-works/06-releases/06-rollback.md), and to deploy each of [a search](how-the-factory-works/08-operations/03-overlapping-windows.md)'s builds; the build runner, to make each of a search's builds; intake, to raise what it found; the notifier, at a [failed](how-the-factory-works/08-operations/02-the-analysis-window.md) exit with no release to return to |
| the pass over the [constraints](how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) in force and the pass over the [advisory](how-the-factory-works/02-intent-into-items/01-intake/03-advisories.md) feed | the third and fourth [detectors](how-the-factory-works/02-intent-into-items/01-intake/README.md), the first two being the health monitor reading a service and reading a [consumer](how-the-factory-works/07-contracts/01-two-versioned-things.md) | intake, once per condition that matches |
| [the drift detector](how-the-factory-works/08-operations/08-drift-detection.md) | one process the owner installs beside the factory, comparing what runs against what the deploy record says and the log's chain against the head it recorded | nothing, which is why the notifier reads its store for itself |
| [the notifier](how-the-factory-works/08-operations/07-pages.md) | what delivers everything waiting on a human, on mail, chat and [the page](how-the-factory-works/08-operations/07-pages.md) | the log, for a [page event](how-the-factory-works/08-operations/07-pages.md) |
| the resolver [seam 3](deferred.md) names | what turns a credential's name into its value, for the models and the deploy targets alike | nothing |
| [the install's first-start step](how-the-factory-works/10-fleet/07-a-fleet-proposal.md) | what runs at the factory's first start on a fresh install, again at an upgrade's first start whenever what shipped changed, and at the factory's first start after its records are restored from a backup; it writes the [fleet proposal](how-the-factory-works/10-fleet/07-a-fleet-proposal.md) covering every role, at install and at an upgrade's first start that added a stage | the artifact store, to enter a shipped [role prompt, skill, or selection rule version](how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md) that changed; intake, to write a changed shipped constraint's [withdrawal-and-arrival pair](how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md); the log, for the [install event](deferred.md) it writes at every upgrade and every restore, and for a shipped [score version](how-the-factory-works/04-risk-score/01-factors-at-least.md) or [policy version](how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md) it appends at install and at an upgrade's first start |
| [Work, Ops, Factory and People](how-the-factory-works/11-screens/01-work-ops-factory-people.md) | the four screens, one client the factory builds and versions with itself | intake, from Work for an answer and an intent ended and from Ops for [a revert](how-the-factory-works/06-releases/06-rollback.md); dispatch, from Work for the priority and an item ended and from Ops for a revert item [a mark](how-the-factory-works/08-operations/03-overlapping-windows.md) made unnecessary; the notifier, from Ops where a human fires a page; the deployer, from Ops to perform or end a [mitigation](deferred.md); the gate component, from Factory at [a safeguard's withdrawal](how-the-factory-works/03-gates/07-what-particular-gates-decide/10-a-safeguards-withdrawal.md); the log, from Factory at each write an owner makes |

**A component calls another and never waits on a call that waits on it.** The dependency
edges the document names as costs close a loop in what depends on what. None of them closes
a loop in what waits on what. What returns to dispatch is a transition written onto the
item, what returns to intake is a new intent, and what an [agent](how-the-factory-works/01-one-pipeline.md)
authoring a stage gives the artifact store is a version. Every loop the table above holds is
of that kind, a record one component writes and another reads on a pass of its own. A call
edge that closes a loop of calls instead is a defect, because a component waiting on a
component waiting on it is a stop no gate,
[hold](how-the-factory-works/03-gates/04-what-a-gate-may-change.md), or
[window](how-the-factory-works/08-operations/02-the-analysis-window.md) can end, and this table is
what makes such an edge visible before it is built.

**The factory is one process, and [the drift
detector](how-the-factory-works/08-operations/08-drift-detection.md) is the second.** [_An
environment per
candidate_](how-the-factory-works/05-environments/02-an-environment-per-candidate.md) says so as
a fact about what an install provides, and it is the whole of the deployment model. Every
component above runs inside that process, with the two exceptions already stated: the way in
ships inside each deployed service, and the drift detector is installed beside the factory
so that it is not in it. Exactly one instance of the factory runs, and what enforces it is
the factory's own store. A starting process takes a lock there and holds it while it runs,
so a second fails to start rather than running beside the first. Two components make that necessary
rather than tidy. The merge queue reads master before it mints and the health monitor is the
only thing that closes a [window](how-the-factory-works/08-operations/02-the-analysis-window.md),
so two instances would mint one number twice and close one window twice, and no record
afterwards would tell either from one instance doing it once.

**One process is not one failure.** A component here is a pass or a loop of its own, and one
can stop or fail on one service's work while the rest go on. That is why [the health
monitor](how-the-factory-works/08-operations/01-the-health-monitor.md) writes a [last
check](how-the-factory-works/08-operations/08-drift-detection.md) per service rather than one for
itself, and why [the home
view](how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md) reads each of
those records against the interval that record carries rather than the newest of a class. What one process does
settle is the other end: the process stopping makes every last check stale at once, which is
one state and not four, and nothing inside the factory is left to read it.

**Every component's restart is a read of its own records.** The merge queue reads master at
every start and writes the release record its own unfinished merge left owing. [The
deployer](how-the-factory-works/08-operations/01-the-health-monitor.md) completes or returns the
[deploy records](how-the-factory-works/06-releases/05-the-deploy-record/README.md) no target has
finished. The health monitor's restart is the set of windows those same records left open: a
window opens when a deploy record is written and closes at exactly one of four exits, so a
start finds the open ones by reading records and never by keeping a list. It evaluates each
of those windows again. An [exit](how-the-factory-works/08-operations/02-the-analysis-window.md)
that a stop interrupted partway is finished by that second evaluation, because the close is
the exit's last step under [_Tight
integration_](what-the-factory-does/01-tight-integration.md)'s rule for an event that
writes more than one record. The notifier's
restart is the [delivery record](how-the-factory-works/08-operations/07-pages.md) it overwrites
per waiting row, so a row still waiting is delivered again and one that stopped waiting is
not. Factory's is the newest [policy
version](how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md) per scope, from
which it writes again any field an owner authored that does not already hold what that
version names.
Every other component holds nothing between calls and resumes by being called again.

**The factory's own store keeps a schema history too, the same shape [_The store is a contract too_](how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md) defines for a service's store.** It is one row per change, naming the version that shipped it, the change's identity, and a checksum of its text. A version's first start reads the history and refuses to start where it holds a change this version does not declare, or where this version declares a change the history cannot honour. The promise carries a number, and the number is one: a version reads what the version before it wrote, and a skipped version is not a supported upgrade. What it costs is a startup refusal an owner can hit on a bad upgrade. The upgrade is the [install event](deferred.md) the log records. Where a migration cannot honour the promise, the owner performs the migration as a one-way step, and the install-event row names that step.

An agent is not a component and has no row. It is a model in a role the factory runs, put on
an item by dispatch, and it calls as the stage it was dispatched to: the artifact store for
the version it authors, dispatch for the transition it reports. [The
grouper](how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) is an agent of that
kind before an intent exists, and calls intake.

What this costs is a second place to edit whenever a component gains a call, the cost
[_Records and their writers_](records.md) already states for a writer that moves, and one
more table an edit that adds a component has to keep true.
