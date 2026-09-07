// The Ops screen's own wire shapes: the board, one service's view on one
// environment, and the arguments of the six calls Ops makes. Every interface
// is one struct of package screens, field for field, the way ./types.ts
// states.
//
// What defines them: ../../../../screens/viewops.go and
// ../../../../screens/callsops.go, against
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.

import { Instant, LastCheck } from './types';

export interface ServiceSummary {
  ServiceID: string;
  EnvironmentID: string;
  CurrentRelease: number;
  Health: string;
}

export interface Ops {
  Services: ServiceSummary[] | null;
}

export interface TargetRelease {
  TargetID: string;
  ReleaseNumber: number;
  DeployID: string;
  CompletedAt: Instant;
  // Deploying is how far a deploy in progress on this target has reached, and
  // empty where no deploy is in progress on it.
  Deploying: string;
}

export interface Incident {
  ID: string;
  Quantity: string;
  OpenedAt: Instant;
}

export interface Rollout {
  ReleaseNumber: number;
  ControlTargetID: string;
}

export interface WindowParameters {
  Quantity: string;
  Size: number;
  FinestSizeReached: number;
  Confidence: number;
  Power: number;
  PassedReachable: boolean;
  AverageRunLength: number;
  AdmittedCrossings: number;
}

export interface Mitigation {
  ID: string;
  TargetID: string;
  Operation: string;
  StandingHours: number;
}

export interface Service {
  ServiceID: string;
  EnvironmentID: string;
  Targets: TargetRelease[] | null;
  DriftMismatch: string;
  ContractsPublished: string[] | null;
  OpenIncidents: Incident[] | null;
  Rollouts: Rollout[] | null;
  LastChecks: LastCheck[] | null;
  Windows: WindowParameters[] | null;
  EmissionVersion: string;
  Unmeasured: boolean;
  Mitigation: Mitigation | null;
}

// The two mitigation operations, named the way ../../../../deploy/mitigation.go
// names them. Share is meaningful for the first and Count for the second; the
// other is left zero.
export const OPERATION_SHIFT_TRAFFIC = 'shift_traffic';
export const OPERATION_SET_INSTANCE_COUNT = 'set_instance_count';

export interface RollBackArgs {
  ServiceID: string;
  Reason: string;
}

export interface RaiseRevertArgs {
  ServiceID: string;
  ReleaseID: string;
  Reason: string;
}

export interface StartMitigationArgs {
  TargetID: string;
  Operation: string;
  Share: number;
  Count: number;
}

export interface EndMitigationArgs {
  MitigationID: string;
}

export interface MarkRollbackNotCausedArgs {
  DeployID: string;
  Reason: string;
}

export interface FirePageArgs {
  ServiceID: string;
  Reason: string;
}
