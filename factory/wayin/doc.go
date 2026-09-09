// Package wayin is the way in: the factory's own software shipped inside
// every service the factory deploys, and the one entrance into the factory
// from outside it. It owns three things — the shipped source, the overlay
// that injects it into a service's build, and the factory-side entrance that
// source calls — and no table at all: what a submission becomes is a row of
// the report store, which is a second database and is reached from here
// through [Store].
//
// # The files
//
// shipped.go holds [ShippedSource] and [Source] that fills it, with
// [FileName], [Shape], the three strings both sides share — [NoticePath],
// [SubmitPath] and [TokenHeader] — and the three names the factory hands a
// deployed service, [TokenEnv], [StoreEnv] and [ListenEnv]. overlay.go holds
// [Overlay] and [OverlayName]. entrance.go holds [Notice], [Submission],
// [Result] with [RefusedNoSession], [Store] and [Entrance] with [NewEntrance].
// sourcekey.go holds the
// one shape a source key may have. The tests are entrance_test.go and
// overlay_test.go, neither of them against a database, and the second of them
// against a Go toolchain: it writes a module of its own, builds it with the
// overlay, and runs the binary.
//
// # The shipped source
//
// [ShippedSource] is a package main file the factory ships and versions with
// itself, the way package agent holds the role prompts the product ships. It
// is a template with one substitution point, the shipped-bundle identity,
// which [Source] fills from what its caller was given: this package holds no
// copy of the factory version, so there is one name for that value and the
// build record and the way in cannot disagree about it.
//
// It uses the standard library and nothing else, because it is compiled
// inside a module the factory does not author and cannot add a requirement
// to. For the same reason it is held to what an old go directive has: the
// module it is compiled in is the service's, so it reads the request method
// itself rather than registering a method pattern on a http.ServeMux, a
// module below 1.22 getting the routing of that release, where a method
// pattern is a literal path that matches nothing; and it writes interface{}
// rather than any. Every name in the file is prefixed borgWayIn so that none
// can collide with the service's own.
//
// # A departure from the code rules
//
// The shipped source's one entry point is a func init, which the root
// CLAUDE.md forbids. It is content shipped into a main the factory did not
// write, where nothing calls a start function, and asking the service's own
// code to call one is what the design refuses: the way in is not something a
// project asks for, and no item stands between a deployed service and it. An
// init is the only hook that needs no cooperation from the service's code.
// The departure is bounded to that file — it is text this package holds, not
// this package's own code, and it is the one init in the repository.
//
// Where any of the three names the factory hands the service is unset, the
// init does nothing beyond one line on the log: a service started outside the
// factory listens nowhere, and the line is what says the way in is off rather
// than broken.
//
// # The overlay
//
// [Overlay] writes the filled source into a directory of the caller's and
// returns the path of the file "go build -overlay" is handed, mapping
// [FileName] in the checkout — where no such file exists — to the one
// written outside it. Nothing here writes into a checkout and nothing is
// committed there. There is no fallback: a build the overlay fails is a
// failed build, reported the way any other is. Both build sites must be
// handed the same overlay, because two builds of one commit are relied on to
// be byte-identical.
//
// # The entrance
//
// [Entrance] is an http.Handler with two routes and no third: the notice at
// the open, read from the store at every open so that a notice moves without
// the service building again, and the submission, whose result is rendered in
// the same response. The way in presents the token the deployer minted at the
// deploy that placed it on both calls, which is how it calls as that deploy;
// what the token resolves to is the store's and never this package's.
//
// A submission is preceded by a notice and followed by an answer: the notice
// shown at the open carries its own identity beside it — [Notice.ID] where it
// has one, a digest of its words where it does not — and a submission names
// the identity it was shown. The entrance reads the notice again at the
// submit, under the same token, and compares: naming the one just read, the
// submission reaches [Store.Submit] under the notice this entrance just
// confirmed and never under what the submission itself said; naming none at
// all, it is refused before the store is ever reached, because a submission
// names the session it followed and one naming none followed nothing; naming
// one no longer current, the notice now in force is rendered in its place
// rather than a refusal, because a notice that moved between the open and the
// submit is shown again and not refused silently.
//
// The entrance writes no record and reads none beyond that second read of the
// notice. What reaches [Store.Submit]
// is the fields of [Submission] and nothing else the request carried: no
// address, no header beyond the token, and nothing a person could be
// recovered from. The submit result is rendered in the session that submitted
// and outlives it by nothing, which is the whole of the acknowledgment the
// channel offers.
//
// The source key is derived in the deployed software and not here: the way in
// mints a session at the open, hands it to the page, takes it back at the
// submit, and digests it with a salt minted once per process, sending the
// key. The salt never leaves the process, so the key covers one process of
// one deployed service and no longer, and a page that returns a session other
// than the one it was handed is keyed as another source, which links nothing
// it should not. A submission carrying no session, and a process that could
// mint no salt, send no key at all rather than a weak one.
//
// What this side does with a key is forward it as received and hold it to the
// one shape a derived key has. The entrance derives nothing, because a key
// derived here would be one the factory computed about a caller rather than
// one the deployed software supplied about its own session; and it stores
// nothing under that name that is not a digest, because a report may carry no
// field a person could be read out of and this address is reachable from
// outside the factory.
//
// # Who may write what
//
// This package writes two files and no record: the filled source and the
// overlay, both under a directory its caller supplies and neither inside a
// checkout. Every report is written by the report store, behind [Store],
// which the composition implements — the notice in force for the service the
// token resolves to, and what one submission did.
//
// What is not built here: the two build sites that call [Overlay] and the
// mount that serves [Entrance] are the composition's. The three environment
// names are this package's, because the shipped source is what reads them,
// and putting them on a started process is the deploy target's own.
//
// What defines it: the way in, the notice at the open, the submit result and
// the source key are
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0406, C0407, C0408, C0414, C0425, C0433, C0434, C0436, C0437, C0469);
// the component and the two calls it makes are ../../end-goal/components.md
// (C0005).
//
// The shipped way in calling as the deploy that placed it is seam 5 of
// ../../end-goal/deferred.md (C0117).
package wayin
