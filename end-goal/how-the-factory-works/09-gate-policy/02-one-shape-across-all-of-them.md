# One shape across all of them

**The score supplies what an owner does not.** Authoring is an override rather than a requirement: a factory with nothing authored in it runs, on values the score sets and moves as outcomes arrive. A default nobody chose is still a decision, and it can stay invisible until it takes effect — the [_window limit_](../08-operations/03-overlapping-windows.md) is the clearest case.

**A safeguard (9) can only add.** A **safeguard** is what an owner puts on top of what this section gathers: a human required at a gate, or a parameter narrowed. The whole of what it may do is add protection. The direction differs per parameter and points the same way in each. It is a ceiling over the window's size, the window limit, the held-out sample rate, the attempt limit, the item-size target, the exposure bound and the [advisory severity](01-what-is-in-it.md), and a floor under the confidence the window requires, the power it requires, its cap, the review sample rate, and the list of allowed predicate kinds. Three add rather than clamp: a human at a gate, a [_rollout strategy_](../03-gates/02-the-rollout-strategy.md) that keeps a control, and an [explicit health threshold](../08-operations/01-the-health-monitor.md) applied in addition to the comparison rather than instead of it. A safeguard clamps and does not take precedence: the value in force is what an owner authored where they authored one and what the score supplies otherwise, clamped by the safeguard — a ceiling caps it, a floor raises it, and a safeguard never replaces a value already narrower than itself. Read as a precedence, a ceiling of five over the window limit would override an authored two and raise the number, which is a safeguard adding throughput and removing safety. The score keeps moving inside a safeguard and never past it, so an owner who adds one on a single value has not frozen the rest.

**A safeguard is a record.** One record with one writer, [Factory](../11-screens/01-work-ops-factory-people.md), naming its subject, what it binds, its direction, and, where it puts a human at a gate, the [duty](../../what-humans-do.md) or the named human its rows route to. The subjects are a stage, a service, a project, an [area](../../what-humans-do.md), a [contract element](../07-contracts/01-two-versioned-things.md), a component of the [design system](../02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) in force on a project, this section's own list, [the report store](../02-intent-into-items/01-intake/02-reports.md), and [the drift detector](../08-operations/08-drift-detection.md)'s last check. One on a component puts a human at [_Implementation_](../03-gates/07-what-particular-gates-decide/05-implementation/README.md) for every item whose build uses it, and it is there so that an owner who distrusts one part of a design system does not have to buy the check for a whole area. The routing field lets the person who authored a check receive it. Without it the rows go to the duty the design names at that gate and to the owner everywhere else, so a [compliance officer](../../what-humans-do.md) who safeguards a regulated area authors a check somebody else answers, and an owner who safeguards a production deploy gate stands in front of every release on that service personally and cannot delegate it. The routing already exists, a [decision](../03-gates/01-where-a-gate-is-and-what-decides-it.md)'s open event naming the duty or named human it waits on, so this is a field on the safeguard rather than a mechanism. What it costs: a safeguard can name a human who later holds no duty, which routes the same way an unheld row already does — it widens to the owner.

