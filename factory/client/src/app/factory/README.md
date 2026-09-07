# Factory

The machine itself, at `/factory` and at no address under it. Its sections, in the order
the screen renders them: the readiness reading and the items stopped at dispatch by cause;
the fleet, the role prompts and the fleet proposals; the rows that decide a record rather
than an item; gate policy, the safeguards, the halts and the legal holds; the environments;
the projects and areas; the constraints in force; and the factory's own numbers.

Two reads answer it. The readiness reading per role is a field of the home view rather than
of the Factory view, and it is on this screen because the record that covers a role — a
fleet entry, a role prompt version — is authored here; the one subscription on this address
keeps both true.

| File | What it owns |
|---|---|
| `factory.ts`, `factory.html` | The screen: the machine, the two reads, the readiness reading, the items stopped at dispatch, and the one path every call takes |
| `factory-fleet.ts`, `.html` | The fleet with every field per entry, the form that writes one — the scope as its three fields and the credential chosen from the lent credentials not taken back — and the withdrawal, the role prompts with the version in force and any version awaiting its gate, that row's three actions, and the fleet proposals |
| `factory-policy.ts`, `.html` | The rows that decide a record, every parameter with its effective value and which read it came from, the form that authors one, the safeguards — whose form takes the service a gate-row safeguard fires for, keyed by the row — the halts, and the legal holds |
| `factory-places.ts`, `.html` | The environments, the projects and areas with their forms and the project each area's chain ends at, and the constraints in force with the three widest reaches listed by reach |
| `factory-numbers.ts`, `.html` | Throughput, rework rate, gate rejection rate, cost per feature, the gates a resolved factor put a human at, the human's load with the approve-and-undone pair and the self-approval counts, the auto-pass rates against the recorded ones with the held-out bands, the spend ceilings, and the page channel |
| `request.ts` | What a section asks the screen to send |

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
`factory.css`, `factory.spec.ts`, `factory.routes.ts`, and `format.ts`. `format.ts` is a
copy of the same file in the other three screen directories, and `request.ts` is a copy of
the one under `../people/`; a screen imports `api/` and `state/` and nothing else outside
its own directory, and those copies are what the boundary costs.

`factoryMachine` is declared in `factory.ts`: the five states, the transitions between
them, and no terminal state.

## What the view does not carry

The Factory view leaves out, because nothing builds them yet, what its own doc comment
names: the report counts, the advisories and the registries watched for them, the mutation
score, and the design-system numbers. Each is left out rather than shown as zero, so a
screen answering nothing is read as unbuilt and not as a quiet week. Beside those, the
design's Factory bullet asks for these and the view carries no field for them: each intent's
outcome beside cost per feature, environment-hours per item and instance-hours per release,
criteria withdrawn per service and per author with the count unreliable, the hours a
mitigation has stood per target — which Ops carries per service — the product licence per
service against what its current release resolved, and the rows each human referred and
rejected split by what put them at the row.

## What defines it

[Work, Ops, Factory, People](../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md);
[What the factory auto-approved, and what was undone](../../../../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md);
[The page channel, and what reached a human](../../../../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md).
