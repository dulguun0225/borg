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

# Pre-existing drift

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

# Steps

## 3. Candidate environment composition and run outcomes

Claims: C0728, C1391, C1392, C1402, C1405, C1485, C1489, C1490, C1491, C1496, C1497, C1498, C1505, C1510, C1512, C1572, C1574, C1575, C1576, C1674, C1895, C2181.

Design files to read: `end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md`; `end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md`; `end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md`; `end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/01-the-store-and-the-configuration.md`; `end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/03-room-and-what-an-environment-costs.md`; `end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/01-the-third-outcome.md`; `end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md`; `end-goal/how-the-factory-works/06-releases/05-the-deploy-record/01-a-schema-change.md`; `end-goal/how-the-factory-works/07-contracts/11-which-producer-a-consumer-reaches.md`; `end-goal/how-the-factory-works/08-operations/09-the-deployer.md`.

Change `factory/environment`, `factory/service`, `factory/deploy`, `factory/targetseam`, `factory/localtarget`, `factory/dispatch`, `factory/criterion`, and the candidate composition in `factory/cmd/factory`. These are existing packages. Wire the deployer as the owner of candidate composition, teardown, deploy records, schema application, configuration resolution, target removal, and target/platform last checks; keep platform composition in the existing environment/deployer seams. The existing dependency direction is sufficient; if a new edge is required, document it in `deps.txt` with the caller's ownership reason before adding it.

Prove candidate creation from the production platform, service-interface addresses, named secrets, seed and value-set versions, schema snapshots, target removal ordering, room holds, failed teardown retry, and the two-attempt unavailable-run wait. Include an end-to-end test where a missing external address or unreachable dependency records no criterion result and eventually opens a Work wait. Checks: focused package tests, `go test -count=1 ./factory/environment ./factory/service ./factory/deploy ./factory/targetseam ./factory/localtarget ./factory/dispatch ./factory/criterion ./cmd/factory -run 'Candidate|Environment|Composition|Unavailable|Teardown'`, `go vet ./...`, `go run ./cmd/depscheck`, and `go run ./cmd/tracecheck`.

## 4. Encoding and mutation at the candidate run

Claims: C1129, C1130, C1131, C1135, C1527, C1599, C1648.

Design files to read: `end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/03-what-the-encoding-rests-on.md`; `end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md`; `end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/README.md`.

Change `factory/criterion`, `factory/securitypredicate`, `factory/gate`, `factory/buildrunner`, and `factory/cmd/factory`. These are existing packages. Add the runner-to-security-predicate dependency only if the runner owns the candidate-run call; otherwise keep it at the composition. Update `deps.txt` and the affected `doc.go` citations accordingly.

Run encodings and security predicates against the candidate environment, restore the seeded store between mutants, enforce the service mutant cap, record mutation counts and score beside candidate results, and make could-not-derive distinct from zero. Add the required end-to-end test in `factory/cmd/factory/mutationrejection_test.go`: `TestMergeToMasterRejectsCandidateOnAnUncaughtMutant` must take an intent through candidate run to Merge to master and assert rejection on a mutant the criteria did not catch, with no release or master fast-forward. Checks: criterion/security-predicate/gate tests, `go test -count=1 ./factory/criterion ./factory/securitypredicate ./factory/gate ./factory/buildrunner ./cmd/factory -run 'Mutation|Predicate|MergeToMaster'`, `go vet ./...`, `go run ./cmd/depscheck`, and `go run ./cmd/tracecheck`.

## 5. Merge queue and release ordering

Claims: C0233, C0234, C0235, C0803, C1479, C1552, C1628.

Design files to read: `end-goal/how-the-factory-works/01-one-pipeline.md`; `end-goal/how-the-factory-works/02-intent-into-items/04-when-an-intents-items-do-not-all-ship.md`; `end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md`; `end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md`; `end-goal/how-the-factory-works/06-releases/01-one-item-per-release.md`.

Change `factory/mergequeue`, `factory/release`, `factory/item`, `factory/gate`, `factory/buildrunner`, and `factory/cmd/factory`. These are existing packages. Keep repository, candidate environment, and criterion work behind `mergequeue.Repository`; the command composition may call the new runner without adding a queue-to-command edge. Update existing package citations.

Complete queue re-verification against actual master, re-resolved sets, recomposed environments, release-number ordering, rollback/revert exceptions, held deploy conditions, and one revert item per shipped sibling. Prove an ordinary queued candidate, a re-verification rebuild, a queue rejection with attempt counting, and a multi-item partial delivery. Checks: `go test -count=1 ./factory/mergequeue ./factory/release ./factory/item ./factory/gate ./cmd/factory -run 'Queue|Reverify|Release|Revert|Partial'`, `go vet ./...`, `go run ./cmd/depscheck`, and `go run ./cmd/tracecheck`.

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

- `go test ./cmd/factory` took 1925s after step 2; the timeout in `factory/README.md` and `.github/workflows/factory.yml` is now 60m. Coordinator runs use it.

- The exact resolver and registry APIs, host capability check for branch-restricted credentials, and process isolation mechanism are not specified. Inference: keep them behind interfaces in `buildrunner` and supply them from `cmd/factory`.
- The named design does not enumerate the initial Go security-predicate kinds or their predicates. Inference: implement the existing list/decision seam first and require an explicit authored or shipped list before treating a predicate as decided.
- The named design does not choose a platform package. Inference: keep platform composition and room reads in `environment`, `deploy`, and `cmd/factory` until a separate component boundary is required.
- The existing `localtarget` refuses traffic shifting. Inference: use a traffic-shifting target fake/adapter for the required rollout demonstration and leave local process deployment as its own target behavior unless the implementation proves a local traffic mechanism.
- The exact seed/value-set storage format, migration runner, snapshot backend, and per-environment address encoding are left to implementation; preserve the record fields and version comparisons in the named designs.
- Step names, file splits, and line counts are planning inferences based on the current package shape; keep each step at about 1,000 changed lines or fewer and split a step if the estimate fails before committing.
- The design's "network reach to the sources the set names and to nothing else" has two readings: an OS-enforced source allow-list and the available Go boundary, which can clear the environment and build with an offline module cache but cannot enforce host filesystem or egress isolation. The conservative implementation is the latter; the OS-level restriction remains a host requirement.

# Summary

Read `CLAUDE.md`, this handoff, the six Step 1 design files, `factory/README.md`, `factory/deps.txt`, and the affected package `doc.go` files before coding. Step 1 is implemented and its full test suite and final checks pass. Unresolved design choices and inferences are listed above.
