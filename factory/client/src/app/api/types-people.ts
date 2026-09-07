// The People screen's own wire shapes: every row of the declaration and the
// arguments of the ten calls People makes. Every interface is one struct of
// package screens, field for field, the way ./types.ts states.
//
// What defines them: ../../../../screens/viewpeople.go and
// ../../../../screens/callspeople.go, against
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.

import { CalendarDate } from './types';

export interface Ceiling {
  Amount: number;
  Currency: string;
  Period: string;
  StartDate: CalendarDate;
  Zone: string;
}

export interface Rate {
  Kind: string;
  ModelVersion: string;
  Effort: string;
  Amount: number;
  Currency: string;
}

export interface LentCredential {
  Name: string;
  Kind: string;
  Ceiling: Ceiling | null;
  Rates: Rate[] | null;
}

export interface PersonRow {
  Key: string;
  Name: string;
  Duties: number[] | null;
  Obligations: string[] | null;
  Credentials: LentCredential[] | null;
  ActsAnywhere: boolean;
}

export interface People {
  Rows: PersonRow[] | null;
}

// The owner's twelve duties, held as their numbers the way
// ../../../../people/identity.go holds them.
export const DUTIES = [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12] as const;

// The three obligations outside the twelve, and the two account kinds a lent
// credential may name, named the way ../../../../people/identity.go and
// ../../../../people/credential.go name them.
export const OBLIGATIONS = ['hosting', 'driftdetector', 'fleet'] as const;
export const ACCOUNT_KINDS = ['person', 'organisation'] as const;

// The two units a ceiling's period length is authored in, named the way
// ../../../../people/credential.go names them.
export const PERIOD_UNITS = ['day', 'month'] as const;

export interface DeclareDutyArgs {
  HumanKey: string;
  Duty: number;
}

export interface WithdrawDutyArgs {
  HumanKey: string;
  Duty: number;
}

export interface DeclareObligationArgs {
  HumanKey: string;
  Obligation: string;
}

export interface WithdrawObligationArgs {
  HumanKey: string;
  Obligation: string;
}

export interface LendCredentialArgs {
  HumanKey: string;
  Credential: string;
  Kind: string;
}

export interface TakeBackCredentialArgs {
  HumanKey: string;
  Credential: string;
}

export interface AuthorCeilingArgs {
  Credential: string;
  Amount: number;
  Currency: string;
  PeriodUnit: string;
  Length: number;
  StartDate: CalendarDate;
  Zone: string;
}

export interface AuthorRateArgs {
  Credential: string;
  Kind: string;
  ModelVersion: string;
  Effort: string;
  Amount: number;
  Currency: string;
}

export interface WriteMappingArgs {
  HumanKey: string;
  Name: string;
}

export interface DeleteMappingArgs {
  HumanKey: string;
}
