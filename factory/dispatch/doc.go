// Package dispatch is the dispatch component: the match of an item's stage
// against a role and of its service and area against a scope, and what runs an
// agent.
//
// # The files
//
// role.go is [Role] with [Roles], [Role.Stage], [RoleAt], the operations
// [Role.Operations] gives each role with [Role.Narrow] and
// [ErrOperationWidened], and [Scope] with [Scope.Covers] and [Scope.String].
// fleet.go is [Entry], the [Fleet] and [Prompts] interfaces the composition
// supplies, and the two errors a caller reads — [ErrHeld] and
// [ErrOutOfAttempts].
//
// dispatch.go is [Dispatch], [Composition] and [New], the [Escalation]
// interface, [Actor], [On] and [Run], and the reads a dispatch makes: the
// attempt limit in force, the item's own count for the stage, the transition
// onto the item, and the agent run record. hold.go is [Hold] with [HoldKind],
// [HoldFormatVersion], the three conditions this component computes, [Open]
// and [Rematch]. run.go is the four dispatches — [Dispatch.SpecAuthor],
// [Dispatch.Planner], [Dispatch.TaskAuthor] and [Dispatch.Implementer] — and
// the sequence they share.
//
// db_test.go is against the database, this component writing records through
// four packages that own tables; hold_test.go, split from it by subject at the
// 500-line bound, is the holds and the role and scope vocabulary, sharing
// db_test.go's fixtures and package.
//
// # What one dispatch does
//
// Dispatch is a match and not a judgment, so nothing here is decided that a
// later gate does not see. One dispatch reads the intent's state, matches the
// role and the scope against a fleet entry and the role prompt version in
// force, writes the transition onto the item, writes the input manifest,
// runs the role under a principal naming the model version, this dispatch and
// the scope, writes one agent run record per call, and compares the item's own
// count for the stage against the attempt limit in force — escalating over it.
//
// # Who may write what
//
// This package owns no table. It writes the item's stage and the count beside
// it through [item.Dispatch], which owns them; the input manifest through
// [inputmanifest.Writer], until context assembly exists to write it; the agent
// run record through [agentrun.Writer]; and its holds into the decision log
// through [decisionlog.Writer]. The version a role authored is submitted by the
// stage that called this component, through the artifact store, and never here.
//
// # Which callers are not built
//
// The fleet entry is not a record: [Fleet] is an interface and the composition
// answers it with what it was configured with, so the effort, the processing
// location, the lender's key and the account kind an entry carries in the
// design are absent from every agent run record written here. [Prompts] is the
// same arrangement for the role prompt version in force, the approved version
// ids being the log's facts and this package not importing them.
//
// Three of the six conditions that stop a dispatch are computed here — a stage
// no fleet entry covers, a stage whose role has no role prompt version in
// force, and the intent's own state, which stops one before the six. The other
// three read records that do not exist: a credential already known
// unreachable, a credential at its spend ceiling, and a constraint of the
// document or the dispatch-decided kind. The claim a dispatch is, and its
// expiry, are not built either, so a stage with a stopped agent is not
// re-entered until something calls this component again; [Run.ID] is minted
// per run and stored on the principal alone.
//
// [Escalation] is the composition's, because the abandonment of an item's
// pending rows is the gate component's and this component's row in
// ../../end-goal/components.md names no gate. Context assembly, which that row
// does name, is not built.
//
// # What defines it
//
// The match, the six holds, the re-match, the tier that orders admission, and
// the claim are
// ../../end-goal/how-the-factory-works/02-intent-into-items/05-dispatch.md.
// The role, the scope, the operations a role carries, the principal every call
// is made under, and what a stage hands an agent — the reject or the rework
// request among it — are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md. The stage and the
// count per stage this component writes are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/02-what-an-item-names.md.
// The limit the count is compared against and the escalation over it are
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md. The
// role prompt version in force is
// ../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md,
// and the agent run record is
// ../../end-goal/how-the-factory-works/10-fleet/01-what-an-agent-runs-on.md.
// This component's row, and the calls it may make, are
// ../../end-goal/components.md; its restart, which is nothing, is
// ../../end-goal/one-process.md.
package dispatch
