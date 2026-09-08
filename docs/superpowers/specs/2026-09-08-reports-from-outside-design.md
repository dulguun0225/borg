# Reports come in from outside

M9, the one way into the factory from outside it: the way in shipped inside every deployed
service, the report store, the grouper, redaction and the erasure list, and the report
under its intent at _Work_.

## The problem

`end-goal/claims.txt` carries 105 claims marked `unbuilt M9`, 93 of them in
`how-the-factory-works/02-intent-into-items/01-intake/02-reports.md`. Nothing in
`factory/` writes a report, and the two duties M8 demonstrated as absent — duties 4 and 5
of `end-goal/what-humans-do.md` — are the ones this milestone performs.

M8 left every piece the channel hangs on and no channel:

- `factory/deploy/rollout.go` mints a way-in token per deploy (`mintWayInToken`) and
  writes its digest to `way_in_token_digest` (`factory/deploy/schema.go`); `restore.go`
  mints one too. `factory/deploy/doc.go` says the column "is written at every deploy and
  read by nothing".
- `targetseam.Deployment.WayInToken` carries the token (`factory/targetseam/seam.go`),
  and `factory/localtarget/local.go` puts it on the started process as `BORG_WAY_IN`.
- `factory/factorysettings/schema.go` holds `report_retention_seconds`,
  `backup_retention_seconds`, `ReportChannelRateTable` and `PageCapTable` with no reader.
- `notifier.KindHarmMarkedReport` exists (`factory/notifier/wait.go`) and
  `factory/notifier/harmmark.go` counts against the cap.
- `factory/score/factorread.go` already resolves `intent.SourceReports` to a human at Spec.
- `artifact.Store.Redact` destroys spans (`factory/artifact/redact.go`) and says its caller
  "is not built: the redaction record has no package".
- `safeguard.SubjectReportStore` exists (`factory/safeguard/safeguard.go`).
- `intent.Delivery.Outcome` is supplied by the caller because "the rate's input is the
  report store, which is not built" (`factory/intent/acceptance.go`).
- `decisionlog.ShapeReadEvent` exists, appended by the unexported
  `(*Reader).appendReadEvent` (`factory/decisionlog/read.go`).

## Decisions

- The way in is content the factory ships and versions with itself (C0406, C0407, C0408,
  C0414), so on this platform it is Go source the factory's build injects into every
  service's build, not a process the factory runs.
- The injection is `go build -overlay`, applied at both build sites in
  `factory/cmd/factory/repo.go`, so nothing is written into the checkout the implementer
  is handed and nothing is committed.
- The report store is outside the record graph (C0376): its own PostgreSQL store, its own
  opener and applier over `REPORTSTORE_DATABASE_URL`, the arrangement
  `factory/driftdetector/schema.go` already has.
- The erasure list is a file on the host, outside the recovery unit (C0452), with one
  writer and two callers (C0453).
- Redaction is a record in the graph written by Factory; each target's own writer destroys
  its own bytes (C0447, C0448), and where a writer is in another store the composition
  hands it the redactions rather than the writer reaching across (section 4).
- The grouper is a package, not a pass inside `cmd/factory`, because a `doc.go` under
  `cmd/` cites no claims and these claims need a home.

## 1. The way in — `factory/wayin/`

Owns the shipped Go source, the overlay that injects it, and the factory-side entrance.
Owns no table. It is the component C0005 names.

**The shipped source.** A `package main` file held as constants in `wayin`, the way
`factory/agent/` holds shipped role prompts. It is a template with one substitution point,
the shipped-bundle identity, so no second name for that value exists: `cmd/factory` passes
`factoryVersion` (`factory/cmd/factory/main.go`), the same value `createBuild` already
writes onto the build record as `ShippedBundleIdentity` (`factory/cmd/factory/repo.go`).
That constant is what makes the way in move only when the factory is upgraded and the
service builds again (C0414, C0415, C0417).

The file's `func init()` reads three environment variables and starts a listener:

| Variable | What it is |
|---|---|
| `BORG_WAY_IN` | the token, existing (`localtarget.WayInEnv`) |
| `BORG_WAY_IN_STORE` | the entrance address, new |
| `BORG_WAY_IN_LISTEN` | a Unix socket in the target's directory, new, the way `localtarget.SignalFile` works |

