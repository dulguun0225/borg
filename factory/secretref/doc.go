// Package secretref is the one place a secret value is read, and [Ref] is what
// every other package uses instead: a name, never a value.
//
// What the type gives is one field, unexported, holding one string, which
// [Ref.String] returns. So a Ref is a single string and there is nowhere in it
// for a second one, and reaching a value takes a [Resolver], which is held by
// the component about to use the value and by nothing that writes a record.
//
// What the type does not give is a guarantee that the string is a name.
// [New] checks a character set, and a character set cannot tell a name from a
// token that looks like one: provider credentials are letters, digits, and
// the same three punctuation marks, so MustNew(os.Getenv("DEPLOY_TOKEN"))
// returns a Ref that renders the token everywhere a Ref is rendered. Nothing
// here detects that, and nothing here should try — a prefix heuristic or a
// length cap would reject real names and still admit real tokens.
//
// What keeps values out is therefore a convention the code keeps rather than
// a property the type enforces: no path in the factory builds a Ref out of a
// value, and the one place a value exists is [Resolver.Resolve]'s return,
// which goes to the component that connects and into nothing that is stored.
// That convention is checkable by reading, because a Ref can only come from
// [New] or [MustNew] and the argument is right there.
//
// The alternative was to mint references through the resolver, so that a Ref
// could only exist for a secret already in the file. That was refused: a spec
// or an environment record names a credential whether or not this install has
// configured it yet, and a reference that cannot exist until the value does
// would make an artifact unwritable for want of a secret it only names. The
// cost of refusing it is this paragraph.
//
// Who may write what: this package writes nothing. It reads one file, whose
// format [Load] states.
//
// What defines it: seam 3 of "Security comes last" in
// ../../end-goal/deferred.md#security-comes-last — artifacts and specs get
// copied, diffed, and given to agents, so they contain names, and one resolver
// answers both the credential an agent reaches its model through and the
// credential on an environment record. What that costs is stated there and is
// true of this implementation: nothing separates the two, so whoever can read
// the file reaches the models and the deploy targets alike. Until policy
// attaches at seam 4 — ../../end-goal/deferred.md#security-comes-last, seam 4,
// which this repository implements in package targetseam — this separates
// storage and not reach.
package secretref
