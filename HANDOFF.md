# Task

Milestone M10 as `roadmap.md` states it, built as ordered steps, one commit per step titled `factory: M10 step N — <name>`, straight to `main`.

# Base commit

`f208d46567758c19a1e83d62fd07253b032ad0c4`

# Rules for a step session

- Read `CLAUDE.md`, then this file, then only the design files and packages the step names.
- First, in `end-goal/claims.txt`, change the step's claims from `unbuilt M10` to `built`, so each package's `doc.go` can cite the claims it implements and `tracecheck` holds. That is the one edit to `end-goal/` a step makes.
- Before coding, estimate the step's size. Over about 1,000 changed lines, split it here into `Na`, `Nb`, … with their own claim lists, and do `Na` alone.
- Do not commit and do not push. Run `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, and the step's focused tests; do not run the whole suite, the coordinator does. The coordinator runs `drift-reviewer` on every directory whose `doc.go` changed, then commits.
- Before returning, record under **Completed** the step, the directories changed, and every check run with its result, and move the step's entry from **Steps** to **Completed**. Keep **Unresolved** current.
- A doubt about what the design means is recorded under **Unresolved** with the two readings, and the more conservative one is built.

# Completed

- Step 1, Build runner and build inputs. No split. Directories changed: `factory/buildrunner` (new), `factory/build`, `factory/exposure`, `factory/wayin`, `factory/cmd/factory`, `factory/cmd/driftdetector`, `factory/contractcheck`, `factory/healthmonitor`, `factory/mergequeue`, `factory/README.md`, `factory/deps.txt`, and the 15 claims flipped to `built` in `end-goal/claims.txt`. Three `drift-reviewer` rounds on `buildrunner`, `build`, `exposure` and `wayin`; the last left **Not implemented** empty except for claims on the pre-existing list. Checks at the commit: `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, `tools/consistency-commands.sh`, and `go test -count=1 -timeout 30m ./...` all passed.
- Step 2, Adoption and master branch. No split. Directories changed: `factory/gate` (**doc.go changed**), `factory/buildrunner`, `factory/build`, `factory/score` (**doc.go changed**), and `factory/cmd/factory`. Adoption source resolves at Spec only for the first item on a deployer-recorded service with no factory release; later items weigh it. The build runner records missing digests separately, detects module licence files, and classifies unresolved Go entries as build-time. Checks: focused adoption/master tests passed; focused package tests passed; `go vet ./...` passed; `go run ./cmd/depscheck` passed; `go run ./cmd/tracecheck` passed; `git diff --check` passed; `graphify update .` passed.
- Step 3, Candidate environment composition and run outcomes. No split. Directories changed: `factory/cmd/factory`, `factory/contractcheck` (**doc.go changed**), `factory/criterion` (**doc.go changed**), `factory/deploy` (**doc.go changed**), `factory/dispatch` (**doc.go changed**), `factory/driftdetector` (**doc.go changed**), `factory/environment` (**doc.go changed**), `factory/gate` (**doc.go changed**), `factory/localtarget` (**doc.go changed**), `factory/mergequeue` (**doc.go changed**), and `factory/targetseam` (**doc.go changed**), plus `factory/deps.txt`, `HANDOFF.md`, and the 22 claims flipped to `built` in `end-goal/claims.txt`. Candidate composition, candidate-environment creation and teardown, candidate configuration, seed preparation, schema application, rollback configuration restoration, and unavailable-run waits are owned by `factory/deploy` behind command adapters; typed value-set parsing, composition identity, seed declarations, and persisted service-interface addresses are owned by `factory/environment`; dispatch holds an unprovisioned service before implementation and rematches that wait; undecided outcomes are recordable in `criterion` and rejected by `gate`; drift comparison leaves the deployer's candidate-composition record alone by identifying its candidate environment subject, while retaining production-environment and production-target checks; empty-seed store declarations and backfills are recorded undecided, and a zero-row backfill cannot complete. Checks: `go test -count=1 ./criterion ./deploy ./environment ./gate ./targetseam ./localtarget` passed; `go test -count=1 ./dispatch ./contractcheck` passed; `go test -count=1 -timeout 60m ./cmd/factory -run 'Candidate|Environment|Composition|Unavailable|Teardown|Undecided|ValueSet|Seed|Rollback'` passed; `go test -count=1 ./driftdetector ./cmd/driftdetector` passed; `go vet ./...` passed; `go run ./cmd/depscheck` passed; `go run ./cmd/tracecheck` passed; `git diff --check` passed; final-round `go test -count=1 ./criterion ./deploy ./contractcheck ./driftdetector ./cmd/driftdetector` passed; final-round `go test -count=1 -timeout 60m ./cmd/factory -run 'Candidate|Environment|Composition|Unavailable|Teardown|Undecided|Drift|Stale'` passed; C1490-round `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, `go test -count=1 ./contractcheck ./deploy ./criterion`, and `go test -count=1 -timeout 60m ./cmd/factory -run 'Backfill|Undecided|Seed|Store'` passed.
- Step 5, Merge queue, deploy queue, release ordering, reverts, and re-verification. No split. Directories changed: `factory/buildrunner` (**doc.go changed**), `factory/cmd/factory`, `factory/deploy` (**doc.go changed**), `factory/gate` (**doc.go changed**), `factory/healthmonitor`, `factory/item` (**doc.go changed**), `factory/mergequeue` (**doc.go changed**), and `factory/release` (**doc.go changed**), plus `HANDOFF.md` and the seven claims flipped to `built` in `end-goal/claims.txt`. The deploy package owns the six typed queue readings and hold decision, release-number ordering, the awaited-revert exception, and bounded redelivery; held candidates remain in the returned queue with their reasons, while the command executes only deployable entries. Item owns the one-transaction, one-revert-item-per-shipped-sibling write and includes the evidenced failed release after rollback. Merge queue owns re-verification security-predicate decisions and rejects a candidate when a predicate kind does not hold. Gate owns the human-to-Implementation resolution beside the no-digests route. The rollback run loop now terminates because an evidenced failed release is not discarded as merely non-live after rollback; a pending human decision on the awaited revert is preserved so its row is not re-fired. Checks: `go test -count=1 ./deploy` passed (`ok github.com/dulguun0225/borg/factory/deploy 2.235s`); `go vet ./...` passed; `go run ./cmd/depscheck` passed; `go run ./cmd/tracecheck` passed; `go test -count=1 -timeout 60m ./cmd/factory -run 'TestTheRollbackHoldsUntilTheRevertShips|TestARollbackSweepsTheReleaseAboveItsTarget|TestTheWindowLimitHoldsTheNextProductionDeploy|Queue|Revert'` passed (`ok github.com/dulguun0225/borg/factory/cmd/factory 466.789s`).