**A safeguard is not edited.** Withdrawing one is a second record naming the first, and correcting one is a withdrawal and a second record — the treatment a [constraint](../02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) and a [role prompt](../10-fleet/03-what-an-agent-is-told/README.md) version already get. The value in force is a read of the newest record for that subject. A field flipped in place would leave nothing saying the check ever existed, when it was removed, or by whom, which matters here more than anywhere else: a withdrawal is the one operation in the factory that removes a human from a gate, and the document refuses that mechanism twice elsewhere. So a withdrawal is decided and not merely written: it takes a gate row of its own, [_A safeguard's withdrawal_](../03-gates/07-what-particular-gates-decide/10-a-safeguards-withdrawal.md), held by a human always and routed to the human the safeguard names rather than to whoever wrote the withdrawal, and until that row approves it the safeguard stands. What the second record costs: a subject with a long history is a query rather than a field read, and the actor on it is self-asserted until [seam 1](../../deferred.md) carries a principal.

The alternative was a field on the record its scope names, and it breaks for three of the subjects. A safeguard on a predicate kind names a contract element as its subject, and that record's writer is [the merge queue](../05-environments/03-the-merge-queue.md), so an owner authoring on it would be a second writer with no [seam](../../deferred.md). A safeguard on a design-system component names a field of a [constraint](../02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) record, which intake writes and nothing ever edits. And a safeguard setting a maximum age on the drift detector's last check would have the factory writing into a store it may never write. Five readers and one writer is the shape the [release record](../06-releases/02-the-release-record.md) already has. What it costs: a query over safeguards by subject rather than a field read on the record already in hand, so every mechanism a safeguard binds does one more read, and a safeguard naming a subject nobody declared is a dangling reference nothing detects until the mechanism looks for it.

Ten of those directions have to be named rather than read off the row, because both ends of each are bad in their own way. A ceiling over the window's size reads backwards: the size is the smallest regression the comparison must rule out, so capping it holds the window to catching something at least that small, where a floor would let the value be raised until it caught nothing worth catching. A floor under the [window's power](../08-operations/02-the-analysis-window.md) is the one clamp that does not simply raise the value the mechanism reads. The exception is stated here because this is where a builder reads the clamp rule. The volume a comparison needs turns on the power, so a floor the service's traffic cannot support would coarsen the size that window watches for, which is a safeguard paying for its own floor in protection. So the size is not coarsened. Where a power floor would coarsen that size, the size stands and the window cannot close [passed](../08-operations/02-the-analysis-window.md). It stays open until its cap ends it, so what the safeguard spends is the window's chance to close early on evidence and never the size the service is watched for, and what that costs is written where the rule is.

An authored power costs the same and not less. [_The analysis window_](../08-operations/02-the-analysis-window.md) states that rule once for whoever set the rate, an owner, the score, or a safeguard alike, so the size is never what pays for a power. The direction is named here because a floor whose cost falls somewhere other than the value it clamps is the one a builder would otherwise read off the row wrongly. A floor under the window's cap keeps a release under watch on a service too quiet to reach that volume, and the next deploy waits for it — protection is the longer end here, which is why the floor is the direction and not the ceiling. A ceiling over the attempt limit adds human work to stop spending on retries, which is what a safeguard does everywhere else. A ceiling over the item-size target produces smaller items, each shipping by itself and passing its own gates.

