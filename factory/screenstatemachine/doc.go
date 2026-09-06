// Package screenstatemachine owns a screen's state machine: the record of the
// service, the states it declares, the events each answers, the transitions
// between them, the chain of supersessions a screen is, the transition check
// over a build, and what drives a screen into its declared states.
//
// # The files
//
// writer.go is [Of], [Machine], [Transition], [Insert] and [InForce].
// validate.go is [Draft], [Validate] and [CheckTransitionTargets], with the
// errors each rejects with. transitioncheck.go is [Extractor], [Cause] with
// [Causes], [DerivedTransition], [ScreenDerivation], [Derivation] with
// [Derivation.Unavailable], and [CheckTransitions] with
// [ForbiddenTransitionError]. derive.go is the Go extractor —
// [GoExtractor], [FileName], [DeriveTransitions], and the [Toolchain],
// [ExtractorName] and [ExtractorVersion] it names itself by. driver.go is
// [Driver], [DriverDerivation], [DeriveDrivers], [Drivers] and [CheckDrivers],
// with [StateNotDrivenError], [DriverNotDeclaredError] and
// [DriversCouldNotDeriveError]. provenance.go is
// [SupersessionsRemovingProtection] with [SupersessionRemovingProtection].
// schema.go is [Table], [IDPrefix], [FormatVersion] and [DDL].
//
// db_test.go and provenance_db_test.go are against the database;
// validate_test.go, transitioncheck_test.go, derive_test.go and driver_test.go
// are the four subjects that need none.
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
// # The transition check
//
// [DeriveTransitions] is the derivation from the build, per screen in force:
// the transitions the extractor can show the implementation admits, or the
// cause it could not derive them. [CheckTransitions] rejects only a transition
// it can show the implementation admits from a state and on an event the
// machine declares, to a destination the machine does not declare there. It
// rejects nothing for a screen that could not be derived, and
// [Derivation.Unavailable] is the value that outcome is read by.
//
// derive.go's convention — one file per screen at the root of the checkout
// holding the screen's own transition function — is stated there. It is a
// departure the design does not settle: the design fixes the check's direction
// and its outcome and names no source shape, so the shape is this extractor's
// and the design's text is what it answers to.
//
// # The drivers
//
// [DeriveDrivers] reads the drivers out of the build the way package
// criterion's extractor reads the encodings, by the marker each names, and
// [CheckDrivers] rejects in both the gate's directions: a state in force that
// nothing drives, and a driver naming a state no machine in force declares.
// The marker's shape is driver.go's and not the design's, on the same terms as
// the transition function's.
//
// # What is not built here
//
// [CheckTransitions] and [CheckDrivers] are the Implementation row's rejection,
// made through gate.ScreenRejection, and [Derivation.Unavailable] is what that
// row hands the score as an input it could not read.
// [SupersessionsRemovingProtection] is read by the score at the Spec row,
// through the reader the command-line interface composes for it — a resolved
// factor routed to the actor of the superseded machine's introducing decision —
// and it takes the spec versions a human decided rather than reaching for the
// decision log.
//
// What is not built is what derives either reading from a checkout at the
// Implementation stage, and the reader that assembles screensInForce for
// [CheckTransitionTargets] from the item's own service and the current release
// of every service it declares a dependency on. [Validate] runs where a machine
// is written; nothing fires [CheckTransitionTargets] yet. The design system's content — the appearance, viewport width,
// text scale and text direction each declared state is decided in — is a
// separate record this package does not read.
//
// Who may write what: [Insert] inserts, and nothing here updates or deletes.
// service_id, spec_artifact_id, item_id and supersedes are id fields and not
// foreign keys; the store checks each for being present where it is required
// and never for pointing at anything, and record's doc.go states that rule
// and its cost once.
//
// What defines it: the machine, its identity, closure, the three ill-formed
// shapes, a transition that leaves the screen, and a superseding machine that
// removes protection are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/04-the-screen-state-machine.md;
// the transition check, its fixed direction and its could-not-derive outcome
// are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/01-the-transition-check.md;
// the extractor per toolchain the check is derived on the terms of is
// ../../end-goal/how-the-factory-works/07-contracts/06-what-a-consumer-declares.md;
// the drivers and the rejection in both directions over them are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/05-implementation/02-the-encoding-and-the-emission.md;
// and a resolved factor and a factor the score cannot compute are
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md.
package screenstatemachine
