// The Factory screen's own wire shapes: the view of the machine itself and the
// arguments of the fourteen calls Factory makes. Every interface is one struct
// of package screens, field for field, the way ./types.ts states.
//
// What defines them: ../../../../screens/viewfactory.go and
// ../../../../screens/callsfactory.go, against
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md,
// ../../../../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md.

import { CalendarDate, Constraint, Instant } from './types';

export interface Parameter {
  Name: string;
  Subject: string;
  Value: string;
  Source: string;
}

export interface Safeguard {
  ID: string;
  Parameter: string;
  Subject: string;
  Direction: string;
  Bound: string;
  RoutedToDuty: number;
  RoutedToHuman: string;
}

export interface Halt {
  ID: string;
  Reason: string;
  At: Instant;
}

export interface LegalHold {
  ID: string;
  Subject: string;
  Reason: string;
  At: Instant;
}

export interface Environment {
  ID: string;
  Kind: string;
  Targets: string[] | null;
}

export interface FleetEntry {
  ID: string;
  ModelVersion: string;
  Effort: string;
  Role: string;
  ScopeProjectID: string;
  ScopeServiceID: string;
  ScopeAreaID: string;
  Credential: string;
  ProcessingLocation: string;
  MaterialClasses: string[] | null;
  ReadAtOnceBound: number;
  DispatchesBetweenEvalRuns: number;
}

// LentCredentialSummary is one lent credential a fleet entry may run on: its
// own name, the human who lent it and their name, the account kind, and
// whether it has already been taken back.
export interface LentCredentialSummary {
  Name: string;
  LenderKey: string;
  LenderName: string;
  Kind: string; // person or organisation
  TakenBack: boolean;
}

export interface RolePrompt {
  Role: string;
  VersionInForce: string;
  AwaitingGateVersion: string;
}

export interface FleetProposal {
  ID: string;
}

export interface Project {
  ID: string;
  Name: string;
}

export interface Area {
  ID: string;
  Name: string;
  Inside: string;
  // ProjectID is the project the chain of areas above this one ends at, which
  // is Inside itself for an area declared inside a project directly.
  ProjectID: string;
  Severity: string;
}

export interface ModelCost {
  ModelVersion: string;
  Amount: number;
  Currency: string;
  IsTotal: boolean;
}

export interface Numbers {
  ThroughputPerService: Record<string, number> | null;
  ReworkRate: number;
  GateRejectionRate: Record<string, number> | null;
  CostPerFeature: ModelCost[] | null;
  CostMeasured: boolean;
}

export interface DispatchCause {
  Cause: string;
  Count: number;
}

export interface ResolvedFactorCount {
  Factor: string;
  Count: number;
}

export interface HumanLoad {
  Duty: number;
  HumanKey: string;
  WaitingNow: number;
  MedianWaitSeconds: number;
  MedianOpenInFrontSeconds: number;
}

export interface ApproveUndonePair {
  HumanKey: string;
  Approved: number;
  Undone: number;
}

export interface SelfApprovalCount {
  HumanKey: string;
  Count: number;
}

export interface AutoPassRate {
  FactorSet: string;
  Threshold: number;
  Realized: number;
  Recorded: number;
}

export interface HeldOutBand {
  FactorSet: string;
  Band: string;
  FailedShare: number;
  ResolvedCount: number;
}

export interface SpendCeiling {
  Credential: string;
  Ceiling: number;
  Currency: string;
  BurnRate: number;
  ProjectedExhaustion: Instant;
  Unbounded: boolean;
  // UnpricedRuns is how many runs in the period returned a kind with no rate
  // authored for it; their units are in no sum, so a burn rate with any of
  // them is a lower bound.
  UnpricedRuns: number;
}

export interface PageChannel {
  PagesPerService: Record<string, number> | null;
  PagesPerHuman: Record<string, number> | null;
  ShareUnacknowledgedAtWiden: number;
  AcknowledgedToAnsweredSeconds: number;
  HumanFiredPages: number;
}