A safeguard on that list may only extend it: a kind of assertion added is coverage added, and one removed would invalidate consumer contracts already [ratified at a gate](../07-contracts/06-what-a-consumer-declares.md). What keeps a wider list from losing the mechanical enforcement its own cell describes is that list's own rule, a predicate decidable against one observed exchange, which is a floor no safeguard goes below. A ceiling over the held-out sample rate is the one whose bad end is not a loss of protection: sampling less keeps humans at gates, at the cost of the evidence a threshold needs to rise at all — the safeguard buys caution and pays for it in calibration. A floor under the [review sample rate](../03-gates/01-where-a-gate-is-and-what-decides-it.md) points the other way, and the pair is why both are named: one keeps a human at a gate by sampling less of what the score would have gated, the other by sampling more of what it would have passed. Its bad end is human time spent on changes the factory was right about. A safeguard on the report store points one of four ways, each adding protection: a human admitting a report-derived intent before anything is spent on it, a human admitting an arrived report before [the grouper](../02-intent-into-items/01-intake/02-reports.md) reads it, a human at [the gate that decides the grouper's role prompt](../03-gates/07-what-particular-gates-decide/09-a-role-prompt-or-a-skill.md), and a narrowing of [the channel's rates](../02-intent-into-items/01-intake/02-reports.md). The last reaches zero where an owner closes one service's way in, and each of those costs is stated where the channel is defined.

**Scope follows the mechanism, not the duty.** Each parameter is a field of the record its
scope names, holding what an owner authored and nothing else: where the field is empty the
value in force is what the score supplies at that moment, so a factory with nothing authored
runs and the score never writes another component's record. That field is the value in
force, and it is the only home the authored value has. The policy version below is the copy
the audit trail keeps, and never what a mechanism reads, so no gate reads the log to fire and
a retention setting cannot destroy a value an owner authored.

**What an owner authored is versioned the way the score's own values are.** A **policy
version** is a row of the [decision log](../../deferred.md), appended by the log at each
owner write, with Factory as the component that calls for the append. One parameter has no
owner to wait on at install: the [list of allowed predicate kinds](01-what-is-in-it.md) ships
with a starting list nobody authored, so the [install's first-start
step](../10-fleet/07-a-fleet-proposal.md) calls for the same append at install and at an
upgrade's first start that changed it, naming the [shipped-bundle
identity](../../deferred.md#the-products-release-channel) that list came from. It is
[chained](../../deferred.md) like every row there, and it names every authored parameter and
the scope it was authored on. It
is a row of the log and not a record of its own because a decision names it by identifier,
and a version rewritten outside the chain would move the meaning of every decision naming it
while the log verified clean. Every
[decision](../03-gates/01-where-a-gate-is-and-what-decides-it.md) names the policy version in
force, which [_Traceability_](../../what-the-factory-does/02-traceability.md) makes the
harder half of the claim it rests on, and a version is what that name can point at.
Overwriting a version in place would leave an auditor told a change was decided under one
and shown nothing, since a decision read back against today's values is not the decision that
was made. It also carries who wrote it, so what an owner set and when is a fact of the record
rather than of somebody's memory. The rest is the same construction the [score
version](../04-risk-score/01-factors-at-least.md) already has for the values the score
supplies.

An owner's write is two records, so it takes the order [_Tight
integration_](../../what-the-factory-does/01-tight-integration.md) sets for one. The log
appends the version first and Factory writes the scope record's field second, the trail's
copy before the value in force, so a stop between them leaves a version naming what nothing
yet reads rather than a value in force that no version records. Factory's start re-derives every
authored field from the newest version naming its scope, which is what finishes a write a
stop interrupted. The version is keyed on the write and the field on its scope and
parameter, so a step taken again writes nothing and a re-derivation that finds the two
already agreeing writes nothing either. It costs a version per authored write, most
differing in one number, and one read of the log per start rather than per gate.

One field on the version is computed rather than authored. Where the write sets a [risk
threshold](01-what-is-in-it.md), the version also carries the [realized auto-pass rate at
that threshold](../04-risk-score/01-factors-at-least.md) as it stood when the write happened,
computed by Factory in the same call that appends the version and frozen there. It is frozen
because the version stays in force while the rate moves under it, and a query over the
decisions taken under that version could not be the reference that movement is read against.
It is also the one field a later version does not restate: a write that touches another
parameter appends a version naming the threshold it did not change and no rate beside it. So
the reference for a threshold in force is the rate on the newest version that set that
threshold on that scope, which is a walk back through the versions rather than a read of the
newest one. That is the cost of freezing a number at the moment it meant something, and the
reason the field is named as the rate at a setting rather than as the version's own.

