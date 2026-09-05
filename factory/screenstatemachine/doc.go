// Package screenstatemachine owns a screen's state machine: the record of the
// service, the states it declares, the events each answers, the transitions
// between them, and the chain of supersessions a screen is.
//
// # The files
//
// writer.go is [Of], [Machine], [Transition], [Insert] and [InForce].
// validate.go is [Draft], [Validate] and [CheckTransitionTargets], with the
// errors each rejects with. schema.go is [Table], [IDPrefix], [FormatVersion]
// and [DDL]. db_test.go is against the database; validate_test.go is the one
// subject that needs none.
//
// # Who writes what
//
// [Insert] is the only way this table is written, and its one caller is the
// artifact store, which calls it inside the transaction that submits the spec
// version introducing or revising the machine — so a version the gate rejects
// takes its machine down with it. Nothing here updates or deletes.
//
// # The screen's identity
//
// There is no screen record: a screen with no state machine is nothing the
// factory can check, so the screen's identity is the id of the machine that
// introduced it. [Insert] sets [Machine.Screen] to the new row's own id where
// draft.Supersedes is empty, and to the superseded machine's screen otherwise
// — the chain of supersessions is the screen. [InForce] is every machine
// introduced by an item in the build, less any machine another one in that
// same set supersedes, so only the newest revision within the build stands.
//
// # Validation
//
// [Validate] refuses the three shapes that make a machine not well formed:
// two transitions on one event from one state, a declared state no path of
// declared transitions reaches from the initial one, and a state that is not
// terminal and declares no event. [CheckTransitionTargets] is the other
// direction, over a machine already in force: a transition leaving the screen
// names another screen by id, and this rejects one naming a screen absent
// from the caller's screensInForce set.
//
// # What is not built here
//
// The gate that fires [Validate] and [CheckTransitionTargets] over a build,
// and the reader that assembles screensInForce from the item's own service and
// the current release of every service it declares a dependency on, are not
// built. The design system's content — the appearance, viewport width, text
// scale and text direction each declared state is decided in — is a separate
// record this package does not read.
//
// Who may write what: [Insert] inserts, and nothing here updates or deletes.
// service_id, spec_artifact_id, item_id and supersedes are id fields and not
// foreign keys; the store checks each for being present where it is required
// and never for pointing at anything, and record's doc.go states that rule
// and its cost once.
//
// What defines it: the machine, its identity, closure, and the three
// ill-formed shapes are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md.
package screenstatemachine