export interface LoadSplit {
  Duty: number;
  HumanKey: string;
  BeforeFirstDeliverySeconds: number;
  BetweenDeliveryAndAcknowledgementSeconds: number;
  AfterFirstAcknowledgementSeconds: number;
}

export interface RecordDecidingRow {
  Kind: string;
  RecordID: string;
  OpenedAt: Instant;
}

export interface RolePromptGateRow {
  Role: string;
  VersionID: string;
  OpenedAt: Instant;
}

export interface Factory {
  Parameters: Parameter[] | null;
  Safeguards: Safeguard[] | null;
  Halts: Halt[] | null;
  LegalHolds: LegalHold[] | null;
  Environments: Environment[] | null;
  FleetEntries: FleetEntry[] | null;
  LentCredentials: LentCredentialSummary[] | null;
  RolePrompts: RolePrompt[] | null;
  FleetProposals: FleetProposal[] | null;
  Constraints: Constraint[] | null;
  Projects: Project[] | null;
  Areas: Area[] | null;
  Numbers: Numbers;
  StoppedAtDispatch: DispatchCause[] | null;
  ResolvedFactorGates: ResolvedFactorCount[] | null;
  HumanLoad: HumanLoad[] | null;
  ApproveUndone: ApproveUndonePair[] | null;
  SelfApprovalCounts: SelfApprovalCount[] | null;
  AutoPassRates: AutoPassRate[] | null;
  HeldOutBands: HeldOutBand[] | null;
  SpendCeilings: SpendCeiling[] | null;
  PageChannel: PageChannel;
  LoadSplits: LoadSplit[] | null;
  RecordDecidingRows: RecordDecidingRow[] | null;
  RolePromptGateRow: RolePromptGateRow | null;
  // Seam5Enforced is whether this factory enforces seam 5, off at install and
  // turned on once at Factory. A document-kind constraint may require it, and
  // an item under such a constraint waits at dispatch until this is true.
  Seam5Enforced: boolean;
}

// The seven classes of material a fleet entry may name, named the way
// ../../../../fleetentry/writer.go names them. A class outside the seven is
// refused by that writer, so the form offers these and nothing else.
export const MATERIAL_CLASSES = [
  'intent_statement',
  'reports',
  'owner_answer',
  'repository',
  'run_output',
  'constraints',
  'failure_records',
] as const;

// The six roles a fleet entry may be put on, named the way
// ../../../../dispatch/role.go names them. The set is closed there, and the
// form falls back to it where the view lists no role of its own.
export const ROLES = [
  'interviewer',
  'decomposer',
  'spec_author',
  'implementation_planner',
  'task_author',
  'implementer',
] as const;

// The nine subject kinds a safeguard may be drawn on, named the way
// ../../../../safeguard/safeguard.go names them.
export const SAFEGUARD_SUBJECT_KINDS = [
  'stage',
  'service',
  'project',
  'area',
  'contract_element',
  'design_system_component',
  'factory_settings',
  'report_store',
  'drift_detector_last_check',
] as const;

// GATE_ROW_SUBJECT_KIND is not a tenth kind package safeguard names: it is
// ../../../../cmd/factory/safeguard.go's own shorthand for the risk
// threshold's subject, a row-scoped safeguard drawn on the service
// [PlaceSafeguardArgs.ServiceName] names and keyed by the gate row
// [PlaceSafeguardArgs.SubjectName] carries.
export const GATE_ROW_SUBJECT_KIND = 'gate_row';

// The three subjects a legal hold reaches, named the way
// ../../../../legalhold/writer.go names them, and the three widest reaches a
// constraint supplied here may name, named the way
// ../../../../constraint/schema.go names them. Intent is the fourth reach and
// is supplied at Work, on its intent.
export const LEGAL_HOLD_SUBJECT_KINDS = ['service', 'project', 'factory'] as const;
export const CONSTRAINT_REACHES = ['factory', 'project', 'area'] as const;

// The five gate rows that decide a record rather than an item, named the way
// ../../../../gate/row.go names them. A row this screen decides carries one of
// these as its RowKind, and Factory.RecordDecidingRows carries the last four
// in its own Kind field.
export const ROW_ROLE_PROMPT_OR_SKILL = 'a_role_prompt_or_a_skill';

