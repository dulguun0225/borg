package gatepolicy

// The eleven rows, named once so that a definition and a test name the same
// string. Ten rows carry one parameter; [RowWindowSizeConfidencePower] carries
// three, the size, the confidence and the power being authored together.
const (
	RowRiskThreshold             = "risk threshold"
	RowExposureBound             = "the exposure bound"
	RowAdvisorySeverity          = "the advisory severity"
	RowAttemptLimit              = "attempt limit"
	RowItemSizeTarget            = "item-size target"
	RowAllowedPredicateKinds     = "the list of allowed predicate kinds"
	RowWindowSizeConfidencePower = "the analysis window's size, confidence, and power"
	RowWindowCap                 = "the analysis window's cap"
	RowWindowLimit               = "window limit"
	RowHeldOutSampleRate         = "held-out sample rate"
	RowReviewSampleRate          = "review sample rate"
)

// Definitions is the thirteen parameters of gate policy's eleven rows, in the
// order gate policy's own table lists them. What is not here is
// [NotAmongTheEleven] and [SafeguardOnly].
var Definitions = []Definition{
	{
		Parameter: RiskThreshold, Row: RowRiskThreshold,
		Kind: KindFraction, Direction: DirectionAddsAHuman, Scope: ScopeEnvironment, Key: KeyGateRow,
		Limits:                "where the score stops auto-passing and puts a human at the gate",
		Unit:                  "the number a gate compares, between 0 and 1",
		ReaderAtThisMilestone: "both gate rows",
	},
	{
		Parameter: ExposureBound, Row: RowExposureBound,
		Kind: KindFraction, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits: "where the exposure factor stops being weighed and puts a human at Implementation instead",
		Unit:   "the exposure factor's value, between 0 and 1",
	},
	{
		Parameter: AdvisorySeverity, Row: RowAdvisorySeverity,
		Kind: KindSeverity, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits: "the bound at or above which a matching advisory rejects at Implementation and holds at Deploy to production",
		Unit:   "a severity on the advisory feed's own scale",
	},
	{
		Parameter: AttemptLimit, Row: RowAttemptLimit,
		Kind: KindCount, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyStage,
		Limits: "how many times a stage is retried, how many rounds the interview asks, and how many times decomposition runs again on a rejected set",
		Unit:   "attempts at one stage",

		ReaderAtThisMilestone: "the stages that retry",
	},
	{
		Parameter: ItemSizeTarget, Row: RowItemSizeTarget,
		Kind: KindCount, Direction: DirectionCeiling, Scope: ScopeArea, Key: KeyNone,
		Limits: "how large an item is meant to be, above the minimum that it ships by itself",
		Unit:   "the count of the intent's requirements an item answers, the unit decomposition sets",
	},
	{
		Parameter: AllowedPredicateKinds, Row: RowAllowedPredicateKinds,
		Kind: KindList, Direction: DirectionFloor, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits:                "what kinds of assertion a consumer contract may draw from",
		ReaderAtThisMilestone: "the derivation of a consumer contract",
	},
	{
		Parameter: WindowSize, Row: RowWindowSizeConfidencePower,
		Kind: KindFraction, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyQuantity,
		Limits:                "the smallest regression the comparison must rule out to close passed",
		Unit:                  "the smallest regression ruled out, as a share",
		ReaderAtThisMilestone: "the boundary, at every read of the comparison",
	},
	{
		Parameter: WindowConfidence, Row: RowWindowSizeConfidencePower,
		Kind: KindFraction, Direction: DirectionFloor, Scope: ScopeService, Key: KeyNone,
		Limits:                "how sure the comparison must be before rolling a release back",
		Unit:                  "the confidence required, as a share",
		ReaderAtThisMilestone: "the boundary, as where it crosses in either direction",
	},
	{
		Parameter: WindowPower, Row: RowWindowSizeConfidencePower,
		Kind: KindFraction, Direction: DirectionFloor, Scope: ScopeService, Key: KeyQuantity,
		Limits: "how reliably a regression of the size in force is caught rather than reaching passed",
		Unit:   "the power required, as a share",
	},
	{
		Parameter: WindowCap, Row: RowWindowCap,
		Kind: KindSeconds, Direction: DirectionFloor, Scope: ScopeService, Key: KeyNone,
		Limits:                "the elapsed time that ends a window which will never reach its volume",
		Unit:                  "seconds",
		ReaderAtThisMilestone: "the health monitor, as the exit a window that will never reach its volume takes",
	},
	{
		Parameter: WindowLimit, Row: RowWindowLimit,
		Kind: KindCount, Direction: DirectionCeiling, Scope: ScopeService, Key: KeyNone,
		Limits:                "how many analysis windows one service may hold open at once",
		Unit:                  "windows open at once, per service",
		ReaderAtThisMilestone: "the production deploy row's hold, and how many releases one rollback undoes",
	},
	{
		Parameter: HeldOutSampleRate, Row: RowHeldOutSampleRate,
		Kind: KindFraction, Direction: DirectionCeiling, Scope: ScopeFactorySettings, Key: KeyNone,
		Limits:                "how often the score auto-passes a change it would have gated, to keep unbiased signal on the authors and areas it has stopped trusting",
		Unit:                  "the share of firings held out, between 0 and 1",
		ReaderAtThisMilestone: "the held-out sample",
	},
	{
		Parameter: ReviewSampleRate, Row: RowReviewSampleRate,
		Kind: KindFraction, Direction: DirectionFloor, Scope: ScopeFactorySettings, Key: KeyDuty,
		Limits: "how often a change the score would have auto-passed is put in front of a duty's human anyway",
		Unit:   "the share of auto-passes put in front of that duty's human, between 0 and 1",
	},
}