# Pre-existing drift

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

# Steps

## 6. Traffic-shifting rollout and held-out control

Claims: C0626, C0627, C0686, C0907, C2052.

Design files to read: `end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md`; `end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/03-hazard-severity.md`; `end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md`; `end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md`; `end-goal/how-the-factory-works/08-operations/09-the-deployer.md`.

Change `factory/targetseam`, `factory/localtarget`, `factory/deploy`, `factory/healthmonitor`, `factory/gate`, `factory/score`, and `factory/cmd/factory`. These are existing packages. Preserve the target seam and use a traffic-shifting fake or adapter for the end-to-end test; this is an implementation choice inferred from the existing `localtarget` refusal of traffic shifting.

Implement control selection and traffic shifting for an existing running service, rollout bounds for irreversible hazards, warm control instances, and rollback traffic shifting. Add the required end-to-end test in `factory/cmd/factory/heldoutcontrol_test.go`: `TestHeldOutReleaseTakesAControlOnATrafficShiftingTarget` must hold the release out, deploy it to a target that reports share support, and assert the control strategy and traffic shift in the deploy/window records. Checks: focused deploy, target, gate, health-monitor, and score tests; `go test -count=1 ./factory/targetseam ./factory/localtarget ./factory/deploy ./factory/healthmonitor ./factory/gate ./factory/score ./cmd/factory -run 'Control|Traffic|HeldOut|Rollback'`; `go vet ./...`; `go run ./cmd/depscheck`; and `go run ./cmd/tracecheck`.

