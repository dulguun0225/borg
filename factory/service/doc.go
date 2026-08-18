// Package service owns a service's identity and its repository, written at
// the cut and by nothing else.
//
// A service is one repository with one long-lived branch — master — which
// does not exist until the first release's fast-forward creates it: the
// implementation role commits the candidate branch with no base, and the
// merge queue's first fast-forward is what creates master. The repository
// field is a filesystem path or URL of that one git repository; the record
// names it and this package neither creates nor reads it.
//
// The name is unique, and the store refuses a second service of one name
// without waiting for a reader to notice.
//
// Who may write what: [Writer.Create] is the cut's call — the cut writes the
// service record in the same write as the item that creates the service, so
// the item's one outbound link points at something. One item creates a
// service and every later item on it reaches the record that item wrote,
// which is what [ByName] is for and why the cut reads before it creates. The
// service's other writer is an owner putting parameters on it — K, the watch
// window's sizes — and the seam between the two is the field: the cut writes
// identity and never a parameter. Those parameters are later milestones'
// columns, so this package holds the cut's fields alone today.
//
// What defines it:
// ../../end-goal/how-humans-do-it/02-intent-into-items.md#the-cut, which sets
// the one-repository rule, the two writers and the field as the seam between
// them, and master not existing until the first release.
package service
