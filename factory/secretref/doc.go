// Package secretref is the one place a secret value is read, and [Ref] is what
// every other package uses instead: a name, never a value.
//
// # The code
//
// ref.go holds [Ref], with [New] and [MustNew] minting one, [Ref.Name],
// [Ref.String], and [Ref.IsZero] reading it, [ErrName] for a name outside the
// character set, and [ErrUnset] for the zero Ref. resolver.go holds
// [Resolver], [Load], which reads the secrets file and states its format, and
// [Resolver.Resolve], with [ErrFormat] and [ErrUnknown].
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
// Who may write what: this package writes nothing. It reads one file, whose
// format [Load] states.
//
// What defines it: a reference in place of a value, and one resolver behind
// both the credential an agent reaches its model through and the credential on
// an environment record, are seam 3 of "Security comes last",
// ../../end-goal/deferred.md#security-comes-last, whose seam 4 is where policy
// would attach and is package targetseam here.
package secretref
