# Contracts

## Two versioned things

A project's software is one or more services, and they reach each other through published interfaces — REST under OpenAPI, gRPC under protobuf, Kafka under topic schemas. Those interfaces have consumers, and the consumers are other services in the same factory.

So there are two versioned things and they must not be combined into one. A release number orders the builds of a single service. A contract version is a compatibility promise to whoever calls it, and semver is precisely what that is for: major means a consumer breaks. One service can publish several contracts — an API, a gRPC service, three topics — and they move at their own rates. A single number per service cannot express several independent promises, and a promise that moves for unrelated reasons stops being read. Most releases publish no new contract version at all.

## No single item may break a contract

**No single item may break a contract.** A breaking change is three items: the producer adds the new form beside the old, each consumer migrates, the producer removes the old. Each ships alone, is verified on an environment of its own, and is independently reversible. This is the same discipline no-batching already forces — an item that cannot ship by itself was cut wrong — and the two rules support each other. Where a change genuinely cannot be decomposed, that is an escalation (12), not permission to batch — and taking it over gives a human no power the factory lacks, since a human authoring a stage instead of an agent changes the author and not the stages, the gates, or this rule. What the escalation asks for is a re-cut, not a co-deploy built by hand.

The three items are also where the version moves, and it moves twice: the addition is a minor, each consumer's migration leaves it where it is, and the removal is the major. Inside the factory that major warns nobody — the list [_Deprecation_](#deprecation) keeps is what emptied to raise the removal item, so every consumer the factory can read had already migrated — and what it records is that the old form is gone. Its promise is kept for the consumer the factory cannot read, which is the one case where major means what semver says it does.

## What a contract promises

Every contract promises **backward** compatibility — the new build reads what the old one wrote — which is what a consumer of an interface needs. A store promises **forward** as well: the build being restored reads what the newer one wrote. Both are enforced on every diff after that.

Nothing is declared. Which promise a contract makes follows from whether it is a store, which the factory already knows, and there is no third case: forward alone has no user, because anything a rollback reads is read going the other way too. A declared mode would be three values where the kind of the thing decides among them, and each of the three is one more thing every builder implements identically.

The drawback is that a derived promise is assumed where a declared one is checked. A publisher that wants more than its kind provides has nowhere to say so, and the factory's answer to a wrong derivation is that there is nothing to derive — the kind is a fact about the contract and not a judgment about it.

## Enforcement

The rule is mechanical rather than a judgment call. The factory diffs the contract a candidate publishes against the one in production: a diff the contract's promise allows passes, and a breaking diff without the migration already shipped ahead of it is a rejection at the merge gate. The same graph answers who is affected — the factory knows which services consume which contracts, so "what does this break" is a query rather than an estimate, and it feeds the context factor of the risk score directly.

## What a diff cannot see

A diff proves the shape is unchanged, not that callers still work. A field that quietly stops being populated, or that keeps its name and its type and changes its units, breaks a consumer without breaking a contract. Neither promise catches it: both are checked against the schema, and the damage is in the semantics. Three layers answer it, and not one of them is a recording.

**Meaning is declared, so the diff can see it.** A unit belongs to a field's identity rather than to a note about it — `send_time_millis`, not `send_time` — and so does whether the field is always populated. A change of units is then a rename, a rename is a breaking diff, and the three items of a migration handle it with nothing new. The drawback is the authoring: the factory holds a contract to the declaration when it is written, an interface adopted from outside the factory arrives without it, and retrofitting the name is itself a breaking change — three items to rename a field no behaviour depends on. It covers what someone thought to declare and nothing else.

**The producer compares its own observables.** Per field, on the producer's own traffic: population rate, the spread of enum values, the distribution of numbers, each against the release this one replaced. A field populated 99.9% of the time for a year that drops to 80% is invisible to a schema check and obvious in a profile, and a unit swapped for one a thousand times finer moves every number by a thousand. What it does with a finding is raise an item and feed the context factor of the risk score; it rejects nothing, because a statistical call is arguable where a schema diff is not, and enforcement stays mechanical by not admitting one. Every number in it is the producer's own, so nothing has to be recorded or attributed across services. An owner who wants explicit thresholds pins them (9).

