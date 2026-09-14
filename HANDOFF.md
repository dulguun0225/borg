# Task

M10 is built: the nine steps of the plan are committed on `main`, every one of the seventy-five claims marked `unbuilt M10` is `built`, and `cmd/tracecheck`, the consistency commands, the Go suite, and the client's lint, unit, build and browser suites pass. No step remains. What remains is the owner's: the drift-reviewer findings below on claims built before M10, each a place the code and the design disagree, and the readings taken where the design left two. Each row names the package, the claim, and what the reviewer saw; none has a disposition. Delete a row when the code or the design settles it, and this file when both lists are empty.

The client's browser suites run on this machine with `CHROME_BIN` pointing at Playwright's Chromium under `~/.cache/ms-playwright` and `psql` reached through the database container; neither is on the path by default.

# Pre-existing drift

- screens C0418 — no field counts submissions under a shape the store could not read.
- screens C0419, C2604, C2634, C2661, C2704, C2860 — the old-way-in list has no ordering; a drift mismatch renders beside the target rather than over it; the spend ceiling view carries no units spent or period; nothing refuses a People row that acts nowhere; a gate row closed by a sibling holder does not reach an open item screen; no fleet proposal ever waits at Factory.
- deploy C0701 — any completed removal clears the current release regardless ordering.
- criterion C1042 — `HumanConfirmed` calls `InForce` without the rejected spec versions.
- deploy C1681 — the kept count is the whole capacity the replaced release had; no owner-authored fraction is read.
- service C1944 — where no run length is authored a component actor's write places no bound, so a safeguard may lengthen it.
- service C2042 — an unauthored window limit resolves to this package's constant for readers outside gate policy rather than a value the score supplies.
- environment C1525 — the composition is stored one line per interface address, so a dependency with two addresses reads back as two entries.
- mergequeue C1432, C1543 — the queue's rejection sends the item back with `ReturnTo`, which counts nothing, so no attempt is counted at Implementation.
- healthmonitor C1977 — the last check's newest time is taken only from series the pass's open windows read.
- score C1377 — a service's first release takes a control when the rollout is an adoption.
- notifier C2110 — `FirstAcceptedAt` is preserved across attempts where the sentence has the record overwritten at each attempt.
- healthmonitor C1983 — the search's deploy record names a release in its delivered-release field where the sentence says it names none.
- healthmonitor C2130 — a failed exit resumed after a stop pages about a rollback the factory performed itself.
- healthmonitor C2097 — the incident carries the policy and score versions off the window's open rather than those in force at the crossing reading.
- score C1262 — whether the diff destroys stored data arrives as a measurement field independent of the exposure extractor, so an unreadable extractor leaves reversibility valued.
- score C2034 — an incident with no rollback behind it moves the window's size and power.
- deploy C1875 — backfill completion is gated on a caller-supplied row count rather than every old-form row being present in the new.
- healthmonitor C2598, C2001, C2136, C2114, C2135, C1964 — the explicit-threshold reading closes with the window; a failed exit resumed after a stop skips intake's step and pages a rollback as outstanding though it ran; the failed-with-no-rollback wait names no duty and never widens; no quantile is fixed with the histogram boundaries on any shipped emission version.
- release C1557 — the next number is one above the higher of the service's highest record and a caller-supplied floor, serialised by an advisory lock rather than the queue's ordering.
- mergequeue C0721 — `FastForward` has no case for the first fast-forward creating a master that does not exist.
- mergequeue C1606 — where the intent's state stops an item the queue opens a wait and leaves its own unfinished merge's record write owing.
- item C0754, C0758 — the graph and the hold's edges leave out dropped items, which are unmerged.
- buildrunner C1454 — the source constraint is an allow-list that applies no rule when it is empty.
- gate C0835 — no call re-fires a row to another holder of its duty; only an edit in place and a refer re-fire one.
- mergequeue C2066 — the backlog-cap stop is gated on a cap above zero, so a cap of zero stops nothing.
- release C1652 — `Between` resolves the range of consumer contracts in force through the release number.
- item C0654, C0800 — `ClearEscalation` refuses every take-over when `escalated_from_stage` is empty, the legacy path the schema contemplates.
- gate C0918 — the pick carries strategy, schedule and why and no share bound, so hazard severity restricts the schedule rather than bounding the share.
- build C1449 — the rule that an entry with missing coverage resolves a human to the gate, and that an advisory match over it is unassessed rather than clear, exists nowhere in this code.
- build C1644 — no link from the build's could-not-derive set to what the release reads.
- build C1650 — the build carries no name, and nothing here holds the two-name vocabulary or its invariance under environment count.
- build C1450 — `required_by` is free text with no link to another entry's id, and `Resolved` returns a flat list, not a graph from the manifests down.
- wayin C0433 — shipped.go:211 and :226 write `notice_id` as a non-pointer string with no `omitempty`, so the entrance's absent-key check at entrance.go:185 never fires for traffic through the shipped way in, and a submission that opened no notice is forwarded whenever no notice is in force rather than refused.
- exposure C1263 — the credential readers are Go-syntax-bound, so a credential name in a configuration file written without double quotes is not read at all.
- exposure C1247 — both readers return on the first match in a line, so a line naming two credentials or adding two outbound calls contributes one evidence entry rather than each.
- exposure C1442 — the licence policy decided against the resolved set as a build-kind constraint, and the "which services ship a package at a version" query over current release, build, and set, exist nowhere in this directory; only the diff of the two sets does.
- wayin C0436 — entrance.go:194-198: the render has a third outcome besides accepted or refused-with-the-rate, and at entrance.go:191,219 an unreachable store renders a plain-text 503 rather than a submit result.
- build C1443 — schema.go:105-108: `source`, `version`, `digest` and `licence` carry no presence or naming rule, so an entry recording a source is not distinguished from one the resolver produced no source for except by an untyped empty string.