The listener serves two things: the notice on GET, read from the store at the open so it
moves without a rebuild (C0434), and a submission on POST, whose result — accepted, or
refused with the rate as the reason — is rendered in the same session (C0433, C0436,
C0437, C0469). It presents the token on every submission, which is how it calls as the
deploy that placed it (C0421). It is not built from the project's design system (C0413).

**The overlay.** `factory/cmd/factory/repo.go`'s `compiles` and `buildInto` each gain one
call that adds the file to the build. Both must get the same overlay: `createBuild`'s
comment records that two builds of one commit were measured byte-identical, which is what
the drift detector's comparison of the build's artifact digest against a running target's
rests on, and an overlay on one site and not the other breaks it.

**The entrance.** An `http.Handler` with two routes, the notice at open and the submit,
over a `Store` interface `cmd/factory` implements; mounted in `served()`
(`factory/cmd/factory/serve.go`).

`deps.txt`: `wayin -> principal record`. `wayin` splits only to "way in", and `in` is a
function word `CheckPackageNames` refuses (`factory/cmd/tracecheck/names.go`), so it needs
a `namedOtherwise` entry with the design phrase it stands for.

Claims: C0005, C0376 (in part), C0406, C0407, C0408, C0414, C0425, C0433, C0434, C0436,
C0437, C0469.

## 2. The report store — `factory/reportstore/`

The store and the report record (C0004, C2792). Its own opener and applier over
`REPORTSTORE_DATABASE_URL`, with `Open` reaching the store once before returning, as
`driftdetector.Open` does. One writer: `Store`.

**The report row.** id `rep_`; `format_version`; `collected_at`;
`shipped_bundle_identity` (C0415, C0417); `deploy_id`, `service_id` and `environment_id`
taken from the deploy record the token digest finds and never from the submission (C0420,
C0421); `kind`, bug or complaint (C0375); `text`; `harm_marked`, the reporter's own field
that nothing infers (C0386, C0387); `source_key`, opaque, salted, session-scoped (C0425,
C0426, C0427); `notice_id` or empty (C0435); `intent_id`, empty meaning ungrouped (C0388,
C0470); `admitted_at` (C0439). There are no actor columns: the row names no person, a
departure from the shape every record package has, stated in the package's `doc.go`
(C0427, C0437).

**Arrival.** The two rates are read from `factorysettings` — the factory-wide one a column
of `Table`, the per-service one a row of `ReportChannelRateTable` — and enforced at the way
in (C0423, C0424). A fixed share of each rate is reserved for a harm-marked report, not
itself authored (C0430, C0431). The factory-wide rate at zero closes the channel and a
safeguard narrowing a service's to zero closes one service's (C0428).

**Counters.** One table keyed by service and by channel, holding refusals (C0471) and
submissions under a shape the store does not read (C0418). These are counters and not
queries, because the record a query would count is the write the rate exists to refuse
(C0472). A submission naming no deploy the store knows is counted on the whole channel and
never on a service (C0422).

**Retention.** `report_retention_seconds` read by this store and by nothing else (C0390),
refused while a legal hold stands (C0389), and reports kept for the life of the install
where an owner authored nothing (C0391).

`deps.txt`: `reportstore -> erasurelist principal record`. Interfaces the composition
implements, which is how the store reaches the graph without importing it: `Deploys`
(digest to service and environment, C0421), `Settings`, `Holds` (C0389), `ReadEvents`
(C0444), `Redactions` (C0448, section 4).

Claims: C0004, C0375, C0376, C0383, C0386–C0391, C0415–C0418, C0420–C0428, C0430, C0431,
C0435, C0447, C0448, C0449, C0453, C0470–C0472, C2792.

## 3. The erasure list — `factory/erasurelist/`

An append-only file on the host, outside the recovery unit (C0452, C2794). One line per
row, the payload format version first, the treatment seam 2 gives a log row (C0455); then
the key, the instant, and what was removed — never the words. Keyed so a step taken again
writes nothing (C0446). Rows are read by kind, so each store replays the rows naming its
own records against whatever a restore brought back before serving again (C0456). The
store retires a row once the erasure it records is older than
`backup_retention_seconds`, which has no other reader (C0457, C0458), and keeps rows for
the life of the install where an owner authored none (C0459). The record inventory lists
it (C0454).

