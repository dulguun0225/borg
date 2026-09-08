// Package service owns the service record: the identity decomposition writes,
// everything an owner authors on it at Factory, and the four fields the deployer
// populates.
//
// # The code
//
// schema.go is [Table] and the seven tables beside it — [WindowSizeTable],
// [WindowPowerTable], [ExplicitThresholdTable], [RecentHistorySizeTable],
// [ChangeFreezeTable], [SeedTable], [ValueSetTable] —
// with their id prefixes, format versions, and [DDL]. writer.go is [Service], [Writer] and [NewWriter]
// with [Writer.Create], and the reads [Get], [ByName] and [All], each of which
// returns everything on the record. parameters.go is [Parameters] and
// the gate-policy writes: [SetWindowSize] and [SetWindowPower] per
// [gatepolicy.Quantity], and
// [SetWindowConfidence], [SetWindowCap], [SetWindowLimit] and [SetExposureBound]
// for the service, beside [SetBakeVolume], [SetBacklogCap],
// [SetInstanceHourRate], [SetMutationFloor], [SetKeptFraction],
// [SetMaxConcurrentKeptFleets], [SetRecentHistoryRunLength],
// [SetProofTestRate], [SetMutantCap], [SetFailureRecordKeyCap],
// [SetUnreliableBound] and [SetIncidentItemBound], which are authored the same way
// and are not gate policy's. provisioning.go is [Provisioned], [CredentialShape],
// [SetProvisioned], [SetTargets] and [Retire]. operations.go is [Objective],
// [PagingHours], [SetObjective], [SetPagingHours], [SetProductLicence] and
// [SetSnapshotRetention]. threshold.go is [Threshold] and
// [SetExplicitThreshold], the absolute number a safeguard sets per quantity
// beside the size it is read at, with [SetRecentHistorySize] per quantity,
// [SetOperationCap] — the cap on how many
// operations one release may hold open per interval and the overflow operation
// the excess lands in — [SetEnvironmentHourRate] and [SetSearchBudget].
// freeze.go is the change freeze: [Period], [AddFreezePeriod], [FreezePeriods]
// and [Frozen], the read a gate row asks at every firing.
// deployer.go is [Reachability] and [Adopt]. versions.go is
// [Version] with [AuthorSeed], [AuthorValueSet], [SeedInForce], [ValueSetInForce]
// and the two lists beside them. shipped.go is the shipped default of each of
// the four fields the design fixes rather than gives a number for —
// [ShippedMutantCap], [ShippedFailureRecordKeyCap], [ShippedUnreliableBound]
// and [ShippedIncidentItemBoundSeconds] — and the reader beside each,
// [MutantCapInForce], [FailureRecordKeyCapInForce], [UnreliableBoundInForce]
// and [IncidentItemBoundSecondsInForce], which every caller of the matching
// field goes through rather than reading the column and falling back to its
// zero value. The tests are one file per subject above —
// db_test.go, parameters_test.go, provisioning_test.go, operations_test.go,
// deployer_test.go, threshold_test.go, freeze_test.go, versions_test.go and
// shipped_test.go —
// sharing the newWriter, acquire, begin
// and commit helpers of db_test.go and helpers_test.go, every one of them against
// the database.
//
// The repository field is a filesystem path or URL of one git repository; the
// record names it and this package neither creates nor reads it. What the
// identity points at is outside the factory that way — the repository, and a
// store on each persistent environment — and [Provisioned] is what an owner
// writes to say both exist.
//
// # Who may write what
//
// Three writers, and the seam between them is the field.
//
// Decomposition writes the identity through [Writer.Create], in the same write as
// the item that creates the service: the name, the repository, and the project.
// The project is identity and no later write moves it. Decomposition writes
// nothing else here.
//
// An owner at Factory writes every parameter, [SetProvisioned], [Retire],
// [SetTargets], the two versioned authorings, and the values that are not gate
// policy's. Each is a function taking the caller's transaction rather than a
// method on [Writer], because [Writer] is decomposition's: package policy calls
// them inside the transaction it appends the policy version in, so the field and
// the version commit together or not at all. A write that updates a column of
// [Table] takes no lease token, that transaction having been fenced by policy
// before anything wrote in it; a write that inserts a record row of its own —
// [SetWindowSize], [SetWindowPower], [AuthorSeed], [AuthorValueSet] — takes the
// token and fences, the arrangement package environment's threshold write and
// package safeguard's insert already have.
//
// The deployer writes [Adopt] and nothing else: the four fields that say what
// runs can be reached, replaced, undone, and read, at adoption and at every first
// release. It takes the token and fences because its caller begins the
// transaction it runs in: package deploy's adoption, called by whatever composes
// the deployer at a service's first release.
//
// # What is not built
//
// Package policy is the caller of every owner-authored write here, package
// gatepolicy naming the parameter each is authored under. [Retire] takes the
// three counts that refuse it as arguments, because each is a read of a package
// this one may not import; what computes them is the interface the owner retires
// through, and the removal the deployer performs when the write lands is what
// package policy calls through what its composition supplies.
//
// Four fields have a fixed default rather than a supplied value, and the
// design states no number for any of them: the mutant cap, the failure-record
// key cap, the bound above which a criterion is unreliable, and the
// incident-raised item bound. The column is null until an owner authors one,
// and shipped.go is the one place a number is chosen where the design leaves
// it unstated — [ShippedMutantCap], [ShippedFailureRecordKeyCap],
// [ShippedUnreliableBound] and [ShippedIncidentItemBoundSeconds] — with the
// reader beside each that returns the authored value or the shipped one. A
// caller elsewhere in the factory that holds a [Service] reads the field
// unauthored where the design gives no number, so a reader of a decision
// taken under one reaches the number through the release that shipped it and
// not through this document. Package criterion's [criterion.Unreliable] is
// the one built caller so far, resolving the bound this way rather than
// taking a raw number that would read an unauthored field as its zero value.
//
// The proof test rate is a fifth field the design fixes without giving a
// number, and it has no shipped default: the design says how often the
// rollback path is exercised and never what the rate is counted per, so a
// default chosen without that unit would be a number nothing can use. The
// field holds the number an owner authored and the component that would run a
// proof test is not built.
//
// # What defines it The identity, the two writers of it, provisioned with the
// credential shape, the deployer's four, the mutant cap, the failure-record key
// cap, the unreliable bound, the incident-raised item bound, and the product
// licence are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md
// (C0718, C0719, C0724, C0725, C0726, C0730, C0731, C0732, C0733, C0734, C0735,
// C0736, C0737, C0738, C0740).
//
// Retirement is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md
// (C0697, C0698, C0706), and the parameters a service starts with are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md.
//
// The project as a field of this record is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md
// (C2616, C2619).
//
// Which of an environment's targets a service runs on is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1399), and the seed and the non-production value set are
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/01-the-store-and-the-configuration.md
// (C1487, C1488, C1492, C1493).
//
// The window's size, confidence and power per quantity, the cap, the window
// limit and the exposure bound are
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md
// (C2188, C2193, C2195, C2196), and the twelve fields authored here that are
// not among the eleven, with the direction a safeguard on each points, are
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/01-authored-and-not-among-the-eleven.md
// (C2277, C2278, C2279, C2280, C2283, C2293, C2294, C2295).
//
// The change freeze is
// ../../end-goal/how-the-factory-works/09-gate-policy/04-stopping-the-factory.md
// (C2350, C2351, C2352, C2353, C2354), the mutation floor is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/07-merge-to-master.md,
// the size and average run length of the reading against the service's own
// recent history are
// ../../end-goal/how-the-factory-works/08-operations/01-the-health-monitor.md
// (C1939, C1940, C1943, C1944, C1945, C1967), and the fraction of its instances
// a release keeps, the maximum concurrent kept fleets, the proof test rate and
// the search budget are
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md
// (C2042, C2055, C2056, C2058, C2066, C2067, C2073). The bake volume between
// one target of a rollout and the next is
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md
// (C0926, C0928), the backlog cap on how many releases wait behind a rollback
// hold is
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md,
// and the instance-hour rate the deployer prices a fleet's span at is
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/02-what-stands-for-a-rollback.md.
// The objective and its period are
// ../../end-goal/how-the-factory-works/08-operations/05-service-level-objectives.md
// (C2082, C2089, C2091), and the hours a service pages within are
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md (C2137, C2138,
// C2141). The schema-change snapshot retention is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md
// (C2304), the bound above which a criterion is unreliable is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/02-in-force-and-withdrawal.md
// (C1058), the mutant cap is
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md
// (C1529), a fixed default being a value the product ships is
// ../../end-goal/deferred.md#the-products-release-channel (C0089, C0090, C0091,
// C0093), and the repository credential pair is seam 3 of
// ../../end-goal/deferred.md.
package service
