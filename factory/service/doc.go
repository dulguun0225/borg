// Package service owns a service's identity and its repository, written at
// decomposition, and the four analysis window parameters an owner authors on
// it.
//
// # The code
//
// writer.go holds [Service], [Writer] and [NewWriter] with [Writer.Create],
// and the reads [Get], [ByName], and [All]. parameters.go holds [Parameters]
// and the four setters — [SetWindowSize], [SetWindowConfidence],
// [SetWindowCap], [SetWindowLimit] — each taking the transaction package
// policy appends the policy version in, so the field and the version commit
// together, and each refusing a value out of range with [ErrShareOutOfRange]
// or [ErrNotPositive]. schema.go holds [Table],
// [IDPrefix], and [DDL], whose unique constraint on the name refuses a second
// service of one name without waiting for a reader to notice.
//
// The repository field is a filesystem path or URL of one git repository; the
// record names it and this package neither creates nor reads it.
//
// Who may write what: [Writer.Create] is decomposition's call, made in the
// same write as the item that creates the service, so the item's outbound link
// points at something; [ByName] is how a later item on that service reaches
// the record, and why decomposition reads before it creates. The other writer
// is an owner authoring the window parameters, and the seam between the two is
// the field — decomposition writes identity and never a parameter.
//
// What defines it: the one-repository rule, master not existing until the
// first release's fast-forward, and the two writers with the field as the seam
// between them are
// ../../end-goal/how-the-factory-works/02-intent-into-items/03-decomposition/README.md.
package service
