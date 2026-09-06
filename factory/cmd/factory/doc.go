// Command factory is the command-line interface: one binary on a terminal, standing in
// for the four screens that are not built yet.
//
// Twenty-three subcommands. "run" walks the whole path once — the install,
// every component's restart, intake, the interview, decomposition,
// Decomposition where decomposition yielded more than one item, the four
// authoring stages per item with the gate row of each, the consumer contract
// derived from the same build, the build, the criteria in force checked in both
// directions, the Merge to master gate with the two contract checks, the
// fast-forward, the release and the contract versions it publishes, the Deploy
// to production gate, a deploy without a control, the watch, the acceptance
// round of every intent whose items are all live, and the deprecation detector —
// stopping with the first error, and asking a human for a verdict at each row
// the score or a safeguard puts one at.
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
// performing one of the class's two operations on a target on a human's instruction,
// and "-end" ends one standing. "truncate" is the decision log's retention pass,
// refused while a legal hold stands, where nothing is authored, and where the
// boundary is inside the value in force.
//
// Two end something for good. "retire <service>" is the owner's write of retired
// on the service record, which is the one thing that ends a service and what
// calls the deployer's removal; with "-environment" it performs that removal for
// one environment and writes nothing on the record, which is the step before an
// environment is withdrawn. "end-project" ends the project once every service in
// it is retired and withdraws production's environment for it in the same write.
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
// on "run", "author", "area" and "policy", names the project a subcommand works
// in and defaults to "default"; run creates it where it does not exist, in the
// same event as production's environment for it, and every other subcommand
// reads the project of that name and refuses where it does not exist.
// "safeguard" names a project as one of its subject kinds, "project:<name>",
// and takes no -project of its own.
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
//     records and the two versions in force; plus ownHistorySize and
//     ownHistoryRunLength, what the reading against a service's own recent past
//     is read at, and serviceOf, subjectsFor, deployOrder, itemsInBuild, and
//     inForceFor, which the stages below read from the path it built.
//   - install.go — installed, the install's three records — the factory-wide
//     settings record, the project, and production's environment for it —
//     created where deps says this composition installs and read and refused
//     where it does not; runsOnProduction, which authors production's addresses
//     on a service naming none; and serviceTargets, serviceAddresses and
//     addressesOf, the service's own set of an environment's targets, which is
//     what every reader of targets here reads.
//
// The path a run walks stage by stage:
//
//   - path.go — the component actors, the four authoring roles' actors, the
//     path struct (a run's collaborators, and the deployer mergequeue,
//     healthmonitor and contractcheck reach through), the seams it satisfies,
//     and run, which walks the whole path once per intent over the install's
//     dependency layers, plus layer, one dependency layer's own walk below
//     decomposition, and admissionOrder, which is the order dispatch admits
//     that layer's candidates in.
//   - seams.go — the values the composition supplies a component that decides
//     events: intentState, raisedByTheHealthMonitor, pagedFiring, the page a
//     firing that pages sends on its own row — a drift mismatch or a revert
//     decided while the rollback holds — with whoTheRowWaitsOn, the duty that
//     page routes on, gateNotifier, which is
//     how a gate reaches a human, dispatchNotifier, the wait an item escalated
//     leaves, and intakeNotifier, a round of the interview, an intent escalated,
//     and the acceptance round.
//   - holds.go — Standing, the factory's own holds at a deploy row, and the
//     three reads enforcement makes of a candidate's own store, each answering
//     with nothing because this platform's candidate environment has no store.
//   - marks.go — marks, the releases a named human at Ops marked as not caused
//     by the release, which the score and its learning pass exclude.
//   - withdrawals.go — withdrawals, what a spec version under decision removes,
//     which the score resolves the Spec row on: the criteria whose provenance
//     names an authority it withdraws and the screen state machines it
//     supersedes, with humanConfirmedSpecVersions, the versions a human decided
//     read off the log, which both queries take. A constraint-derived or
//     hazard-derived provenance resolves no named human here: who holds a duty
//     over one constraint or one area is a narrowing the People declaration does
//     not carry, so those rows route to the duty the row already names.
//   - strategysafeguard.go — strategySafeguard, the safeguard that keeps a
//     control: the write the production deploy row's fourth action makes, and
//     the read the score's strategy pick is made against.
//   - rollout.go — how a deploy is performed on this platform: the deployer's
//     principal, the targets in the environment's order, intoCandidate and
//     intoProduction, strategyOf, and adopt, the deployer's four fields on the
//     service record.
//   - decomposition.go — decomposeItems, one item per service an intent
//     changes with what each item answers written on its record, deriveShares
//     and shareOf, one item's share of a requirement the split spreads over
//     several, decompositionGate, the Decomposition row fired over the set
//     where it yielded more than one, and intentAttemptLimit, the limit each
//     of the intent's two counts is read against.
//   - setcompleteness.go — setRejection and derivedFrom, the Decomposition
//     row's two checks over what the set answers, which reject mechanically
//     before a human is asked and name which of gate.DecompositionChecks
//     rejected.
//   - specrejection.go — specRejection, the same thing per item at the Spec
//     row: the uncontrolled hazard, read from package criterion, and both
//     directions over the requirement a criterion names.
//   - authorintent.go — take, the intent a decomposition is authored from;
//     authorIntent, intake through the four item stages for one intent;
//     interview, the one round or none with the role put on the intent plus the
//     confirming round every requester owes, whose reading is what the intent's
//     requirements are written from; and defaultTier, the interview's own
//     placeholder for a tier value gate policy does not yet author.
//   - acceptance.go — acceptanceRounds with acceptanceQuestion and liveItems,
//     the round the run asks of every intent whose items are all live and the
//     delivery of one the factory raised; acceptCommand and outstandingRound,
//     the answer a requester gives at this terminal in place of the screen.
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
//     share, with the mechanical rejection its caller computed; and on,
//     specMaterial, refining and requirementFor, what a dispatch is given.
//   - author.go — implementationStage with startBranch, commitAndBuild and
//     hazardOf, and consumerContractStage; Publishes, Declares,
//     DeclaresSchemaChange, DeclaresBackfill, repoOfItem, the deployer's side of
//     contractcheck;
//     and filesSize and rolePromptCriteria, what a stage hands a role.
//   - backfill.go — declaresBackfill with backfillStore and backfillIn, the
//     pair a backfill item's checkout declares it copies between, and the file
//     name and directive that convention is, which DeclaresBackfill reads.
//   - fleet.go — oneModelFleet, the [dispatch.Fleet] this interface is composed
//     with; rolePrompts, the role prompt version in force per role;
//     shippedPromptFor and enterShippedPrompts, the install's first-start step
//     for what an agent is told; intentLimits, the [dispatch.Limits] that reads
//     a stage's limit through package policy and an intent's rounds through
//     intentAttemptLimit; and gateEscalation, which is what performs an
//     escalation dispatch decided.
//   - restart.go — restart, every component's restart run once by compose and
//     so by the subcommands that compose a path and by no other: the merge
//     queue's master read, the deployer's unfinished deploys, the
//     health monitor's open windows, the notifier's waiting rows, Factory's and
//     People's re-derivation from the newest policy version, and dispatch's
//     re-match of its open holds.
//   - repo.go — the git and filesystem operations a stage needs: masterHead,
//     compiles, buildInto, runEncodings, repoFiles, copyFile; and createBuild,
//     resolvedGoModules and readGoModule, the build record with its resolved
//     set of Go modules, the exposure list its runner derived, and whether its
//     checkout declares a schema change.
//   - measure.go — measure, the build's diff taken once at firing and handed
//     to the score, and the numstat parsing beneath it; destroysStoredData with
//     DestructiveStatements, the reading the reversibility factor resolves on,
//     which is a convention per toolchain the design names without describing —
//     a git that will not answer leaves it unavailable, which resolves that
//     factor; reaches, packagesOf, path.currentReleaseResolved and
//     declaresSchemaChange, the readings the build runner makes of its own
//     checkout — the exposure list package exposure derives from this build's
//     resolved set against the current release's build's, and whether the
//     checkout ships a schema change — and factorExposure and path.exposureOf,
//     which read that list off the build record and hand it to the score.
//   - authorship.go — authorship, the join package score reads what an agent
//     authoring a version worked from through: the artifact version names the
//     input manifest and the agent run of that manifest names the effort and the
//     versions of the role prompt and the skills.
//   - gateio.go — fired, and the gate mechanics every row shares: report,
//     settle — which offers refer, acknowledge and Edit in place beside the
//     row's own verdicts — editInPlace with authoredAtTheGate, who a version a
//     human typed there is recorded as, reading a human's typed verdict, and
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
//     factoryHolds, windowHold, rollbackHold, outstandingRevert and
//     revertWhileRollbackHolds, the one item the rollback's hold does not
//     reach.
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
//     TearDownKept, RollBack and DeploySearch, with artifactsOf, the digest a
//     rollback is verified against.
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
//     the log's retention pass. The acceptance round's own answer is
//     acceptance.go's acceptCommand.
//   - retirement.go — removeService, the [policy.Factory.Removal] this
//     composition supplies; retireCommand with path.retire and
//     path.removeFromEnvironment, the owner's write that ends a service and the
//     removal performed for one environment; endProjectCommand, the project
//     ended once every service in it is retired; and unmergedItemsNaming and
//     servicesWithACurrentRelease, the counts each write is refused on.
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
//     scope names; authored, printing what was authored; and writeShortening,
//     which is where a shorter decision-log retention value goes instead, that
//     one being decided at a row rather than authored.
//   - withdrawal.go — approveWithdrawal, the four rows outside every item a
//     human closes here: a safeguard's withdrawal, a halt's withdrawal, a legal
//     hold's withdrawal, and a shortening of decision-log retention. Each names
//     the record it decides, which is what one such row is pending per, and each
//     is routed away from the actor that record names. priorsRestartedBy is what
//     the shortening's row names beside it: the authors whose per-author prior
//     stands drifted and whose held-out decisions the cut would remove.
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
// reaching a repository is the deployer's, [contractcheck.Exchanges] and
// [contractcheck.StoreState] because observing a run and reading a candidate's
// own store are, [healthmonitor.Deployer] because reaching a deploy target is,
// and [gate.Holds] because computing the factory's own holds reads most of the
// graph. [contractcheck.Checkout] is also where the build's own reading of
// whether its checkout declares a schema change, and of whether it is a
// backfill, is answered — the first off the build record the run wrote it on.
//
// Every subcommand acquires the lease before it touches the store, whether it
// writes or only reads: a read still appends a read event, which is itself a
// write of the log, so the one-process rule holds for every subcommand
// and not only for "run". acquireLease in main.go applies
// package lease's own table — the one thing created before the lease, since a
// lease cannot be taken in a store whose lease table does not exist — takes the
// lease under this process's own instance, the machine's hostname and this
// process's id, starts a goroutine renewing it every third of its ttl for
// the life of the process, and returns
// the token every writer the composition constructs and every [decisionlog.Reader]
// carries. [postgres.Start] runs after that, under the lease: it reads the
// store's schema history, refuses to start against a store this version cannot
// read, and applies the rest of the schema. A held lease is a start failure,
// printed on stderr naming the holder, with a non-zero exit; the stop function
// each subcommand defers releases the lease, so the next one starts rather than
// waiting out the ttl.
//
// What is not built here: the fleet entry is not a record, so oneModelFleet
// answers for every role over the whole factory and no dispatch of this
// interface holds on a stage no entry covers. The gate every role prompt
// version fires is not fired, so a version an upgrade entered stays out of
// force with the install's in force below it. A refer at the Decomposition row
// is refused by the gate, [gate.Gate.Refer] re-firing through
// [gate.Gate.Fire], which decides one item and not a set. The Decomposition
// row's rejection over what the set answers is closed through [gate.Gate.Decide]
// as the gate component rather than [gate.Gate.AutoReject], which refuses a
// check name that row does not offer, and package gate offers none there. The
// mechanical
// rejection of a build whose emission does not count the area's hazardous
// operation is not built: the implementer is told the operation and nothing
// reads the count back off the build. [policy.Factory.WithdrawEnvironment] has
// no subcommand: production's is the only persistent environment this interface
// composes, and it is withdrawn with the project by "end-project".
//
// What defines it: the command-line interface in place of the four screens is
// ../../../roadmap.md#m1--one-change-ships; more than one service and the
// contract queries are ../../../roadmap.md#m5--contracts-bind-services. The
// path it composes is ../../../end-goal/how-the-factory-works/01-one-pipeline.md;
// the component that puts an agent on a stage is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md;
// every component's restart and the lease is ../../../end-goal/one-process.md;
// what ends a service, an environment, or a project is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md;
// the duties the subcommands give a way in to are
// ../../../end-goal/what-humans-do.md; and what each component may call is
// ../../../end-goal/components.md.
package main
