// Command factory is the crude interface M1 defers the four screens with:
// one binary on a terminal, until M7 builds Work, Ops, Factory, and People.
//
// Eleven subcommands. "run" walks the whole path once — the install, intake,
// the interview, decomposition, Decomposition where decomposition yielded more
// than one item, the spec and implementation stages per item, the consumer
// contract derived from the same build, the build, the criteria in force
// checked in both directions, the Merge to master gate with the two contract
// checks, the fast-forward, the release and the contract versions it publishes,
// the Deploy to production gate, a deploy without a control, the watch, and the
// deprecation detector — stopping with the first error, and asking a human for
// a verdict at each row the score or a safeguard puts one at.
//
// It knows more than one service, which contracts need: an interface has consumers
// and the consumers are other services in the same factory. "-service name=path" is
// given once per service, and an intent that changes several names them before its
// statement — "svcA,svcB: what is wanted" — which is this interface being told what
// decomposition yields. Where an item waits on another, the run takes the layers in order:
// a consumer's environment is composed from its producer's current release, so the
// producer ships first, which is what the hold at the candidate deploy row would
// otherwise make happen across two runs.
//
// "walk <deploy-id>" follows the links from an existing deploy record back to
// its intent, which is the direction M1 is demonstrated in — every step a
// stored field, none reconstructed — and then prints every decision the item's
// gates left in the log. "watch <service>" is the health monitor alone, which
// is the one thing that closes a analysis window. "approve <item-id>" is the
// emergency action at the production deploy row. "contracts" is every query
// this milestone makes: the contracts and their versions, the elements and
// their deprecation marks, the consumer contracts in force per service with the
// release range they were derived over, the deprecation list per marked
// element, and — with "-breaks <item-id>" — what one candidate would break and
// whom.
//
// The other five are duty 8, duty 9, the priority an owner reorders a queue with,
// and the People declaration a page routes on, none of which has a screen of its own
// until the four of M7 arrive. "area" declares a grouping; "author" writes one
// parameter on the record its scope names; "safeguard" places a safeguard or
// withdraws one; "policy" prints every parameter as it is in force, where its value
// came from, and what reads it; "people" declares who holds a duty.
//
// Every subcommand but "run" reads the services out of the store rather than taking
// a name and a repository: both are the service record's own fields, a flag naming
// one could disagree with the record, and a flag naming one service would leave a
// two-service install's other one unknown.
//
// Who may write what: nothing of its own. Every record the run causes to
// exist is written by the package that owns it; this command composes the
// writers and holds no table, and every read goes through the owning
// package's readers. What it implements for two components is a seam rather than a
// record: it is [mergequeue.Repository] and [contractcheck.Checkout] because
// reaching a repository is the deploy agent's, [contractcheck.Exchanges] because
// observing a run is, and [healthmonitor.Rollbacker] because reaching a deploy target
// is.
//
// What defines it: roadmap M1 — "the interface the human decides through is
// whatever is cheapest — the four screens come at M7, and a crude interface
// until then is what deferring them costs" —
// ../../../roadmap.md#m1--one-change-ships; the two services and the queries are
// ../../../roadmap.md#m5--contracts-bind-services.
package main
