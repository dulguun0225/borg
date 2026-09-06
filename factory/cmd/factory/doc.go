// Command factory is the command-line interface: one binary on a terminal, standing in
// for the four screens that are not built yet.
//
// Twenty subcommands. "run" walks the whole path once — the install, every
// component's restart, intake, the interview, decomposition, Decomposition
// where decomposition yielded more than one item, the four authoring stages per
// item with the gate row of each, the consumer contract derived from the same
// build, the build, the criteria in force checked in both directions, the Merge
// to master gate with the two contract checks, the fast-forward, the release and
// the contract versions it publishes, the Deploy to production gate, a deploy
// without a control, the watch, and the deprecation detector — stopping with the
// first error, and asking a human for a verdict at each row the score or a
// safeguard puts one at.
//
// It knows more than one service. "-service name=path" is given once per
// service, and an intent that changes several names them before its statement —
// "svcA,svcB: what is wanted". Where an item waits on another, the run takes
// the layers in order, a consumer's environment being composed from its
// producer's current release.
//
// "walk <deploy-id>" follows the links from an existing deploy record back to
// its intent — every step a stored field, none reconstructed — and then prints
// every decision the item's gates left in the log. "watch <service>" is the
// health monitor alone, which is the one thing that closes a analysis window.
// "learn" is the score's own pass over the outcomes, printing what moved and
// what moved it. "approve <item-id>" is the emergency action at the production
// deploy row. "contracts" is every query contracts make: the contracts and
// their versions, the elements and their deprecation marks, the consumer
// contracts in force per service with the release range they were derived over,
// the deprecation list per marked element, and — with "-breaks <item-id>" —
// what one candidate would break and whom.
//
// Six more are what a human does to something already running. "rollback
// <service>" is duty 10: the deployer returns production to the release below
// the one running, with the human as the actor, and "-revert" raises the revert
// intent instead, which is the same duty once the build a rollback would return
// to is gone. "drop <item|intent>" ends work for good. "accept-commit <service>
// <commit>" is a human accepting a commit the queue did not make, which is what
// ends the stop that commit leaves. "mark-rollback <deploy-id>" is a named human
// at Ops saying a rollback was not caused by the release, which the score and
// its learning pass then exclude. "mitigate <deploy-id>" is the deployer
// performing one of the three operations on a target on a human's instruction,
// and "-end" ends one standing. "truncate" is the decision log's retention pass,
// refused while a legal hold stands.
//
// The other eight are duty 8, duty 9, the priority an owner reorders a queue
// with, and the People declaration a page routes on, none of which has a screen
// yet. "area" declares a grouping, inside the project -project names unless
// -inside names an area; "author" writes one parameter on the record its scope
// names; "safeguard" places a safeguard or withdraws one; "halt" sets the one
// authored record whose subject is the factory, or withdraws one; "legal-hold"
// sets a legal hold, or withdraws one; "policy" prints every parameter as it is
// in force, where its value came from, and what reads it; "priority" writes the
// one field that orders a queue; "people" declares who holds a duty. -project,
// on run/author/area/policy and safeguard's row-scoped subject, names the
// project a subcommand works in and defaults to "default"; run creates it
// where it does not exist, in the same event as production's environment for
// it, and every other one of these reads it and refuses where it does not.
//
// Every subcommand but "run" reads the services out of the store rather than taking
// a name and a repository: both are the service record's own fields, a flag naming
// one could disagree with the record, and a flag naming one service would leave a
// two-service install's other one unknown.
//
// # The files
//
// The entry point and dispatch:
//
//   - main.go — the entry point, chosen, which is the switch on the subcommand
//     name — not called dispatch, that being the component that puts an agent
//     on a stage —
//     provider-to-model selection, the secrets and deploy-credential helpers,
//     the lease this process holds, and runCommand/walkCommand, which parse
//     those two subcommands' flags.
//   - flags.go — serviceFlag and statements, the two repeated flags "run"
//     takes, with namesService beneath them.
//
// The run's composition and configuration:
//
//   - deps.go — serviceRepo and deps, everything a run composes, explicit so
//     the end-to-end test drives the same code the run subcommand does; and
//     targetSet, one target per environment.
//   - compose.go — compose, which builds the path from deps: the score
//     version ensured first, then the collaborators, the install's three
//     records — the factory-wide settings record, the project, and
//     production's environment for it — and the two versions in force; plus
//     runsOnProduction, which authors production's addresses on a service
//     naming none, ownHistorySize and ownHistoryRunLength, what the reading
//     against a service's own recent past is read at, and serviceOf,
//     subjectsFor, deployOrder, itemsInBuild, and inForceFor, which the stages
//     below read from the path it built.
//
// The path a run walks stage by stage:
//
//   - path.go — the component actors, the four authoring roles' actors, the
//     path struct (a run's collaborators, and the deployer mergequeue,
//     healthmonitor and contractcheck reach through), the seams it satisfies,
//     and run, which walks the whole path once per intent over the install's
//     dependency layers, plus layer, one dependency layer's own walk below
//     decomposition.
//   - seams.go — the three small values the composition supplies a component
//     that decides events: intentState, raisedByTheHealthMonitor, and
//     gateNotifier, which is how a gate reaches a human.
//   - holds.go — Standing, the factory's own holds at a deploy row, and the
//     four reads enforcement makes of a candidate's own store and of a
//     backfill's completion, each saying which records it cannot reach.
//   - marks.go — marks, the releases a named human at Ops marked as not caused
//     by the release, which the score and its learning pass exclude.
//   - rollout.go — how a deploy is performed on this platform: the deployer's
//     principal, the targets in the environment's order, intoCandidate and
//     intoProduction, strategyOf, and adopt, the deployer's four fields on the
//     service record.
//   - decomposition.go — decomposeItems, one item per service an intent
//     changes, decompositionGate, the Decomposition row fired over the set
//     where it yielded more than one, and decompositionAttemptLimit, the limit
//     its re-decompositions are read against.
//   - authorintent.go — take, the intent a decomposition is authored from;
//     authorIntent, intake through the four item stages for one intent;
//     interview, the one round or none with the spec author plus the confirming
//     round every requester owes, with sentencesOf, the criteria the intent's
//     requirements are written from; and defaultTier, the interview's own
//     placeholder for a tier value gate policy does not yet author.
//   - candidate.go — asked, shipped, decompositionSet, and candidate: the
//     run's own data shapes for one intent, what it did, one decomposition,
//     and one item's build in progress. asked.resumeIntentID names an intent
//     already waiting for the one caller that knows which, in place of the
//     statement-keyed lookup package intent's rewrite no longer offers.
//   - candidateenv.go — candidateEnvironment, the Deploy to candidate
//     environment row and composing and deploying to it; platformWait and
//     PlatformWaitKind, the wait a full platform writes into the log; and
//     decideCriteria, checkEncodings, compositionFor, dependencyHold,
//     describeComposition, recordCriterionRun, and nextCriterionRun, which two
//     runs of the encodings are recorded as on a build's criterion results.
//   - authorstages.go — specStage with submitSpec and screenMachine, planStage
//     and tasksStage: the three stages above the build, each dispatching its
//     role, submitting what it authored, firing its own gate row, and
//     re-authoring against a reject; itemGate, the firing the four item rows
//     share; and on, specMaterial, refining and requirementFor, what a dispatch
//     is given.
//   - author.go — implementationStage with startBranch, commitAndBuild and
//     hazardOf, and consumerContractStage; Publishes, Declares,
//     DeclaresSchemaChange, repoOfItem, the deployer's side of contractcheck;
//     and filesSize and rolePromptCriteria, what a stage hands a role.
//   - fleet.go — oneModelFleet, the [dispatch.Fleet] this interface is composed
//     with; rolePrompts, the role prompt version in force per role;
//     shippedPromptFor and enterShippedPrompts, the install's first-start step
//     for what an agent is told; and gateEscalation, which is what performs an
//     escalation dispatch decided.
//   - restart.go — restart, every component's restart run once at every start:
//     the merge queue's master read, the deployer's unfinished deploys, the
//     health monitor's open windows, the notifier's waiting rows, Factory's and
//     People's re-derivation from the newest policy version, and dispatch's
//     re-match of its open holds.
//   - repo.go — the git and filesystem operations a stage needs: masterHead,
//     compiles, buildInto, runEncodings, repoFiles, copyFile; and createBuild,
//     resolvedGoModules and readGoModule, the build record with its resolved
//     set of Go modules, the exposure list its runner derived, and whether its
//     checkout declares a schema change.
//   - measure.go — measure, the build's diff taken once at firing and handed
//     to the score, and the numstat parsing beneath it; reaches and
//     declaresSchemaChange, the two readings the build runner makes of its own
//     checkout — the exposure list package exposure derives, and whether the
//     checkout ships a schema change — and factorExposure and path.exposureOf,
//     which read that list off the build record and hand it to the score.
//   - gateio.go — fired, and the gate mechanics every row shares: report,
//     settle — which offers refer, acknowledge and Edit in place beside the
//     row's own verdicts — editInPlace, reading a human's typed verdict, and
//     recording what a firing closed as.
//
// The merge queue and production deploy:
//
//   - merge.go — mergeGate, the Merge to master row, and enforceContracts,
//     the two contract checks it reads before a human decides.
//   - reverify.go — the whole of [mergequeue.Repository]: Head and Holds, the
//     two readings of master; Reverify, which merges master and every candidate
//     ahead of this one before it builds; Confirm, the confirming run over the
//     criteria a re-verification failed; FastForward; and VerifyCommit, a commit
//     a human accepted.
//   - queuerun.go — runQueue, running the merge queue once for a service and
//     tearing down what it merged; candidateFor, tearDown.
//   - productiondeploy.go — productionDeploy, the Deploy to production row
//     and its five factory holds; fireProduction, putOnProduction,
//     factoryHolds, windowHold, rollbackHold.
//
// The watch and its operations:
//
//   - watch.go — the loop: watchTo, watchPass, reportWatched and reportAfter,
//     driftDetectorPages, escalated, approveThrough; terminal, where a delivery
//     goes; and Observed, readExchange, raiseRemovals for contractcheck.
//   - emission.go — signalFiles, [healthmonitor.Emission] over the file each
//     deployed process writes; the two emission versions the factory has
//     shipped and the interval resolution the second is cut by; readSignal,
//     emitted.intervals and paired beneath them; and the two readings this
//     platform cannot give.
//   - rollback.go — [healthmonitor.Deployer]: StartControl, TearDownControl,
//     RollBack and DeploySearch, with artifactsOf, the digest a rollback is
//     verified against.
//   - ops.go — peopleCommand, watchCommand, approveCommand: the three
//     subcommands downstream of a deploy; and pathFlags/withPath, composing a
//     path for one of them with no model.
//
// What a human does to something already running:
//
//   - undo.go — rollbackCommand with rollBackNow and revertIntent, which is
//     duty 10 in its two forms, and markRollbackCommand, the mark that a
//     rollback was not caused by the release.
//   - ending.go — dropCommand, acceptCommitCommand, mitigateCommand and
//     truncateCommand: an item or an intent ended for good, a commit the queue
//     did not make accepted, the deployer acting on a human's instruction, and
//     the log's retention pass.
//
// The authoring subcommands:
//
//   - area.go — areaCommand, declaring an area inside -project unless -inside
//     names an area.
//   - authoring.go — humanNamed, which resolves -human's name to the
//     per-person key the People mapping gives it and mints one where the name
//     is new, and withPool, opening the database and applying the schema for
//     the first command an owner reaches.
//   - parameter.go — authorCommand, authoring one parameter on the record its
//     scope names, and authored, printing what was authored.
//   - withdrawal.go — approveWithdrawal, the three rows outside every item a
//     human closes here: a safeguard's withdrawal, a halt's withdrawal, and a
//     shortening of decision-log retention.
//   - safeguard.go — safeguardCommand, placing a safeguard or writing its
//     withdrawal, and safeguardSubject, resolving -subject to what it binds — "gate_row:" is
//     drawn on -service, keyed by the row, because package policy's own reader
//     keys a row-scoped safeguard that way.
//   - halt.go — haltCommand, setting the one authored record whose subject is
//     the factory, or withdrawing one, writing directly through package halt's
//     own writer, package policy not importing it yet.
//   - legalhold.go — legalHoldCommand, setting a legal hold or withdrawing
//     one, the same way, and legalHoldSubject, resolving -subject.
//   - priority.go — priorityCommand, writing the one field that orders a
//     queue.
//   - namedsubject.go — namedService, namedProject and namedArea, resolving a
//     -service/-project/-area flag to its record by name, shared by author,
//     area, safeguard, and policy below.
//
// The reads and reporting:
//
//   - policy.go — policyCommand, printing every parameter as it is in force,
//     where its value came from, the safeguards that reached it, and what
//     reads it.
//   - contracts.go — contractsCommand, every query contracts make;
//     printContracts and printBreaks, what one candidate would break and
//     whom.
//   - learn.go — learnCommand, the score's own pass over the outcomes,
//     printing what moved and what moved it; printHeldOut.
//   - walk.go — walk, following the links from a deploy record back to its
//     intent and printing every decision the item's gates left in the log.
//
// The tests: fixtures_test.go, fakemodel_test.go, and the three files named
// <subject>fixtures_test.go (authoringfixtures_test.go,
// contractsfixtures_test.go, watchfixtures_test.go) hold the fixtures every
// other test shares. The rest are one subject each. Four keep the name they
// were written under — main_test.go the end-to-end demonstration, watch_test.go
// the bad deploy rolled back, contracts_test.go the two-service pair,
// authoring_test.go the author subcommand — and every other one is named for
// its subject.
//
// Who may write what: nothing of its own. Every record the run causes to
// exist is written by the package that owns it; this command composes the
// writers and holds no table, and every read goes through the owning
// package's readers. What it implements for two components is a seam rather than a
// record: it is [mergequeue.Repository] and [contractcheck.Checkout] because
// reaching a repository is the deployer's, [contractcheck.Exchanges],
// [contractcheck.StoreState] and [contractcheck.Backfills] because observing a
// run and reading a candidate's own store are, [healthmonitor.Deployer] because
// reaching a deploy target is, and [gate.Holds] because computing the factory's
// own holds reads most of the graph. [contractcheck.Checkout] is also where the
// build's own reading of whether its checkout declares a schema change is
// answered, read off the build record the run wrote it on.
//
// Every subcommand acquires the lease before it touches the store, whether it
// writes or only reads: a read still appends a read event, which is itself a
// write of the log, so the one-process rule holds for the command-line interface's
// twelve subcommands and not only for "run". acquireLease in main.go takes it
// under this process's own instance — the machine's hostname and this
// process's id — starts a goroutine renewing it every third of its ttl for
// the life of the process, and returns
// the token every writer the composition constructs and every [decisionlog.Reader]
// carries. A held lease is a start failure, printed on stderr naming the
// holder, with a non-zero exit.
//
// What is not built here: the fleet entry is not a record, so oneModelFleet
// answers for every role over the whole factory and no dispatch of this
// interface holds on a stage no entry covers. The gate every role prompt
// version fires is not fired, so a version an upgrade entered stays out of
// force with the install's in force below it. A refer at the Decomposition row
// is refused by the gate, [gate.Gate.Refer] re-firing through
// [gate.Gate.Fire], which decides one item and not a set. The mechanical
// rejection of a build whose emission does not count the area's hazardous
// operation is not built: the implementer is told the operation and nothing
// reads the count back off the build.
//
// What defines it: the command-line interface in place of the four screens is
// ../../../roadmap.md#m1--one-change-ships; more than one service and the
// contract queries are ../../../roadmap.md#m5--contracts-bind-services. The
// path it composes is ../../../end-goal/how-the-factory-works/01-one-pipeline.md;
// the component that puts an agent on a stage is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md;
// every component's restart and the lease is ../../../end-goal/one-process.md;
// the duties the subcommands give a way in to are
// ../../../end-goal/what-humans-do.md; and what each component may call is
// ../../../end-goal/components.md.
package main