## 7. Rollback ordering, room cost, and operational holds

Claims: C0709, C0909, C1347, C2065, C2470, C2472.

Design files to read: `end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md`; `end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md`; `end-goal/how-the-factory-works/06-releases/05-the-deploy-record/02-what-stands-for-a-rollback.md`; `end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md`; `end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md`; `end-goal/how-the-factory-works/10-fleet/05-an-account-that-runs-out-is-a-hold.md`.

Change `factory/healthmonitor`, `factory/deploy`, `factory/environment`, `factory/mergequeue`, `factory/score`, `factory/notifier`, `factory/factorysettings`, and `factory/cmd/factory`. These are existing packages. Use the existing health-monitor store and deployer seams for instance-hours, rollback target selection, kept-fleet limits, and credential holds; update citations and `deps.txt` only if an edge changes.

Prove overlapping-window rollback reaches every release in the window limit, the revert deploys before releases held behind it, environment-hours and instance-hours are read from records, a kept-fleet cap stops forward deploys, and target credential failure pages on rollback but not on the same forward hold. Checks: focused health-monitor, deploy, queue, score, notifier, and command tests; `go test -count=1 ./factory/healthmonitor ./factory/deploy ./factory/environment ./factory/mergequeue ./factory/score ./factory/notifier ./cmd/factory -run 'Rollback|Window|Kept|Credential|Hours'`; `go vet ./...`; `go run ./cmd/depscheck`; and `go run ./cmd/tracecheck`.

## 8. Operational and Factory read models

Claims: C1509, C1516, C1689, C2290, C2641, C2684.

Design files to read: `end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/03-room-and-what-an-environment-costs.md`; `end-goal/how-the-factory-works/06-releases/05-the-deploy-record/02-what-stands-for-a-rollback.md`; `end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md`; `end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md`; `end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md`.

Change `factory/screens`, `factory/healthmonitor`, `factory/deploy`, `factory/environment`, `factory/score`, `factory/criterion`, `factory/contractcheck`, `factory/lastcheck`, and the read-model composition in `factory/cmd/factory`. These are existing packages. Read each record independently at its owning scope: service, item, persistent target, platform, and production drift target. Keep candidate environment hosting facts in the existing records and expose converted amounts only where the service rate is authored.

Prove Factory read models for composition time, environment-hours per item, instance-hours per release, mutation/criteria counts, kept-fleet stop, and all required last-check scopes. Checks: focused read-model and command tests, `go test -count=1 ./factory/screens ./factory/healthmonitor ./factory/deploy ./factory/environment ./factory/score ./factory/criterion ./factory/contractcheck ./factory/lastcheck ./cmd/factory -run 'View|Screen|Hours|LastCheck|Fleet'`, `go vet ./...`, `go run ./cmd/depscheck`, and `go run ./cmd/tracecheck`.

## 9. Work, Ops, and Factory presentation

Claims: C2573, C2625, C2626.

Design files to read: `end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md`.

Change `factory/screens`, `factory/cmd/factory`, and `client/`. These are existing packages/directories; no new Go package is planned. Add the Work board/list queue and window rows, Factory mutation score, withdrawn criteria, and unreliable criteria. Keep server read models in `factory/cmd/factory` and the existing screen package; keep presentation changes in the Angular client.

