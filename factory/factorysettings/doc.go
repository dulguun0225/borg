// Package factorysettings owns the factory-wide settings record: every value an
// owner authors that has no customer record its scope reaches. The five gate
// policy names — the attempt limit, the list of allowed predicate kinds, the
// review sample rate, the held-out sample rate and the advisory severity — the
// threshold the role-prompt-or-skill gate row reads, retention, the report
// channel's two rates, the remediation period, the harm mark's page cap and
// whether it pages at all, and whether seam 5 is enforced.
//
// schema.go is the six tables. [Table] is the record, one row the store keeps
// singular with a constant column and a unique constraint on it, and the five
// beside it hold the parameters that have a key: [LimitTable] per stage,
// [ReviewSampleRateTable] per duty, [RemediationPeriodTable] per severity,
// [ReportChannelRateTable] and [PageCapTable] per service.
//
// writer.go is [Settings], the record as it is stored; [Writer] and
// [Writer.Ensure], which creates it with nothing authored and is idempotent on
// the singleton constraint; [Get], which reads the whole record; and
// [SetAllowedPredicateKinds] and [SetRolePromptOrSkillThreshold]. Every authoring
// call takes a transaction. attemptlimit.go is [AttemptLimitSubject] and its six
// values, [OfStage], [SetAttemptLimit] and [AttemptLimit]. samplerates.go is
// [SetHeldOutSampleRate], [SetReviewSampleRate] and [ReviewSampleRate].
// advisories.go is [SetAdvisorySeverity], [SetRemediationPeriod] and
// [RemediationPeriod]. retention.go is [SetDecisionLogRetention],
// [SetReportRetention], [SetBackupRetention] and [SetRetentionFloor].
// reports.go is [SetReportChannelRate], [SetServiceReportChannelRate],
// [ReportChannelRate], [SetHarmMarkPageCap], [HarmMarkPageCap] and
// [SetHarmMarkPages]. seam.go is [SetSeam5Enforced], which refuses turning it
// off.
//
// An unauthored field is null or empty rather than zero, so what stands in its
// place is what the score supplies, what the product shipped, or the life of the
// install, per parameter.
//
// Who may write what: [Writer.Ensure] creates the record, as Factory. Every
// authoring call is called by package policy inside the transaction that appends
// the policy version, so the field and the version commit together or not at all.
// Two have a second caller the design names and the code does not have yet:
// [SetDecisionLogRetention] is called by the gate row that decides a shortening,
// and [SetRetentionFloor] by that row and by intake on the arrival of a
// records-retention constraint. Neither row nor intake's constraint kind is
// built.
//
// What is not read yet: the advisory severity, the remediation period, the report
// channel's two rates, the harm mark's page cap and whether it pages, report
// retention, backup retention, and whether seam 5 is enforced. Each is a field
// with no reader because the mechanism that would read it — the advisory
// detector, the report store, the erasure list, the seam — is not built, and the
// field is here rather than a substitute for it.
//
// What defines it: what shares this record, and the retention values and rates
// beside them, are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// and ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md;
// the attempt limit itself is
// ../../end-goal/how-the-factory-works/03-gates/05-the-attempt-limit.md; the harm
// mark's page cap and its off switch are
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md.
package factorysettings
