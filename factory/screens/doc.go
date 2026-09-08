// Package screens is the server side of the four screens: every address a
// human can be on — an item, a decision, a service on an environment, or a
// constraint — as the view the client fetches for it; every write a screen
// makes as the call the client makes; the refusal of a call whose factory
// version is not its own; one server-sent-events stream per address; and
// the principal.Principal every call carries.
//
// # The files
//
// views.go is [Views], the interface with one method per address plus [Home]
// and the two board reads, [Work] and [Ops], and [ErrNotFound], the one error
// an address or a referenced record that resolves to nothing may return.
// viewwork.go, viewops.go, viewfactory.go and viewpeople.go are the view
// types the four methods on [Views] and the calls on [Calls] that create or
// name a record return or take an id for, one file per screen: plain
// structs of strings, numbers, bools, RFC 3339 UTC time strings, and slices
// of the same, restating the fields of the record packages they summarise
// rather than naming a record type — the repetition the code rules already
// prefer over a helper shared across packages, paid here at its largest, per
// roadmap.md's M8 entry. viewwork.go also carries [Constraint]: the design
// gives [Views] no bullet of its own for it, but a constraint arriving with a
// single intent is read at Work, on its intent, so its view sits beside
// [Item] and [Decision] rather than in a fifth file.
//
// calls.go is [Calls], the interface with one method per write, grouped by
// screen in the same order as [Views]; callswork.go, callsops.go,
// callsfactory.go and callspeople.go are the argument struct per method,
// one file per screen matching calls.go's own grouping. A method whose call
// creates an address a later call names — a project, an area, a safeguard,
// a halt, a legal hold, a fleet entry, a constraint, or a mitigation —
// returns that id; every other method returns only an error. Every calendar
// value a human authors through one of these carries the IANA zone it was
// authored in, as a field beside it, the way the design asks of every such
// value.
//
// server.go is [Server] and [New], the [http.ServeMux] wiring every route
// this package answers, the small JSON helpers every handler shares, and
// statusFor: the status of one error a view or a call returned. Four of them
// come from the three errors views.go declares — 404 for [ErrNotFound], 403
// for [ErrNotPermitted], 422 for [ErrRefused], and 500 for anything else,
// which is a fault of this server's own and not something the caller can
// answer for. A call the switch in call.go does not name is 400, and so is a
// body it cannot decode; a body over the limit is 413; and the version refusal
// version.go makes is 409, which is the one failure that does not take the
// {"error": ...} shape.
// version.go is [Server.checked], the middleware every /api/ route is
// wrapped in: the version refusal and the principal every call carries.
// call.go is [Server.handleCall], the POST /api/call/{name} handler: an
// exhaustive switch over every name [Calls] performs, decoding the body into
// that call's own argument struct — a map keyed by string is the dispatch
// the code rules refuse, and a switch is what a static reader enumerates in
// its place. stream.go is the server-sent-events subscription: [streams],
// [Server.Changed], and the /api/stream/{kind}/{id} handler.
//
// The tests are server_test.go, call_test.go and version_test.go, in package
// screens_test against fakeViews and fakeCalls (fakeviews_test.go and
// fakecalls_test.go) — a func field per method, the arrangement
// contractcheck's fakes_test.go uses for the seams it is composed with — and
// stream_test.go, in package screens itself so that a subscriber's removal
// from the map can be read directly.
//
// # Who may write what
//
// This package owns no table. It writes nothing: every record it reads and
// every writer it reaches is behind [Views] and [Calls], which whatever
// composes a [Server] implements — the arrangement package healthmonitor and
// package contractcheck already have with the seams they are composed with.
// That is also the departure from the shape a record package takes: there is
// no schema.go and no db_test.go, because there is no table to migrate or to
// test against — the ground healthmonitor's and contractcheck's own doc.go
// already state.
//
// # What is not enforced
//
// The principal a call carries is claimed and never verified: seam 5 of
// "Security comes last" is where a principal is checked, and this package
// attaches none. version.go turns the X-Factory-Principal header into
// principal.OfHuman with a claimed basis whatever key arrives, the same
// treatment package principal already gives every caller before that seam is
// built.
//
// # What nothing checks
//
// [Server.Changed] is what a caller tells a subscriber a record changed; it
// is a call the writer that changed the record has to remember to make, and
// nothing here verifies that every write reaches it. A stream that misses a
// change is indistinguishable from one that was never told, until a human
// reloads and sees the record moved.
//
// # What the views leave out
//
// [Factory] and [Service] answer less than the design asks a Factory or an
// Ops view for; a field the design names and no view carries is listed here
// so a reader finds it absent on purpose and not forgotten: each intent's
// outcome beside cost per feature, environment-hours per item and
// instance-hours per release, criteria withdrawn and unreliable per service
// and per author, the product licence per service against its release's
// resolved licences, and the rows each human referred and rejected split by
// cause.
//
// # What defines it The four screens, what waits on a human, and the badge:
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2565, C2566, C2567, C2568, C2569, C2572, C2576, C2577, C2578, C2579, C2580,
// C2584, C2585, C2588, C2590, C2591, C2592, C2593, C2594, C2595, C2596, C2597,
// C2599, C2600, C2601, C2602, C2604, C2605, C2606, C2608, C2611, C2612, C2613,
// C2615, C2620, C2624, C2629, C2631, C2632, C2633, C2634, C2638, C2643, C2651,
// C2652, C2653, C2654, C2656, C2658, C2660, C2661, C2662).
//
// Two audiences, designed for silence, and push not poll:
// ../../end-goal/how-the-factory-works/11-screens/02-three-properties-every-screen-needs.md
// (C2680, C2681, C2682, C2683, C2686, C2687, C2688, C2691, C2693).
//
// The client as one address per screen, the subscription and its disconnected
// state, the version refusal, and the principal:
// ../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md
// (C2695, C2697, C2700, C2701, C2703, C2704, C2708, C2709, C2715, C2716).
//
// The auto-approve and undone pair, and the two rates a threshold is read
// against:
// ../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
// (C2719, C2722, C2724, C2725).
//
// The page channel's four numbers and the load split at first delivery and
// first acknowledgement:
// ../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md
// (C2730, C2731, C2734, C2736, C2738).
//
// The principal and seam 5, not yet enforced: ../../end-goal/deferred.md
// (C0110, C0111, C0121, C0128).
//
// The twelve duties this package's calls perform:
// ../../end-goal/what-humans-do.md (C2850, C2851, C2857, C2858, C2860, C2862,
// C2863, C2864, C2867, C2868, C2870, C2873, C2874, C2875, C2878, C2880, C2881,
// C2882, C2883, C2884).
package screens