Prove the four screen API responses and the browser end-to-end suite show queue/window waits and the three Factory metrics without writing records. Checks: `go test -count=1 ./factory/screens ./cmd/factory`, `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, `cd client && npm ci && npm run lint && npm test && npm run build && npm run e2e`.

# Coverage

| Step | Claim ids |
|---|---|
| 1 | C0092, C0620, C1434, C1435, C1446, C1454, C1456, C1458, C1459, C1464, C1469, C1480, C1482, C1831, C2184 |
| 2 | C0192, C0196, C0623, C0624 |
| 3 | C0728, C1391, C1392, C1402, C1405, C1485, C1489, C1490, C1491, C1496, C1497, C1498, C1505, C1510, C1512, C1572, C1574, C1575, C1576, C1674, C1895, C2181 |
| 4 | C1129, C1130, C1131, C1135, C1527, C1599, C1648 |
| 5 | C0233, C0234, C0235, C0803, C1479, C1552, C1628 |
| 6 | C0626, C0627, C0686, C0907, C2052 |
| 7 | C0709, C0909, C1347, C2065, C2470, C2472 |
| 8 | C1509, C1516, C1689, C2290, C2641, C2684 |
| 9 | C2573, C2625, C2626 |

## Claims that may be "stated"

None identified. All 75 M10 claims have an implementation owner and a test or end-to-end demonstration above. The exact initial Go security-predicate kinds are unspecified by the named design; the mechanism can be implemented, but its initial list remains unresolved below.

# Unresolved

- C0803: `item.RevertSiblings` forces the failed release's item into the set of shipped siblings, because after the rollback the live reading no longer counts it though it did ship; the other reading, live siblings only, produced no revert item at all.
- `go test ./cmd/factory` took 1925s after step 2; the timeout in `factory/README.md` and `.github/workflows/factory.yml` is now 60m. Coordinator runs use it.

- The exact resolver and registry APIs, host capability check for branch-restricted credentials, and process isolation mechanism are not specified. Inference: keep them behind interfaces in `buildrunner` and supply them from `cmd/factory`.
- The named design does not enumerate the initial Go security-predicate kinds or their predicates. Inference: implement the existing list/decision seam first and require an explicit authored or shipped list before treating a predicate as decided.
- The named design does not choose a platform package. Inference: keep platform composition and room reads in `environment`, `deploy`, and `cmd/factory` until a separate component boundary is required.
- The existing `localtarget` refuses traffic shifting. Inference: use a traffic-shifting target fake/adapter for the required rollout demonstration and leave local process deployment as its own target behavior unless the implementation proves a local traffic mechanism.
- The exact seed/value-set storage format, migration runner, snapshot backend, and per-environment address encoding are left to implementation; preserve the record fields and version comparisons in the named designs.
- Step names, file splits, and line counts are planning inferences based on the current package shape; keep each step at about 1,000 changed lines or fewer and split a step if the estimate fails before committing.
- The design's "network reach to the sources the set names and to nothing else" has two readings: an OS-enforced source allow-list and the available Go boundary, which can clear the environment and build with an offline module cache but cannot enforce host filesystem or egress isolation. The conservative implementation is the latter; the OS-level restriction remains a host requirement.
- Candidate interface addresses have two readings: infer them again from the value set at each run, or persist the addresses selected by the consumer-contract composition with the dependency release. The conservative implementation persists the selected addresses in the candidate composition, so re-verification compares the exact reached set rather than reconstructing it from mutable configuration.
- buildrunner C1465 — Go declares no licence. The notice currently names the licence file shipped by the module; the other reading is that the notice's licence should be Go's declared licence, which Go does not provide. The design does not choose between those readings.

# Summary

Read `CLAUDE.md`, this handoff, the named M10 step-5 design files, `factory/README.md`, `factory/deps.txt`, and the affected package `doc.go` files before coding. Steps 1–5 are implemented; no step was split. The corrected focused checks and unresolved design choices are recorded above.
