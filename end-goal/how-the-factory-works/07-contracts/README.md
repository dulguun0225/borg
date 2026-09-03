# Contracts

What binds one service to another and to its own past, and what stops a change breaking either.

| Subsection | What it settles |
|---|---|
| [Two versioned things](01-two-versioned-things.md) | Why a release number and a contract version must stay separate |
| [No single item may break a contract](02-no-single-item-may-break-a-contract.md) | Why a breaking change ships as four items, not one |
| [What a contract promises](03-what-a-contract-promises.md) | What backward and forward compatibility each promise, and to whom |
| [Enforcement](04-enforcement.md) | How the factory diffs a candidate's contract against production, mechanically |
| [What a diff cannot see](05-what-a-diff-cannot-see.md) | What a schema diff misses, and the three layers that catch it |
| [What a consumer declares](06-what-a-consumer-declares.md) | What a consumer contract is, and how it is derived rather than written |
| [Who owns a contract](07-who-owns-a-contract.md) | Who may change a contract, and what a consumer owns instead |
| [Deprecation](08-deprecation.md) | How marking, the brownout, and removal retire an old form |
| [The store is a contract too](09-the-store-is-a-contract-too.md) | Why a service's own store is a contract, with its own past as consumer |
| [Work that spans services](10-work-that-spans-services.md) | How the intent already joining items answers work spanning services |
| [Which producer a consumer reaches](11-which-producer-a-consumer-reaches.md) | How a call site is resolved to a producer contract, and where the edge between two services is held |
| [What the derivation records](12-what-the-derivation-records.md) | Which extractor derived a consumer contract, what it could not follow, and what an upgrade that changes an extractor derives again |