- score C0069 — rejection.go:42-50, learn.go:185-187: the score reads the `queue_rejection` row and lowers the threshold one band per rejection at Merge to master, rather than ignoring it the way it ignores a hold.
- score C1275 — learn.go:76-87, rules.go:147-153: four supplied values are moved by nothing, not two; the pass moves no exposure bound and no advisory severity.
- score C1286 — version.go:374-390: a recalibration's version differs in more than the weights; it also carries a refitted per-set `Scale`, a cleared `Drift` list, and a new `RecalibratedThrough`.
- score C1340 — rules.go:108-114: the two parameters stated as having no second end are the window's confidence and the item-size target, and the item-size target is not a window parameter.
- gate C0885 — fire.go:235: a drift mismatch, a could-not-derive, a service missing a deployer field, and an irreversible area with no share each set `HumanDecides` with `Marks` empty, so the row carries no mark and reads as sent by the number.
- gate C0883 — set.go:403: the Decomposition firing refuses only `dropped` and `escalated`, so it fires over an `unrefined` or `re-decomposing` intent (same for C0576).
- gate C0868 — waits.go:126, refuse.go:177: a row waiting on a named human re-fires on a refer to that same human rather than widening to the owner.
- gate C0898 — verdict.go:154: why it auto-passed is written onto the close event, not the open event beside the selection's mark.
- environment C1406 — the deployer's last check per platform is neither a field nor a key of the record; only the concurrent-environment count half exists.
- environment C2253, C1385, C2233 — a gate row's threshold is a row in a separate `environment_gate_threshold` table, not a field of the environment record, and its absence is an absent row.
- environment C2246 — the threshold upsert always writes, so a re-derivation that agrees still writes the row.
- environment C1503, C1514 — composition-start and run-could-start are on `environment_cycle` rows, not the environment record, and an open cycle is counted to now rather than to a teardown.
- service C0616, C0730, C0732 — the deployer writes five fields, `taking_traffic` beside the four, and adoption refuses on a fifth property; `end-goal/open.md` holds the owner's question on this.
- service C2056 — the default kept fraction of all instances is a comment only; no reader resolves an unauthored value to it.
- deploy C1722 — a record with a target already complete may still be marked failed.
- deploy C1923 — the restart has a third outcome, failed at `StepCannotBeCarried`, and for a record that reached nothing deploys the previous release onto every target rather than the ones reached.
- deploy C1691, C1753 — the restart's rollback keeps the stopped deploy's configuration rather than the one the returned-to release's record named.
- deploy C2054 — fast versus slow rollback is chosen from whether a kept fleet stands, not whether the rollout kept a control.
- deploy C0925 — with no bake volume supplied the hold returns at once.
- criterion C1057 — once unreliable at `EnteredAt`, a criterion stays so until a re-authored encoding, not while its rate is above the bound.
- targetseam C0096 — `Seeder.Seed` sits outside the `Target` interface and `Ops`, so the named set is two declarations.
- targetseam C0103 — both mitigation operations are keyed by build, not by the release deployed.
- contractcheck C1793, C1893 — `Checked.Affected` collects every service with a predicate naming the producer service, not only those naming a contract the candidate publishes.
- contractcheck C1880 — a newly added uniqueness rule is never decided by whether a write declared inside the range would violate it.
- dispatch C2541 — the ceiling clear is admitted for any human actor rather than the owner.
- gate C0897 — the Decomposition row passes `reviewSampled` as false and never reads the review sample rate.
- deploy C1731 — `SourceOfRestart` is a third rollback source beside the health monitor and the human at Ops.
- environment C1417 — `Compose` refuses a candidate environment naming no project and stores `project_id` on it, where the design gives the candidate's environment to the item and no project; `doc.go` states the opposite of the code.
- driftdetector C2148 — the detector's store is a second schema on the factory's own PostgreSQL instance under the factory role, so "no factory component may write it" holds only by which Go type a caller is handed.
- contractcheck C1836 — a nil `checks` writer makes the deprecation pass write no last check, so the pass is not a named row in that composition.
- contractcheck C1669 — the backfill-complete condition on a drop applies only where the dropped element carried a deprecation mark.
- contractcheck C1817 — `doc.go` says re-verification is made with this component's actor, where the sentence makes the merge queue the actor.
- dispatch C0007, C0204 — dispatch writes the input manifest itself rather than calling context assembly, with no selection rule version and only entry-withheld exclusions.
- dispatch C0810 — rows are written for five of the six conditions; the dispatch-decided constraint condition has no constant, no read, and no row.
- dispatch C0675 — the stage is the caller's argument rather than read off the item, and nothing re-enters a stage whose agent stopped.
- dispatch C2381 — one agent run record is written per provider call, so a retried run writes several records.
- dispatch C2538, C2582 — the credential's ceiling row carries no owner routing; only the per-item hold is routed to the owner.

