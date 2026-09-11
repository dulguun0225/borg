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
// [Writer], [Writer.Ensure], [Writer.Recalibrate] and [Writer.EnterShipped];
// versionread.go, split off at the length a file is held to, is the reads
// [Newest], [Get] and [All] and the comparison a version is appended over, and
// [InForceAt], which is the version that
// decides a gate an authored threshold binds. Five of its fields are what the
// product ships or what a recalibration fits, and none of them is a constant
// here: [Version.BandWidthOrShipped] is the width of a band of the number and
// with it the step the risk threshold moves by, the two being one number;
// [Version.ShippedPrior] is the prior the product shipped for a model version,
// where an unseen author of that version starts; [Version.ScaleOf] is the set's
// fitted last step, moved with the weights by a recalibration and by nothing
// else; RecalibratedThrough is the newest decision the last
// recalibration read, which is what ends a drift; and [Version.RestartedAt]
// is when an author's prior restarted as an unseen author's, carried forward
// by every version this package appends once it is set.
// supplied.go is [Supplied],
// [SuppliedValues], [Starting], [StartingValues] and [QuantitySubject].
//
// learn.go is the pass — [Learn] over the store, [LearnFrom] over an [Evidence],
// both answering a [Learned] and both taking an [Under], which is what the pass
// reads off the version below it rather than off this source. evidence.go is [ReadEvidence] and the [Outcome] of
// each item; windowlearn.go moves the analysis window's size and power per
// quantity, its cap per service, and the window limit; rejection.go resolves a human's rejection one of four ways
// and publishes the [FalseAlarm]s, and reads the merge queue's own rejection
// off the decision log's queue_rejection row, without importing package
// mergequeue, as a gate the factory needed at the merge to master row where it
// learns as from a reject and its criterion was not unreliable over the two
// builds compared, and as nothing at all where it learns as from a hold;
// bands.go is the [Band]s of the number;
// drift.go is the two calibration readings and the [Drift] each publishes, and
// the restart a prior takes where a truncation of the log has removed every
// held-out decision behind one standing drifted;
// fit.go is [Fit], the weights a recalibration refits, [FitScale] and [Scale],
// the last step it fits with them, and neverWeighed, the
// factors it leaves where the product shipped them. The two halves are one fit:
// every term of the formula is a weighted mean, so the weights rank one change
// against another and cannot move where that ranking sits against a failure
// share, and the scale is the line from the number to the share of held-out
// windows that failed at it — the one scale the three sets share. rules.go is [Rules] and
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
// reportsandadvisories.go is [ReportsAndAdvisories], the releases a later
// advisory or report arrived on. The exposure factor learns from no outcome of
// its own, and the one outcome that speaks to it arrives on the prior instead;
// it is an interface for the reason [GroupedReports] is, a report being a field of a
// store of its own and an advisory a feed no record of the graph carries, and
// [NoReportsAndAdvisories] is what a composition supplying none hands in.
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
// groupedreports.go is [GroupedReports], whether the intent an item answers
// carries text the factory did not author that no gate has admitted — its own
// source, or a report the grouper has since grouped into it — and whether a
// report grouped into it says a person is being harmed by the software: a
// field of a report, which is in a store of its own no record of the graph
// carries either from. The mark's factor is the one a refit never weighs:
// fit.go's neverWeighed holds it at the nothing the product ships it at,
// because the design says the mark adds no gate a report did not already meet
// and a weight above nothing would be one. The three
// are interfaces because each joins records this package does not read, and
// [NoAuthorship], [NoWithdrawals] and [NoGroupedReports] are what a composition
// supplying none hands in. What a withdrawal's provenance names is carried on
// [Resolution.RoutedTo], which is what routes the Spec row to that human. A
// constraint-derived or a hazard-derived withdrawal naming nobody the factory
// can resolve routes to the owner by default; a human-confirmed one naming
// nobody — the composition found no actor of the introducing decision still
// holding the row's duty, and no other holder of it either — is unavailable
// instead, the treatment every other unreadable input takes, and never the
// owner.
//
// [ReportsAndAdvisories] is the third such input and arrives as a seam rather
// than a parameter, for the reason the harm mark does: it is a join over the
// report store and the advisory feed, and cmd/factory composes
// [NoReportsAndAdvisories] because no read of either answers which releases a
// later report or advisory arrived on. So the prior counts none of them today
// and counts every one a composition that answers does.
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
// Six things here the design names without fixing, each stated where it is
// made rather than invented as though it were the document's.
// The prior the product ships per model version is one: the design says an
// unseen author starts at it where the product shipped one, and prior.go's own
// table is empty — a prior for a model version is a calibration over that
// version's outcomes across installs, this product has run none, and a number
// invented there would narrow every unseen author's prior on nothing. What the
// version carries is the table, so an install that is given one reads it.
// What part of a version a rejection named is the other: the design resolves a
// rejection on the re-authored version differing by content digest in what the
// rejection named, and nothing in the graph divides a version into parts, so
// rejection.go takes the digest over the lines of the version that carry the
// words the human named. The weight
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
// (C1214, C1215, C1216, C1219, C1220, C1221, C1222, C1223, C1224, C1225, C1226,
// C1227, C1228, C1229, C1230, C1231, C1232, C1233, C1234, C1235, C1237, C1238,
// C1239, C1242, C1243, C1244, C1245, C1246, C1247, C1250, C1251, C1252, C1253,
// C1254, C1256, C1257, C1258, C1259, C1260, C1261, C1262, C1264, C1265, C1267,
// C1268, C1270, C1273, C1275, C2997, C1276, C1277, C1278, C1279, C1280, C1282, C1283,
// C1284, C1285, C1286, C1287, C1288, C1290, C1292, C1294, C1296, C1298, C1299,
// C1300, C1302, C1303, C1304, C1305, C1306, C1307, C1308, C1309, C1310, C1311,
// C1315, C1316, C1317); the loop, the held-out
// sample, the bands and the drift readings are
// ../../end-goal/how-the-factory-works/04-risk-score/02-how-it-learns.md
// (C1319, C1320, C1322, C1323, C1324, C1325, C1326, C1327, C1328, C1329, C1330,
// C1331, C1332, C1333, C1334, C1335, C1336, C1337, C1338, C1340, C1341, C1342,
// C1343, C1344, C1345, C1352, C1353, C1354, C1355, C1356, C1357, C1358, C1359,
// C1360, C1361, C1362, C1364, C1365, C1366, C1367, C1368, C1369, C1370, C1371,
// C1372, C1373, C1374, C1376, C1377, C1378, C1379); the values it supplies
// are the rows of
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md
// (C0742, C2187, C2188, C2191, C2193, C2195, C2196, C2197, C2198) and their scopes are
// ../../end-goal/how-the-factory-works/09-gate-policy/02-one-shape-across-all-of-them.md
// (C2199, C2200, C2252, C2258, C2260, C2261);
//
// the window limit and the mark are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2042, C2044, C2045);
//
// the window's size, power and cap, and counting only a health-monitor-
// traceable undo as evidence, are
// ../../end-goal/how-the-factory-works/08-operations/02-the-analysis-window.md
// (C2033, C2034, C2035, C2036);
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
// and the intent carrying text the factory did not author, whose source or
// whose grouped reports factorread.go resolves at Spec rather than weighing —
// so a human confirms the criteria whatever the rest of the vector says, and
// sample.go's held-out draw cannot select past what a resolved factor put
// there — with the harm mark read beside it through [GroupedReports],
// resolving the same row on its own account and weighed at nothing because it
// adds no gate a report did not already meet, is
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/02-reports.md
// (C0401, C0402, C0403, C0404, C0405) and
// ../../end-goal/how-the-factory-works/02-intent-into-items/01-intake/01-constraints-and-the-design-system.md
// (C0371).
//
// The score as a vector reduced by a published formula is
// ../../end-goal/how-the-factory-works/04-risk-score/README.md (C1381).
//
// Resolving the report-grouped and the adoption intent's source value, and
// what an owner leaves unauthored, are
// ../../end-goal/how-the-factory-works/01-one-pipeline.md (C0190) and
// ../../end-goal/what-humans-do.md (C2877); resolving a report-grouped intent
// to Spec is
// ../../end-goal/how-the-factory-works/02-intent-into-items/02-the-interview.md
// (C0543).
//
// Review sampling moving the threshold one way, and learning from a
// review-sampled rejection alone, are
// ../../end-goal/how-the-factory-works/03-gates/01-where-a-gate-is-and-what-decides-it.md
// (C0902, C0903); picking only the widening schedule at irreversible, and
// withholding the other two there, are
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0919, C0920); supplying the floor a safeguard may raise is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md
// (C1159).
//
// [ProtectionRemoved] resolving the Spec row and carrying the routing, and
// [Resolution.RoutedTo] as the introducing actor still holding the duty, or
// another holder of it where that actor no longer does, are
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/02-in-force-and-withdrawal.md
// (C1063, C1064);
//
// [PickStrategy] returning no control where no share served is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1409); the report-sourced intent resolving the source factor is
// ../../end-goal/how-the-factory-works/05-environments/04-what-the-candidate-environment-decides/02-the-verdict.md
// (C1583); and [Measurement]'s reading destroying stored data resolving
// reversibility at Implementation is
// ../../end-goal/how-the-factory-works/07-contracts/08-deprecation.md
// (C1847).
//
// An unreliable criterion's queue rejection teaching this package nothing,
// beside a repeated failure the two compositions agree on being read as from a
// reject at the merge to master row, is
// ../../end-goal/how-the-factory-works/05-environments/03-the-merge-queue.md
// (C1539).
package score
