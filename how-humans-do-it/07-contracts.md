# Contracts

## Two versioned things

A project's software is one or more services, and they reach each other through published interfaces — REST under OpenAPI, gRPC under protobuf, Kafka under topic schemas. Those interfaces have consumers, and the consumers are other services in the same factory.

So there are two versioned things and they must not be collapsed into one. A release number orders the builds of a single service. A contract version is a compatibility promise to whoever calls it, and semver is precisely what that is for: major means a consumer breaks. One service can publish several contracts — an API, a gRPC service, three topics — and they move at their own rates. A single number per service cannot carry several independent promises, and a promise that moves for unrelated reasons stops being read. Most releases publish no new contract version at all.

## No single item may break a contract

**No single item may break a contract.** A breaking change is three items: the producer adds the new form beside the old, each consumer migrates, the producer removes the old. Each ships alone, is verified on an environment of its own, and is independently reversible. This is the same discipline no-batching already forces — an item that cannot ship by itself was cut wrong — and the two rules hold each other up. Where a change genuinely cannot be decomposed, that is an escalation (12), not a licence to batch — and taking it over grants a human no power the factory lacks, since the pen changing hands changes the author and not the stages, the gates, or this rule. What the escalation asks for is a re-scope, not a co-deploy built by hand.

## Compatibility mode

Every contract carries a **compatibility mode**, declared by the service that publishes it and enforced on every diff after that.

| Mode | The promise | Who needs it |
|---|---|---|
| **backward** | the new build reads what the old one wrote | a consumer of an interface — the default |
| **forward** | the build coming back reads what the newer one wrote | anything a rollback reads |
| **full** | both, and costs both | a store |

The mode belongs to each contract rather than to the factory, because an interface nobody rolls back and a store every rollback reads do not need the same promise.

## Enforcement

The rule is mechanical rather than a judgment call. The factory diffs the contract a candidate publishes against the one in production: a diff the contract's mode allows passes, and a breaking diff without the migration already in front of it is a rejection at the merge gate. The same graph answers who is affected — the factory knows which services consume which contracts, so "what does this break" is a query rather than an estimate, and it feeds the context factor of the risk score directly.

## What a diff cannot see

A diff proves the shape still holds, not that callers survive. A field that quietly stops being populated, or that keeps its name and its type and changes its units, breaks a consumer without breaking a contract. No mode catches it: a mode is checked against the schema, and the damage is in the semantics. Three layers answer it, and not one of them is a recording.

**Meaning is declared, so the diff can see it.** A unit belongs to a field's identity rather than to a note about it — `send_time_millis`, not `send_time` — and so does whether the field is always populated. A change of units is then a rename, a rename is a breaking diff, and the three items of a migration handle it with nothing new. The cost lands on authoring: the factory holds a contract to the declaration when it is written, an interface adopted from outside the factory arrives without it, and retrofitting the name is itself a breaking change — three items to rename a field no behaviour depends on. It covers what someone thought to declare and nothing else.

**The producer compares its own observables.** Per field, on the producer's own traffic: population rate, the spread of enum values, the distribution of numbers, each against the release this one replaced. A field populated 99.9% of the time for a year that drops to 80% is invisible to a schema check and loud in a profile, and a unit swapped for one a thousand times finer moves every number by a thousand. What it does with a finding is raise an item and feed the context factor of the risk score; it rejects nothing, because a statistical call is arguable where a schema diff is not, and enforcement stays mechanical by not admitting one. Every number in it is the producer's own, so nothing has to be recorded or attributed across services. An owner who wants explicit thresholds pins them (9).

