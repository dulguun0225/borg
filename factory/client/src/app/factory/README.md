# Factory

The machine itself, at `/factory`, at two child paths that scroll the same screen to one of
its sections, and at one address with an id: a constraint's own. Its sections, in the order
the screen renders them: the readiness reading and the items stopped at dispatch by cause;
the fleet, the role prompts and the fleet proposals; the rows that decide a record rather
than an item; gate policy, the safeguards, the halts and the legal holds; the environments;
the projects and areas; the constraints in force; the report channel with the erasure an
owner performs here; and the factory's own numbers.

`/factory/fleet` and `/factory/constraints` are two dead links made live: dispatch names
both as where a stop it wrote is lifted from (`liftedAt` in `../../../../cmd/factory/views.go`),
and a safeguard's rows route the same way. Each renders `factory.ts` again with the named
section as its `section` input, an effect scrolling to the element of that id once the
screen is ready. `/factory/constraints/:id` is the fourth address a human can be at on this
screen: `factory-constraint.ts` reads `GET /api/constraint/{id}` and subscribes to the
`constraint` stream kind, with the standard four-state machine besides ready.

Two reads answer it. The readiness reading per role is a field of the home view rather than
of the Factory view, and it is on this screen because the record that covers a role — a
fleet entry, a role prompt version — is authored here; the one subscription on this address
keeps both true.

| File | What it owns |
|---|---|
| `factory.ts`, `factory.html` | The screen: the machine, the two reads, the readiness reading, the items stopped at dispatch, the one path every call takes, the `section` input the two child routes carry, and the busy signal that disables every submitting control while a call is in flight |
| `factory-constraint.ts`, `.html` | One constraint's own address, `/factory/constraints/:id`: every field the list row shows, read-only, on the `constraint` stream |
| `factory-fleet.ts`, `.html` | The fleet with every field per entry, the form that writes one — the scope as its three fields and the credential chosen from the lent credentials not taken back — and the withdrawal, the role prompts with the version in force and any version awaiting its gate, that row's three actions, and the fleet proposals |
| `factory-policy.ts`, `.html` | The rows that decide a record, every parameter with its effective value and which read it came from, the form that authors one, the safeguards — whose form takes the service a gate-row safeguard fires for, keyed by the row — the halts, the legal holds, and seam 5 enforcement, off at install and turned on once here |
| `factory-places.ts`, `.html` | The environments, the projects and areas with their forms and the project each area's chain ends at, ending a project and retiring a service — each armed by a first click and sent by a second on the same typed name — and the constraints in force with the three widest reaches listed by reach and a link to each one's own address; the form that supplies one takes a kind, document or notice, a notice's reach always one project |
| `factory-reports.ts`, `.html` | The report channel: reports waiting to be grouped, refusals per service and over the whole channel, submissions the store could not read, each service serving a way in built under another release of the product, each service whose project has no notice — and the erasure as one action, armed by a first click and sent by a second, `spansTyped` refusing anything that is not a half-open byte range |
| `factory-numbers.ts`, `.html` | Throughput, rework rate, gate rejection rate, cost per feature with each intent's outcome beside it, the gates a resolved factor put a human at, the human's load with the approve-and-undone pair and the self-approval counts, the auto-pass rates against the recorded ones with the held-out bands, the spend ceilings, and the page channel |
| `request.ts` | What a section asks the screen to send |
| `factory.e2e.ts` | The browser run: the screen driven in Chromium against the factory's own process |
| `factory-constraint.spec.ts`, `factory-reports.spec.ts` | The two specs split off the screen's own at the 500-line bound, each named for what it drives |

The screen is split into sections because one component for all of it would pass the
500-line bound. A section is not a fifth screen: it holds no address, no subscription and
no state machine, and it performs no call — it emits the call it wants made, and
`factory.ts` holds the one path every call takes, refused while the subscription is down,
re-reading the address before it sends and again after. `../../../eslint.config.js` allows
`Section` beside `Screen` and `Shell` as a component class suffix for that reason.

The rows and the forms are rendered outside the state machine's `@switch` rather than
inside its branches. A change on this address re-reads it, which moves the screen through
loading, and a dropped subscription moves it to disconnected: a body inside a branch would
be destroyed and built again on either move, and the sections hold what a human has half
written into a form. It is withheld only in the failed state, which is the one state that
says nothing was read.

The shape every screen directory holds: `README.md`, `factory.ts`, `factory.html`,
`factory.css`, `factory.spec.ts`, `factory.e2e.ts`, `factory.routes.ts`, and `format.ts`,
with one further file pair — `factory-constraint.ts`, `.html` — for the address under it,
the way Work and Ops each hold one for theirs. The sections beside them are named
`factory-<subject>.ts` with a template each. `format.ts` is a copy of the same file in
the other three screen directories, and `request.ts` is a copy of the one under
`../people/`; a screen imports `api/` and `state/` and nothing else outside its own
directory, and those copies are what the boundary costs.

Tests for `factory-constraint.ts`, and for the two child routes that scroll `factory.ts` to
a section, are in `factory-constraint.spec.ts` rather than in `factory.spec.ts`, and the
report channel's numbers and its erasure are in `factory-reports.spec.ts`: `factory.spec.ts`
is at the 500-line bound, and locality's answer to a file at the bound is to split by subject
rather than grow it. Those two are this screen's departure from a spec per screen directory,
each named for what it drives.

`factoryMachine` is declared in `factory.ts`: the five states, the transitions between
them, and no terminal state.

## What the view does not carry

The Factory view leaves out, because nothing builds them yet, what its own doc comment
names: the advisories and the registries watched for them, the mutation score, and the
design-system numbers. Each is left out rather than shown as zero, so a screen answering
nothing is read as unbuilt and not as a quiet week. Beside those, the design's Factory
bullet asks for these and the view carries no field for them: whether the page a report
marking harm can cause is turned off, environment-hours per item and instance-hours per
release, criteria withdrawn per service and per author with the count unreliable, the hours
a mitigation has stood per target — which Ops carries per service — the product licence per
service against what its current release resolved, and the rows each human referred and
rejected split by what put them at the row.

## What defines it

[Work, Ops, Factory, People](../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md) (C2572, C2588, C2589, C2593, C2607, C2608, C2612, C2613, C2620, C2624, C2629, C2631, C2632, C2633, C2634, C2638, C2650, C2651, C2652, C2653);
[What the factory auto-approved, and what was undone](../../../../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md) (C2719, C2722, C2724, C2725);
[The page channel, and what reached a human](../../../../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md) (C2730, C2731, C2732, C2733, C2734, C2736, C2738);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md) (C2698, C2700, C2702, C2703, C2705, C2711, C2712, C2713, C2714, C2715, C2716, C2992, C2993, C2994);
[Reports](../../../../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md) (C0392, C0418, C0419, C0443, C0445, C0470, C0471, C0472);
[Constraints and the design system](../../../../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md) (C0266).
