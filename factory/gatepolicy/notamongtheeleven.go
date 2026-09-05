package gatepolicy

// NotAmongTheEleven is what an owner authors on the factory-wide settings record
// that is not gate policy: retention, the report channel's rates, the remediation
// period, and the harm mark's page cap. Each carries a direction because a
// safeguard binds it and the direction differs per parameter, and none carries a
// row because the rows are the eleven.
//
// What is authored and not among the eleven on a service, an area or an
// environment record is not here: those are fields of the record that owns them
// and no safeguard on them is placed through this vocabulary yet.
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md
// is the whole list.
var NotAmongTheEleven = []Definition{
	{
		Parameter: DecisionLogRetention,
		Kind:      KindSeconds, Direction: DirectionFloor, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits: "how long the decision log is kept; a safeguard on it may lengthen and never shorten, the protection being the evidence a shorter value destroys",
		Unit:   "seconds, and unauthored is the life of the install",
	},
	{
		Parameter: ReportRetention,
		Kind:      KindSeconds, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits: "how long the report store keeps a report; a safeguard on it may shorten and never lengthen, report text being personal data",
		Unit:   "seconds, and unauthored is the life of the install",
	},
	{
		Parameter: BackupRetention,
		Kind:      KindSeconds, Direction: DirectionNone, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits: "how far back a backup may reach, read by the erasure list's retirement",
		Unit:   "seconds, authored outright with nothing supplied",
	},
	{
		Parameter: RetentionFloor,
		Kind:      KindSeconds, Direction: DirectionNone, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits: "how low an authored value or a safeguard may ever take decision-log retention",
		Unit:   "seconds, written at the gate row that decides a shortening or by a records-retention constraint",
	},
	{
		Parameter: RemediationPeriod,
		Kind:      KindSeconds, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeySeverity,
		Limits: "how long a matching advisory of one severity may stand before the intent it raised pages",
		Unit:   "seconds, authored outright with nothing supplied",
	},
	{
		Parameter: ReportChannelRate,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyService,
		Limits: "what bounds arrival at the way in, per service and factory-wide",
		Unit:   "reports admitted per hour, and unauthored is unbounded",
	},
	{
		Parameter: HarmMarkPageCap,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyService,
		Limits: "how many intents one service's marked reports may page per interval",
		Unit:   "intents per interval, shipped with a default rather than supplied",
	},
}

// SafeguardOnly is every parameter that a safeguard binds and nobody authors.
// There is one. It is a list of its own rather than a row of [Definitions]
// because gate policy is what an owner authors — eleven rows, counted by
// TestElevenRows — and a parameter only a safeguard sets, listed among them,
// would make that count twelve while changing nothing about what an owner may
// write.
//
// The direction is derived rather than read off the design's list, which names
// twelve clamped parameters and their directions and not this one, while that
// same section's argument for a safeguard being a record rather than a field
// rests on it: a safeguard's predicate names a contract element as its subject,
// whose writer is the merge queue. So the direction comes from the rule the whole
// list is an instance of — a safeguard can only add — and a safeguard's predicate
// adds a consumer contract and removes none, which is a floor.
var SafeguardOnly = []Definition{
	{
		Parameter: SafeguardPredicate,
		Kind:      KindPredicate, Direction: DirectionFloor, Scope: ScopeNothing, Key: KeyNone,
		Limits:                "what a consumer assumes of a producer where the derivation of a consumer contract cannot see the read",
		Unit:                  "one predicate on one element of a contract",
		ReaderAtThisMilestone: "enforcement, beside the consumer contracts derived from a consumer's build",
	},
}
