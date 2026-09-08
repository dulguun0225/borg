// Package score is the risk score — a vector of named factors reduced to one
// number by a published formula, computed once per gate firing — and the pass
// that moves what the score supplies as outcomes arrive.
//
// # The code
//
// score.go is [Score], [Composition], [New], [Score.Assess] and
// [Score.AssessUnder], with [Change], [Measurement], [ExposureEvidence] and
// [FleetChange] — what the caller hands in — and [OpenEvent] and [CloseEvent],
// the parts of a decision's payloads this package reads back. A gate an authored
// threshold binds assesses through [Score.AssessUnder], the version in force at
// its scope not always being the newest. factor.go is [Factor], [Group] and [Term]; factorsets.go is
// [FactorSet], the three sets, [Weights] and the weights the product ships;
// formula.go is [Formula], [FormulaVersion] and [Assessment]. factorread.go
// reads the change, author and context factors out of the records, exposure.go
// the exposure factor out of the evidence handed in, and prior.go the per-author
// prior with the width and the count of resolved window exits behind it.
// resolution.go is [Resolution] and the [Cause] of each: a resolved factor is
// left out of the weighted means, and a firing that resolved anything is a
// human's whatever the number reads. strategy.go is the rollout strategy the
// score picks at a production deploy — [Strategy], [Schedule], [Pick],
// [Rollout], [PickStrategy] and [ShippedControlBound] — read against the bound
// the version names, package gate storing the answer in a shape of its own.
//
// windowhistory.go is the [Evidence] methods read off the windows and the
// stages, unexported, that windowlearn.go, rejection.go and learn.go fold: a
// service's window and rollback history in time order, the finest size and the
// timed-out run its traffic reached, how long a window took to resolve, and an
// area's or a stage's stalls and successes.
//
// version.go is [Version] — a row of the decision log, appended through
// [decisionlog.Writer.AppendScoreVersion] and read back by shape — with
// [Writer], [Writer.Ensure], [Writer.Recalibrate], [Writer.EnterShipped], the
// reads [Newest], [Get] and [All], and [InForceAt], which is the version that
// decides a gate an authored threshold binds. supplied.go is [Supplied],
// [SuppliedValues], [Starting], [StartingValues] and [QuantitySubject].
//
// learn.go is the pass — [Learn] over the store, [LearnFrom] over an [Evidence],
// both answering a [Learned]. evidence.go is [ReadEvidence] and the [Outcome] of
// each item; windowlearn.go moves the analysis window's size and power per
// quantity, its cap per service, and the window limit; rejection.go resolves a human's rejection one of four ways
// and publishes the [FalseAlarm]s; bands.go is the [Band]s of the number;
// drift.go is the two calibration readings and the [Drift] each publishes;
// fit.go is [Fit], the weights a recalibration refits, and neverWeighed, the
// factors it leaves where the product shipped them; rules.go is [Rules] and
// [LearningVersion]. Learning is a pass and never a write at a firing, so every
// decision of one run names the version the run started with.
//
// sample.go is the held-out sample: the [Draw] interface with [RandomDraw] and
// [NeverDraw], [Score.HoldOut] and the [Selection] it writes, and the reads
// [HeldOut] and [HeldOutItems]. It selects an item and not a firing, and it
// passes nothing a safeguard put a human at and nothing a resolution did.
//
// report.go is the two read-time reporting queries a screen asks for beside
// the pair it already publishes, neither writing anything: [RealizedAutoPass]
// and [ThresholdRealized] are the realized auto-pass rate at each risk
// threshold in force, per factor set, against the rate a [policy] version
// recorded at the write, which the caller supplies onto
// [ThresholdRealized.RecordedRate] because this package does not import
// policy; [HeldOutByBand] and [BandOutcome] are the share of held-out
// releases whose windows failed within each band of the number, factory-wide,
// over the span the caller names, excluding a release [markedReleases] finds
// marked, the same exclusion [Learn]'s own pass applies.
//
// marks.go is [Marks], what a named human at Ops marked as not caused by the
// release. The record is package window's rollback mark, written by a named
// human at Ops; it is an interface here rather than a read of that package
// because what the score learns from is the set of marked releases and nothing
// else, and [NoMarks] is what a factory with none composes.
//
// authorship.go is [Authored] and the [Authorship] that reads it, what an agent
// authoring the version under decision worked from — the input manifest, the
// effort, and the versions of the role prompt and the skills — which the vector
// names and no factor weighs. withdrawal.go is [ProtectionRemoved] and the
// [Withdrawals] that reads it, what a spec version under decision withdraws.
// harmmark.go is [HarmMarks], whether a report grouped into the item's intent
// says a person is being harmed by the software — a field of a report, which is
// in a store of its own no record of the graph carries the mark from. Its
// factor is the one a refit never weighs: fit.go's neverWeighed holds it at the
// nothing the product ships it at, because the design says the mark adds no
// gate a report did not already meet and a weight above nothing would be one. The three
// are interfaces because each joins records this package does not read, and
// [NoAuthorship], [NoWithdrawals] and [NoHarmMarks] are what a composition
// supplying none hands in. What a withdrawal's provenance names is carried on
// [Resolution.RoutedTo], which is what routes the Spec row to that human rather
// than to the owner by default.
//
// Two inputs arrive as parameters because nothing writes them yet:
// [ExposureEvidence], which the component that built the change derives per
// toolchain the way it takes the diff, and [FleetChange], which is the fleet's
// own records. A factory that fires without either resolves the factor rather
// than reading it as nothing. [Measurement]'s reading of whether the diff
// destroys stored data arrives the same way and for the same reason, and its
// own could-not-derive resolves the reversibility factor.
//
// # What the design does not decide
//
// Four things here the design names without fixing, each stated where it is
// made rather than invented as though it were the document's. The weight
// context.protection_withdrawn ships at is nothing: the design names the factor's
// resolution and no weight for it, and a recalibration fits one like every other.
// Where the line falls for [ShippedControlBound] is the code's: the design states
// which half of the vector the strategy reads and not the bound. What makes a
// diff destroy stored data is a reading per toolchain the design does not
// describe, derived by the caller that takes the diff. And which factor carries
// a screen the transition check could not derive is this package's:
// the design fixes that outcome as a resolution the way an unavailable factor
// resolves, with the vector naming the screen and the constructs, and names no
// factor for it, so [Change.ScreensNotDerived] reads through
// context.protection_withdrawn — the factor whose other reading is whether what
// is under decision admits what a human-confirmed machine forbade.
//
// # The shape
//
// This package owns no table, which is where it departs from the shape a record
// package has: the score version is a row of the decision log, so there is no
// schema.go and no DDL, and package postgres does not name it. What it writes is
// that row and nothing else, and it reads every other record through the owning
// package's readers.
//
// Who may write what: this package appends score versions to the log through
// [decisionlog.Writer]. It writes no record.
//
// What defines it: the four factor groups, the score version, the resolutions
// and the three factor sets are
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md
// (C1214, C1215, C1216, C1219, C1220, C1222, C1223, C1224, C1225, C1226, C1227,
// C1229, C1230, C1231, C1233, C1234, C1235, C1237, C1238, C1239, C1242, C1243,
// C1245, C1246, C1247, C1250, C1251, C1252, C1253, C1254, C1256, C1257, C1258,
// C1259, C1260, C1261, C1262, C1264, C1265, C1267, C1268, C1270, C1273, C1276,
// C1277, C1278, C1279, C1282, C1283, C1284, C1285, C1286, C1287, C1288, C1292,
// C1294, C1296, C1298, C1299, C1300, C1302, C1303, C1304, C1305, C1306, C1307,
// C1308, C1309, C1310, C1311, C1315, C1316, C1317); the loop, the held-out
// sample, the bands and the drift readings are
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md
// (C1319, C1320, C1322, C1323, C1324, C1325, C1326, C1327, C1328, C1329, C1330,
// C1331, C1332, C1333, C1334, C1335, C1336, C1337, C1338, C1340, C1341, C1342,
// C1343, C1344, C1345, C1352, C1353, C1354, C1355, C1358, C1359, C1360, C1361,
// C1362, C1364, C1365, C1366, C1367, C1368, C1369, C1370, C1371, C1372, C1373,
// C1374, C1376, C1377, C1378, C1379); the values it supplies are the rows of
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md
// (C2187, C2188, C2191, C2193, C2195, C2196, C2197, C2198) and their scopes are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2199, C2200, C2252, C2258, C2260, C2261);
//
// the window limit and the mark are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2042, C2044, C2045);
//
// the window's size, power and cap are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md
// (C2033, C2035, C2036);
//
// the hazard severity the context group reads is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/03-hazard-severity.md
// (C0683, C0685, C0687);
//
// the score version as a row of the chained log is seam 2 of
// ../../end-goal/deferred.md (C0066, C0069, C0071); and the two read-time
// reporting queries report.go adds are
// ../../end-goal/how-the-factory-works/11-screens/04-what-the-factory-auto-approved-and-what-was-undone.md
// (C2723, C2724, C2725); the empty author of a shipped role prompt or
// skill version, read by prior.go as the unavailable factor a human decides,
// is ../../end-goal/how-the-factory-works/10-fleet/03-what-an-agent-is-told/README.md
// (C2447);
//
// and the intent grouped from reports, whose source factorread.go resolves at
// Spec rather than weighing — so a human confirms the criteria whatever the
// rest of the vector says, and sample.go's held-out draw cannot select past
// what a resolved factor put there — with the harm mark read beside it through
// [HarmMarks], resolving the same row on its own account and weighed at nothing
// because it adds no gate a report did not already meet, is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0401, C0402, C0403, C0404, C0405) and
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md
// (C0371).
package score
