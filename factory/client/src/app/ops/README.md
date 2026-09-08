# Ops

Deployed software per environment: which release of each service is running per target and
when each target's deploy completed, what the drift detector reads over that record where
it disagrees, what each service publishes, health, the open incidents, the rollouts in
progress and the control each is measured against, and every last check record — all of
them and not only the ones past their interval. It answers what is running rather than what
needs a human, and it acts.

| Address | File pair | What it is |
|---|---|---|
| `/ops` | `ops.ts`, `ops.html` | The board: every service on every environment, with the release per target, the drift mismatch over it, the incidents, the rollouts and the last checks |
| `/ops/service/:serviceId/on/:environmentId` | `service.ts`, `service.html` | One service on one environment: the window parameters per quantity, the emission version, unmeasured, the mitigation standing, the contracts published, how far a deploy still widening on a target has reached, and the five actions |
| — | `ops.e2e.ts` | The browser run: the screen driven in Chromium against the factory's own process |

The board's own read answers only what routes to a service's own address — which is what
`../../../../screens/viewops.go` says of `ServiceSummary` — so the board reads each
service's view beside it: one read of `/api/ops` and one per row, the per-row reads run
together with `Promise.all` rather than one after another, so the board's wait is one round
trip and not the sum of every row's. One subscription still covers the address, and a
change on it re-reads the whole of that. A service whose own view could not be read leaves
that row's readings withheld rather than the board failed.

The shape every screen directory holds: `README.md`, `ops.ts`, `ops.html`, `ops.css`,
`ops.spec.ts`, `ops.e2e.ts`, `ops.routes.ts`, and `format.ts`, with one further file pair
per address under the first. `format.ts` is a copy of the same file in the other three
screen directories; a screen imports `api/` and `state/` and nothing else outside its own
directory, and that copy is what the boundary costs.

`opsMachine` is declared in `ops.ts` and is the machine both addresses declare: the five
states, the transitions between them, and no terminal state.

The service address is an acting one, not watch-only. Every action re-reads the service
first, is refused while the subscription is down, and carries the reason the human wrote:
an undo in either of duty 10's two forms — rolling back while the build it returns to is
still running, or raising the revert after — a mitigation instructed and one already
standing ended, a rollback marked not caused by the release, and a page fired on a human's
own judgment, which is the one action nothing else fires.

The last check ages give way to the disconnected state and are not shown while the
subscription is down: a client that went on rendering ages from a store it could no longer
reach would read healthier the longer it stood.

## What defines it

[Work, Ops, Factory, People](../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md) (C2596, C2597, C2599, C2600, C2601, C2602, C2604, C2605, C2606, C2627);
[Three properties every screen needs](../../../../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md) (C2688, C2690, C2693);
[The screens as software](../../../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md) (C2700, C2702, C2703, C2704, C2705, C2711, C2712, C2713, C2714, C2715, C2992, C2993, C2994).
