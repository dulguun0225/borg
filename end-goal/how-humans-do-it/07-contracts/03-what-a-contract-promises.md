# What a contract promises

Every contract promises **backward** compatibility — the new build reads what the old one wrote — which is what a consumer of an interface needs. A store promises **forward** as well, and over writes as well as reads: the build being restored reads what the newer one wrote, and the schema the newer one installed still accepts what the restored build writes. Both are enforced on every diff after that.

Nothing is declared. Which promise a contract makes follows from whether it is a store, which the factory already knows, and there is no third case: forward alone has no user, because anything a rollback reads is read going the other way too. A declared mode would be three values where the kind of the thing decides among them, and each of the three is one more thing every builder implements identically.

The drawback: a derived promise is assumed where a declared one is checked. A producer that wants more than its kind provides has nowhere to say so, and the factory's answer to a wrong derivation is that there is nothing to derive — the kind is a fact about the contract, not a judgment about it.