The package owns the row type, `Append` and a read by kind. `reportstore` is the one
writer (C0453): Factory calls the store at a redaction, and `people.DeleteMapping` takes
an erasure-list appender the composition supplies, so `people`'s `deps.txt` line does not
change and the two callers meet one writer.

`deps.txt`: `erasurelist -> record`.

Claims: C0450, C0452, C0454–C0459, C2794.

## 4. Redaction — `factory/redaction/`

The redaction record (C2793): id, actor, reason, `target_kind` — a report, a statement, or
an artifact version — `target_id`, spans, the erasure-list row's key, `created_at`. It
never carries the words (C0441), and no link breaks, because what is erased is text inside
a record and never the record (C0442). One writer: Factory, through `policy`, the writer a
safeguard, a halt and a legal hold already have (C0445). The same action writes the
erasure-list row first (C0446).

The destruction is each target's own writer (C0447), each on a pass of its own (C0448):

| Target | Writer | How it reads the redactions |
|---|---|---|
| report | `reportstore` | the `Redactions` interface the composition implements — the store is a second database and an import would cross it |
| statement | `intent` | imports `redaction` |
| artifact version | `artifact` | imports `redaction`; `artifact.Store.Redact` already destroys spans |

Every reader reads the target through its redactions from the moment the redaction exists,
so a store whose destruction lags serves nothing meanwhile (C0449). A read appends a read
event, which is what makes who had already read the words answerable (C0444).

`deps.txt`: `redaction -> record lease legalhold`; `policy -> redaction` added to policy's
line; `intent -> record lease redaction erasurelist`; `artifact -> ... redaction
erasurelist`; `postgres -> ... redaction` so `Apply` applies `redaction.DDL`, with a
`postgres.Changes` entry.

Claims: C0440–C0446, C2793.

## 5. The grouper — `factory/grouper/`

The pass that turns reports into intents. It reads the ungrouped admitted reports of one
project, dispatches the grouper role, and applies the answer.

- Five hundred reports of one slow button are one intent (C0377). It is an agent, because
  deciding whether two free-text reports are one problem is model work (C0378).
- It runs under a fleet entry in a role put on neither an intent nor an item: its scope is
  a project, and it reads that project's reports and nothing else (C0379).
- The first report of a group raises an intent through intake and later matching ones
  attach; nothing waits for a batch or a count (C0384, C0385).
- A later report attaches and never rewrites (C0395, C0396).
- Decomposition is the boundary (C0397): before it the grouper may split a group it got
  wrong; after it a matching report attaches and raises the count, and one that does not is
  a new intent linked to the first as a recurrence (C0398, C0399), which is also the rule
  for a report arriving after the fix shipped.
- The grouper prefers to group, because the two failure directions are not symmetric
  (C0394).
- For a grouped intent the reports are what the interview and the spec are authored
  against, and the statement summarizes them (C0400).
- A harm-marked report fires the page `notifier.KindHarmMarkedReport` already carries,
  bounded by the cap in `PageCapTable` and not by the source key (C0432).
- Grouping takes no gate (C0393) and moves no per-author prior (C0382). Both are absences,
  demonstrated by a test rather than by code.
- The two admission safeguards, both on `safeguard.SubjectReportStore`: one holding a
  report-derived intent until a human admits it at _Work_, dispatch putting no agent on it
  (C0438); one holding an arrived report ungrouped until a human admits it, so only an
  admitted report reaches the grouper (C0439).
- The grouper's own run record names the project and the processing location the credential
  resolved to (C0462), beside the named credentials report-derived work spends through
  (C0461), and intake writes the grouping link (C0031, C0381, C0383).

`deps.txt`: `grouper -> dispatch intent reportstore notifier record lease principal`.

Claims: C0031, C0377–C0379, C0381, C0382, C0384, C0385, C0393–C0400, C0432, C0438, C0439,
C0461, C0462.

## 6. The notice — `constraint`

