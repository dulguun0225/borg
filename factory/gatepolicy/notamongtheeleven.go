package gatepolicy

// NotAmongTheEleven is what an owner authors that is not gate policy: retention,
// the report channel's rates, the remediation period and the harm mark's page
// cap on the factory-wide settings record; the strategy default and the ceiling
// on concurrent candidate environments on production's environment record; and
// the twelve the design names on the service record, with the explicit
// threshold's size and the values authored beside them there. Each carries a
// direction because a safeguard binds it and the direction differs per
// parameter, and none carries a row because the rows are the eleven.
//
// The explicit threshold is here rather than a field alone because a safeguard
// is what sets it, and a safeguard names the parameter it binds through this
// vocabulary. What is authored and not among the eleven on an area record is
// still not here: the hazard severity is the area's own.
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
		Parameter: ExplicitThreshold,
		Kind:      KindFraction, Direction: DirectionAdds, Scope: ScopeService, Key: KeyQuantity,
		Limits: "the absolute number a service's quantity is read against beside the comparison; the release passes both or neither, so a safeguard here can only add a check",
		Unit:   "the share of the work the service may be at, between 0 and 1",

		ReaderAtThisMilestone: "the health monitor, as the third reading beside the comparison and the service's own recent history",
	},
	{
		Parameter: ExplicitThresholdSize,
		Kind:      KindFraction, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyQuantity,
		Limits: "the smallest change from an explicit threshold worth catching, which the owner sets when they set the number",
		Unit:   "the smallest change ruled out, as a share",
	},
	{
		Parameter: HarmMarkPageCap,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyService,
		Limits: "how many intents one service's marked reports may page per interval",
		Unit:   "intents per interval, shipped with a default rather than supplied",
	},
	{
		Parameter: StrategyDefault,
		Kind:      KindStrategy, Direction: DirectionAdds, Scope: ScopeEnvironment, Key: KeyNone,
		Limits: "the rollout strategy production takes where nothing narrows the pick; a safeguard on it keeps a control, which adds a comparison rather than clamping a number",
		Unit:   "one of the two rollout strategies, and production's record alone holds it",
	},
	{
		Parameter: MaxConcurrentCandidateEnvironments,
		Kind:      KindCount, Direction: DirectionNone, Scope: ScopeEnvironment, Key: KeyNone,
		Limits:                "how many candidate environments the platform may hold at once, beside the platform's own room",
		Unit:                  "candidate environments live at once, authored outright with nothing supplied",
		ReaderAtThisMilestone: "the candidate deploy row's hold",
	},
	{
		Parameter: BakeVolume,
		Kind:      KindCount, Direction: DirectionFloor, Scope: ScopeService, Key: KeyNone,
		Limits:                "the traffic the targets a rollout has already reached serve before the next is reached; a safeguard may raise it and never lower it",
		Unit:                  "traffic served, the volume the window is read over being the same unit",
		ReaderAtThisMilestone: "the rollout, as the hold between one target and the next",
	},
	{
		Parameter: MutationFloor,
		Kind:      KindFraction, Direction: DirectionFloor, Scope: ScopeService, Key: KeyNone,
		Limits: "the mutation score below which Merge to master rejects; a safeguard may raise it and never lower it, as with the bake volume",
		Unit:   "the share of seeded defects the encodings caught, between 0 and 1",
	},
	{
		Parameter: BacklogCap,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "how many releases may wait behind a rollback hold before the merge queue stops fast-forwarding this service's candidates",
		Unit:   "releases waiting, and unauthored is the window limit",
	},
	{
		Parameter: SearchBudget,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "what a search may spend before it stops, each build it deploys putting a build that passed no gate in front of real traffic",
		Unit:   "builds, with the maximum total time production spends on them authored beside the count",
	},
	{
		Parameter: KeptFraction,
		Kind:      KindFraction, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "the fraction of its instances a release keeps while a rollback could return to it",
		Unit:   "the share of the release's capacity, between 0 and 1, and the fixed default is all of them",
	},
	{
		Parameter: MaxConcurrentKeptFleets,
		Kind:      KindCount, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "how many kept fleets one service may hold at once, a service at the cap stopping deploying rather than losing a recovery a window could still call for",
		Unit:   "kept fleets held at once, authored outright with nothing supplied",
	},
	{
		Parameter: RecentHistorySize,
		Kind:      KindFraction, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyQuantity,
		Limits: "the smallest change in one quantity the reading against a service's own recent history has to detect",
		Unit:   "the smallest change detected, as a share",
	},
	{
		Parameter: RecentHistoryRunLength,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "the average run length that reading is taken at; a safeguard may only shorten it, which adds a check rather than removing one",
		Unit:   "the mean volume a service whose behaviour has not changed runs before the reading crosses once",
	},
	{
		Parameter: Objective,
		Kind:      KindFraction, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "the proportion of a quantity that must be good over a stated period, which the error budget is read against",
		Unit:   "the proportion, between 0 and 1, with the period authored beside it",
	},
	{
		Parameter: PagingHours,
		Kind:      KindList, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "the hours within which the service pages, and the default is every hour because nothing the factory observes says which service may wait for morning",
		Unit:   "the first hour, the last hour and the zone they were written in",
	},
	{
		Parameter: ProofTestRate,
		Kind:      KindRate, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "how often the deployer, inside an open window, shifts a share of traffic onto the instances of the rollback's target and back again",
		Unit:   "the rate an owner authors, and no proof test runs at all where they author none",
	},
	{
		Parameter: ChangeFreeze,
		Kind:      KindList, Direction: DirectionFloor, Scope: ScopeService, Key: KeyNone,
		Limits:                "the periods within which this service's production deploys are held; a safeguard may add a period or lengthen one and may never shorten one",
		Unit:                  "one period per entry, the first moment and the last, and unauthored is no freeze at all",
		ReaderAtThisMilestone: "the production deploy row's hold, which lifts itself when the period passes",
	},
	{
		Parameter: InstanceHourRate,
		Kind:      KindRate, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits:                "what one instance-hour converts to, which turns the recorded instance-hours into money",
		Unit:                  "currency per instance-hour, and an absent rate leaves those figures units only",
		ReaderAtThisMilestone: "the deployer, at every write that closes a fleet's span",
	},
	{
		Parameter: EnvironmentHourRate,
		Kind:      KindRate, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits:                "what one environment-hour converts to, the second of the two rates that price hosting outside the factory",
		Unit:                  "currency per environment-hour, and an absent rate leaves those figures units only",
		ReaderAtThisMilestone: "the deployer, at every write that closes a compose-and-reclaim cycle",
	},
	{
		Parameter: OperationCap,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "how many operations one release may hold open per interval, capped as the failure record's keys are",
		Unit:   "operations per interval, with the overflow operation the excess lands in authored beside the count",
	},
	{
		Parameter: MutantCap,
		Kind:      KindCount, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "how many mutants the mutation score may spend per item, a fixed default leaving a safeguard nothing to constrain",
		Unit:   "mutants tested per item",
	},
	{
		Parameter: FailureRecordKeyCap,
		Kind:      KindCount, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "how many distinct keys a release may hold open per interval for its failure records; a safeguard may lower it and never raise it",
		Unit:   "distinct keys per interval",
	},
	{
		Parameter: UnreliableBound,
		Kind:      KindFraction, Direction: DirectionFloor, Scope: ScopeService, Key: KeyNone,
		Limits: "the rate of disagreement above which a criterion of this service is unreliable; a safeguard may raise it and never lower it, lowering being what takes a criterion out of the gate",
		Unit:   "the rate at which the criterion's outcome disagrees across builds, between 0 and 1",
	},
	{
		Parameter: IncidentItemBound,
		Kind:      KindSeconds, Direction: DirectionNone, Scope: ScopeService, Key: KeyNone,
		Limits: "how long an incident-raised item may be worked before a human is reached",
		Unit:   "seconds, with a fixed default",
	},
	{
		Parameter: SnapshotRetention,
		Kind:      KindSeconds, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "how long a schema-change snapshot is kept; a safeguard may shorten it and never lengthen it, what it retains being a copy of production data",
		Unit:   "seconds, and where an owner authors none a snapshot stands until they delete it",
	},
}

// SafeguardOnly is every parameter that a safeguard binds and nobody authors.
// There are two. It is a list of its own rather than a row of [Definitions]
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
	{
		Parameter: DriftDetectorLastCheckMaxAge,
		Kind:      KindSeconds, Direction: DirectionCeiling, Scope: ScopeNothing, Key: KeyNone,
		Limits:                "how old the drift detector's own last check may be before the production deploy row holds",
		Unit:                  "seconds since that record was written",
		ReaderAtThisMilestone: "the production deploy row, beside the mismatch it already reads",
	},
}
