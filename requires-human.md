# Requires human

Findings from bringing `factory/` into compliance with `end-goal/` that are not the code's to
decide: design-internal inconsistencies, subjects the design never settles, and small decisions
coders made in the design's silence that the owner should confirm or override. No disposition is
recorded here beyond what each item already states; an owner triages each into `end-goal/` or a
refusal recorded there.

## Design-internal inconsistency

**How many gate rows belong to no item.** `end-goal/how-the-factory-works/03-gates/README.md:13`
says "the two gates on no item's path". `07-what-particular-gates-decide/README.md:3` and
`03-actions-at-each-gate.md:18` both say three (a role prompt or a skill, a safeguard's
withdrawal, a halt's withdrawal). `09-gate-policy/03-what-is-not-in-it/02-retention.md:7` names a
fourth, undocumented anywhere as such: the gate row that decides shortening decision-log
retention. The factory now builds **five** such rows: `gate.KindRolePromptOrSkill`,
`KindSafeguardWithdrawal`, `KindHaltWithdrawal`, `KindRetentionShortening`, and
`KindLegalHoldWithdrawal` — the fifth built because `09-gate-policy/03-what-is-not-in-it/03-a-legal-hold.md:3`
says a legal hold "ends only at a gate row of its own, held by a human always and routed away from
the human who wrote it, the treatment _A safeguard's withdrawal_ already gets", which is a row of
the same kind that no passage counts. Someone who edits `end-goal/` needs to settle the count and
correct the passages, or say why the retention and legal-hold rows are not two more of the kind.

**Which component raises a deprecation's brownout and removal.** `end-goal/components.md:22` says
"A component does not exist until it has a row here, and a call edge does not exist until the row
of the component making the call names it", and there is no row for a contract check. The code has
one — `contractcheck` writes the brownout intent and the removal intent through intake — so either
`components.md` gains that row with intake among its calls, or the raises belong to a component
that already has one. The design's own candidate is the merge queue, which
`07-contracts/01-two-versioned-things.md` already makes the sole writer of a contract and its
versions "because a contract changes only inside its service's items and every write to it happens
at a release", so it is the one component that sees a mark appear. The cost of that reading: the
removal is raised on a window's close, which the queue has no occasion to read, so on that half the
health monitor fits better and the two raises split across two components on one evidence key —
which is what makes a row of its own the cleaner answer.

**What identifies a revert a human raised.** `06-releases/06-rollback.md:9` gives a revert two
origins, the health monitor at a failed exit and a named human at Ops, and says nothing on the item
says it is one — the reverted release is reachable through the intent's evidence. But `intent`'s
schema constrains evidence to a detector's intent, so an Ops revert has no stored link to what it
undoes, and `09-gate-policy/04-stopping-the-factory.md:33` needs one: a halt must pass a revert
whatever raised it. The code reads it through a seam (`mergequeue.Reverts`) that the composition
supplies with nothing. A field, or a link the human's rollback writes, is the decision.

## Decisions made in the design's silence

- **How much a resolved rejection lowers the threshold by.** `04-risk-score/02-how-it-learns.md`
  gives the direction and no quantum. The code moves it one band (0.05), the amount the threshold's
  rise already uses, floored at 0.05 (`factory/score/rules.go`). Confirm or change the number.
- **The starting risk threshold.** Recalibrated to 0.30 after the exposure factor's formula fix
  (`factory/score/supplied.go`), measured so a service's first release is decided by a human and
  the item after it is not, per the design's own sentence. This is a starting value the score
  moves from outcomes; flag if 0.30 is not where the factory should start.
- **The mutation tool contract.** The design says only "derived per toolchain, with a coverage
  field and a could-not-derive outcome" (`03-gates/07-what-particular-gates-decide/05-implementation/03-what-the-encoding-rests-on.md`).
  The Go extractor (`factory/criterion/mutation.go`) requires a `tool` directive in `go.mod` naming
  one of a fixed list (`mutate`, `go-mutesting`, `ooze`) and reads its output for two counts; any
  checkout without one reads could-not-derive. This convention is not in `end-goal/`.
- **What counts as "an outbound call added" etc. for the exposure factor.** `factory/exposure`'s Go
  extractor is a pattern match over imports, calls, and identifier names (network/exec/db packages,
  a name containing KEY/TOKEN/SECRET/PASSWORD, a removed Authorize/Authenticate/Permit call). The
  design names the four kinds and not how to detect them in Go source; review the patterns.
- **The declares-schema-change reading.** `factory/build`'s `DeclaresSchemaChange` is read off a
  changed `migrations/` or `schema/` directory in the diff — a repository convention `end-goal/`
  does not name and the implementer is not told to follow.
- **How long a spend ceiling's period is measured in.** `10-fleet/08-a-spend-ceiling.md` says an
  owner authors the period "as a length and a start date" and names no unit. The code admits days
  and months (`factory/people`), months because a billing cycle is authored in them and a monthly
  cycle is not expressible in days. Confirm.
- **Where a credential's currency lives.** The design says a ceiling is "in the currency the
  owner's rates are authored in" and does not say what holds that currency. The code makes it a
  field of the lent credential and refuses a second currency on one credential.
- **What makes a diff destroy stored data.** `04-risk-score/01-factors-at-least.md:25` makes
  reversibility a resolved factor where the diff destroys stored data and names no detection rule.
  The code matches a list of destructive statements on added lines (`factory/cmd/factory`), and a
  checkout it cannot read resolves the factor rather than valuing it. Review the list.
- **The source conventions the transition check and the drivers rest on.** `05-implementation/01-the-transition-check.md`
  fixes the check's direction and its could-not-derive outcome and names no source convention. The
  Go extractor expects one file per screen holding a transition function, and a driver marked by
  the screen and state (`factory/screenstatemachine`), parallel to the criterion encoding's id
  match. These conventions are not in `end-goal/`.
- **The weight of the withdrawn-protection factor, and the bound the strategy reads.** The score
  ships the new context factor at weight zero, so no existing number moves, and keeps the control
  bound at 0.5 — the design names which half of the vector the strategy reads and not where the
  bound falls. Both are now published on the score version, so an owner can argue with them.
- **The unreliable bound's fixed default.** `02-spec/02-in-force-and-withdrawal.md:9` says the
  bound is "a field of the service record with a fixed default" and gives no number. The column
  stays empty until an owner authors one; nothing reads a default.
- **Instance-hour and environment-hour rates, the bake volume, the backlog cap, the mutant cap,
  the operation cap.** All now fields an owner authors on the service record
  (`factory/service/parameters.go`), each with a `gatepolicy`/`safeguard` entry, per the design.
  Nothing has authored a value yet; every reader treats an absent one as no bound. Confirm the
  fields match what `02-an-environment-per-candidate/README.md` and `03-room-and-what-an-environment-costs.md`
  intend before an install relies on the defaults.

## Questions the design does not settle

Each of these is a place where two readings of `end-goal/` are both defensible and the code had to
pick one. The pick is named so it can be overruled.

- **Whether the retention-shortening row and Decomposition name a duty.** Both rows route away from
  the human who wrote what they decide, but neither names a duty, so `anotherDeciderExists` is
  false and the bar can mark a self-approval and never refuse one. That follows the design's own
  "wherever one exists", but "routed to a human other than the one who authored the shorter value"
  is only honoured in full if a duty attaches — 8 is the obvious candidate for retention. The same
  question attaches to duty 10 and the revert's own gate row.
- **One page per drift mismatch or two.** A mismatch now pages twice: the detector's sweep, on the
  mismatch, which a human ends by clearing it, and the gate firing it holds, on the row, which a
  human ends at the row. They reach the same holders. Collapsing them loses the second's tie to the
  decision; keeping both means one condition, two pages.
- **Which page a firing sends where a drift mismatch and a revert decision hold at once.** A page is
  the sequence of events on one row, so the code sends the mismatch's. The design names both
  conditions and not their order.
- **Whether a decomposition member that answers no requirement is a third rejection direction.**
  [_Decomposition_](end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md)
  names two directions and neither covers an item answering nothing. The row now fires and that
  member scores as the smallest set rather than erroring the run out, which is the smallest change
  that is grounded; a third direction is the design's to add.
- **Which component writes the platform's last check.** `deploy.RecordPlatformCheck` and
  `lastcheck.Writer.RecordPlatformPass` are two writers of one record and neither is called. The
  one-writer rule says one of them goes.
- **How a backfill declares itself.** [_The store is a contract too_](end-goal/how-the-factory-works/07-contracts/09-the-store-is-a-contract-too.md)
  says a backfill item declares "only the element it fills and the element it fills from" and names
  no file or syntax. The Go derivation expects `backfill.<store>.go` carrying one
  `//borg:backfill <element> from <element>` line, chosen to match the contract extractor's file
  convention. A backfill item cannot merge on this platform either way: the double-application check
  needs a candidate environment with a store, and this one has none, so the check reports that
  nothing ran — which the design reads as not passing.

- **What a security predicate asserts.** [_What a consumer declares_](end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md)
  says the second list exists, is per toolchain, is decided against the build, and takes a
  could-not-derive outcome, and names no kind. The list, its versioning and its derivation are
  built and ship no kind, so the merge row's rejection over it has a producer and nothing to fire
  on until the content is decided. "An owner may only extend it" waits on the install event.
- **What exit a window that measures nothing takes.** [_Overlapping windows_](end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md)
  says "timing out counts only because a release that never served cannot time out", and
  [_The analysis window_](end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md)
  says such a window "records only that it measures nothing" without naming an exit. It closes
  timed out — leaving it open would fill the window limit and hold the service on a reading nobody
  can take — and the readers that pick a rollback target now descend past it. A fifth exit is the
  alternative, and it is the design's to add.
- **Whether the control belongs to the deploy or to the target.** [_The health monitor_](end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md)
  says "There is one control per production target the release has reached … and the deploy record
  names each"; the record carries one control target and one control release for the whole deploy,
  and what says a control runs on a target is that row's instance count. Moving the two fields onto
  the target rows is the change, and it touches the deploy's readers.

## Left to a component the factory does not have (stated in the owning package's `doc.go`; listed
here only so the count is not lost)

