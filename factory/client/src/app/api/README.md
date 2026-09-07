# api

Every call the four screens make to the factory, and nothing else. This directory
imports nothing of the app: it is what the screens are built from, and an edge back
into a screen would be a cycle the lint wall refuses. Its one exception is
`../../environments/version.ts`, the constant the build fills.

| File | What it owns |
|---|---|
| `types.ts` | The two time types, the shapes the shell and Work read, and Work's call arguments — one interface per struct of `../../../../screens` |
| `types-ops.ts` | The same for Ops: the board, one service on one environment, and the six calls Ops makes |
| `types-factory.ts` | The same for Factory, and the closed vocabularies its forms choose from |
| `types-people.ts` | The same for People, and the duties, obligations, account kinds and period units its forms choose from |
| `version.ts` | The factory version this client was built from, and the names of the two headers every call carries |
| `principal.ts` | The People key the human declared, held in `localStorage`, sent as `X-Factory-Principal` |
| `client.ts` | `get` and `call` over `fetch`, and the required reload a version refusal is rendered as |
| `stream.ts` | One `EventSource` subscription per address, read into signals |

One file per screen's own shapes, because one file for all four passes the 500-line bound;
each of the three imports the two time types from `types.ts`.

The view structs of package `screens` carry no JSON tags, so a key on the wire is the Go
field name — which is why every property in `types.ts` is capitalised. A call's name on
the wire is its method on package `screens`' `Calls` with a lowercased first letter.

`client.ts` answers with one of four outcomes and throws nothing. A value is a value; a
`reload` is the version refusal, which flips a signal the shell renders as a required
reload for the whole client and moves the calling screen nowhere; an `absent` is the 404
an address or a named record resolves to nothing with, which is a read's empty state and
a call's refusal and not a failure; a `failed` carries the message the calling screen
shows in its failed state. A network failure and any other status the server writes
`{"error": "..."}` for are both `failed`.

A call made with no People key declared is refused here without a request, because the
server's own refusal names a header rather than the thing a human has to do.

Every call blocks the application's reported stability while it is in flight, so nothing
reads a screen as settled with a read still open.

`client.ts` and `stream.ts` are the only two providers this client declares. The
principal is a plain module rather than a third.

## What is not enforced

A stream that the transport accepts and then never delivers on reads as connected for as
long as it lasts. What is detected is a connection that drops or fails to open.

## What defines it

[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md).