// The verdicts a row outside every item offers, which is what
// ../../../../gate/row.go's Actions returns for these five kinds: approve,
// reject with feedback, and refer.
export const RECORD_ROW_VERDICTS = ['approve', 'reject', 'refer'] as const;

// The role prompt row's third action, chosen alongside the two
// DecideRecordRowArgs verdicts above but never sent as one: it authors a
// version and re-fires the row over it, which EditRecordRowArgs carries
// instead of a verdict.
export const VERDICT_EDIT_IN_PLACE = 'edit_in_place';

export interface CreateProjectArgs {
  Name: string;
}

export interface DeclareAreaArgs {
  Name: string;
  InsideAreaID: string;
  ProjectName: string;
}

export interface AuthorParameterArgs {
  Parameter: string;
  Value: string;
  ServiceID: string;
  AreaID: string;
  GateRow: string;
  Stage: string;
  Quantity: string;
  ProjectName: string;
}

// ServiceName is read for one subject kind alone, [GATE_ROW_SUBJECT_KIND],
// where SubjectName carries the gate row that keys it; it is empty for every
// other kind, whose SubjectName names its own record.
export interface PlaceSafeguardArgs {
  Parameter: string;
  SubjectKind: string;
  SubjectName: string;
  ServiceName: string;
  Bound: string;
  RouteDuty: number;
  RouteHuman: string;
}

export interface WithdrawSafeguardArgs {
  SafeguardID: string;
}

export interface SetHaltArgs {
  Reason: string;
}

export interface WithdrawHaltArgs {
  HaltID: string;
}

export interface SetLegalHoldArgs {
  SubjectKind: string;
  SubjectName: string;
  Reason: string;
}

export interface WithdrawLegalHoldArgs {
  LegalHoldID: string;
}

// The scope's three fields are names, resolved against their record on the
// server; a name that resolves to nothing is refused there rather than
// stored.
export interface WriteFleetEntryArgs {
  ModelVersion: string;
  Effort: string;
  Role: string;
  ScopeProjectName: string;
  ScopeServiceName: string;
  ScopeAreaName: string;
  Credential: string;
  ProcessingLocation: string;
  MaterialClasses: string[];
  ReadAtOnceBound: number;
  DispatchesBetweenEvalRuns: number;
}

export interface WithdrawFleetEntryArgs {
  FleetEntryID: string;
}

export interface SupplyConstraintArgs {
  Statement: string;
  ReachKind: string;
  ReachName: string;
  BindsFrom: CalendarDate;
  ReviewDate: CalendarDate;
  Zone: string;
  // RequiresSeam5Enforced holds every item within this constraint's reach at
  // dispatch until Factory.Seam5Enforced is true.
  RequiresSeam5Enforced: boolean;
}

// SetSeam5EnforcedArgs turns enforcement of seam 5 on. It is one-way — off at
// install, turned on once, and never off again — so a false is refused
// rather than stored.
export interface SetSeam5EnforcedArgs {
  Enforced: boolean;
}

export interface DecideRecordRowArgs {
  RowKind: string;
  RecordID: string;
  Verdict: string;
  Reason: string;
  OpenedInWorkAt: Instant;
}

// EditRecordRowArgs is the role-prompt row's third action: not a verdict but
// authoring a version and re-firing the row. Row is the row kind
// DecideRecordRowArgs's RowKind already uses.
export interface EditRecordRowArgs {
  Row: string;
  RecordID: string;
  Version: string;
  OpenedInWorkAt: Instant;
}

// RetireServiceArgs ends a service: the owner's write of retired on the
// service record, and what calls the deployer's removal. EnvironmentName,
// where given, performs the removal for that one environment and writes
// nothing on the service record — the step an owner takes before an
// environment other than production may be withdrawn, and the order is by
// hand: remove, then withdraw.
export interface RetireServiceArgs {
  ServiceID: string;
  EnvironmentName: string;
}

// EndProjectArgs ends a project once every service in it is retired, and
// withdraws production's environment for it in the same write.
export interface EndProjectArgs {
  ProjectName: string;
}