The four screens, the fleet entry and its claim/expiry/renewal, the evaluation set and its result,
the report store and the way in, the redaction record, the constraint record, advisories, the
install/upgrade first-start step, context assembly's selection of what an agent reads, and the
role-prompt-or-skill gate's own firing. Every mechanism that reads one of these takes it as a
parameter or an interface the composition does not yet supply, rather than a substitute the code
invented.

Several seams are built on both sides and joined by nothing, each because the component between
them is one of the above: the People declaration's credentials and rates reach no agent run, and
the spend ceiling is authored and compared by nobody, because dispatch reads the fleet entry that
does not exist; the transition check and the screen drivers are derived and checked but fired by no
build, because nothing hands the Implementation row a checkout; a platform that keeps a fleet closes
no instance-hour span, this one keeping none; the unreliable bound is authored and read back by
nobody; and no agent is ever handed a skill, so the versions the vector names are empty for want of
anything to name — nothing composes a skill at all.

Nothing writes a rework request anywhere, so the outcome
[_Going back up_](end-goal/how-the-factory-works/03-gates/06-going-back-up.md) gives the per-author
prior has no writer and the prior would read zero even once it reads the shape. An authored service
level objective holds that service's production deploys for good on this composition, because the
emission cannot say whether its store covers the period, which is what would lift the hold.

Three smaller gaps of the same kind, each stated where it sits: the effort an entry names is sent to
the provider in the field that provider documents, but no model this install can reach offers the
parameter, so the path is never exercised against one that accepts it; the repository credential an
install writes is a name resolving to nothing, a repository here being a directory; and an owner
clears a score version waiting at the factory-settings scope only by re-authoring the number, there
being no confirmation call for that scope as there is for an environment's.
