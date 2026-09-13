// Command factory is the composition behind the four screens, and the
// command-line interface beside them: one binary that serves the screens and
// answers a terminal.
//
// "serve" is the process ../../../end-goal/one-process.md has described since M0
// and nothing has been: it acquires the lease, holds it for its own life, runs
// each component's pass on its own interval, and serves the four screens over
// HTTP between them, where every other subcommand acquires the lease, makes one
// pass, and exits. It takes "-port" (8080 by default), the composition flags "run"
// takes, and one "-every-<name>" per pass, and exits on SIGINT or SIGTERM.
// passes.go is the passes and their intervals.
//
// # The four screens
//
// What "serve" serves is package screens' own handler over two interfaces this
// package implements, with the client's build output embedded beside them.
// [views] is [screens.Views], every read the four screens make mapped from the
// records onto the view types that package serves; [calls] is [screens.Calls],
// every write, each reaching the writer of the record it changes. Package screens
// imports no record package — what crosses that seam is the screen's own view of
// a record — so both mappings are here.
//
// One call is new and one field is: [calls.FirePage], a page a human fires on
// their own judgment, which notifier already defines; and
// [gate.Given.OpenedInWorkAt], when the actor opened the row in Work, a field
// decisionlog has carried on every close event and nothing had filled.
//
// Two refusals are every call's, before any screen-specific check.
// [calls.acting] refuses the read-only row — a People row holding no duty, no
// obligation and lending no credential — and says why the owner's own key is
// exempt. [calls.openRow] re-reads the open event at the submit and refuses a
// row a close event or an abandonment has reached since it was drawn, which is
// the log's refusal of a second close met at the screen. After a write the call
// tells every subscriber on the addresses it touched, through
// [screens.Server.Changed]; a pass announces the home view and the lists a tick
// could have moved, which is what makes push not poll hold inside the product.
//
// # The subcommands
//
// Eight, and what each is for is in ../../README.md's own runbook, the map the
// code rules require to ship with the code; what is here is what the shape of one
// costs. Each is a pass or a read: "serve" above, "run", "walk", "watch",
// "learn", "contracts", "policy" and "truncate". Every write a human makes is at
// a screen, through the call in calls.go reaching the same writer, so what a
// subcommand acted on — an item, an intent, a service, a project, a record, the
// People declaration — is an address a human can be on instead.
//
// "run" walks the whole path once — the install, every component's restart, the
// intent stage, the four authoring stages per item with the gate row of each,
// the build and the contracts derived from it, the merge, the release, the
// production deploy, the watch, the acceptance rounds, and the deprecation
// detector — stopping with the first error. It asks nothing: standard input is
// read by nothing here, a round of the interview is answered by "-answer" or
// waits in Work, and a firing that puts a human at a row writes nothing and
// reports the item waiting there, so an install with no screen open is shown as
// stuck rather than being stuck, which is what
// ../../../roadmap.md#m8--the-screens-and-the-fleet requires of it.
// The next pass continues it: a run is [path.takeIn] then [path.advance] until
// nothing moves, each pass reading every live item back out of the records;
// resume.go's comment sets out what has no record behind it, and rehydrate.go
// is the read.
//
// It knows more than one service. "-service name=path" is given once per
// service, and an intent that changes several names them before its statement —
// "svcA,svcB: what is wanted", which the record carries too, the intent record
// holding no service. Where an item waits on another the run takes the layers in
// order, a consumer's environment composed from its producer's current release.
// Every subcommand but "run" and "serve" reads the services out of the store
// rather than taking a name and a repository: both are the service record's own
// fields, a flag naming a repository could disagree with the record, and one
// naming a service would leave a two-service install's other unknown.
//
// -project, on "run", "serve" and "policy", names the project a subcommand works
// in and defaults to "default"; the first two create it and production's
// environment in one event where it does not exist, and "policy" refuses where
// it does not. Every project after that is written at Factory,
// [calls.CreateProject].
//
// files.go maps the composition files and their tests.
//
// Who may write what: nothing of its own. Every record the run causes to exist is
// written by the package that owns it; this command composes the writers, holds
// no table, and reads through the owning package's readers. What it implements is
// a seam rather than a record, seven of them: [mergequeue.Repository] and
// [contractcheck.Checkout] because reaching a repository is the deployer's,
// [contractcheck.Exchanges] and [contractcheck.StoreState] because observing a
// run and reading a candidate's own store are, [healthmonitor.Deployer] because
// reaching a deploy target is, [healthmonitor.Brownouts] because naming a
// brownout is a walk over the contracts in force, [gate.Holds] because the
// factory's own holds read most of the graph, and [screens.Views] and
// [screens.Calls] because what crosses is a view of a record, never the record.
// [contractcheck.Checkout] also answers the build's own reading of whether its
// checkout declares a schema change and of whether it is a backfill — the first
// off the build record the run wrote it on.
//
// Every subcommand takes the lease before it touches the store, whether it
// writes or only reads: a read appends a read event, itself a write of the log,
// so the one-process rule holds for all eight. What differs is how long it is
// held — one pass, and the whole life of "serve". acquireLease in lease.go
// applies package lease's own table first, a lease not being takeable in a
// store whose lease table does not exist; takes the lease under this process's
// own instance, its hostname and its process id; renews it every third of its
// ttl for the life of the process; and returns the token every writer the
// composition constructs and every [decisionlog.Reader] carries, beside the
// channel that says the lease is gone. [postgres.Start] runs after it and under
// it: it reads the store's schema history, refuses a store this version cannot
// read, and applies the rest of the schema. A lease another process holds is a
// start failure naming the holder on stderr with a non-zero exit, a fenced
// process having every write refused anyway; the stop each subcommand defers
// releases it, so the next starts rather than waiting out the ttl.
//
// The fleet entry is a record an owner writes at Factory through
// [calls.WriteFleetEntry]. compose writes one per role from -model, -provider,
// -effort and the credential that provider reads, as the human -human names,
// wherever the install holds none in force for that role — the stand-in for that
// act on a composition no screen is open on, leaving an owner's own entries alone
// whatever the flags say. "serve" composes through the same call, so a terminal
// and a screen dispatch the same install; [deps.withoutFleetEntries] writes none,
// which is the state the readiness reading and dispatch's holds are for and what
// the episode-one test drives.
//
// What is not built here: a refer at the Decomposition row reaches nobody, the
// design naming no duty for that row, so it waits on the owner from its first
// firing and a refer there has nobody left to refer to — [gate.Gate.Refer]
// re-fires the set that row decided and never gets that far. The Implementation
// row rejects over the screens and nothing else: the encodings are checked one
// row below, at the candidate environment, where a defect is carried on the
// candidate rather than stopping the run, and reject at Merge to master; so are
// the emission in both directions and the count of the area's hazardous
// operation, which nothing reads back off the build.
// [policy.Factory.WithdrawEnvironment] has no call: production's is the only
// persistent environment this interface composes, and it is withdrawn with the
// project by [calls.EndProject]. [path.approveThrough] is reached from Work by
// [calls.ApproveThroughHold] and by no subcommand: the four factory holds it
// approves through stop the production deploy row being fired at all, so there is
// no open event for [calls.Decide] to name and that call fires the row with the
// holds on it and approves it in one act. Every other hold the factory sets is on
// a row the gate already fired, which [calls.Decide] approves through by naming
// the set on the open event. What the item view offers the action against is
// [path.factoryHoldsAsRead], the same four holds with the objective's budget read
// rather than raised.
//
// What defines it: the four screens as software and the fleet as records an
// owner composes is ../../../roadmap.md#m8--the-screens-and-the-fleet; what
// each screen holds is
// ../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md;
// the client versioned with the factory, the subscription per address and the
// version refusal are
// ../../../end-goal/how-the-factory-works/11-screens/03-the-screens-as-software.md;
// more than one service and the contract queries are
// ../../../roadmap.md#m5--contracts-bind-services; the path it composes is
// ../../../end-goal/how-the-factory-works/01-one-pipeline.md; the component
// that puts an agent on a stage is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md;
// every component's restart and the lease is ../../../end-goal/one-process.md;
// what ends a service, an environment, or a project is
// ../../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md;
// the duties the four screens' calls give a way in to are
// ../../../end-goal/what-humans-do.md; and what each component may call is
// ../../../end-goal/components.md.
package main
