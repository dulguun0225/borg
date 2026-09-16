# Task

Milestone M11 — The watch reads what the software emits. Build [the health monitor](end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md) over an emission the implementer is told to produce: two records per request, `unfinished` among the outcomes, the emission version, a histogram at fixed boundaries, and kept failure counts — so that request rate, error rate and latency are read per operation and per target, and a fourth quantity for an `irreversible` area's hazardous operation, where M4 read one count of units failed. The values the service record already carries and nothing reads — the fraction the replaced build keeps, the proof test, the recent-history size the composition hands the health monitor as a constant — are read by what the design says reads them; the drift detector reads a standing mitigation as intended state; and the [window limit](end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md), the search, and the [brownout](end-goal/how-the-factory-works/07-contracts/08-deprecation.md)'s window are demonstrated over a control M10 made available rather than over the confounded comparison alone. Demonstrated by M4's deliberately bad release caught on latency at one operation while its error rate stays flat, on a target keeping a control.

Build ordered steps, one commit per step titled `factory: M11 step N — <name>`, straight to `main`.

# Base commit

`a4f04c9a00fd328162ae0c03efda0a24727fbc59`

# Rules for a step session

- Read `CLAUDE.md`, then `HANDOFF.md`, then only the design files and packages the step
  names.
- First, in `end-goal/claims.txt`, change the step's claims from `unbuilt` to `built`, so
  each package's `doc.go` can cite the claims it implements and `cmd/tracecheck` holds.
  That is the one edit to `end-goal/` a step makes.
- A `doc.go` cites a claim only where the package holds the claim's mechanism; a package
  the claim's subject passes through cites it nowhere, and `cmd/` composes and holds no
  mechanism. A mechanism found in `cmd/` at review moves to its package.
- Before coding, estimate the step's size. Over about 1,000 changed lines, split it in
  the plan into `Na`, `Nb`, … with their own claim lists, and do `Na` alone.
- Run `go vet`, `depscheck`, `tracecheck`, and the step's focused tests; the coordinator
  alone runs the whole suite, which takes most of an hour, and runs it detached from any
  session limit.
- Do not commit and do not push. The coordinator runs `drift-reviewer` on every directory
  whose `doc.go` or screen `README.md` changed, sends the **Not implemented** and
  **Implemented differently** lists back to the same worker session as the next round,
  and commits when **Not implemented** is empty and the suite is green.
- A finding on a claim built before this milestone is recorded in `HANDOFF.md` under a
  heading for the owner and not fixed in the step, unless the step's own change
  introduced it.
- Before returning, record under **Completed** the step, the directories changed, and
  every check run with its result, and move the step's entry out of **Steps**. Keep
  **Unresolved** current.
- A doubt about what the design means is recorded under **Unresolved** with the two
  readings, and the more conservative one is built.

# Where the coordinator stopped

The owner stopped the session during step 4's commit gate. Steps 1 to 3 are committed and pushed. Step 4's tree is committed as the commit after `bd2d1a4` with this note, on these facts: its drift review of `factory/driftdetector` on the final tree returned **Not implemented** empty, and its remaining findings are recorded under **Pre-existing drift**; `go vet`, `depscheck`, `tracecheck`, `git diff --check`, the five focused packages, and the end-to-end drift, rollback, mitigation and bad-release tests passed; the coordinator's full suite was started on the final tree and stopped by the owner before `cmd/factory` finished, with every package that had completed green. The next session runs the full suite first — from `factory/`, detached as below — and, if it is green, moves on; if not, the failure is step 4's and is sent to a fresh worker session as round 3 with a two-sentence preface pointing at the committed tree and step 4's Completed entry.

