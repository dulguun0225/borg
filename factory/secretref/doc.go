// Package secretref is the one place a secret value is read, and [Ref] is what
// every other package uses instead: a name, never a value.
//
// # The code
//
// ref.go holds [Ref], with [New] and [MustNew] minting one, [Ref.Name],
// [Ref.String], and [Ref.IsZero] reading it, [ErrName] for a name outside the
// character set, and [ErrUnset] for the zero Ref. resolver.go holds
// [Resolver], [Load], which reads the secrets file and states its format,
// [Resolver.Resolve], with [ErrFormat] and [ErrUnknown], and the in-memory log
// [Resolution] and [Resolver.Resolutions].
//
// # The four kinds of credential
//
// One resolver answers all four, and it separates storage and not reach:
// whoever can read the file reaches every one of them. They are the credential
// an agent reaches its own model through, the credential on an environment
// record that the deployer resolves at every deploy, a service's own
// credentials for the outside world — held by name in its configuration and
// resolved at deploy through this same resolver — and a service's repository
// credential, which is two names where the repository host can tell master from
// a branch and one where it cannot. The repository pair is the one place reach
// is separated today, by the host and not by this package.
//
// A [Ref] is one unexported string field, so there is nowhere in it for a
// value beside the name, and reaching a value takes a [Resolver], which is
// held by the component about to use the value and by nothing that writes a
// record.
//
// What the type does not give is a guarantee that the string is a name: [New]
// checks a character set, and a character set cannot tell a name from a token
// that looks like one, so MustNew(os.Getenv("DEPLOY_TOKEN")) returns a Ref
// that renders the token. What keeps values out is a convention the code keeps
// — no path in the factory builds a Ref out of a value — and it is checkable
// by reading, because a Ref can come only from [New] or [MustNew] and the
// argument is right there. Minting references through the resolver was
// refused: a spec or an environment record names a credential whether or not
// this install has configured it yet.
//
// Who may write what: this package writes nothing to the store. It reads one
// file, whose format [Load] states, and appends to the in-memory log of who
// asked for which name. Nothing in ../../end-goal/records.md holds a row for
// that log.
//
// [Resolver.Resolve] takes the principal on every call and records it beside
// the name asked for, deciding nothing on it. The resolver is addressed by
// credential name and answers whoever asks, so the record of who asked is what
// a policy would read if one attached here and is not itself a refusal.
//
// What defines it: a reference in place of a value, one resolver behind the
// four kinds of credential above, and the principal this resolver takes on
// every call, are seams 3 and 5 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last, whose seam 4 is where policy
// would attach and is package targetseam here.
package secretref
