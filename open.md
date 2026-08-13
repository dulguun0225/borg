# Open

## Is a design system a contract, or only a standing constraint?

A design system the factory builds is a published thing with consumers inside the factory, and a token renamed or a spacing scale rescaled breaks them the way a schema change breaks a caller. That is the contract machinery exactly — a compatibility mode, a breaking diff caught at the merge gate, the three items of a migration, an old form carrying its own deprecation list. Calling it a contract costs one stretched word: _Two versioned things_ scopes consumers to other services and _Who owns a contract_ gives a contract to the service that publishes it, and a package of tokens is not obviously either. Leaving it a constraint (2) costs the enforcement — the factory checks nothing, and one token change breaks forty screens with no gate standing in front of it.

A design system supplied as a document rather than as code is the constraint case whichever way this settles, because there is no build to diff. What that in turn costs, and whether an owner should be pushed to supply code instead, is open with it.

## What bounds K, and who picks it?

Overlapping watch windows buy throughput on the high-frequency service and pay in how much a rollback takes with it. K = 1 is the serial factory and gives back the queue the built control was meant to dissolve; a large K makes a rollback a bundle in the one place the factory otherwise refuses one, and the size of that bundle is the number itself. Nothing in the document supplies a basis to pick it — the throughput gained is a property of the service's change rate and the loss is a property of how often a rollback fires, and those are not the same evidence.

Who picks it turns on the same gap. Authored per service with the rest of gate policy (8), it is one more thing an owner has to know something about before the factory runs well, on a parameter whose cost only shows up on the day something is rolled back. Scored, it is the risk score sizing its own blast radius — an objection the strategy pick already survives, though there a bad call costs the wrong rollout and here it costs the number of items one rollback undoes.