**A consumer's assumptions are checked against the candidate.** The factory already knows who consumes what, so the candidate standing on its own environment is checked against every consumer's declaration, and a failure is a rejection at the merge gate like any breaking diff. No window bounds it — a consumer called once a month is covered exactly as well as one on a hot path. [_What a consumer declares_](#what-a-consumer-declares) sets out what may be declared, where a declaration comes from, and what it costs.

Replaying each consumer's recorded actual usage is the layer that is not built, and not for its size. What a recording proves is bounded by what ran inside its retention window, so the expensive option is also the one with the hole: a consumer on a monthly path falls outside any window worth keeping. A declaration buys the same protection without the recording.

What survives all three is a semantic change nobody declared and no distribution shift reveals. What catches that is the consumer breaking in its own error rate and raising an item there, the criteria confirmed at the Spec gate, and veto after the fact (10) while the change is still undoable.

## What a consumer declares

**A declaration is a predicate, not prose.** It has to be decidable by the factory against one observed exchange, and it is drawn from a catalog the factory owns: the field is read at all, it arrives populated, it carries this unit, its values stay inside this domain or this range. The catalog grows with the rest of gate policy (8); a consumer picks from it and cannot invent a kind of assertion at declaration time. An assumption that will not fit a predicate is not a declaration — it stays the consumer's own acceptance criteria, and an item against the producer if it is ever violated.

**It is derived from the consumer's build, not filed.** The factory reads the consumer and derives what it assumes, again at every release, so a declaration cannot outlive the code that motivated it. A consumer that stops reading a field stops declaring it: the list [_Deprecation_](#deprecation) waits on shortens itself and the removal item is raised, that mechanism unchanged and now fed by something nobody has to remember. A consumer with no items for a year declares what its last release read, which is right — its code has not changed, so neither have its assumptions.

**Its authority comes from the gate, not from the derivation being right.** A derived declaration rides in the consumer's item as an artifact of it, ratified at the gates that item already passes — factory-authored, scored like anything else, read by a human where a pin (9) puts one there. Ratified that way it can reject a producer's candidate; before that it rejects nothing, which is true of every artifact here. A rejection later found wrong is a bad call the score learns from, like a veto.

**Two baselines, kept apart.** A producer's own diff runs against the contract in production, because the promise is to what is running. A declaration is checked against the producer's newest release, because that is what the consumer will meet. Both merge gates check at the moment they fire, so the race between the two resolves either way round it happens: a consumer that newly declares a field the producer is part-way through removing fails at its own gate, or the producer's removal candidate fails at its.

**Where derivation cannot see, an owner pins the predicate.** How much of a consumer's reading is visible is a property of that interface's toolchain rather than of the factory — a generated stub turns a field read into an accessor call, hand-parsed access hides it. A read the derivation misses is an unprotected assumption; one it invents pins a field until the next derivation drops it or the producer's blocked removal item asks the consumer to confirm. A pin is authored and maintained by an owner, so the blind case does not rot unseen. Deriving at all presumes the consumer's build is the factory's to read, which [_Two versioned things_](#two-versioned-things) settles: a consumer outside the factory is the blind case with a harder cause — no build to read and no mode of its own — so declared meaning and the producer's own observables carry it alone, with a pinned predicate standing in for the declaration.

## Who owns a contract

A contract belongs to the service that publishes it, and changes only inside that service's items. It gets no thread of its own: a promise is only real when something serves it, so the authority to accept a change to one belongs where there is a build to check it against.

What a consumer gets is the mode the publisher declared for its need, and what it owns is the assumptions its own build declares — not a seat at every gate the producer stands at. A declaration checked mechanically is not a veto — what answers it is the diff, not a person. The alternative was a per-change consumer veto, and it deadlocks — in a graph where nearly every service is somebody's consumer, items wait on each other until the attempt bound turns the pile into escalations (12), which is the human load the factory exists to remove. An objection raised after the fact is an item against the producer, joined to the original by intent, like any bug, and an owner who wants a human on a particular contract buys one with a pin (9).

## Deprecation

Deprecation is an obligation the contract carries, not a note someone leaves. When the producer adds the new form, the old one is marked with its consumers attached; as each migrates the list shortens, and when it empties the factory raises the removal item itself. Nobody has to remember step three.

## The store is a contract too

A service's store is a contract too, and its consumer is the service's own past — the release that was running a minute ago, which a rollback can put back. That consumer is why a store's mode is **full**: every rollback reads across the change in the direction backward alone does not cover.

The rule holds there unchanged: **no single item may break the store.** A breaking schema change is three items — the store gains the new form beside the old, the code migrates onto it, the old form is dropped — and while it stands, the old form carries the same deprecation obligation an old interface carries.

Enforcement costs nothing new: the factory diffs the schema a candidate carries against the one in production, and a destructive diff without the migration already in front of it is a rejection at the merge gate. The cost lands on small work — renaming a column is three items and three trips through the pipeline. What it buys is the rollback in Releases, which is otherwise a promise about code made over data that has already moved on.

## Work that spans services

Work that spans services needs no new noun. One request can produce four items in four services, and the intent is what joins them — every item already walks back to the intent that caused it, so "everything that came from this request" is a query that works today. The three items of a contract migration are the same shape: one intent, three items, three releases.