**A consumer's assumptions are checked against the candidate.** The factory already knows who consumes what, so the candidate running on its own environment is checked against every consumer's declaration, and a failure is a rejection at the merge gate like any breaking diff. No window limits it — a consumer called once a month is covered exactly as well as one on a hot path. [_What a consumer declares_](#what-a-consumer-declares) sets out what may be declared, where a declaration comes from, and what it requires.

Replaying each consumer's recorded actual usage is the layer that is not built, and not for its size. What a recording proves is limited to what ran inside its retention window, so the expensive option is also the one with the gap: a consumer on a monthly path falls outside any window worth keeping. A declaration gives the same protection without the recording.

What none of the three catches is a semantic change nobody declared and no distribution shift reveals. What catches that is the consumer breaking in its own error rate and raising an item there, the criteria confirmed at the Spec gate, and veto after the fact (10) while the change can still be undone.

## What a consumer declares

**A declaration is a predicate, not prose.** It has to be decidable by the factory against one observed exchange, and it is drawn from a catalog the factory owns: the field is read at all, it arrives populated, it has this unit, its values stay inside this domain or this range. The catalog grows with the rest of gate policy (8); a consumer picks from it and cannot invent a kind of assertion at declaration time. An assumption that will not fit a predicate is not a declaration — it stays the consumer's own acceptance criteria, and an item against the producer if it is ever violated.

**It is derived from the consumer's build, not entered by hand.** The factory reads the consumer and derives what it assumes, again at every release, so a declaration cannot outlive the code that motivated it. A consumer that stops reading a field stops declaring it: the list [_Deprecation_](#deprecation) waits on shortens itself and the removal item is raised, that mechanism unchanged and now triggered by something nobody has to remember. A consumer with no items for a year declares what its last release read, which is right — its code has not changed, so neither have its assumptions.

**Its authority comes from the gate, not from the derivation being right.** A derived declaration travels with the consumer's item as an artifact of it, ratified at the gates that item already passes — factory-authored, scored like anything else, read by a human where a pin (9) puts one there. Ratified that way it can reject a producer's candidate; before that it rejects nothing, which is true of every artifact here. A rejection later found wrong is a bad decision the score learns from, like a veto.

**Two baselines, kept apart.** A producer's own diff runs against the contract in production, because the promise is to what is running. A declaration is checked against the producer's newest release, because that is what the consumer will meet. Both merge gates check at the moment they fire, so the race between the two resolves either way round it happens: a consumer that newly declares a field the producer is part-way through removing fails at its own gate, or the producer's removal candidate fails at its.

**Where derivation cannot see, an owner pins the predicate.** How much of a consumer's reading is visible is a property of that interface's toolchain rather than of the factory — a generated stub turns a field read into an accessor call, hand-parsed access hides it. A read the derivation misses is an unprotected assumption; one it invents pins a field until the next derivation drops it or the producer's blocked removal item asks the consumer to confirm. A pin is authored and maintained by an owner, so the blind case does not go unnoticed. Deriving at all assumes the consumer's build is the factory's to read, which [_Two versioned things_](#two-versioned-things) settles: a consumer outside the factory is the blind case with a harder cause — no build to read at all — so declared meaning and the producer's own observables are all that cover it, with a pinned predicate replacing the declaration.

## Who owns a contract

A contract belongs to the service that publishes it, and changes only inside that service's items. It gets no thread of its own: a promise is only real when something serves it, so the authority to accept a change to one belongs where there is a build to check it against.

What a consumer gets is the promise its producer's kind of contract makes, and what it owns is the assumptions its own build declares — not a decision at every gate the producer passes. A declaration checked mechanically is not a veto — what answers it is the diff, not a person. The alternative was a per-change consumer veto, and it deadlocks — in a graph where nearly every service is somebody's consumer, items wait on each other until the attempt bound turns them into escalations (12), which is the human work the factory exists to remove. An objection raised after the fact is an item against the producer, joined to the original by intent, like any bug, and an owner who wants a human on a particular contract gets one with a pin (9).

## Deprecation

Deprecation is an obligation of the contract, not a note someone leaves. When the producer adds the new form, the old one is marked with its consumers attached; as each migrates the list shortens, and when it empties the factory raises the removal item itself. Nobody has to remember step three.

The list contains the consumers the factory can derive, so a consumer outside the factory is on it only where an owner pinned a predicate replacing that consumer's declaration — the blind case [_What a consumer declares_](#what-a-consumer-declares) sets out, and blocking the removal item is what the pin does here. Unpinned, it is not on the list at all: the list empties while that consumer still reads the old form, and the factory raises the removal on schedule. The drawback is that the only warning an outside consumer gets is the major version, and nothing here delays the removal for it.

## The store is a contract too

A service's store is a contract too, and its consumer is the service's own past — the release that was running a minute ago, which a rollback can restore. That consumer is why a store promises **forward** as well: every rollback reads across the change in the direction backward alone does not cover, and it is the only kind of contract with a consumer of that kind.

The rule is the same there: **no single item may break the store.** A breaking schema change is three items — the store gains the new form beside the old, the code migrates onto it, the old form is dropped — and while it remains, the old form has the same deprecation obligation an old interface has.

Enforcement requires nothing new: the factory diffs the schema a candidate declares against the one in production, and a destructive diff without the migration already shipped ahead of it is a rejection at the merge gate. The drawback falls on small work — renaming a column is three items and three trips through the pipeline. What it makes possible is the rollback in Releases, which is otherwise a promise about code made over data that has already changed.

## Work that spans services

Work that spans services needs no new record type. One request can produce four items in four services, and the intent is what joins them — every item already links back to the intent that caused it, so "everything that came from this request" is a query that works today. The three items of a contract migration are the same: one intent, three items, three releases.