- contractcheck C1833, C1903 — `partial` marks a consumer partial from any partial derivation in its range without checking that the record names the producer whose list is being built.
- contractcheck C1817 — `doc.go` says re-verification is made with this package's actor; `check.go` and the design make the merge queue the actor.
- contractcheck C1836 — composed with a nil intake or a nil last-check writer, the deprecation pass writes no last check, so a stopped pass is not a named row.
- securitypredicate C1804 — the shipped list holds zero predicates, `Kinds` is freely settable with no extend-only rule, and the version carried is a caller string rather than the install event.
- securitypredicate C1820 — the could-not-derive outcome is computed in memory and never recorded; putting a human at the gate is the caller's.
- securitypredicate C1821 — which toolchains have a derivation is an in-process function, published nowhere readable before adoption.
- C1648 — one reading is that the build runner recompiles the checkout with one line altered and the deployer places that artifact with no record; the other is that the deployer alters a compiled artifact directly. The first is built because a compiled Go artifact cannot be altered on a line without recompiling.

- targetseam C0119 — `seam.go` names `[WayInTokenName]`, an identifier the package does not define, and `Deployment.Validate` requires the deploy id but no way-in token.

- localtarget C2052, deploy C1753 — the fast rollback goes through the seam's `Reconfigure`, which on the local target drains the kept process and starts the rollback build afresh with a newly minted way-in token; the design has the rollback as a traffic shift onto instances still running and warm, already holding their configuration. `Reconfigure` predates M10 and exists to re-key the way in; reconciling the two is the owner's.
- localtarget C0914 — `DeployWithControl` reports a drain while the replaced instance is left alive as the control.
- deploy C1738 — the deploy package's own notifier seam still pages only at the snapshot and artifact-digest exits; `cmd/factory` now pages a rollback target refusal through the credential wait after the rollback record exists.