Step 5 is next: `sed 's/MILESTONE/M11/g; s/STEPNUM/5/g' prompts/build-step.md > <tmp>/step5.md && tools/codex-step.sh <tmp>/step5.md <tmp> high`, then the loop below. Resume the worker's session for rounds 2 and 3; from round 4 on start a fresh session with a two-sentence preface pointing at the uncommitted tree and this file's Completed entry, which is cheaper and does not die at compaction (step 1's single session reached 4.4M tokens and did). A worker's report may claim a drift review it did not run; strike the claim, since the coordinator alone runs them. `git push` from a headless session needs `git -c credential.helper='!f() { echo username=dulguun0225; echo "password=$(gh auth token)"; }; f' push`.

The loop per step: when the worker returns, for each directory whose `doc.go` changed, collect the sentences — `ids=$(grep -o 'C[0-9]\{4\}' factory/<pkg>/doc.go | sort -u | paste -sd'|'); grep -P "^($ids)\t" end-goal/claims.txt | cut -f1,4 > <tmp>/<pkg>-claims.txt` — and dispatch `drift-reviewer` with the directory's absolute path, the claims file's path described as "the current sentence of every claim its doc.go cites, one per line: id, tab, sentence — read that file and the directory and nothing else", the ids cited for the first time in the step named for the closest reading, and "findings on claims whose mechanism is in a file other than <the step's changed files> are pre-existing and already recorded; list them by id only". Start the suite detached at the same time: from `factory/`, `setsid nohup bash -c "go test -count=1 -timeout 60m ./... > <tmp>/suite.log 2>&1; echo \"exit \$?\" >> <tmp>/suite.log" >/dev/null 2>&1 < /dev/null & disown`, and poll `tail -1` for `exit `. It takes 45–60 minutes; `cmd/factory` is nearly all of it. Do not `pkill` it with a pattern the polling shell's own command line contains — that killed the coordinator's shell twice. A round prompt states the rules in one line, lists the findings with `file:line` and the coordinator's decision on each, names pre-existing findings as *owner* items to record here and not fix, and ends with the focused test command to run. Commit when **Not implemented** is empty and the suite is green, condense the step's entry to the form step 1's has, and push.

What step 1 taught, for the rounds ahead: the worker cites claims where the reading happens rather than where the mechanism is (the emitting claims belonged to the implementer's instruction in `agent`, not to `healthmonitor`); it reaches for a value-carrying override when a proportion test needs a transform (the `boundary.Counts` rate override, reverted); it invents a record kind where the design keeps a count (the `failure` record); and a "timed_out, want failed" failure that moves between bad-release tests across suite runs is one timing bound, not several tests. Each of those cost two rounds to find; check for them in the first review of every step.

# Completed

## 1. Versioned emissions and operation quantities

- Directories changed: `factory/agent`, `factory/artifact`, `factory/healthmonitor`, `factory/localtarget` (each with its `doc.go`); `factory/cmd/factory` (its `emission.go` moved into `healthmonitor`; tests, fake model, and fixtures); `HANDOFF.md`; the sixteen flips in `end-goal/claims.txt`.
- Claims built: C0693, C0694, C0695, C1950, C1951, C1952, C1954, C1955, C1957, C1958, C1959, C1961, C1962, C1963, C1971, C1974. C0693, C0694, C1950, C1951, C1952 and C1974 are cited in `agent` (the implementer's standing instruction is what the software emits); the reading claims in `healthmonitor`; acceptance (C1957) and the deploy identity on the record (C1953) in `localtarget`.
- What was built: the software emits two JSON records per request, arrival and completion, naming service, build, deploy, target, operation and the emission version; a failed completion names its failure class and code location; a hazardous operation's count is emitted per interval from the service's store; the arrival carries the service's `unfinished` deadline. The local target is the store's stand-in: it reads the process's standard output and appends each record to the signal file with the acceptance time by its own clock and the deploy fixed at placement. The health monitor reads intervals per operation and target at the shipped resolution, keeps counts and fixed-boundary histograms per outcome, reads `unfinished` once the deadline has passed since the interval ended, and puts every quantity to the boundary as a proportion of counts. `artifact` compares the shipped prompt text of every kind at each start and enters a changed text awaiting its gate. Demonstration: `cmd/factory/emission_test.go`, the M4 bad release slow on one operation with its error rate flat, caught on a target keeping a control, over records the running software emitted.
- Checks on the final tree: `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, `git diff --check` passed; the focused tests passed; the bad-release family passed three consecutive runs; the coordinator's full suite, `go test -count=1 -timeout 60m ./...`, passed (58 packages).
- Drift review, ten rounds: every **Not implemented** list empty; **Implemented differently** items remaining are recorded under **Unresolved** or **Pre-existing drift**.

## 2. The emission gate

- Directories changed: `factory/criterion` (its `doc.go`); `factory/cmd/factory` (the emission gate, its tests, and the watch fixture); `factory/README.md`; `HANDOFF.md`; the two flips in `end-goal/claims.txt`.
- Claims built: C1120 and C1121, cited by `criterion` where it derives and checks the build emission against the shipped readable shape and the area's hazardous operation.
- What was built: `criterion` derives one emitted record shape from a Go build's non-test files, statically finds hazardous-operation record literals, rejects a readable field the previous build emitted but this build stopped emitting, rejects a field the shipped health monitor cannot read, and rejects a missing hazardous operation. A build naming no recognised shape derives as an empty emission; parse failure or multiple shapes is could-not-derive. A previous emission that is absent or could not be derived supplies no stopped-emission comparison. `cmd/factory` derives the previous production build's emission from its recorded commit, passes it with the newest `healthmonitor` shape and the item hazard operation through the existing candidate check, and carries defects to the existing Merge to master mechanical rejection or could-not-derive to the human gate. The watch fixture's `unfinished` deadline is 50ms, so the bad-release test reads the same way at every speed.
- Checks on the final tree: `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, `git diff --check` passed; the focused tests passed; the bad-release test passed five consecutive runs alone; the coordinator's full suite, `go test -count=1 -timeout 60m ./...`, passed (58 packages).
- Drift review, four worker rounds: every **Not implemented** list empty; the one **Implemented differently** item, C1120, is recorded under **Unresolved**.

## 3. Brownout rollback capacity

- Directories changed: `factory/contractcheck` (its `doc.go`); `factory/healthmonitor` (its `doc.go`); `factory/deploy` (its `doc.go`); `factory/cmd/factory`; `HANDOFF.md`; the four flips in `end-goal/claims.txt`.
- Claims built: C1842, C1843, C1844 in `healthmonitor`, where the reading and the exit are; C2057 in `deploy`. The plan had C1842 and C1843 in `contractcheck`, which only answers which release is a brownout.
- What was built: the brownout seam stays one bool. The health monitor opens a brownout's window with the passed exit unavailable, the way a held-out release's is, so it runs to the cap; reads every service against its own recent history while it is open, any crossing failing it; and takes the ordinary failed exit. The fast rollback reads each target's full count from the returned-to release's own deploy record and the kept count from the record that kept it, scales a target back to full before shifting only where the kept count is below it, and a scale-out that fails pages with production still on the failed release named.
- Checks on the final tree: `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, `git diff --check` passed; `deploy`, `healthmonitor` and `contractcheck` passed whole; the end-to-end brownout, rollback, bad-release and revert tests passed; the coordinator's full suite passed on the round 2 tree except two `deploy` resume tests, which the below-full decision from the record fixed.
- Drift review, two worker rounds and one coordinator edit: every **Not implemented** list empty; the remaining **Implemented differently** items are recorded under **Pre-existing drift**.

## 4. Kept-fleet drift

- Directories changed: `factory/driftdetector` (its `doc.go`); `factory/cmd/driftdetector` (its `doc.go`); `factory/targetseam`; `factory/localtarget`; `factory/deploy`; `HANDOFF.md`; and the four flips in `end-goal/claims.txt`.
- Claims built: C2059, C2162, C2163 and C2164, cited by `driftdetector` where the kept-fleet comparison and standing instance-count mitigation are decided.
- What was built: the target seam reports served and side-by-side builds with per-build instance counts, and localtarget reads its running, control and kept process files. The detector selects the kept build and effective count only from open, incomplete windows, compares the reported kept count separately from the served build's count, and persists both counts on an ordinary mismatch. A standing instance-count mitigation carries the count it set and replaces the deploy record's expected count; deploy persists and reads that value.
- Checks on the final tree: `go test -count=1 ./driftdetector ./cmd/driftdetector ./targetseam ./localtarget ./deploy` passed; `go test -count=1 -timeout 30m ./cmd/factory -run 'Test.*Drift|Test.*Rollback|Test.*Mitigation|Test.*Bad.*'` passed; `go vet ./...`, `go run ./cmd/depscheck`, `go run ./cmd/tracecheck`, and `git diff --check` passed.
- Drift review, two worker rounds: every **Not implemented** list empty; the remaining **Implemented differently** items are recorded under **Pre-existing drift**.

# Steps

## 5. Schema history and configuration drift

Claim ids: C2165, C2166, C2170.

Design files to read: `end-goal/how-the-factory-works/08-operations/08-drift-detection.md`.

Packages to read: `factory/driftdetector`, `factory/deploy`, `factory/targetseam`, `factory/localtarget`, `factory/cmd/driftdetector`, and `factory/cmd/factory`.

Mechanism ownership: `driftdetector` compares the target's schema history and configuration digest with the current release build and deploy record, raises the ordinary holding mismatch for each difference, and cites C2165, C2166, and C2170. `deploy` remains the source of the recorded deploy data; `targetseam` adds the running configuration digest to `Running`; `localtarget` reads it from the target; the command packages compose inputs and hold no mechanism. These carrier packages cite no M11 claim. No new dependency edge is needed.

Size bound: about 700 changed lines.

Proof: driftdetector tests cover an extra history row, a checksum mismatch, a missing declared change, and a matching history; target and local-target tests cover reporting the configuration digest; command tests show each mismatch holds and pages production deploys.

Focused checks: from `factory/`, `go test -count=1 ./driftdetector ./cmd/driftdetector ./targetseam ./localtarget ./cmd/factory -run 'Test.*Schema|Test.*Configuration|Test.*Digest|Test.*Drift'`; `go vet ./...`; `go run ./cmd/depscheck`; `go run ./cmd/tracecheck`.

## 6. Values the service record carries, and the demonstrations over a control

Claim ids: none flipped; the claims below are `built` and their mechanism is drifted or absent. The roadmap entry names them for M11.

Design files to read: `end-goal/how-the-factory-works/06-releases/05-the-deploy-record/02-what-stands-for-a-rollback.md`; `end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md`; `end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md`; `end-goal/how-the-factory-works/07-contracts/08-deprecation.md`.

Packages to read: `factory/service`, `factory/deploy`, `factory/healthmonitor`, `factory/contractcheck`, and `factory/cmd/factory`.

Mechanism ownership: `deploy` reads `kept_fraction` off the service record when it sets the per-target kept count (C1681; closes the pre-existing drift row on it) and reads the proof test (C2058) to shift a share of traffic onto the rollback target's instances and back inside an open window. `healthmonitor` reads the recent-history size off the service record through the reader the composition supplies, in place of the constant `cmd/factory` hands it (C1941, C2006, C2014). `service` already holds the values and cites nothing new. `cmd/factory` composes and holds no mechanism. No new dependency edge is needed.

Size bound: about 600 changed lines.

Proof: deploy tests cover the kept count as capacity times the authored fraction, the default when none is authored, and one proof-test shift and return; healthmonitor tests cover the size read from the record. Three `cmd/factory` end-to-end tests, each on a target keeping a control: the window limit reached over a control, the search over a control, and a brownout window over a control. These are the roadmap's demonstrations and depend on steps 1, 3 and 4.

Focused checks: from `factory/`, `go test -count=1 ./deploy ./healthmonitor -run 'Test.*Fraction|Test.*Kept|Test.*Proof|Test.*History'`; `go test -count=1 -timeout 60m ./cmd/factory -run 'Test.*Control|Test.*Brownout|Test.*Limit|Test.*Search'`; `go vet ./...`; `go run ./cmd/depscheck`; `go run ./cmd/tracecheck`.

# Coverage

| Claim id | Step | Package that cites it |
|---|---:|---|
| C0693 | 1 | `agent` |
| C0694 | 1 | `agent` |
| C0695 | 1 | `healthmonitor` |
| C1120 | 2 | `criterion` |
| C1121 | 2 | `criterion` |
| C1842 | 3 | `healthmonitor` |
| C1843 | 3 | `healthmonitor` |
| C1844 | 3 | `healthmonitor` |
| C1950 | 1 | `agent` |
| C1951 | 1 | `agent` |
| C1952 | 1 | `agent` |
| C1953 | 1 | `localtarget` |
| C1954 | 1 | `healthmonitor` |
| C1955 | 1 | `healthmonitor` |
| C1957 | 1 | `localtarget` |
| C1958 | 1 | `healthmonitor` |
| C1959 | 1 | `healthmonitor` |
| C1961 | 1 | `healthmonitor` |
| C1962 | 1 | `healthmonitor` |
| C1963 | 1 | `healthmonitor` |
| C1971 | 1 | `healthmonitor` |
| C1974 | 1 | `agent` |
| C2057 | 3 | `deploy` |
| C2059 | 4 | `driftdetector` |
| C2099 | owner | — |
| C2162 | 4 | `driftdetector` |
| C2163 | 4 | `driftdetector` |
| C2164 | 4 | `driftdetector` |
| C2165 | 5 | `driftdetector` |
| C2166 | 5 | `driftdetector` |
| C2170 | 5 | `driftdetector` |

# Unresolved

- Where more than one open window names the same target and kept build, the kept-fleet expectation uses the largest kept count; the design does not state that case.
- Step 1 leaves three readings the last drift review still listed: the newest-record time on the last check is taken over the windows a pass evaluated, so a pass with no open window writes none (C1977, `watch.go`); the objective's `Spent` read does not pass through the staleness rule the other reads do (C1977, `objective.go`, pre-existing); an emission/3 arm whose arrivals name no deadline has its error rate read as no volume, where the instruction requires every arrival to name one. And C1974's version is a literal in the prompt in force, so after an upgrade a service moves to the new version when the upgrade's prompt passes its gate, not at its next build.
- C0693 has two readings: the design's service-owned store is one count every instance on every target reads, while this local target exposes one file per target and build. The conservative implementation keeps the per-target/build count and does not claim a service-wide aggregate; a service-owned shared store remains unresolved.
- C1973 has two readings: legacy emission/1 and emission/2 lines cannot name their version or carry the current record fields; the reader retains those formats by inferring the version from the recognised line shape and using the outcome line as their only failure signal, while emission/3 names its version and carries failure fields on its completion. The legacy departure remains unresolved.
- The localtarget stand-in accepts process stdout in a factory-process goroutine. A factory restart can leave a running instance accepted by nothing; the design's store is outside the factory. This departure remains unresolved.
- C1964 has two readings: a latency safeguard is a point quantile comparison, or its release tail in the bucket holding the stated duration and above is compared with the threshold's allowed tail share through the boundary. The latter is built; the objective is the separate non-failed-completions-over-arrivals reading stated in `doc.go`.
- C1960 has two readings: request rate is each arm's share of the two arms' interval arrivals, or a per-instance share. The local stand-in builds the former; unequal fleets would require the per-target instance counts carried by the deploy record.
- C0695 has two readings: the hazardous reading is the operation count over the count plus that arm's arrivals, or a rate over another denominator. The bounded count-plus-arrivals proportion is built.
- The history reading has two readings: intervals pair by rank where records have no usable common time, or by timestamps synthesized from their order. Rank pairing through `alignIntervals` is built.
- C2099 is not built in this milestone and is not `stated`: it is a sentence the design contradicts. `02-reports.md` defines a redaction as naming one of three targets — a report, the statement summarizing it, or an artifact version quoting it — and `redaction.TargetKinds` and its database constraint ship those three; `06-incidents.md` says the failure records copied onto an incident are removable by a redaction. The two readings: a fourth target kind, the incident's copied failure records, is added to the three in `02-reports.md` and the code follows; or `06-incidents.md` stops naming redaction and the failure records go with the incident on retention. Neither is the more conservative, because each edits a design file, and a step edits none. The owner decides; the claim stays `unbuilt M11` until then.
- C1120 has two readings: a number "its windows were being decided on" is every field the shipped health monitor can read, since each readable field feeds a quantity a window reads, or only the fields a window of that service actually read. The former is built: the stopped-emission direction rejects any readable name the previous build emitted and this build does not, and reads no window.
- The designs do not specify the exact wire encoding for arrival, completion, failure, histogram, and hazardous-operation records. Inference: retain the existing versioned signal-file boundary and define the smallest versioned record shape that carries the named fields; the service-side writer and fixture format remain for the implementer to choose.
- The designs do not state whether emission declarations are a separate build artifact or fields on the existing encoding derivation. Inference: keep the derivation and rejection in `criterion`, with the command passing the health monitor's readable shape.
- The health monitor currently receives `NamesAHazardousOperation` from composition, while the design says the area names the hazardous operation. Inference: add the area-derived operation mapping at the existing composition seam and do not make healthmonitor import the area package.
- The designs require fixed histogram boundaries and resolution but do not give their values. Inference: use one factory-shipped constant shape for all builds and reject a declaration that differs from it.
- The target seam currently reports instances and schema history but not configuration digest. Inference: add the digest to `targetseam.Running` and have each target implementation report it; the deploy record's digest already exists and remains the comparison source.
- The design says a brownout reads all services' own history while open but does not specify the exact composition callback for that set. Inference: extend the existing `contractcheck.Check.IsBrownout` input seam and keep the health monitor as the only window closer.
- C1969 has two readings: the service record itself enforces the cap on distinct failure-record keys and supplies an overflow bucket, or the outside store enforces that cap while the service only names failure class and raised code location. The local stand-in is the latter and does not enforce the cap; it keeps the count per interval.
- Open's emission-version fallback has two readings: the build record names the emission version it emits, or, until that field exists, open reads the newest shipped emission version. The conservative current reading is the newest shipped version, and the build-record reading remains unresolved until build records carry it.
- C2446 has two readings: the composition tells the store whether a start is the install or an upgrade, or the store infers install from finding no shipped entry. The store builds the latter and enters all install words atomically; the composition reading remains unresolved.

# Pre-existing drift

- contractcheck C1885 `brownout.go:81-127` — the removal is raised on the same evidence key as the brownout rather than on a link naming the brownout's window, and the walk stops at the intent's evidence rather than the contract version the item minted.
- driftdetector C2164 `instances.go:11-15` — whether an instance-count mitigation still stands is decided by the composition, which hands the detector only standing ones, not by the package; the mitigation record is `deploy`'s and the detector does not import it.
- driftdetector C2163 `driftdetector.go:100-113` — a mismatch raised for a kept-count shortfall on a target whose served build was excused still words the served build as the disagreement before the count clause; the row carries no excused flag.
- driftdetector `instances.go:58-60` — a recorded kept count of zero agrees with any running count, a convention the design does not state.
- driftdetector C2161 `exemption.go:55-61` — a nil deployer last check keeps the exemption in force, where the sentence covers only a stale one.
- contractcheck `brownout.go:52-77` — a brownout window closed passed before its cap, skipped, or capped with no volume is reported as a stall where the sentences give that consequence only for a failed window.
- contractcheck `brownout.go:232-241` — "received volume" is either arm counting anything across every quantity.
- contractcheck `brownout.go:92-95,155-157` — the brownout's release is the oldest on the evidence key and the removal any later one.
- contractcheck `check.go:141-157` — what is running is read over the targets the service record names or every target of the environment.
- deploy C1347, C1689 — the score's instance-hours read and the Factory screen's report are not in `deploy`.
- deploy C0938 `rollout.go:412-416` — a refused shift records "without control" and performs nothing further.
- deploy `deploy.go:202-204` — `Backfill.Undecided`.
- deploy `restore.go:99-101` — the fast rollback holds nowhere between targets.
- deploy `restore.go:89-93` — the comment says a reconfiguration refusal falls back to `Restore` and nothing does.
- screens C0418 — no field counts submissions under a shape the store could not read.
- screens C0419, C2604, C2634, C2661, C2704, C2860 — the old-way-in list has no ordering; a drift mismatch renders beside the target rather than over it; the spend ceiling view carries no units spent or period; nothing refuses a People row that acts nowhere; a gate row closed by a sibling holder does not reach an open item screen; no fleet proposal ever waits at Factory.
- deploy C0701 — any completed removal clears the current release regardless ordering.
- criterion C1042 (`queries.go`) — `HumanConfirmed` calls `InForce` without the rejected spec versions.
- criterion C1119 — `encoding.go:176-184`: `NotInForceError` is a third rejection direction over the encodings; this predates M11 step 2 and remains outside it.
- cmd/factory — `reverify.go:142`: re-verification rejects an encoding could-not-derive as a defect where the Merge to master row put a human at it and the human approved; this predates M11 and remains outside step 2.
- deploy C1681 — the kept count is the whole capacity the replaced release had; no owner-authored fraction is read.
- service C1944 — where no run length is authored a component actor's write places no bound, so a safeguard may lengthen it.
- service C2042 — an unauthored window limit resolves to this package's constant for readers outside gate policy rather than a value the score supplies.
- environment C1525 — the composition is stored one line per interface address, so a dependency with two addresses reads back as two entries.
- mergequeue C1432, C1543 — the queue's rejection sends the item back with `ReturnTo`, which counts nothing, so no attempt is counted at Implementation.
- healthmonitor `quantities.go:241-249` — when no health-monitor last-check row exists, staleness falls back to the composed pass interval. This fallback predates M11 step 1 and remains unresolved because the design does not state the case.
- healthmonitor `quantities.go:241-249` — when a last-check row exists with a non-positive interval, staleness falls back to the composed pass interval. This fallback predates M11 step 1 and remains unresolved because the design does not state the case.
- healthmonitor `quantities.go:403-408` — when a quantity carries no authored power, `powerFor` returns zero rather than the design's one-half fallback. This fallback predates M11 step 1 and remains unresolved because the design does not state the case.
- healthmonitor `objective.go:175-190` — the service is held and raised on whichever operation has the least budget left, with no sentence naming which per-operation reading stands for the service. This predates M11 step 1 and remains unresolved.
- artifact `schema.go` — the claimed-nowhere `redacted_content_digest` field and its redaction behavior are pre-existing schema mechanism, outside this step.
- artifact `schema.go` — the claimed-nowhere unauthored-item-kind bound is pre-existing DDL mechanism, outside this step.
- artifact `schema.go` — the claimed-nowhere `format_version` value is pre-existing schema mechanism, outside this step.
- artifact lease fencing — the claimed-nowhere lease token is pre-existing store mechanism, outside this step.
- agent C2430 — seven shipped prompts and none for the role that argues a fleet proposal.
- agent C0525, C0559, C1043, C1082, C0765, C0772 — the interviewer's one-question cap; a criterion authored with no requirement id where the user message lists none; `DraftCriterion` carrying two sources and the default where the sentence has three; the screen machine's states not required to include empty, loading and failed; the `ANSWERS` line naming no requirement; and an unanswerable requirement's reason as free text.
- agent C2386 — `anthropic.go` reads the provider's returned unit fields through its existing response decoding, but the provider-shape reading predates this step and remains unresolved.
- localtarget `ExchangeEnv` — the environment variable is still a platform spelling rather than the design's exchanged record contract.
- localtarget way-in socket — the socket remains bound by the operating system's path limit.
- localtarget `Reconfigure` — the local target's refresh behavior predates this step and remains its existing stand-in.
- localtarget `SetInstanceCount` — the local platform remains bounded to one instance.
- localtarget `ArtifactDigest` — the target's artifact digest behavior predates this step.
- localtarget one `Local` per target — the local target remains one value per target rather than the design's outside target arrangement.
- score C1377 — a service's first release takes a control when the rollout is an adoption.
- notifier C2110 — `FirstAcceptedAt` is preserved across attempts where the sentence has the record overwritten at each attempt.
- healthmonitor C1983 — the search's deploy record names a release in its delivered-release field where the sentence says it names none.
- healthmonitor C2130 — a failed exit resumed after a stop pages about a rollback the factory performed itself.
- healthmonitor C2097 — the incident carries the policy and score versions off the window's open rather than those in force at the crossing reading.
- score C1262 — whether the diff destroys stored data arrives as a measurement field independent of the exposure extractor, so an unreadable extractor leaves reversibility valued.
- score C2034 — an incident with no rollback behind it moves the window's size and power.
- deploy C1875 — backfill completion is gated on a caller-supplied row count rather than every old-form row being present in the new.
- healthmonitor C2598, C2001, C2136, C2114, C2135 — the explicit-threshold reading closes with the window; a failed exit resumed after a stop skips intake's step and pages a rollback as outstanding though it ran; the failed-with-no-rollback wait names no duty and never widens.
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
- criterion C1057 (`unreliable.go`) — once unreliable at `EnteredAt`, a criterion stays so until a re-authored encoding, not while its rate is above the bound.
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
- healthmonitor C1842 — `watch.go:126`, `open.go:64,120`: whether a window is a brownout's is re-read from contract enforcement on every pass rather than resolved at the open and named on the window, which carries only `PassedAvailable` false; a mark lifted mid-window stops the cross-service reading while the window still cannot pass. The window record has no brownout field; adding one is the owner's.
- healthmonitor C1985, C1972, C2021 — `brownout.go:86-87`: the cross-service reading of a brownout's window reads every other service at the composition's own-history size and run length, with the full size map and the producer's operation set, rather than the values in force for that service; the mechanism predates M11 and step 6 changes where the size is read from.
- healthmonitor C2096, C2098 — `rollback.go:241-243,341-347`: a crossing another service took during a brownout's window is recorded on the producer's incident as the producer's own-history reading, and the failure records copied are the producer's on that target rather than the crossing service's; `Crossing` carries no service. This predates M11.
- healthmonitor `brownout.go:22-23,72-78` — a cross-service crossing attributes nothing across services and records no call, and a service running nothing or whose deploy has not completed reads as nothing crossing: conventions no sentence states.
- deploy `restore.go:254-263` — a scale-out refused on a later target pages and leaves the rollback record started rather than failed, the pattern `refused` already takes for a later target.
- deploy C1738 — the deploy package's own notifier seam still pages only at the snapshot and artifact-digest exits; `cmd/factory` now pages a rollback target refusal through the credential wait after the rollback record exists.


- environment C1522 — with no composition reader supplied, `validComposition` skips both checks.
- environment C1397, C1398 — the only post-creation write of the ordered target field appends; no write reorders it.
- criterion C1484 — nothing refuses a candidate-run result for a criterion the build's own process decided; `Latest` lets the run's result stand over the build's.
- criterion `mutation.go` — the Go mutation extractor's `tool` directive, fixed recognized tool list, and two output counts are a pre-existing convention not stated by the design.

- Factory screen C1080, C0865 — no share of criteria in the unwanted-condition pattern; no per-human count of rows acknowledged and decided by another holder.
- Factory screen C0472, C0825, C1289, C0684, C0895, C2607, C2650, C2993 — two store-kept report counters where the design has one; the fleet table shows configuration and not what each agent does; a waiting policy version sits apart from its threshold; area severity per row rather than a count naming none; the approved/undone pair carries no split by cause; no skill version in force; the browser run decides only that the screen is served.
- Work screen C0536, C0609, C0880, C1106, C2570, C0788, C2680, C2683, C2714, C2993 — the intent is not a timeline entry; a question is answered by typing its id; a row waiting on another holder refuses nothing; a version renders as an id; one decomposition decision has no address over its timelines; the empty predicate folds readiness rows in; two screens have no spec over their declared states; the browser run drives no subscription write-back.
- screens C2601, C2605 — the emission version is reported beside no span; one mitigation pointer per service hides a second target's.

# Unresolved from M10

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