The window limit, the analysis window's parameters and the [exposure
bound](01-what-is-in-it.md) are per service and are fields of the [_service
record_](../02-intent-into-items/03-decomposition/README.md): the [exposure
list](../04-risk-score/01-factors-at-least.md) the bound is read against is derived from one
service's build diffed against that service's current release, so what counts as reach the
service did not have before is that service's own. The item-size target
is per area and is a field of the [_area
record_](../02-intent-into-items/03-decomposition/02-what-an-item-names.md). A gate row's
threshold is a field of an environment record — a deploy row into a persistent environment
reads the environment it deploys into, and every other row reads production's, which
[_Records, and one long-lived
branch_](../05-environments/01-records-and-one-long-lived-branch.md) says exists everywhere,
so it is there before the item is. All eight rows of the [default
path](../03-gates/03-actions-at-each-gate.md) read production's,
[Decomposition](../03-gates/07-what-particular-gates-decide/01-decomposition.md) firing
before any [candidate environment](../05-environments/02-an-environment-per-candidate.md)
exists and a candidate's own environment being created at the gate that decides its deploy,
so unable to hold the threshold deciding it. A row a customer's extra environment adds reads
that environment. [_A role prompt or a
skill_](../03-gates/07-what-particular-gates-decide/09-a-role-prompt-or-a-skill.md) reads the
factory-wide settings record this section defines, having no project and so no production
environment to read, and it is still the same parameter: the eight rows are eight whatever a
row's threshold is a field of.

Five parameters have no customer record their scope reaches, and they share one. The attempt limit is per stage, with the interview's rounds and decomposition's re-decompositions counted against it too, and the stages are the factory's own. The list of allowed predicate kinds is one list the factory owns and an owner extends. The [review sample rate](../03-gates/01-where-a-gate-is-and-what-decides-it.md) is per [duty](../../what-humans-do.md), and a duty is the factory's own the way a stage is. The [held-out sample rate](../04-risk-score/02-how-it-learns.md) is the score's own: one formula selects, factory-wide, and what the sample is evidence for is that formula's calibration, so the rate has no service to be a field of, and a safeguard (9) on a service, a project or an area is what narrows it there. The [advisory severity](01-what-is-in-it.md) is read by one pass over one feed, the factory's own, and reaches every project at once. All five are fields of the **factory-wide settings record**, written by Factory, which exists before any project does and which an owner may never open. Putting the limit on production's environment record instead would make it per project as well as per stage — a scope this section does not give it, and one that duplicates by field what a safeguard already does. The threshold [_A role prompt or a skill_](../03-gates/07-what-particular-gates-decide/09-a-role-prompt-or-a-skill.md) reads is a field of it as well, and belongs there for the reason the other three do — the row is the factory's own and reaches every project at once.

Report retention, [decision-log](../../deferred.md) retention, and the [report channel's two rates](../02-intent-into-items/01-intake/02-reports.md), per service and factory-wide, bounding arrival at the way in, are fields of it too, being authored, factory-wide, and not gate policy. A **retention floor** is a field of it too, bounding how low an authored value or a safeguard may ever take decision-log retention: neither may go under it. It is written two ways only: at [the gate row that decides a shortening](03-what-is-not-in-it/02-retention.md), or by the constraint kind whose subject is a factory parameter. A records-retention law arriving as such a constraint writes the floor at arrival, instead of being read at drafting. A **remediation period** per [advisory](../02-intent-into-items/01-intake/03-advisories.md) severity is a field of it too, authored outright with nothing supplied: no outcome teaches how long a fix should take, so there is nothing for the score to supply. A safeguard (9) narrows the report channel's two rates and the remediation period further, on any subject a safeguard may name. Decision-log retention takes a stated direction instead: a safeguard on it is a floor, and it may lengthen and never shorten. Report retention takes the opposite direction: a safeguard on it may shorten and never lengthen, report text being personal data. Whether [seam 5](../../deferred.md#security-comes-last) is enforced is a field of it too: an owner turns it on once and nothing turns it off again. A safeguard does not reach that field; only a [constraint](../02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) of the document kind may require that it be turned on.

**The duty does not shrink, and its reach grows.** The [backstop duties](../../what-humans-do.md) (11, 12) fall away as the factory improves. This one does not, and what it governs widens as they go: these eleven rows are what the factory may do with no human deciding anywhere, so the better it gets, the more of its behaviour they describe. The review sample rate is on the list from the other side of the same fact, being how much of what the factory may do unattended an owner has a human read anyway.
