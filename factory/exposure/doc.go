// Package exposure is the exposure factor's extractor: what the change reaches
// that the service did not reach before, read out of the diff between the base
// and the candidate's commit and out of the build's own resolved set.
//
// The derivation is per toolchain and Go is the one built. It runs where the
// repository is, because the score cannot read a repository and must not re-take
// a diff later — a diff re-taken against a master other items have merged into
// is not the diff the decision was made on. What it produces is handed to the
// score and recorded on the build record the run performed.
//
// # The code
//
// derive.go is [Checkout], [Coverage] and [Derive], which runs one git diff and
// hands the four kinds below to the readers beside it. reach.go is those
// readers — three over the diff's own lines and one over the two resolved sets
// the checkout carries — with the package list, the credential words and the
// secret-name pattern each reads by. diff.go is the hunk parser: [change], one
// added or removed line with the file and line it is at. derive_test.go is the
// tests, each over a temporary git repository with a real commit in it.
//
// # The four kinds
//
//   - An outbound call added: a new import of one of [OutboundPackages], a new
//     call into one of them, or a new call to an http client method.
//   - A credential named or read: a string literal shaped like a secret's name,
//     or a call to secretref.Resolve or os.Getenv on a name carrying one of
//     [CredentialWords], added or removed — a name removed is read the way one
//     added is, a change to the file that holds it being a diff like any other.
//   - An authorization check removed or weakened: a removed call to a function
//     whose name carries one of [AuthorizationWords], or a removed if guard
//     around one.
//   - A dependency change: the build's own resolved set diffed against the set
//     of the service's current release's build, each package added or moved
//     named with its version and its declared licence. An unpinned range that
//     resolved differently with nothing in the manifest changed is one, and its
//     entry says the manifest named no line to point at.
//
// Each entry names the file and the line, because what a human at Implementation
// argues with is that list beside the diff. The list only ever raises the number
// and a diff adding none of it reads as nothing; where the extractor could not
// run at all — no git, no such commit — the evidence is unavailable with the
// reason, which resolves the factor rather than reading it as nothing.
//
// Who may write what: this package owns no table and writes no record. It reads
// a checkout and answers; the build record its answer is stored on is package
// build's, written by the build runner that called this.
//
// What defines it: the exposure group, its four kinds, the evidence list with
// the file and the line, and unavailable where no extractor runs for the
// toolchain are
// ../../end-goal/how-the-factory-works/04-risk-score/01-factors-at-least.md; the
// resolved set the dependency change is read against is
// ../../end-goal/how-the-factory-works/05-environments/01-records-and-one-long-lived-branch.md.
package exposure
