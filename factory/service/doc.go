// Package service owns the service record: the identity decomposition writes,
// everything an owner authors on it at Factory, and the four fields the deployer
// populates.
//
// # The code
//
// schema.go is [Table] and the five tables beside it — [WindowSizeTable],
// [WindowPowerTable], [ExplicitThresholdTable], [SeedTable], [ValueSetTable] —
// with their id prefixes, format versions, and [DDL]. writer.go is [Service], [Writer] and [NewWriter]
// with [Writer.Create], and the reads [Get], [ByName] and [All], each of which
// returns everything on the record. parameters.go is [Parameters] and
// the gate-policy writes: [SetWindowSize] and [SetWindowPower] per
// [gatepolicy.Quantity], and
// [SetWindowConfidence], [SetWindowCap], [SetWindowLimit] and [SetExposureBound]
// for the service, beside [SetBakeVolume], [SetBacklogCap],
// [SetInstanceHourRate], [SetMutantCap], [SetFailureRecordKeyCap],
// [SetUnreliableBound] and [SetIncidentItemBound], which are authored the same way
// and are not gate policy's. provisioning.go is [Provisioned], [CredentialShape],
// [SetProvisioned], [SetTargets] and [Retire]. operations.go is [Objective],
// [PagingHours], [SetObjective], [SetPagingHours], [SetProductLicence] and
// [SetSnapshotRetention]. threshold.go is [Threshold] and
// [SetExplicitThreshold], the absolute number a safeguard sets per quantity
// beside the size it is read at, with [SetOperationCap] — the cap on how many
// operations one release may hold open per interval and the overflow operation
// the excess lands in — [SetEnvironmentHourRate] and [SetSearchBudget]. deployer.go is [Reachability] and [Adopt]. versions.go is
// [Version] with [AuthorSeed], [AuthorValueSet], [SeedInForce], [ValueSetInForce]
// and the two lists beside them. The tests are one file per subject above —
// db_test.go, parameters_test.go, provisioning_test.go, operations_test.go,
// deployer_test.go, threshold_test.go and versions_test.go — sharing the newWriter, acquire, begin
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
// release. It takes the token and fences because its caller is not built.
//
// # What is not built
//
// Package policy is the caller of every owner-authored write here, and it calls
// [SetWindowConfidence], [SetWindowCap], [SetWindowLimit] and [SetWindowSize]
// today. The rest wait on package gatepolicy naming the parameter each is
// authored under, and until it does they are reachable from a test and from
// nothing else. [Retire] takes the three counts that refuse it as arguments,
// because each is a read of a package this one may not import; the caller that
// computes them is not built either.
//
// # What defines it
//
// The identity, the two writers of it, provisioned with the credential shape, the
// deployer's four, the mutant cap, the failure-record key cap, the unreliable
// bound, the incident-raised item bound, and the product licence are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
// Retirement is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/04-retirement.md,
// and the parameters a service starts with are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md.
// The project as a field of this record is
// ../../end-goal/how-the-factory-works/11-screens/01-work-ops-factory-people.md.
// Which of an environment's targets a service runs on is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md,
// and the seed and the non-production value set are
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/01-the-store-and-the-configuration.md.
// The window's size, confidence and power per quantity, the cap, the window limit
// and the exposure bound are
// ../../end-goal/how-the-factory-works/09-gate-policy/01-what-is-in-it.md. The
// bake volume between one target of a rollout and the next is
// ../../end-goal/how-the-factory-works/03-gates/02-the-rollout-strategy.md, the
// backlog cap on how many releases wait behind a rollback hold is
// ../../end-goal/how-the-factory-works/08-operations/03-overlapping-windows.md,
// and the instance-hour rate the deployer prices a fleet's span at is
// ../../end-goal/how-the-factory-works/06-releases/05-the-deploy-record/02-what-stands-for-a-rollback.md. The
// objective and its period are
// ../../end-goal/how-the-factory-works/08-operations/05-service-level-objectives.md,
// and the hours a service pages within are
// ../../end-goal/how-the-factory-works/08-operations/07-pages.md. The
// schema-change snapshot retention is
// ../../end-goal/how-the-factory-works/09-gate-policy/03-what-is-not-in-it/02-retention.md,
// the bound above which a criterion is unreliable is
// ../../end-goal/how-the-factory-works/03-gates/07-what-particular-gates-decide/02-spec/02-in-force-and-withdrawal.md,
// the mutant cap is
// ../../end-goal/how-the-factory-works/05-environments/02-an-environment-per-candidate/README.md,
// and the repository credential pair is seam 3 of ../../end-goal/deferred.md.
package service