- Step 8, operational and Factory read models. No split. Directories changed: `factory/cmd/factory` (**doc.go changed**), `factory/criterion` (**doc.go changed**), `factory/deploy` (**doc.go changed**), `factory/environment` (**doc.go changed**), `factory/policy`, `factory/screens` (**doc.go changed**), and `HANDOFF.md`, plus the six claims flipped to `built` in `end-goal/claims.txt`. Factory now exposes recorded composition time and candidate environment-hours, platform room counts, per-item hosting and per-release instance-hours with explicit pricing presence, mutation scores, criteria counts grouped by withdrawal author, kept-fleet stops, and independent target, platform, service, and drift last-check scopes. Production fleet replacement and control/kept teardown persist instance-hours at the service rate in force; candidate teardown persists environment-hours at its rate. Reviewer follow-up makes production's authored room the only composition ceiling, reads deploy completion through the supplied environment-target reader, groups all addresses of a dependency in one composition row, validates dependency releases and candidate state, and removes C2641 from criterion's documentation. Pre-existing drift is recorded above. Checks: `go test -count=1 ./environment ./deploy ./criterion ./screens ./mergequeue` passed (`ok github.com/dulguun0225/borg/factory/environment 9.228s`, `ok github.com/dulguun0225/borg/factory/deploy 4.564s`, `ok github.com/dulguun0225/borg/factory/criterion 3.197s`, `ok github.com/dulguun0225/borg/factory/screens 0.160s`, `ok github.com/dulguun0225/borg/factory/mergequeue 14.737s`); `go test -count=1 -timeout 60m ./cmd/factory -run 'Candidate|Environment|Composition|Room|Remove|Retire|View|Screen|Hours'` passed (`ok github.com/dulguun0225/borg/factory/cmd/factory 333.522s`); `go vet ./...` passed; `go run ./cmd/depscheck` passed; `go run ./cmd/tracecheck` passed; `git diff --check` passed; `graphify update .` passed; all changed source and test files are under 500 lines.

- environment C1522 — with no composition reader supplied, `validComposition` skips both checks.
- environment C1397, C1398 — the only post-creation write of the ordered target field appends; no write reorders it.
- criterion C1484 — nothing refuses a candidate-run result for a criterion the build's own process decided; `Latest` lets the run's result stand over the build's.
- Step 9, Work, Ops, and Factory presentation. No split. Directories changed: `factory/screens` (**doc.go changed**), `factory/cmd/factory`, `factory/client/src/app/api`, `factory/client/src/app/work` (**README changed**), and `factory/client/src/app/factory` (**README changed**), plus `HANDOFF.md` and the three claims flipped to `built` in `end-goal/claims.txt`. Work now reads and presents merge-queue rows with their standing waits and open analysis-window rows; Factory presents mutation score per service and criteria withdrawn per service and author beside in-force and unreliable counts. The server keeps these read models in the existing screen and composition packages, and screen handler tests cover the added Work and Factory response shapes. The empty `/work/all` state now renders the shared queue and window sections, including their named headings and empty messages. Checks: `go vet ./...` passed; `go run ./cmd/depscheck` passed; `go run ./cmd/tracecheck` passed; `go test -count=1 ./screens` passed; `go test -count=1 ./cmd/factory -run '^TestQueueWaitsKeepOnlyQueuePayloads$'` passed; `npm ci` passed; `npm run lint` passed; `npm test` passed (87 of 87); `npm run build` passed; `npm run e2e` passed (9 of 9); `graphify update .` passed. The first focused `go test -count=1 ./screens ./cmd/factory` attempt timed out in the existing database-backed test and is retained in the session record; the coordinator's full suite remains responsible for that package.

