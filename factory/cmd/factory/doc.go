// Command factory is the composition behind the four screens, and the
// command-line interface beside them: one binary that serves the screens and
// answers a terminal.
//
// "serve" is the process ../../../end-goal/one-process.md has described since M0
// and nothing has been: it acquires the lease, holds it for its own life, runs
// each component's pass on its own interval, and serves the four screens over
// HTTP between them, where every other subcommand acquires the lease, makes one
// pass, and exits. It takes "-port" (8080 by default), the composition flags
// "run" takes, and one "-every-<name>" per pass; it exits on SIGINT or SIGTERM.
// passes.go is the passes and their intervals.
//
// # The four screens
//
// What "serve" serves is package screens' own handler over two interfaces this
// package implements, with the client's build output embedded beside them.
// [views] is [screens.Views], every read the four screens make mapped from the
// records onto the view types that package serves; [calls] is [screens.Calls],
// every write, each reaching the writer of the record it changes. Package
// screens imports no record package, what crosses that seam being the screen's
// own view of a record and never the record, so both mappings are here.
//
// One call is new and one field is. The call is a page a human fires on their
// own judgment, [calls.FirePage], which package notifier already defines. The
// field is [gate.Given.OpenedInWorkAt] — when the actor opened the row in Work,
// which decisionlog has carried on every close event since the log's shapes
// were written and which nothing else has ever filled.
//
// Two refusals are every call's, before any screen-specific check.
// [calls.acting] refuses the read-only row — a People row holding no duty, no
// obligation and lending no credential — and says why the owner's own key is
// the one it exempts. [calls.openRow] re-reads the open event at the submit and
// refuses a row a close event or an abandonment has reached since it was drawn,
// which is the refusal the log holds for a second close, met at the screen.
//
// After every successful write the call tells every subscriber on the
// addresses it touched, through [screens.Server.Changed]; every tick of every
// pass announces the home view and the lists that pass could have moved, which
// is what makes push not poll hold inside the product.
//
// # The subcommands
//
// Eight, and what each is for is in ../../README.md's own runbook, which is the
// map the code rules require to ship with the code; what is here is what the
// shape of one costs. Every one of them is a pass or a read: "serve" above,
// "run", "walk", "watch", "learn", "contracts", "policy" and "truncate". Every
// write a human makes is at a screen, through the call in calls.go that reaches
// the same writer, so what a subcommand acted on — an item, an intent, a
// service, a project, a record, the People declaration — is an address a human
// can be on instead.
//
// "run" walks the whole path once — the install, every component's restart, the
// intent stage, the four authoring stages per item with the gate row of each,
// the build and the contracts derived from it, the merge, the release, the
// production deploy, the watch, the acceptance rounds, and the deprecation
// detector — stopping with the first error.
//
// It asks for no verdict. Where a firing puts a human at the row the pass
// writes nothing, the row stays pending in Work, and the run reports the item
// waiting there — so an install with no screen open is not stuck but shown as
// stuck, which is what ../../../roadmap.md#m8--the-screens-and-the-fleet
// requires of this subcommand. What continues such an item is the next pass: a
// run is [path.takeIn] and then [path.advance] repeated until nothing moves,
// and each pass reads every live item back out of the records rather than out
// of memory. resume.go's own comment sets out that reading, what has no record
// behind it, and what it costs; rehydrate.go is the read.
//
// It asks nothing at all, in fact: standard input is read by nothing here, and
// a round of the interview is answered by "-answer" where the run names one and
// waits in Work where it does not.
//
// It knows more than one service. "-service name=path" is given once per
// service, and an intent that changes several names them before its statement —
// "svcA,svcB: what is wanted", which is also what the statement carries on the
// record, the intent record holding no service. Where an item waits on another,
// the run takes the layers in order, a consumer's environment being composed
// from its producer's current release.
//
// Every subcommand but "run" and "serve" reads the services out of the store
// rather than taking a name and a repository: both are the service record's own
// fields, a flag naming a repository could disagree with the record, and a flag
// naming one service would leave a two-service install's other one unknown.
//
// -project, on "run", "serve" and "policy", names the project a subcommand
// works in and defaults to "default"; the first two create it where it does not
// exist, in the same event as production's environment for it, and "policy"
// refuses where it does not. Every project after that one is written at
// Factory, through [calls.CreateProject].
//
// # The files
//
// The entry point and dispatch:
//
//   - main.go — the entry point, chosen, which is the switch on the subcommand
//     name — not called dispatch, that being the component that puts an agent
//     on a stage —
//     provider-to-model selection, the secrets and deploy-credential helpers,
//     and runCommand/walkCommand, which parse those two subcommands' flags.
//   - serve.go — serveCommand, the process: the pool, the lease held for its
//     life, the schema, the composition, the views and the calls over it, the
//     handler [screens.New] composes over those with the client's embedded
//     build, the passes, and the HTTP server; served, that handler plus GET
//     /healthz; and shutDown.
//   - views.go, viewwork.go, viewops.go, viewfactory.go, viewnumbers.go,
//     viewpeople.go — [views], one file per screen.
//   - calls.go, callswork.go, callsops.go, callsfactory.go, callspeople.go —
//     [calls], one file per screen, over the two refusals in calls.go.
//   - recordrows.go — the five rows that decide a record rather than an item,
//     every one of them decided at Factory: decideOutsideEveryItemAt for the
//     four withdrawals and shortenings, and decideRolePrompt with
//     fireRolePromptRow for the row every version of what an agent is told
//     fires. decideOutsideEveryItem decides a row already open on the record
//     rather than firing a second one, one gate on one subject having at most
//     one pending row — so a verdict the gate turned away is not the last one
//     the row can take; alreadyOpen is that read.
//   - interview.go — the confirming round as two halves in two passes: the
//     format the pass states the reading in and the round at Work reads back,
//     and servicesFor.
//   - passes.go — pass and passes with newPasses, Run and Tick: one ticker per
//     component's pass and one goroutine running them, so no two run at once
//     and the path's state needs no lock; the eight intervals and the
//     "-every-<name>" flag per pass; announce, which tells every subscriber the
//     addresses a tick could have moved; and the four passes that exist only
//     here — watchServices, reevaluatePending (the first caller
//     [gate.Gate.ReevaluatePending] has ever had), acceptancePass and
//     ensureScore.
//   - lease.go — leaseTTL, leaseRenewEvery, defaultInstance and acquireLease,
//     which every subcommand calls before it touches the store and which
//     "serve" holds for the life of the process.
//   - flags.go — serviceFlag and statements, the two repeated flags "run" and
//     "serve" take, with namesService beneath them. statements is also what the
//     intent stage reads an intent's services back off its statement with, the
//     intent record holding no service.
//
// The run's composition and configuration:
//
//   - deps.go — serviceRepo and deps, everything a run composes, explicit so
//     a test drives the same code a subcommand does; and targetSet, one target
//     per environment.
//   - compose.go — compose, which builds the path from deps: the score version
//     ensured first, then the collaborators, the install's three records and
//     the two versions in force; ownHistorySize and ownHistoryRunLength, what
//     the reading against a service's own recent past is read at; and
//     serviceOf, subjectsFor, deployOrder, itemsInBuild and inForceFor.
//   - install.go — installed, the install's three records — the factory-wide
//     settings record, the project, and production's environment for it —
//     created where deps says this composition installs and refused where it
//     does not; runsOnProduction, which authors production's addresses on a
//     service naming none; and serviceTargets, serviceAddresses and
//     addressesOf, the addresses every read of what is running is performed
//     against.
//
// The path a run walks stage by stage:
//
//   - path.go — the component actors, the four authoring roles' actors, the
//     path struct (a run's collaborators, and the deployer mergequeue,
//     healthmonitor and contractcheck reach through), the seams it satisfies,
//     layers and layer, readyFor, and admissionOrder.
//   - advance.go — run, the subcommand's whole pass driven to a standstill,
//     with decideWaiting and itemOfDeploy; takeIn, the intake of the intents it
//     is given; advance, one pass — the intent stage and then every step over
//     every live item — with advanced, what a pass did; resumeSets, what a
//     verdict at the Decomposition row causes when a human left it; and
//     authorFrom with sendBack and sentBackToImplementation, the authoring
//     stages entered from where the records place the item.
//   - resume.go — the pass's own comment; step and stepFrom, where the pass
//     enters one item; read and readLog, the log taken once per pass; decided
//     and closedRows with closedOn, approvedOver, rejectedOver and heldOver;
//     and liveItemIDs, intentIDs and liveCandidates.
//   - rehydrate.go — rehydrate, one item's candidate read back out of the
//     records; criteriaOf, what deciding a build's criteria produced;
//     ranAgainst, the composition a run ran against; and requirementTold and
//     screenTold.
//   - seams.go — the values the composition supplies a component that decides
//     events: intentState, raisedByTheHealthMonitor, pagedFiring, the page a
//     firing that pages sends on its own row — a drift mismatch or a revert
//     decided while the rollback holds — with whoTheRowWaitsOn, the duty that
//     page routes on; gateNotifier, how a gate reaches a human;
//     dispatchNotifier, the wait an item escalated leaves; and intakeNotifier,
//     a round of the interview, an intent escalated, and the acceptance round.
//   - holds.go — Standing, the factory's own holds at a deploy row; the three
//     reads enforcement makes of a candidate's own store, each answering with
//     nothing because this platform's candidate environment has no store;
//     dependencyHold, the one hold both deploy rows compute — a declared
//     dependency that is not its service's current release; and the
//     objective's own, objectiveHold with budgetHold and
//     passesTheBudgetHold, split so that the hold is readable without the
//     raise beside it.
//   - marks.go — marks, the releases a named human at Ops marked as not caused
//     by the release, which the score and its learning pass exclude.
//   - withdrawals.go — withdrawals, what a spec version under decision removes,
//     which the score resolves the Spec row on, with humanConfirmedSpecVersions
//     beneath it. A constraint-derived or hazard-derived provenance resolves no
//     named human here: who holds a duty over one constraint or one area is a
//     narrowing the People declaration does not carry, so those rows route to
//     the duty the row already names.
//   - strategysafeguard.go — strategySafeguard, the safeguard that keeps a
//     control: the write the production deploy row's fourth action makes, and
//     the read the score's strategy pick is made against.
//   - rollout.go — how a deploy is performed on this platform: the deployer's
//     principal, the targets in the environment's order, intoCandidate,
//     intoProduction, strategyOf, and adopt, the deployer's four fields on the
//     service record.
//   - decomposition.go — decomposeItems, one item per service an intent
//     changes; deriveShares and shareOf, one item's share of a requirement the
//     split spreads over several; decompositionGate; and intentAttemptLimit.
//   - setcompleteness.go — setRejection and derivedFrom, the Decomposition
//     row's two checks over what the set answers, which reject mechanically
//     before a human is asked and name which of gate.DecompositionChecks
//     rejected.
//   - specrejection.go — specRejection, the same thing per item at the Spec
//     row: the uncontrolled hazard, read from package criterion, and both
//     directions over the requirement a criterion names.
//   - authorintent.go — the intent stage: take and authorIntent, one intent
//     taken in and driven as far as this composition can take it; intentStage
//     and unworkedIntents, the same over every intent that has not reached its
//     items yet, which is how one taken in at Work reaches one; refineIntent
//     with answerARound and statesTheReading, the interview as rounds two
//     passes apart, a round nothing answers waiting in Work; decomposeSet;
//     gaveUp and the three readings of what stopped an item; and defaultTier.
//   - candidate.go — asked, shipped, decompositionSet, and candidate: the
//     run's own data shapes for one intent, what it did, one decomposition,
//     and one item's build in progress. asked.resumeIntentID names an intent
//     already waiting for the one caller that knows which, in place of the
//     statement-keyed lookup package intent's rewrite no longer offers.
//     candidate.sentBack, candidate.mergeRejectReason and
//     candidate.resetForRebuild are what [path.mergeUntilQueued] carries a
//     Merge to master rejection's reason into a rebuild with and clears between
//     one build and the next; candidate.from, candidate.rows,
//     candidate.pending, candidate.waiting and candidate.heldAt are what
//     [path.rehydrate] fills and resume.go derives.
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
//   - fleet.go — models, the [dispatch.Models] this interface answers with the
//     client each fleet entry's model version and credential are reached
//     through; ensureFleetEntries with processingLocationOf, which writes one
//     entry per role from -model, -provider and -effort where the install holds
//     none in force; readiness, the readiness reading per role;
//     rolePrompts, the role prompt version in force per role;
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
//     resolvedGoModules and readGoModule.
//   - measure.go — measure, the build's diff taken once at firing and handed
//     to the score, and the numstat parsing beneath it; destroysStoredData with
//     DestructiveStatements, the reading the reversibility factor resolves on,
//     which is a convention per toolchain the design names without describing —
//     a git that will not answer leaves it unavailable; reaches, packagesOf,
//     path.currentReleaseResolved and declaresSchemaChange, the readings the
//     build runner makes of its own checkout; and factorExposure and
//     path.exposureOf, which read the exposure list off the build record and
//     hand it to the score.
//   - authorship.go — authorship, the join package score reads what an agent
//     authoring a version worked from through: the artifact version names the
//     input manifest and the agent run of that manifest names the effort and the
//     versions of the role prompt and the skills.
//   - gateio.go — fired, and the gate mechanics every row shares: report,
//     settle and settled, which close a firing the factory decides and leave one
//     a human decides pending in Work; reportWaiting, what the pass says where
//     it asked for a verdict before; editInPlace with authoredAtTheGate, who a
//     version a human typed there is recorded as; recordFiring; and
//     decideOrResume and firingFor, one row's verdict where the pass that fired
//     it left it for Work and the firing rebuilt for a row that fires again.
//
// The merge queue and production deploy:
//
//   - merge.go — mergeGate, the Merge to master row, blockingCriteria — the
//     acceptance criteria the candidate's run did not pass, read before every
//     other mechanical check — and enforceContracts, the two contract checks,
//     all rejecting on their own terms before a human decides; and
//     mergeUntilQueued, which fires mergeGate again on a build made against
//     what it found wrong until it approves or the implementer's own attempt
//     limit escalates — the loop that builds the item again rather than
//     leaving it at Implementation for good.
//   - reverify.go — the whole of [mergequeue.Repository]: Head and Holds, the
//     two readings of master; Reverify; Confirm; FastForward; and VerifyCommit,
//     a commit a human accepted at Work.
//   - queuerun.go — runQueue, running the merge queue once for a service and
//     tearing down what it merged; candidateFor, tearDown.
//   - productiondeploy.go — productionDeploy, the Deploy to production row
//     and its five factory holds; fireProduction, putOnProduction,
//     recordTargetChecks, the deployer's own last check per target of the
//     deploy record; factoryHolds with factoryHoldsAsRead, the same four for a
//     caller that writes nothing; windowHold, rollbackHold, outstandingRevert
//     and revertWhileRollbackHolds, the one item the rollback's hold does not
//     reach.
//
// The watch and its operations:
//
//   - watch.go — the loop: watchWindows, one evaluation of everything
//     downstream of a deploy on one service, with watchTo and watchWindowsTo,
//     which run it to a deadline; notifierPasses, the notifier's two,
//     per-factory rather than per-service and performed once per run because
//     the second sweep after a delivered page is what widens it; watchPass, the
//     two together, which the watch subcommand and the process's watch pass
//     make; reportWatched, reportAfter, pagesHeldToTheHours,
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
//   - ops.go — watchCommand, the health monitor over one service, and
//     pathFlags/withPath, composing a path for a subcommand that drives one
//     step of it with no model.
//
// What ends something, and what a call at Ops reaches:
//
//   - undo.go — rollBackNow and revertIntent, which is duty 10 in its two
//     forms, and markRollback, the mark that a rollback was not caused by the
//     release, the revert item it ends and the hold it lifts. Ops reaches all
//     three through calls.go.
//   - ending.go — dropItem, an item ended for good with its candidate
//     environment torn down, and truncateCommand, the log's retention pass.
//   - retirement.go — removeService, the [policy.Factory.Removal] this
//     composition supplies; path.retire and path.removeFromEnvironment, the
//     owner's write that ends a service and the removal performed for one
//     environment; path.endProject, the project ended once every service in it
//     is retired; and unmergedItemsNaming and servicesWithACurrentRelease, the
//     counts each write is refused on.
//   - acceptance.go — acceptanceRounds with acceptanceQuestion and liveItems,
//     the round the run asks of every intent whose items are all live and the
//     delivery of one the factory raised; and outstandingRound, the round
//     Work's own answer is given against.
//
// What an owner authors, reached from Factory and People:
//
//   - authoring.go — humanNamed, which resolves a name to the per-person key
//     the People mapping gives it and mints one where the name is new, and
//     withPool, opening the database and applying the schema for the first
//     command an owner reaches.
//   - parameter.go — authoring and authorParameter, one parameter authored on
//     the record its scope names, which is the whole of the dispatch on which
//     subject the parameter is a field of; and authored, which tells a
//     lengthening in force apart from a shortening written pending, that one
//     being decided at a row rather than authored.
//   - withdrawal.go — priorsRestartedBy, what the shortening's row names
//     beside the value: the authors whose per-author prior stands drifted and
//     whose held-out decisions the cut would remove; safeguardWithdrawalRouting,
//     who the row that decides one safeguard's withdrawal waits on; and
//     rowGate, the gate a row outside every item is fired through.
//   - safeguard.go — placeSafeguard, placing one, and safeguardSubject,
//     resolving a subject to what it binds — "gate_row:" is drawn on a service,
//     keyed by the row, because package policy's own reader keys a row-scoped
//     safeguard that way.
//   - legalhold.go — legalHoldSubject, resolving a legal hold's subject
//     written kind:name.
//   - namedsubject.go — namedService, namedProject and namedArea, resolving a
//     name to its record, shared by the parameter and safeguard writes and by
//     "policy".
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
// The tests: fixtures_test.go, atworkfixtures_test.go, fakemodel_test.go,
// screensfixtures_test.go, and the three files named <subject>fixtures_test.go
// (authoringfixtures_test.go, contractsfixtures_test.go, watchfixtures_test.go)
// hold the fixtures every other test shares. atworkfixtures_test.go is the
// human at Work: atWork closes each pending row with the next token of its
// script, through the same calls a screen's own reach. The script is the
// value's own — standard input is read by nothing now — and a test's one string
// is split by newPath into [deps.answer] and the verdicts after it.
// screensfixtures_test.go is the four screens as a test drives them: the real
// composition behind package screens' handler, over HTTP, with both headers on
// every call, which is what every screensepisode_test.go, screensdecision_test.go,
// screensopsepisode_test.go, screensmoneyepisode_test.go,
// screensrecordrows_test.go, screenspeoplechain_test.go and
// screensoverall_test.go drive the demonstration through. The rest are one
// subject each. Three keep the name they were written under — main_test.go the
// end-to-end demonstration, watch_test.go the bad deploy rolled back,
// contracts_test.go the two-service pair — and every other one is named for its
// subject.
//
// Who may write what: nothing of its own. Every record the run causes to
// exist is written by the package that owns it; this command composes the
// writers and holds no table, and every read goes through the owning package's
// readers. What it implements is a seam rather than a record, six of them:
// [mergequeue.Repository] and [contractcheck.Checkout] because reaching a
// repository is the deployer's, [contractcheck.Exchanges] and
// [contractcheck.StoreState] because observing a run and reading a candidate's
// own store are, [healthmonitor.Deployer] because reaching a deploy target is,
// [gate.Holds] because computing the factory's own holds reads most of the
// graph, and [screens.Views] and [screens.Calls] because what crosses that seam
// is a view of a record and never the record. [contractcheck.Checkout] is also
// where the build's own reading of whether its checkout declares a schema
// change, and of whether it is a backfill, is answered — the first off the
// build record the run wrote it on.
//
// Every subcommand acquires the lease before it touches the store, whether it
// writes or only reads: a read still appends a read event, which is itself a
// write of the log, so the one-process rule holds for every one of the eight —
// the difference being how long it is held, one pass for a subcommand and the
// process's whole life for "serve". acquireLease in lease.go applies package
// lease's own table — the one thing created before the lease, since a lease
// cannot be taken in a store whose lease table does not exist — takes the lease
// under this process's own instance, the machine's hostname and this process's
// id, starts a goroutine renewing it every third of its ttl for the life of the
// process, and returns the token every writer the composition constructs and
// every [decisionlog.Reader] carries. [postgres.Start] runs
// after that, under the lease: it reads the store's schema history, refuses to
// start against a store this version cannot read, and applies the rest of the
// schema. A held lease is a start failure, printed on stderr naming the holder,
// with a non-zero exit; the stop function each subcommand defers releases the
// lease, so the next one starts rather than waiting out the ttl.
//
// The fleet entry is a record and an owner writes it at Factory, through
// [calls.WriteFleetEntry]. compose writes one entry per role from -model,
// -provider, -effort and the credential that provider reads, as the human
// -human names, wherever the install holds no entry in force for that role —
// the stand-in for that act on a composition no screen is open on, and an
// owner's own entries are left alone whatever the flags say. "serve" composes
// through the same call, so a terminal and a screen dispatch the same install;
// [deps.withoutFleetEntries] is what a composition says to write none with,
// which is the state the readiness reading and dispatch's holds are for and
// what the episode-one test drives.
//
// What is not built here: a refer at the Decomposition row reaches nobody, the
// design naming no duty for that row, so it waits on the owner from its first
// firing and a refer there has nobody left to refer to — [gate.Gate.Refer]
// re-fires the set that row decided, and never gets that far.
// The Implementation row rejects over the screens and over nothing else: the
// encodings are checked one row below, at the candidate environment, where a
// defect is carried on the candidate rather than stopping the run, and rejects
// at Merge to master; so are the emission in both directions and the count of
// the area's hazardous operation, which nothing reads back off the build.
// [policy.Factory.WithdrawEnvironment] has no call: production's is the only
// persistent environment this interface composes, and it is withdrawn with the
// project by [calls.EndProject].
// [path.approveThrough] is reached from Work by [calls.ApproveThroughHold] and
// by no subcommand: the four factory holds it approves through stop the
// production deploy row being fired at all, so there is no open event for
// [calls.Decide] to name and that call fires the row with the holds on it and
// approves it in one act. Every other hold the factory sets is on a row the
// gate already fired, which [calls.Decide] approves through by naming the set
// on the open event. What the item view offers the action against is
// [path.factoryHoldsAsRead], the same four holds with the objective's budget
// read rather than raised.
//
// What defines it: the four screens as software and the fleet as records an
// owner composes is ../../../roadmap.md#m8--the-screens-and-the-fleet, what
// each screen holds is
// ../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md,
// and the client versioned with the factory, the subscription per address and
// the version refusal are
// ../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md.
// More than one service and the contract queries are
// ../../../roadmap.md#m5--contracts-bind-services. The path it composes is
// ../../../end-goal/how-the-factory-works/01-one-pipeline.md;
// the component that puts an agent on a stage is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md;
// every component's restart and the lease is ../../../end-goal/one-process.md;
// what ends a service, an environment, or a project is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md;
// the duties the four screens' calls give a way in to are
// ../../../end-goal/what-humans-do.md; and what each component may call is
// ../../../end-goal/components.md.
package main
