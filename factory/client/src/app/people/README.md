# People

Every row of the People declaration, at `/people` and at no address under it: who holds
which duty, what obligation a row holds outside the twelve, the credentials each person or
organisation lent with the account kind beside each, the ceiling and the rates authored on
those credentials, and whether the row acts anywhere at all. A row that acts nowhere was
added only so a human can read the four screens.

Declared, not enforced: the declaration is what routes work, and it is the seam
authentication attaches to later. The one field the factory does enforce is the spend
ceiling, which holds work when reached. Factory converts the rates into cost per feature
and reports the number there rather than here.

| File | What it owns |
|---|---|
| `people.ts`, `people.html` | The screen: the machine, the read, every row of the declaration, and the one path every call takes |
| `people-forms.ts`, `.html` | The ten writes: a duty declared or withdrawn, an obligation declared or withdrawn, a credential lent or taken back, a ceiling authored, a rate authored, and a mapping written or erased |
| `request.ts` | What the forms section asks the screen to send |
| `people.e2e.ts` | The browser run: the screen driven in Chromium against the factory's own process |

The screen is split into a section because one component for all of it would pass the
500-line bound. A section is not a fifth screen: it holds no address, no subscription and
no state machine, and it performs no call — it emits the call it wants made, and
`people.ts` holds the one path every call takes, refused while the subscription is down,
re-reading the address before it sends and again after. A refusal the factory wrote — an
erasure a legal hold reaches, among them — is shown as the message the factory sent.

The rows and the forms are rendered outside the state machine's `@switch` rather than
inside its branches, for the reason `../factory/README.md` gives: a body inside a branch
would be destroyed and built again on a re-read and on a dropped subscription, and the
section holds what a human has half written into a form.

The start date a ceiling's period runs from carries the viewer's IANA zone beside it,
taken from `Intl.DateTimeFormat().resolvedOptions().timeZone`, because every reader of that
value computes in that zone and no other: a period ends at that zone's midnight.

The shape every screen directory holds: `README.md`, `people.ts`, `people.html`,
`people.css`, `people.spec.ts`, `people.e2e.ts`, `people.routes.ts`, and `format.ts`.
`format.ts` is a copy of the same file in the other three screen directories, and
`request.ts` is a copy of the one under `../factory/`; a screen imports `api/` and
`state/` and nothing else outside its own directory, and those copies are what the
boundary costs.

`peopleMachine` is declared in `people.ts`: the five states, the transitions between them,
and no terminal state.

Who a human is on the four screens is declared in the shell rather than here, in
`../../api/principal.ts`: the key is carried on every call as the principal, and nothing
verifies it until seam 5 is built.

## What defines it

[Work, Ops, Factory, People](../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md) (C2654, C2657, C2658, C2660, C2673);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md) (C2702, C2703, C2705, C2711, C2712, C2713, C2714, C2715, C2716, C2992, C2993, C2994).
