# Open

## Is a design system a contract, or only a standing constraint?

A design system the factory builds is a published thing with consumers inside the factory, and a token renamed or a spacing scale rescaled breaks them the way a schema change breaks a caller. That is the contract machinery exactly — a compatibility mode, a breaking diff caught at the merge gate, the three items of a migration, an old form carrying its own deprecation list. Calling it a contract costs one stretched word: _Two versioned things_ scopes consumers to other services and _Who owns a contract_ gives a contract to the service that publishes it, and a package of tokens is not obviously either. Leaving it a constraint (2) costs the enforcement — the factory checks nothing, and one token change breaks forty screens with no gate standing in front of it.

A design system supplied as a document rather than as code is the constraint case whichever way this settles, because there is no build to diff. What that in turn costs, and whether an owner should be pushed to supply code instead, is open with it.
