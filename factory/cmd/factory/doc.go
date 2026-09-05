// Command factory is the crude interface: one binary on a terminal, standing in
// for the four screens that are not built yet.
//
// Twelve subcommands. "run" walks the whole path once — the install, intake,
// the interview, decomposition, Decomposition where decomposition yielded more
// than one item, the spec and implementation stages per item, the consumer
// contract derived from the same build, the build, the criteria in force
// checked in both directions, the Merge to master gate with the two contract
// checks, the fast-forward, the release and the contract versions it publishes,
// the Deploy to production gate, a deploy without a control, the watch, and the
// deprecation detector — stopping with the first error, and asking a human for
// a verdict at each row the score or a safeguard puts one at.
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
//   - main.go — the entry point, the switch on the subcommand name,
//     provider-to-model selection, the secrets and deploy-credential helpers,
//     the -service/-intent flag types, and runCommand/walkCommand, which
//     parse those two subcommands' flags.
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
//     serviceOf, subjectsFor, deployOrder, itemsInBuild, and inForceFor, which
//     the stages below read from the path it built.
//
// The path a run walks stage by stage:
//
//   - path.go — the component actors, the path struct (a run's collaborators,
//     and the deploy agent mergequeue and contractcheck reach through), and
//     run, which walks the whole path once per intent over the install's
//     dependency layers, plus layer, one dependency layer's own walk below
//     decomposition.
//   - decomposition.go — decomposeItems, one item per service an intent
//     changes, decompositionGate, the Decomposition row fired over the set
//     where it yielded more than one, and decompositionAttemptLimit, the limit
//     its re-decompositions are read against.
//   - authorintent.go — take, the intent a decomposition is authored from;
//     authorIntent, intake through the item stages for one intent; interview,
//     the one round or none with the spec author plus the confirming round
//     every requester owes; and defaultTier, the interview's own placeholder
//     for a tier value gate policy does not yet author.
//   - candidate.go — asked, shipped, decompositionSet, and candidate: the
//     run's own data shapes for one intent, what it did, one decomposition,
//     and one item's build in progress. asked.resumeIntentID names an intent
//     already waiting for the one caller that knows which, in place of the
//     statement-keyed lookup package intent's rewrite no longer offers.
//   - candidateenv.go — candidateEnvironment, the Deploy to candidate
//     environment row and composing and deploying to it; substrateWait and
//     SubstrateWaitKind, the wait a full substrate writes into the log; and
//     decideCriteria, checkEncodings, compositionFor, dependencyHold,
//     describeComposition, recordCriterionRun, and nextCriterionRun, which two
//     runs of the encodings are recorded as on a build's criterion results.
//   - author.go — specStage, implementationStage, and consumerContractStage,
//     the three authoring stages against the model; Publishes, Declares,
//     repoOfItem, the deploy agent's side of contractcheck; and writeManifest
//     and filesSize, the input manifest a dispatch writes before the agent
//     runs.
//   - repo.go — the git and filesystem operations a stage needs: masterHead,
//     compiles, buildInto, runEncodings, repoFiles, copyFile; and createBuild,
//     resolvedGoModules and readGoModule, the build record and its resolved
//     set of Go modules.
//   - measure.go — measure, the build's diff taken once at firing and handed
//     to the score, and the numstat parsing beneath it.
//   - attempt.go — stageAttempts and attempt, the per-stage attempt limit and
//     spend a call to the model is wrapped in; and recordAgentRun and
//     recordIntentRun, the agentrun record each call writes.
//   - gateio.go — fired, and the gate mechanics every row shares: report,
//     settle, reading a human's typed verdict, and recording what a firing
//     closed as.
//
// The merge queue and production deploy:
//
//   - merge.go — mergeGate, the Merge to master row, and enforceContracts,
//     the two contract checks it reads before a human decides.
//   - reverify.go — Reverify and FastForward, the deploy agent's two calls
//     the merge queue makes: re-verifying a candidate and landing it.
//   - queuerun.go — runQueue, running the merge queue once for a service and
//     tearing down what it merged; candidateFor, tearDown.
//   - productiondeploy.go — productionDeploy, the Deploy to production row
//     and its five factory holds; fireProduction, putOnProduction,
//     factoryHolds, windowHold, rollbackHold.
//
// The watch and its operations:
//
//   - watch.go — the health monitor's own seams (signalFiles, terminal,
//     RollBack) and its loop: watchTo, watchPass, reportReading,
//     driftDetectorPages, escalated, approveThrough; and Observed,
//     readExchange, raiseRemovals for contractcheck.
//   - ops.go — peopleCommand, watchCommand, approveCommand: the three
//     subcommands downstream of a deploy; and pathFlags/withPath, composing a
//     path for one of them with no model.
//
// The authoring subcommands:
//
//   - area.go — areaCommand, declaring an area inside -project unless -inside
//     names an area.
//   - authoring.go — owner, the actor an authoring write is made as, and
//     withPool, opening the database and applying the schema for the first
//     command an owner reaches.
//   - parameter.go — authorCommand, authoring one parameter on the record its
//     scope names, and authored, printing what was authored.
//   - safeguard.go — safeguardCommand, placing or withdrawing a safeguard, and
//     safeguardSubject, resolving -subject to what it binds — "gate_row:" is
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
// reaching a repository is the deploy agent's, [contractcheck.Exchanges] because
// observing a run is, and [healthmonitor.Rollbacker] because reaching a deploy target
// is.
//
// Every subcommand acquires the lease before it touches the store, whether it
// writes or only reads: a read still appends a read event, which is itself a
// write of the log, so the one-process rule holds for the crude interface's
// twelve subcommands and not only for "run". acquireLease in main.go takes it
// under this process's own instance — the machine's hostname and this
// process's id — starts a goroutine renewing it every third of its ttl for
// the life of the process, and returns
// the token every writer the composition constructs and every [decisionlog.Reader]
// carries. A held lease is a start failure, printed on stderr naming the
// holder, with a non-zero exit.
//
// What defines it: the crude interface in place of the four screens is
// ../../../roadmap.md#m1--one-change-ships; more than one service and the
// contract queries are ../../../roadmap.md#m5--contracts-bind-services.
package main