`factory/constraint/schema.go` has one kind, `KindDocument`. It gains `KindNotice`, the
sixth kind read by no drafting stage and rejecting nothing, its reach one project (C0262),
versioned the way every constraint is — never edited, replaced by a withdrawal and a
second record (C0264). A reader, `NoticeInForce`, gives the report store the notice at the
open so a notice moves without the service building again, and the report names the notice
in force when it arrived or that none was (C0265). Where an owner authored none the way in
shows none, and Factory lists each service under no notice beside its refused count
(C0266). The text is the owner's to author and never the factory's.

Claims: C0262, C0264, C0265, C0266.

## 7. Existing packages that change

| Package | Change | Claims |
|---|---|---|
| `deploy` | a read by way-in token digest, the column `doc.go` says nothing reads | C0421, C0422 |
| `targetseam` | `Deployment.WayInAddress` beside `WayInToken` | C0421 |
| `localtarget` | the two new environment names and the socket path | — |
| `dispatch` | `RoleGrouper`, a role whose scope is a project | C0379 |
| `agent` | the shipped grouper role prompt | C0381 |
| `agentrun` | a `project_id` column and a widened `served_names_something` check, which today is `item_id <> '' or intent_id <> ''` | C0379, C0462 |
| `intent` | `Intake.Redact`, a recurrence link, and `Delivery.Outcome` computed from the store's rates rather than supplied | C0392, C0398, C0399 |
| `artifact` | the redaction pass `redact.go` says is not built, and the replay | C0448, C0456 |
| `decisionlog` | `appendReadEvent` exported as `AppendReadEvent` | C0444 |
| `people` | `DeleteMapping` takes an erasure-list appender and a replay reader | C0453 |
| `policy` | the redaction write, beside the safeguard, halt and legal-hold writes | C0445 |
| `score` | cites what `factorread.go` already does at Spec | C0371, C0401–C0405 |
| `postgres` | `redaction.DDL` in `Apply` and a `Changes` entry | — |
| `cmd/factory` | the overlay at both build sites, the entrance in `served()`, the grouper pass in serve, the erasure ordering, the replay at start, the view fields | — |
| `factory/README.md`, `deps.txt`, `names.go` | the four new packages | — |

`cmd/factory` additions are new files. `advance.go` is 16.0K and `viewfactory.go` 14.5K
against a 500-line bound, so neither grows.

## 8. The screens

**Work** gains the report under its intent and nowhere else (C0473, C2678): `Reports
[]ReportSummary` on `screens.Item` (`factory/screens/viewwork.go`), two `Calls` methods,
readers in `cmd/factory`, the two admissions as actions (C0438, C0439), and the rendering
in `client/src/app/work/item.html` and `item.ts`. The claim ids go in the `What defines
it` section of `client/src/app/work/README.md`, which already cites C2679 onward from the
same file.

**Factory** gains: ungrouped reports (C0470); refusals per service and over the channel
(C0471, C0472); submissions under an unreadable shape (C0418); each service whose current
release's build names an identity older than the running release's, read off the
`ShippedBundleIdentity` `createBuild` already writes (C0419); each service under no notice
(C0266); the intent's outcome beside cost per feature (C0392); and the erasure as one
action (C0443, C0445).

## 9. Build order

Each step is one commit, keeping `go vet`, `depscheck`, `tracecheck` and `go test` green,
flipping its claims from `unbuilt M9` to `built`, adding its `deps.txt` line and
`factory/README.md` row, and followed by one `drift-reviewer` dispatch on every `doc.go`
or screen `README.md` it writes.

1. `erasurelist` — no database.
2. `reportstore`, with `deploy`'s read by token digest.
3. `wayin`, with a test that builds a throwaway module with the shipped file injected.
4. The wiring: `targetseam`, `localtarget`, the overlay at both sites in `repo.go`, the
   entrance mounted in `served()`.
5. The notice: `constraint.KindNotice`, `NoticeInForce`.
6. `redaction` and the destruction paths: `policy`, `intent`, `artifact`, `people`,
   `decisionlog`, `postgres` `Apply` and `Changes`.
