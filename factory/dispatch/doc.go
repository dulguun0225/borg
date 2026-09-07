// Package dispatch is the dispatch component: the match of an item's stage
// against a role and of its service and area against a scope, or of an intent
// against one of the two roles put on an intent, and what runs an agent.
//
// # The files
//
// role.go is [Role] with [Roles], [Role.Stage], [Role.OnAnIntent],
// [ErrRoleNamesNoStage], [RoleAt], the operations [Role.Operations] gives each
// role with [Role.Narrow] and [ErrOperationWidened], and [Scope] with
// [Scope.Covers], [On.Areas] and [Scope.String].
// fleet.go is [Entry], the fleet entry as this component reads it, with the
// match against the record — [Dispatch.matchFor] and [Dispatch.entryFor] — the
// [Models] and [Prompts] interfaces the composition supplies, and the errors a
// caller reads: [ErrHeld], [ErrOutOfAttempts] and [ErrMaterialClassUnknown].
//
// dispatch.go is [Dispatch], [Composition] and [New], the [Escalation] and
// [Notifier] interfaces with [NoNotifier] and [EscalatedByTheAttemptLimit],
// [Actor], [On] and [Run], and the reads a dispatch makes: the
// attempt limit in force, the item's own count for the stage, the transition
// onto the item, and the agent run record. hold.go is [Hold] with [HoldKind],
// [HoldFormatVersion], the six conditions' constants, [RoutedToTheOwner],
// [Open], [Rematch] and the document-kind constraint's own read. credential.go
// is the two conditions a credential stops a dispatch on: [CredentialWait] and
// its two kinds, [WantsARate], the arithmetic of a spend ceiling with
// [NotifiedAtFraction] and the notice at it, and [Dispatch.ClearCeiling] with
// [ErrNoCeilingHold] and [ErrNotTheOwner].
// paidfor.go is what the People declaration says about that credential at the
// run, which every run record carries. admit.go is [Dispatch.Admit], the order
// items are admitted in where more is ready than the infrastructure admits.
// run.go is the five dispatches — [Dispatch.Interviewer],
// [Dispatch.SpecAuthor], [Dispatch.Planner], [Dispatch.TaskAuthor] and
// [Dispatch.Implementer] — the sequence they share, the withholding of the
// classes of material an entry does not name, and [sourcesOf], the material
// handed over as the run record names it.
//
// db_test.go is against the database, this component writing records through
// four packages that own tables; hold_test.go, credential_test.go,
// ceiling_test.go and material_test.go are split from it by subject at the
// 500-line bound — the holds with the role and scope vocabulary, the credential
// a run could not reach with what a run records about whose account it spent,
// the spend ceiling with the notice at a fraction of it, and the classes of
// material — all sharing db_test.go's fixtures and package.
//
// # What one dispatch does
//
// Dispatch is a match and not a judgment, so nothing here is decided that a
// later gate does not see. One dispatch reads the intent's state, matches the
// role and the scope against the fleet entries in force and the role prompt
// version in force, computes the three conditions the entry makes readable,
// writes the transition onto the item, writes the input manifest with the
// classes of material the entry does not name withheld,
// runs the role under a principal naming the model version, this dispatch and
// the scope, asking the provider for the effort the entry names, writes one
// agent run record per call naming the sources it was handed, and compares the
// item's own count for the stage against the attempt limit in force —
// escalating over it and telling the notifier that it did.
//
// A dispatch of a role put on an intent — [RoleInterviewer] and
// [RoleDecomposer] — makes the same sequence without the three parts that are
// an item's: it reads no intent state, the interview being what refines an
// unrefined intent; it writes no transition, there being no item; and the limit
// it compares against is the rounds the caller carries in [On.CountedSoFar]
// against [Limits.RoundsOnAnIntent], no per-stage row existing to read. Putting
// one of the two on an item is [ErrRoleNamesNoStage].
//
// The scope's area is matched against the item's area chain and not against
// its own area alone, so an entry drawn on a coarser area covers an item in a
// finer one. The chain is [On.AreaChain], read by the caller.
//
// # Who may write what
//
// This package owns no table. It writes the item's stage and the count beside
// it through [item.Dispatch], which owns them — for a run on an item, a run on
// an intent writing neither; the input manifest through
// [inputmanifest.Writer], until context assembly exists to write it; the agent
// run record through [agentrun.Writer]; and its holds, and the two rows a
// credential itself stands as, into the decision log through
// [decisionlog.Writer]. It reads the fleet entry through [fleetentry.InForceForRole]
// and writes none: the entry's one writer is an owner at Factory. The version a
// role authored is submitted by the stage that called this component, through
// the artifact store, and never here.
//
// # Which callers are not built
//
// Five of the six conditions that stop a dispatch are computed here, and so is
// the intent's own state, which stops one before the six: a stage no fleet
// entry covers, a stage whose role has no role prompt version in force, a
// credential already known unreachable, a credential at its spend ceiling, and
// a constraint of the document kind requiring seam 5 enforced. The sixth is a
// constraint of the dispatch-decided kind, whose predicate is decided against
// an entry's processing location: package constraint builds the document kind
// alone, so there is no record here to read and the condition is unbuilt.
//
// The claim a dispatch is, and its expiry, are not built either, so a stage
// with a stopped agent is not re-entered until something calls this component
// again; [Run.ID] is minted per run and stored on the principal alone. What
// that costs is the one thing an expiry would have caught: a credential's own
// row leaves the item that opened it as the run which reaches for that
// credential again, so a credential whose only such item has ended stands held
// until something else dispatches onto it.
//
// The withholding is what the manifest and the run record name, and not what
// the provider is sent: the payload each of the five methods passes the role is
// the caller's own, assembled from the same sources, and this strips nothing
// out of it. Selecting what reaches the model is context assembly's, which is
// not built, and the read-at-once bound the entry carries is recorded on every
// manifest and truncates nothing for the same reason.
//
// [Prompts] is an interface because the approved version ids the store's
// in-force read needs are the log's facts and this package does not import
// them. [Models] is one because the client an entry's model version and
// credential are reached through is the one thing the record cannot hold, and
// which provider a credential resolves to is the composition's knowledge.
//
// The decomposer is matched, prompted and dispatched like any other role and
// nothing calls it: the component that would put an agent in that role is a
// stage that decides a decomposition, and the factory is told its
// decomposition. So there is no method for it here, and package agent has the
// words the product ships for it and no type that runs them.
//
// [Escalation] is the composition's, because the abandonment of an item's
// pending rows is the gate component's and this component's row in
// ../../end-goal/components.md names no gate. The wait that follows it is this
// component's own call, on [Notifier], which that row does name, and so is the
// notice at [NotifiedAtFraction] of an authored ceiling: this component compares
// the sum at every report and holds no record of what has been delivered, so
// the composition is what keys that delivery on the credential and the period
// and drops the rest. Context assembly, which that row names too, is not built.
//
// # What defines it
//
// The match, the six holds, the re-match, the tier that orders admission, and
// the claim are
// ../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md.
// The role, the two roles put on an intent, the scope, the operations a role
// carries, the principal every call is made under, and what a stage hands an
// agent — the reject or the rework request among it — are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md. The interview those
// roles run, and the rounds it counts, are
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md. The stage and the
// count per stage this component writes are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md.
// The limit the count is compared against and the escalation over it are
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md. The
// role prompt version in force is
// ../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md,
// and the fleet entry's nine fields, the classes of material withheld before a
// run, and the agent run record are
// ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md.
// The credential a run could not reach is
// ../../end-goal/how-the-factory-works/10-fleet/05-an-account-that-runs-out-is-a-hold.md
// and
// ../../end-goal/how-the-factory-works/10-fleet/06-a-credential-taken-back.md;
// the spend ceiling, the period a sum is taken over, the clear that authorises
// an overage for one period, and a credential failing closed on an unpriced run
// are ../../end-goal/how-the-factory-works/10-fleet/08-a-spend-ceiling.md. The
// constraint requiring seam 5 enforced is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md.
// This component's row, and the calls it may make, are
// ../../end-goal/components.md; its restart, which is nothing, is
// ../../end-goal/one-process.md.
package dispatch
