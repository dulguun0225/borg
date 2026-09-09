// The report channel and erasure, split out of ./types-factory.ts because one
// file for Factory's own shapes passes the 500-line bound. Every interface is
// one struct of package screens, field for field, the way ./types.ts states.
//
// What defines them: ../../../../screens/viewfactory.go and
// ../../../../screens/callsfactory.go, against
// ../../../../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// and
// ../../../../../end-goal/how-the-factory-works/11-screens/05-the-page-channel-and-what-reached-a-human.md.

// IntentOutcome is one closed intent's outcome, computed once at its close:
// the acceptance round's verdict for a requested intent, the rate of reports
// before and after the release for one grouped from reports, and nothing for
// one the factory raised — absence being the answer and not a gap.
export interface IntentOutcome {
  IntentID: string;
  Source: string;
  Outcome: string;
}

// ServiceReportCounts is one service's own report-channel counts, beside
// whether the project it lies in has a notice for the way in to show.
export interface ServiceReportCounts {
  ServiceID: string;
  ServiceName: string;
  Refused: number;
  NoNoticeInForce: boolean;
}

// ServiceOnAnOldWayIn is one service whose current release's build names a
// shipped-bundle identity other than the running factory's, which is the way in
// it serves until it builds again.
export interface ServiceOnAnOldWayIn {
  ServiceID: string;
  ServiceName: string;
  Identity: string;
  FactoryIdentity: string;
}

// ReportChannel is what Factory reports of the one way into the factory from
// outside it. Refused is the one counter the report store keeps and every
// other number on this screen is a query at read time: the record a query
// would count is the write the rate exists to refuse.
export interface ReportChannel {
  Ungrouped: number;
  RefusedOverTheChannel: number;
  Services: ServiceReportCounts[] | null;
  OnAnOldWayIn: ServiceOnAnOldWayIn[] | null;
}

// ErasureSpan is a half-open byte range of the report's text, [Start, End),
// which is the unit a redaction names and each target's own writer destroys.
export interface ErasureSpan {
  Start: number;
  End: number;
}

// PerformErasureArgs is one erasure as an owner performs it here: the report
// whose words go, the spans of that report's text to destroy, and the reason
// the redaction record carries in place of the words. An owner names the words
// once — the factory walks the links from the report to its intent's statement
// and to the artifact versions quoting them, and finds the same words in each.
export interface PerformErasureArgs {
  ReportID: string;
  Spans: ErasureSpan[];
  Reason: string;
}
