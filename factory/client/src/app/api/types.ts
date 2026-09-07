// The wire shapes the shell and the Work screen read, and the two types every
// other file of this directory is written in terms of. Every interface here is
// one struct of package screens, field for field: the Go structs carry no JSON
// tags, so a key on the wire is the Go field name, which is why every property
// below is capitalised. A property renamed on one side and not the other is a
// template type error at the screen that reads it, which is the whole reason
// the client is compiled.
//
// One file per screen's own shapes, because one file for all four passes the
// 500-line bound: ./types-ops.ts, ./types-factory.ts and ./types-people.ts
// hold the other three, and each imports Instant and CalendarDate from here.
//
// What defines them: ../../../../screens/viewwork.go and
// ../../../../screens/callswork.go, against
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.

// A time written by a record: RFC 3339 in UTC, rendered in the viewer's zone
// by format.ts in each screen and by nothing else. Empty where the record
// carries none.
export type Instant = string;

// A date a human authored, carried with the IANA zone it was authored in.
export type CalendarDate = string;

export interface Badge {
  Total: number;
  PendingGates: number;
  UATAssignments: number;
  InterviewQuestions: number;
  Escalations: number;
  FactoryHoldsForAHuman: number;
  ConstraintCausedStops: number;
}

export interface LastCheck {
  Component: string;
  Checks: string;
  LastPass: Instant;
  IntervalSeconds: number;
  FurtherPassOwed: boolean;
}

export interface RoleReadiness {
  Role: string;
  EntryCovers: boolean;
  RolePromptInForce: boolean;
  OldestUnmatchedHoldAgeSeconds: number;
}

export interface Digest {
  Releases: number;
  Decisions: number;
  AutoApprovals: number;
}

export interface Home {
  Badge: Badge;
  LastChecks: LastCheck[] | null;
  Readiness: RoleReadiness[] | null;
  Digest: Digest | null;
}

export interface DispatchStop {
  Cause: string;
  Since: Instant;
  LiftedAt: string;
}

export interface WorkRow {
  ItemID: string;
  Stage: string;
  Priority: number;
  Waiting: string;
  Stop: DispatchStop | null;
}

export interface Work {
  Rows: WorkRow[] | null;
}

export interface ArtifactVersion {
  ID: string;
  Kind: string;
  AuthoredAt: Instant;
}

export interface Acknowledgement {
  HumanKey: string;
  At: Instant;
}

export interface DecisionSummary {
  OpenEventID: string;
  GateRow: string;
  Vector: Record<string, string> | null;
  Score: number | null;
  Verdict: string;
  OpenedAt: Instant;
  OpenedInWorkAt: Instant;
  Acknowledgements: Acknowledgement[] | null;
}

export interface ReleaseSummary {
  ServiceID: string;
  Number: number;
}

export interface DeploySummary {
  EnvironmentID: string;
  TargetID: string;
  CompletedAt: Instant;
}

export interface WindowSummary {
  ID: string;
  Exit: string;
}

export interface Item {
  ID: string;
  IntentStatement: string;
  Versions: ArtifactVersion[] | null;
  Decisions: DecisionSummary[] | null;
  ImplementationDiff: string;
  Release: ReleaseSummary | null;
  Deploys: DeploySummary[] | null;
  Windows: WindowSummary[] | null;
  PartlyDelivered: boolean;
  Stop: DispatchStop | null;
  // FactoryHold is the hold standing at this item's production deploy row, in
  // the words the hold names, and empty where none stands. It is no record: it
  // is a reading computed from records that already exist, taken when the view
  // was read.
  FactoryHold: string;
  // ApprovableThrough is true where FactoryHold may be approved through from
  // here, the emergency action that fires the row rather than deciding a row
  // already open. [ApproveThroughHoldArgs] is what it calls.
  ApprovableThrough: boolean;
}

export interface DecisionClose {
  Verdict: string;
  Reason: string;
  At: Instant;
  Actor: string;
}

export interface DecisionAbandon {
  At: Instant;
  Reason: string;
}

export interface Delivery {
  Channel: string;
  Recipient: string;
  Accepted: boolean;
  FirstAcceptedAt: Instant;
}

export interface Decision {
  OpenEventID: string;
  ItemID: string;
  GateRow: string;
  OpenedAt: Instant;
  OpenedInWorkAt: Instant;
  Vector: Record<string, string> | null;
  Score: number | null;
  VersionsInForce: string[] | null;
  RoutedToDuty: number;
  RoutedToHuman: string;
  Acknowledgements: Acknowledgement[] | null;
  Closed: DecisionClose | null;
  Abandoned: DecisionAbandon | null;
  Deliveries: Delivery[] | null;
}

export interface Constraint {
  ID: string;
  Kind: string;
  Reach: string;
  Statement: string;
  SuppliedAt: Instant;
  BindsFrom: CalendarDate;
  ReviewDate: CalendarDate;
  Zone: string;
  WithdrawnAt: Instant;
  Replaces: string;
}

// The arguments of every call Work makes, one interface per method of package
// screens' Calls. A call name on the wire is the method name with a
// lowercased first letter.

export interface SupplyIntentArgs {
  Statement: string;
  Services: string[];
  ProjectName: string;
}

export interface AnswerQuestionArgs {
  QuestionID: string;
  Answer: string;
}

export interface ConfirmReadingArgs {
  IntentID: string;
  Confirmed: boolean;
  Correction: string;
}

export interface AcceptDeliveryArgs {
  ServiceID: string;
  Commit: string;
}

export interface EndIntentArgs {
  IntentID: string;
}

export interface SetPriorityArgs {
  ItemID: string;
  Priority: number;
}

export interface EndItemArgs {
  ItemID: string;
}

export interface DecideArgs {
  OpenEventID: string;
  Verdict: string;
  Reason: string;
  OpenedInWorkAt: Instant;
}

// ApproveThroughHoldArgs is the emergency action at the production deploy
// row — approve now, not skip. It names the item and not an open event, the
// way ../../../../screens/callswork.go's own comment on the type states.
export interface ApproveThroughHoldArgs {
  ItemID: string;
  Reason: string;
  OpenedInWorkAt: Instant;
}

export interface ReferArgs {
  OpenEventID: string;
  Reason: string;
}

export interface EditInPlaceArgs {
  OpenEventID: string;
  VersionText: string;
}

export interface AcknowledgeArgs {
  OpenEventID: string;
}

export interface TakeOverArgs {
  ItemID: string;
  Stage: string;
}

export interface SupplyIntentConstraintArgs {
  IntentID: string;
  Statement: string;
  BindsFrom: CalendarDate;
  ReviewDate: CalendarDate;
  Zone: string;
}

export interface WithdrawConstraintArgs {
  ConstraintID: string;
}

export interface ClearCeilingArgs {
  Credential: string;
}

// What a call that creates an address answers with.
export interface CreatedAddress {
  id: string;
}
