# Requires human

Findings from bringing `factory/` into compliance with `end-goal/` (2026-09-05/06) that are not
the code's to decide: a design-internal inconsistency, and small decisions coders made in the
code's absence that the owner should confirm or override. No disposition is recorded here beyond
what each item already states; an owner triages each into `end-goal/` or a refusal recorded there.

## Design-internal inconsistency

**How many gate rows belong to no item.** `end-goal/how-the-factory-works/03-gates/README.md:13`
says "the two gates on no item's path". `07-what-particular-gates-decide/README.md:3` and
`03-actions-at-each-gate.md:18` both say three (a role prompt or a skill, a safeguard's
withdrawal, a halt's withdrawal). `09-gate-policy/03-what-is-not-in-it/02-retention.md:7` names a
fourth, undocumented anywhere as such: the gate row that decides shortening decision-log
retention. The factory now builds four such rows (`gate.KindRolePromptOrSkill`,
`KindSafeguardWithdrawal`, `KindHaltWithdrawal`, `KindRetentionShortening`). Someone who edits
`end-goal/` needs to settle the count and correct the three passages, or say why the retention row
is not a fourth of the same kind.

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
- **Instance-hour and environment-hour rates, the bake volume, the backlog cap, the mutant cap,
  the operation cap.** All now fields an owner authors on the service record
  (`factory/service/parameters.go`), each with a `gatepolicy`/`safeguard` entry, per the design.
  Nothing has authored a value yet; every reader treats an absent one as no bound. Confirm the
  fields match what `02-an-environment-per-candidate/README.md` and `03-room-and-what-an-environment-costs.md`
  intend before an install relies on the defaults.

## Left to a component the factory does not have (stated in the owning package's `doc.go`; listed
here only so the count is not lost)

The four screens, the fleet entry and its claim/expiry/renewal, the report store and the way in,
advisories, the install/upgrade first-start step (so no schema history has ever run, no shipped
role prompt has ever been entered by an upgrade, and no per-author prior has ever shipped),
context assembly's selection of what an agent reads, and the role-prompt-or-skill gate's own
firing. Every mechanism that reads one of these takes it as a parameter or an interface the
composition does not yet supply, rather than a substitute the code invented.