- Factory screen C1080, C0865 — no share of criteria in the unwanted-condition pattern; no per-human count of rows acknowledged and decided by another holder.
- Factory screen C0472, C0825, C1289, C0684, C0895, C2607, C2650, C2993 — two store-kept report counters where the design has one; the fleet table shows configuration and not what each agent does; a waiting policy version sits apart from its threshold; area severity per row rather than a count naming none; the approved/undone pair carries no split by cause; no skill version in force; the browser run decides only that the screen is served.
- Work screen C0536, C0609, C0880, C1106, C2570, C0788, C2680, C2683, C2714, C2993 — the intent is not a timeline entry; a question is answered by typing its id; a row waiting on another holder refuses nothing; a version renders as an id; one decomposition decision has no address over its timelines; the empty predicate folds readiness rows in; two screens have no spec over their declared states; the browser run drives no subscription write-back.
- screens C2601, C2605 — the emission version is reported beside no span; one mitigation pointer per service hides a second target's.

# Unresolved

- Step 8 platform room has two readings: an external platform adapter could report the held count and room, or the available seam can report only the factory's standing candidate count. The conservative implementation keeps the platform pass shape, mirrors the standing count for held capacity, and leaves room unreported until that adapter exists.

- C0803: `item.RevertSiblings` forces the failed release's item into the set of shipped siblings, because after the rollback the live reading no longer counts it though it did ship; the other reading, live siblings only, produced no revert item at all.
- `go test ./cmd/factory` took 1925s after step 2; the timeout in `factory/README.md` and `.github/workflows/factory.yml` is now 60m. Coordinator runs use it.

- The exact resolver and registry APIs, host capability check for branch-restricted credentials, and process isolation mechanism are not specified. Inference: keep them behind interfaces in `buildrunner` and supply them from `cmd/factory`.
- The named design does not enumerate the initial Go security-predicate kinds or their predicates. Inference: implement the existing list/decision seam first and require an explicit authored or shipped list before treating a predicate as decided.
- The named design does not choose a platform package. Inference: keep platform composition and room reads in `environment`, `deploy`, and `cmd/factory` until a separate component boundary is required.
- The exact seed/value-set storage format, migration runner, snapshot backend, and per-environment address encoding are left to implementation; preserve the record fields and version comparisons in the named designs.
- Step names, file splits, and line counts are planning inferences based on the current package shape; keep each step at about 1,000 changed lines or fewer and split a step if the estimate fails before committing.
- The design's "network reach to the sources the set names and to nothing else" has two readings: an OS-enforced source allow-list and the available Go boundary, which can clear the environment and build with an offline module cache but cannot enforce host filesystem or egress isolation. The conservative implementation is the latter; the OS-level restriction remains a host requirement.
- Candidate interface addresses have two readings: infer them again from the value set at each run, or persist the addresses selected by the consumer-contract composition with the dependency release. The conservative implementation persists the selected addresses in the candidate composition, so re-verification compares the exact reached set rather than reconstructing it from mutable configuration.
- buildrunner C1465 — Go declares no licence. The notice currently names the licence file shipped by the module; the other reading is that the notice's licence should be Go's declared licence, which Go does not provide. The design does not choose between those readings.
