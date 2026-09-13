// Package buildrunner performs builds and writes their build records. It
// selects the candidate or detached checkout, resolves dependencies before
// running the checkout, derives exposure and schema declarations, injects the
// shipped way in, compiles the artifact, and writes the result through the
// build package. It reaches no deploy target and holds no store access of its
// own.
//
// Repository and registry credentials are named inputs resolved through the
// interfaces in runner.go. Credentials are used before the process starts;
// the process receives a cleared environment, its checkout, resolved set and
// output directory. The package
// has no table, so it follows the component shape with doc.go, runner.go, and
// runner_test.go only.
//
// Who may write what: the runner calls the build writer supplied by its
// composition; clone, resolver, process, and schema seams write no factory
// record.
//
// What defines it: the build runner and its build record are
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md
// (C1434, C1446, C1454, C1456, C1458, C1459, C1464, C1465, C1469, C1480,
// C1482); repository credentials and the build boundary are
// ../../end-goal/deferred.md
// (C0092); adoption through the ordinary pipeline is
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/01-a-service-that-already-exists.md
// (C0620); the schema mark is
// ../../end-goal/how-the-factory-works/07-contracts/08-deprecation.md
// (C1831); and the split from the deployer is
// ../../end-goal/how-the-factory-works/08-operations/09-the-deployer.md
// (C2184).
package buildrunner
