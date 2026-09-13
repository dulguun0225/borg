// # The files
//
// The entry point and dispatch:
//
//   - main.go — the entry point, chosen (the switch on the subcommand name; not
//     dispatch, the component that puts an agent on a stage), provider-to-model
//     selection, the secrets and deploy-credential helpers, and
//     runCommand/walkCommand, which parse those two subcommands' flags.
//   - serve.go — serveCommand, the process: the pool, the lease held for its
//     life, the schema, the report store, the composition, the views and the
//     calls over it, the handler [screens.New] composes over those with the
//     client's embedded build, the passes, and the HTTP server; served, that
//     handler plus GET /healthz and the way in's entrance; and shutDown.
//   - reports.go — the report store as serve opens it: the reportChannel that
//     is the entrance's [wayin.Store], the five values implementing the
//     interfaces that store reaches the graph through, the redactions among
//     them, and the address a deployed service's way in posts to.
//   - erasure.go — the erasure as one action: calls.PerformErasure, which walks
//     the report to its intent's statement and the artifact versions quoting the
//     same words, appends every erasure-list row, writes the redactions and has
//     each store destroy its own bytes; the legal hold's refusal, recorded by
//     policy; and replayTheErasureList, which serve runs over four stores first.
//   - viewchannel.go — Factory's numbers over the report channel: the ungrouped
//     count, the two counters, each service under no notice and each serving a
//     way in built under another release of the product, and each intent's
//     outcome beside cost per feature.
//   - grouping.go — the grouper as "serve" composes it: groupsThroughTheFleet,
//     servicesInAProject, itemsOfAnIntent and admissionSafeguards, the four
//     seams package grouper reaches the factory through, with newGrouper and
//     groupReports; and groupedReports, the seam the score reads an intent's
//     group and the mark a report carries through, no record of the graph
//     carrying either.
//   - admission.go — the two safeguards on the report store: awaiting, with
//     reportsAwaitingAdmission, intentsAwaitingAdmission and intentAwaitsAdmission,
//     what each is holding as Work shows it; and calls.AdmitIntent and
//     calls.AdmitReport, the writes that end each wait.
//   - views.go, viewwork.go, viewops.go, viewfactory.go, viewfleet.go,
//     viewnumbers.go, viewpeople.go — [views], one file per screen, Factory's fleet
//     and its spend ceilings split off at the 500-line bound.
//   - calls.go, callswork.go, callsops.go, callsfactory.go, callsfleet.go,
//     callspeople.go — [calls], one file per screen, over the two refusals in
//     calls.go; callsfleet.go is Factory's writes dispatch re-matches on.
//   - recordrows.go — the five rows that decide a record rather than an item,
//     every one of them at Factory: decideOutsideEveryItemAt for the four
//     withdrawals and shortenings; decideRolePrompt with fireRolePromptRow for
//     the row every version of what an agent is told fires, which re-matches
//     dispatch's holds on that condition where the approval put a version in
//     force; and decideOutsideEveryItem with alreadyOpen, which decides a row
//     already open rather than firing a second one.
//   - interview.go — the confirming round as two halves in two passes: the
//     format the pass states the reading in and the round at Work reads back,
//     confirmTheReading, which ends with the re-match an intent leaving the
//     state that stopped it runs, and servicesFor.
//   - passes.go — pass and passes with newPasses, Run and Tick: one ticker per
//     component's pass and one goroutine running them, so no two run at once —
//     what an HTTP handler reaches beside them is locked in shared.go; the nine
//     intervals and the "-every-<name>" flag per pass; announce, which tells
//     every subscriber the addresses a tick could have moved; and the four
//     passes in this file alone — watchServices, reevaluatePending (the first
//     caller [gate.Gate.ReevaluatePending] has had), acceptancePass and
//     ensureScore.
//   - lease.go — leaseTTL, leaseRenewEvery, defaultInstance and acquireLease,
//     called by every subcommand before it touches the store and held by
//     "serve" for the life of the process; renewals.after ends "serve" when
//     another instance took the lease.
//   - flags.go — serviceFlag and statements, the two repeated flags "run" and
//     "serve" take, with namesService beneath them; statements is what the
//     intent stage reads an intent's services back off its statement with.
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
//   - criteriaforce.go — rejectedSpecs and criteriaInForce, excluding the
//     introductions and withdrawals of rejected spec revisions.
//   - install.go — installed, the install's three records — the factory-wide
//     settings record, the project, and production's environment for it —
//     created where deps says this composition installs and refused where it
//     does not; runsOnProduction, which authors production's addresses on a
//     service naming none; and serviceTargets, serviceAddresses and addressesOf,
//     the addresses every read of what is running is performed against.
//
// The path a run walks stage by stage:
//
//   - path.go, shared.go — the component actors, the four authoring roles'
//     actors, the path struct (a run's collaborators, and the deployer
//     mergequeue, healthmonitor and contractcheck reach through), the seams it
//     satisfies, layers and layer, readyFor, and admissionOrder; and beside
//     them the accessors over the three fields of path a pass and an HTTP
//     handler both reach, the one lock on them, and what it does not cover.
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
//     events: safeguardRouting, where a safeguard that added a human says its
//     rows route; intentState, raisedByTheHealthMonitor, pagedFiring, the page a
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
//     objective's own, objectiveHold with budgetHold and itemAgainstTheBudget,
//     split so the hold is readable without the raise beside it.
//   - marks.go — marks, the releases a named human at Ops marked as not caused
//     by it, which the score and its learning pass exclude.
//   - withdrawals.go — withdrawals, what a spec version under decision removes,
//     which the score resolves the Spec row on, with humanConfirmedSpecVersions
//     beneath it.
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
//     row's checks over what the set answers, which reject mechanically before
//     a human is asked and name which of gate.DecompositionChecks rejected;
//     the order the set declares is checked beside them by
//     gate.SetCycleRejection.
//   - specrejection.go — specRejection, the same thing per item at the Spec
//     row: the uncontrolled hazard, read from package criterion, and both
//     directions over the requirement a criterion names.
//   - authorintent.go — the intent stage: take and authorIntent, one intent
//     taken in and driven as far as this composition can take it; intentStage
//     and unworkedIntents, the same over every intent that has not reached its
//     items yet, which is how one taken in at Work reaches one; refineIntent
//     with answerARound and statesTheReading, the interview as rounds two
//     passes apart, a round nothing answers waiting in Work; decomposeSet;
//     gaveUp and the three readings of what stopped an item; intentLeftItsStop,
//     the re-match every writer of a record that ends an intent's stopping state
//     calls; and defaultTier.
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
//     share, with the mechanical rejection its caller computed;
//     criteriaTheBuildDecided, what the Implementation row rejects over; and
//     on, specMaterial, refining and requirementFor, what a dispatch is given.
//   - author.go — implementationStage with startBranch, commitAndBuild and
//     hazardOf, and consumerContractStage; Publishes, Declares,
//     DeclaresSchemaChange, DeclaresBackfill and repoOfItem, the deployer's side
//     of contractcheck; publishedGoConvention, composed from what package
//     contract, screenstatemachine and criterion each publish beside
//     consumercontract's own mirror convention, and handed to
//     consumercontract.GoExtractor; and filesSize and rolePromptCriteria, what
//     a stage hands a role.
//   - backfill.go — declaresBackfill with backfillStore and backfillIn, the
//     pair a backfill item's checkout declares it copies between, with the file
//     name and directive that convention is, read by DeclaresBackfill.
//   - fleet.go — models, the [dispatch.Models] this interface answers with the
//     client each fleet entry's model version and credential are reached
//     through; ensureFleetEntries with processingLocationOf, which writes one
//     entry per role from -model, -provider and -effort where the install holds
//     none in force, and ensureModelCredentialLent beside it, which declares
//     that credential as the owner's own where the People declaration does not
//     already hold it; readiness, the readiness reading per role;
//     rolePrompts, the role prompt version in force per role;
//     shippedPromptFor and enterShippedPrompts, the install's first-start step
//     for what an agent is told; intentLimits, the [dispatch.Limits] that reads
//     a stage's limit through package policy and an intent's rounds through
//     intentAttemptLimit; and gateEscalation, which is what performs an
//     escalation dispatch decided.
//   - restart.go — restart, every component's restart run once by compose and so
//     by the subcommands that compose a path and by no other: the merge queue's
//     master read, the deployer's unfinished deploys, the health monitor's open
//     windows, the notifier's waiting rows, Factory's and People's re-derivation
//     from the newest policy version, and dispatch's re-match of its open holds.
//     RunningBuild and Rebuild are [deploy.Reading] and [deploy.Rebuilding]: what
//     a stopped record's own targets run, and the live seams and artifact
//     [deploy.Resume] carries a record forward or back with, which it decides and
//     performs inside the package — this file only supplies what it cannot see
//     and prints what came back.
//   - repo.go — the git and filesystem operations a stage needs: masterHead,
//     compiles, buildInto, the wayInOverlay both are handed, runEncodings,
//     repoFiles, copyFile; and createBuild, resolvedGoModules and readGoModule.
//   - measure.go — measure, the build's diff taken once at firing and handed to
//     the score, and the numstat parsing beneath it; destroysStoredData with
//     DestructiveStatements, the reading the reversibility factor resolves on;
//     reaches, packagesOf, path.currentReleaseResolved and declaresSchemaChange,
//     the readings the build runner makes of its own checkout; and
//     factorExposure and path.exposureOf, which read the exposure list off the
//     build record and hand it to the score.
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
//   - queuerun.go — runQueue, the merge queue run once for a service and what
//     it merged torn down; candidateFor, tearDown.
//   - reliability.go — markUnreliable, the criteria a candidate's run just
//     decided read against their own outcome history and marked in place, and
//     raiseUnreliable, the intent one becoming unreliable raises.
//   - productiondeploy.go — productionDeploy, the Deploy to production row
//     and its five factory holds; fireProduction, putOnProduction,
//     recordEnvironmentCheck, the deployer's own last check for the deploy
//     record's production environment; factoryHolds with factoryHoldsAsRead,
//     the same four for a caller that writes nothing, and windowHold, the
//     window limit among them. rollbackhold.go is the rollback hold:
//     rollbackHold, outstandingRevert, rollbackHolds, which is the same hold
//     in the form decomposition computes the graph's edges from,
//     rollbackHoldsSeam, wiring rollbackHolds onto item.Decomposition.Holds so
//     decomposition reads it itself at every write, and
//     revertWhileRollbackHolds, the one item the rollback's hold does not reach.
//
// The watch and its operations:
//
//   - watch.go — the loop: watchWindows, one evaluation of everything
//     downstream of a deploy on one service, with watchTo and watchWindowsTo,
//     which run it to a deadline; notifierPasses, the notifier's two,
//     per-factory rather than per-service and performed once per run;
//     watchPass, the two together, which the watch subcommand and the process's
//     watch pass make; reportWatched, reportAfter, pagesHeldToTheHours,
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
//     pathFlags/withPath, a path for a subcommand driving one step with no model.
//
// What ends something, and what a call at Ops reaches:
//
//   - undo.go — rollBackNow and revertIntent, duty 10 in its two forms, and
//     markRollback, the mark that a rollback was not caused by the release,
//     the revert item it ends and the hold it lifts. Ops reaches all three
//     through calls.go.
//   - ending.go — dropItem, an item ended for good with its candidate torn
//     down, and truncateCommand, the log's retention pass.
//   - retirement.go — removeService, the [policy.Factory.Removal] this
//     composition supplies; path.retire and path.removeFromEnvironment, the
//     owner's write that ends a service and the removal performed for one
//     environment; path.endProject, the project ended once every service in it
//     is retired; and unmergedItemsNaming with servicesWithACurrentRelease,
//     the counts each write is refused on.
//   - acceptance.go — acceptanceRounds with acceptanceQuestion and liveItems,
//     the round the run asks of every intent whose items are all live and the
//     delivery of one the factory raised; and outstandingRound, the round
//     Work's own answer is given against.
//
// What an owner authors, reached from Factory and People:
//
//   - authoring.go — humanNamed, resolving a name to the per-person key the
//     People mapping gives it and minting one where the name is new; and
//     withPool, opening the database and applying the schema.
//   - parameter.go — authoring and authorParameter, one parameter authored on
//     the record its scope names, which is the whole of the dispatch on which
//     subject the parameter is a field of; and authored, which tells a
//     lengthening in force apart from a shortening written pending, that one
//     being decided at a row rather than authored.
//   - withdrawal.go — priorsRestartedBy, the authors whose per-author prior
//     stands drifted and whose held-out decisions the cut would remove, which
//     the shortening's row names beside the value; safeguardWithdrawalRouting,
//     who the row deciding a safeguard's withdrawal waits on; and rowGate, the
//     gate a row outside every item is fired through.
//   - safeguard.go — placeSafeguard, placing one, and safeguardSubject,
//     resolving a subject to what it binds — "gate_row:" is drawn on a service,
//     keyed by the row, because package policy's own reader keys a row-scoped
//     safeguard that way. legalhold.go is legalHoldSubject, the same for a
//     legal hold's subject, written kind:name.
//   - namedsubject.go — namedService, namedProject and namedArea, a name
//     resolved to its record, shared by the parameter and safeguard writes and by
//     "policy".
//
// The reads and reporting:
//
//   - policy.go — policyCommand, every parameter as it is in force, where its
//     value came from, the safeguards that reached it, and what reads it.
//   - contracts.go — contractsCommand, every query contracts make, with
//     printContracts and printBreaks: what one candidate would break and whom.
//   - learn.go — learnCommand, the score's pass over the outcomes, printing
//     what moved and what moved it; printHeldOut.
//   - walk.go — walk, following the links from a deploy record back to its
//     intent and printing every decision the item's gates left in the log.
//
// The tests: fixtures_test.go, atworkfixtures_test.go, fakemodel_test.go,
// screensfixtures_test.go, screensworkfixtures_test.go and the four named
// <subject>fixtures_test.go — authoring, contracts, watch and reports — hold the
// fixtures the rest share. atworkfixtures_test.go is the human at Work: atWork
// closes each pending row with the next token of its script through the same
// [screens.Calls] a screen reaches, so every verdict carries when the row was
// opened, and a test's one string is split by newPath into [deps.answer] and the
// verdicts after it. screensfixtures_test.go is the four screens as a test
// drives them — the real composition behind package screens' handler, over
// HTTP, with both headers on every call. The rest are one subject each, named
// for it, except three keeping the name they were written under: main_test.go
// the end-to-end demonstration, watch_test.go the bad deploy rolled back,
// contracts_test.go the two-service pair.
package main