7. The grouper role: `dispatch`, `agent`, `agentrun` with its `Changes` line.
8. The grouper pass, with `score` citing C0371 and C0401–C0405.
9. Work.
10. Factory.
11. The demonstration tests, `DEMO.md`, `README.md`, and `REPORTSTORE_DATABASE_URL` in
    `.github/workflows/factory.yml`, which today sets `DATABASE_URL` and
    `DRIFTDETECTOR_DATABASE_URL`.

## 10. The demonstration

Three tests in `cmd/factory` on the existing `newPath`/`run`/`newScreens` fixture
(`factory/cmd/factory/fixtures_test.go`, `advance.go`, `screensfixtures_test.go`).

**`reports_test.go`.** `run` ships one change, so `localtarget` has a live process started
with the token, the address and the socket. The test dials the socket and asserts:

- the notice is the owner-authored one (C0434);
- a posted bug report returns its submit result in the same call, with no record about a
  person written (C0436, C0437, C0469);
- the stored row names the deploy record's service and environment, not the submission's
  (C0421);
- an unknown token is refused and counted on the channel and not on a service (C0422);
- an unknown shipped-bundle identity is counted per service (C0418);
- a channel rate authored to zero closes the channel and the refusal is counted (C0428,
  C0471).

**`grouping_test.go`.** The fake model (`fakemodel_test.go`) scripted with a grouper reply:

- two intents and not three (C0377);
- the first report raises one (C0384) and a later one attaches without rewriting (C0395);
- the statement summarizes the reports (C0400);
- the source resolves a human onto Spec (C0371, C0402, C0403);
- the run record names the project and the processing location (C0462);
- a harm-marked report fires one page per intent (C0432);
- no gate row is written for the grouping and no per-author prior moves (C0382, C0393).

**`erasure_test.go`.**

- the erasure-list row lands before the redaction (C0446);
- the words are gone from the report, the statement and the artifact version, with the
  links standing (C0442, C0447);
- a read before the redaction appended a read event (C0444);
- the list names the removal and never the words (C0452);
- a legal hold refuses the removal and the refusal is recorded (C0389);
- a replay after a rewind destroys the words again (C0456).

## Decisions taken in the session

- **`func init()` in the shipped way-in source.** The root `CLAUDE.md`'s "Explicit over
  implicit" forbids `init` in the factory's own packages. The way in is content shipped
  into someone else's `main`, where nothing calls a start function, so `init` is the only
  hook that needs no cooperation from the service's code, which C0406 and C0408 refuse to
  ask for. The departure is stated in `wayin`'s `doc.go`, and the shipped source is the
  one file in the repository that carries an `init`.
- **`go build -overlay` and no fallback.** The overlay maps the path of a file in the
  checkout to a file the factory writes in a temporary directory, which is what the flag
  is for, so the checkout is never written to and nothing can be left behind. A build the
  overlay fails is a failed build, reported the way any other is.

## Open items

- The `deps.txt` edges for the redaction pass were not stated in the plan this document
  was written from. Section 4 resolves them one way — `intent` and `artifact` import
  `redaction`, `reportstore` takes it through an interface because it is a second database
  — and that resolution is the design's, not the owner's, but it is the one place a
  reviewer should look first.

## Claims this milestone does not build

| Claims | Where they go | Why |
|---|---|---|
| C0409–C0413 | M12 | predicate-kind constraints over the way in; M12 is where the predicate-kind constraint is built at all |
| C0380 | M12 | the processing-location constraint on the grouper's dispatch, for the same reason |
| C0429, C0451, C0460 | `stated` | statements with no mechanism to build |

C0382 and C0393 stay in M9 and become `built`: they are absences, and the test in section
10 is what holds them.

## Out of scope

- A reporter handle, a status for a reporter, or a way to contest a grouping (C0468).
- A subject key indexing reports by the people their text mentions (C0466).
- Screening report content before an agent or a screen reads it (C0467).
- Recalling words from a provider that received them (C0463).
- A browsable list of reports on any screen (C0473).
- The build runner as a component of its own, which is M10: the overlay goes into
  `cmd/factory`'s existing build sites and moves with them.
- A shipped-bundle identity that is the bundle's rather than `factoryVersion`, which is
  M13.
